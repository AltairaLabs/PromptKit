package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// transitionProvider calls workflow__transition on its first round.
type transitionProvider struct {
	*toolGrantProvider
	event string
}

func newTransitionProvider(event string) *transitionProvider {
	return &transitionProvider{toolGrantProvider: newToolGrantProvider(), event: event}
}

func (p *transitionProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.mu.Lock()
	p.rounds++
	round := p.rounds
	p.mu.Unlock()

	if round == 1 {
		calls := []types.MessageToolCall{{
			ID:   "t1",
			Name: "workflow__transition",
			Args: []byte(`{"event":"` + p.event + `","context":"done gathering"}`),
		}}
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	}
	return providers.PredictionResponse{Content: "done"}, nil, nil
}

// A workflow conversation's transition must land on its OWN state machine.
//
// A tools.Registry keeps exactly one executor per name, and a TransitionExecutor
// is bound to one conversation's state machine and holds its pending
// transition. Two workflow conversations sharing a registry meant the second
// one's executor received the first's transition: the first's CommitPending
// found nothing pending and dropped it silently, while the second was left
// holding a transition its own model never asked for. See #2011.
func TestWorkflowTransition_DoesNotLeakAcrossASharedRegistry(t *testing.T) {
	shared := tools.NewRegistry()
	packPath := writeWorkflowTestPack(t, workflowPackJSON)

	open := func() *WorkflowConversation {
		wc, err := OpenWorkflow(packPath,
			WithSkipSchemaValidation(),
			WithProvider(newTransitionProvider("InfoComplete")),
			WithToolRegistry(shared),
		)
		require.NoError(t, err)
		t.Cleanup(func() { _ = wc.Close() })
		return wc
	}

	alice := open()
	bob := open() // registers second, taking over "workflow-transition" by name

	require.Equal(t, "intake", alice.CurrentState())
	require.Equal(t, "intake", bob.CurrentState())

	_, err := alice.Send(context.Background(), "here is my info")
	require.NoError(t, err)

	assert.Equal(t, "processing", alice.CurrentState(),
		"alice's transition must commit on alice's own state machine")
	assert.Equal(t, "intake", bob.CurrentState(),
		"bob never transitioned and must not have absorbed alice's event")
}
