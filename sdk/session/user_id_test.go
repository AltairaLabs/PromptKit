package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func userIDTestPipeline(t *testing.T) *stage.StreamPipeline {
	t.Helper()
	p, err := stage.NewPipelineBuilder().Chain(&testNoOpStage{}).Build()
	require.NoError(t, err)
	return p
}

// A new conversation's state records the session's user; an existing one is
// left exactly as it was, whether or not it names a user.
func TestUnarySession_UserIDOnNewConversationsOnly(t *testing.T) {
	ctx := context.Background()
	t.Run("new conversation", func(t *testing.T) {
		store := statestore.NewMemoryStore()
		_, err := NewUnarySession(UnarySessionConfig{ConversationID: "c1", UserID: "u1",
			StateStore: store, Pipeline: userIDTestPipeline(t), Metadata: map[string]any{"tenant": "acme"}})
		require.NoError(t, err)
		state, err := store.Load(ctx, "c1")
		require.NoError(t, err)
		assert.Equal(t, "u1", state.UserID)
		assert.Equal(t, "acme", state.Metadata["tenant"], "metadata is seeded too")
	})
	t.Run("existing conversation without a user", func(t *testing.T) {
		store := statestore.NewMemoryStore()
		msgs := []types.Message{{Role: "user", Content: "kept"}}
		require.NoError(t, store.Save(ctx, &statestore.ConversationState{ID: "c2", Messages: msgs}))
		_, err := NewUnarySession(UnarySessionConfig{ConversationID: "c2", UserID: "u2",
			StateStore: store, Pipeline: userIDTestPipeline(t)})
		require.NoError(t, err)
		state, err := store.Load(ctx, "c2")
		require.NoError(t, err)
		assert.Equal(t, "", state.UserID, "an existing state is not rewritten")
		assert.Equal(t, msgs, state.Messages)
	})
	t.Run("no user", func(t *testing.T) {
		store := statestore.NewMemoryStore()
		_, err := NewUnarySession(UnarySessionConfig{ConversationID: "c3", StateStore: store,
			Pipeline: userIDTestPipeline(t)})
		require.NoError(t, err)
		_, err = store.Load(ctx, "c3")
		assert.ErrorIs(t, err, statestore.ErrNotFound, "nothing is written without a user")
	})
}

// A duplex session records its user and seeds its metadata on a new
// conversation.
func TestDuplexSession_UserIDAndMetadataOnNewConversation(t *testing.T) {
	ctx := context.Background()
	store := statestore.NewMemoryStore()
	builder := func(context.Context, providers.Provider, providers.StreamInputSupport,
		*providers.StreamingInputConfig, string, statestore.Store) (*stage.StreamPipeline, error) {
		return stage.NewPipelineBuilder().Chain(&testNoOpStage{}).Build()
	}
	sess, err := NewDuplexSession(ctx, &DuplexSessionConfig{
		ConversationID: "d1", UserID: "u9", StateStore: store, Metadata: map[string]any{"tenant": "acme"},
		Provider: mock.NewProvider("mock", "mock-model", false), PipelineBuilder: builder,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sess.Close() })
	state, err := store.Load(ctx, "d1")
	require.NoError(t, err)
	assert.Equal(t, "u9", state.UserID)
	assert.Equal(t, "acme", state.Metadata["tenant"])
}
