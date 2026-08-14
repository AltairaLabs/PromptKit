package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// handoffSDKProvider records the system prompt per round and transitions on the
// first round, so a single Send spans two workflow states.
type handoffSDKProvider struct {
	*mock.ToolProvider
	seenPrompts []string
}

// Pin to the unary tool loop for determinism.
func (p *handoffSDKProvider) SupportsStreaming() bool { return false }

func (p *handoffSDKProvider) Predict(
	_ context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.seenPrompts = append(p.seenPrompts, req.System)
	return providers.PredictionResponse{Content: "no tools available"}, nil
}

func (p *handoffSDKProvider) PredictWithTools(
	_ context.Context,
	req providers.PredictionRequest,
	_ providers.ProviderTools,
	_ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.seenPrompts = append(p.seenPrompts, req.System)

	if len(p.seenPrompts) == 1 {
		calls := []types.MessageToolCall{{
			ID:   "call-1",
			Name: "workflow__transition",
			Args: []byte(`{"event":"InfoComplete","context":"name and account collected"}`),
		}}
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	}
	return providers.PredictionResponse{Content: "destination speaking"}, nil, nil
}

// The end-to-end guarantee. One Send: the LLM transitions intake -> processing,
// and the PROCESSING state produces the reply, in the same turn, with no second
// user message. Before this, Send returned the intake state's text and the
// processing state stayed silent until the user typed again.
func TestWorkflowConversation_DestinationStateSpeaksInSameSend(t *testing.T) {
	packPath := writeWorkflowTestPack(t, workflowPackJSON)
	provider := &handoffSDKProvider{
		ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil),
	}

	wc, err := OpenWorkflow(packPath, WithProvider(provider), WithSkipSchemaValidation())
	require.NoError(t, err)
	defer func() { require.NoError(t, wc.Close()) }()

	require.Equal(t, "intake", wc.CurrentState())

	resp, err := wc.Send(context.Background(), "I need help with my account")
	require.NoError(t, err)

	require.Equal(t, "processing", wc.CurrentState(),
		"the transition must have been committed")

	require.Len(t, provider.seenPrompts, 2,
		"the transition tool call must produce a second round")
	require.Equal(t, "You gather information.", provider.seenPrompts[0],
		"round 1 runs as the origin state")
	require.Equal(t, "You process requests.", provider.seenPrompts[1],
		"round 2 must run as the DESTINATION state -- this is the fix")

	require.Contains(t, resp.Text(), "destination speaking",
		"Send must return the destination state's reply, not the origin's")
}

// A turn with no transition must behave exactly as before: one round, one
// state, origin prompt throughout.
func TestWorkflowConversation_NoTransitionIsUnchanged(t *testing.T) {
	packPath := writeWorkflowTestPack(t, workflowPackJSON)
	provider := &handoffSDKProvider{
		ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil),
	}
	// Consume the scripted transition so the first real round answers plainly.
	provider.seenPrompts = append(provider.seenPrompts, "PRIMED")

	wc, err := OpenWorkflow(packPath, WithProvider(provider), WithSkipSchemaValidation())
	require.NoError(t, err)
	defer func() { require.NoError(t, wc.Close()) }()

	_, err = wc.Send(context.Background(), "hello")
	require.NoError(t, err)

	require.Equal(t, "intake", wc.CurrentState(), "state must not move")
	require.Len(t, provider.seenPrompts, 2, "primed entry plus one real round")
	require.Equal(t, "You gather information.", provider.seenPrompts[1])
}
