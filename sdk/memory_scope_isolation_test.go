package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// rememberOnceProvider calls memory__remember on its first round and then
// answers with text.
type rememberOnceProvider struct {
	*toolGrantProvider
	content string
}

func newRememberOnceProvider(content string) *rememberOnceProvider {
	return &rememberOnceProvider{toolGrantProvider: newToolGrantProvider(), content: content}
}

func (p *rememberOnceProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.mu.Lock()
	p.rounds++
	round := p.rounds
	p.mu.Unlock()

	if round == 1 {
		calls := []types.MessageToolCall{{
			ID:   "call-remember",
			Name: memory.RememberToolName,
			Args: []byte(`{"content":"` + p.content + `"}`),
		}}
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	}
	return providers.PredictionResponse{Content: "done"}, nil, nil
}

// Two conversations sharing one tools.Registry must write memories into their
// own scopes.
//
// WithToolRegistry is public, and registration is guarded per conversation, not
// per registry -- so the second conversation's RegisterTools overwrites the
// first's memory executor by mode key. The scope used to be captured on that
// executor, so after the overwrite the first conversation wrote into the
// second's scope and could not see its own memory. See #2011.
func TestMemoryScope_DoesNotLeakAcrossASharedRegistry(t *testing.T) {
	store := memory.NewInMemoryStore()
	shared := tools.NewRegistry()
	packPath := writeWorkflowTestPack(t, toolGrantPackJSON)

	open := func(who, content string) *Conversation {
		conv, err := Open(packPath, "chat",
			WithProvider(newRememberOnceProvider(content)),
			WithSkipSchemaValidation(),
			WithToolRegistry(shared),
			WithMemory(store, map[string]string{"user_id": who}),
		)
		require.NoError(t, err)
		t.Cleanup(func() { _ = conv.Close() })
		return conv
	}

	alice := open("alice", "alice likes coffee")
	bob := open("bob", "bob likes tea")

	_, err := alice.Send(context.Background(), "remember this")
	require.NoError(t, err)
	_, err = bob.Send(context.Background(), "remember this")
	require.NoError(t, err)

	aliceMems, err := store.List(context.Background(),
		map[string]string{"user_id": "alice"}, memory.ListOptions{})
	require.NoError(t, err)
	require.Len(t, aliceMems, 1, "alice's memory must land in alice's scope")
	assert.Equal(t, "alice likes coffee", aliceMems[0].Content)

	bobMems, err := store.List(context.Background(),
		map[string]string{"user_id": "bob"}, memory.ListOptions{})
	require.NoError(t, err)
	require.Len(t, bobMems, 1, "bob's memory must land in bob's scope")
	assert.Equal(t, "bob likes tea", bobMems[0].Content)
}
