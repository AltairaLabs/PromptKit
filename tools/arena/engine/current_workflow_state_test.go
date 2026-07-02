package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	runtimestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/arenaconfig"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
)

// TestStampCurrentWorkflowState_LastAssistant verifies the explicit per-turn
// stamp lands current_workflow_state on the LAST assistant message and leaves
// the legacy _workflow_state path untouched.
func TestStampCurrentWorkflowState_LastAssistant(t *testing.T) {
	ctx := context.Background()
	store := statestore.NewArenaStateStore()
	convID := "conv-stamp"

	require.NoError(t, store.Save(ctx, &runtimestore.ConversationState{
		ID: convID,
		Messages: []types.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "first"},
			{Role: "user", Content: "again"},
			{Role: "assistant", Content: "second"},
		},
	}))

	e := &Engine{stateStore: store}
	meta := map[string]interface{}{"current_state": "analyzing"}
	e.stampCurrentWorkflowState(convID, meta)

	got, err := store.Load(ctx, convID)
	require.NoError(t, err)
	require.Len(t, got.Messages, 4)

	// Last assistant message is stamped.
	last := got.Messages[3]
	require.NotNil(t, last.Meta)
	ws, ok := last.Meta["current_workflow_state"].(map[string]interface{})
	require.True(t, ok, "current_workflow_state must be a map")
	assert.Equal(t, "analyzing", ws["current_state"])

	// Earlier assistant message is NOT stamped (only the last one).
	assert.Nil(t, got.Messages[1].Meta["current_workflow_state"])

	// Legacy key is never introduced by the explicit stamp.
	assert.Nil(t, last.Meta["_workflow_state"])
}

// TestStampCurrentWorkflowState_NilSafe verifies nil meta is a no-op (no key
// added) and that a conversation with no assistant message is left untouched.
func TestStampCurrentWorkflowState_NilSafe(t *testing.T) {
	ctx := context.Background()
	store := statestore.NewArenaStateStore()
	e := &Engine{stateStore: store}

	t.Run("nil meta adds no key", func(t *testing.T) {
		convID := "conv-nil-meta"
		require.NoError(t, store.Save(ctx, &runtimestore.ConversationState{
			ID:       convID,
			Messages: []types.Message{{Role: "assistant", Content: "x"}},
		}))
		e.stampCurrentWorkflowState(convID, nil)
		got, err := store.Load(ctx, convID)
		require.NoError(t, err)
		assert.Nil(t, got.Messages[0].Meta["current_workflow_state"])
	})

	t.Run("no assistant message is no-op", func(t *testing.T) {
		convID := "conv-no-assistant"
		require.NoError(t, store.Save(ctx, &runtimestore.ConversationState{
			ID:       convID,
			Messages: []types.Message{{Role: "user", Content: "x"}},
		}))
		e.stampCurrentWorkflowState(convID, map[string]interface{}{"current_state": "analyzing"})
		got, err := store.Load(ctx, convID)
		require.NoError(t, err)
		assert.Nil(t, got.Messages[0].Meta["current_workflow_state"])
	})
}

// stampingTurnExecutor appends one assistant message per assistant turn, mirroring
// MockTurnExecutor's store-append behaviour, so the loop's stamp guard sees a new
// assistant message.
type stampingTurnExecutor struct{}

func (s *stampingTurnExecutor) ExecuteTurn(ctx context.Context, req turnexecutors.TurnRequest) error {
	if req.StateStoreConfig == nil || req.StateStoreConfig.Store == nil || req.ConversationID == "" {
		return nil
	}
	store := req.StateStoreConfig.Store.(runtimestore.BulkWriter)
	loader := req.StateStoreConfig.Store.(runtimestore.Store)
	state, err := loader.Load(ctx, req.ConversationID)
	if err != nil || state == nil {
		state = &runtimestore.ConversationState{ID: req.ConversationID}
	}
	state.Messages = append(state.Messages,
		types.Message{Role: "user", Content: req.ScriptedContent},
		types.Message{Role: "assistant", Content: "response"},
	)
	return store.Save(ctx, state)
}

func (s *stampingTurnExecutor) ExecuteTurnStream(
	ctx context.Context, req turnexecutors.TurnRequest,
) (<-chan turnexecutors.MessageStreamChunk, error) {
	ch := make(chan turnexecutors.MessageStreamChunk)
	close(ch)
	return ch, s.ExecuteTurn(ctx, req)
}

// TestExecuteConversation_StampsTurnStartState verifies the executor turn loop
// stamps current_workflow_state captured at TURN START onto each turn's assistant
// message, and that nil hooks (no workflow) add no key.
func TestExecuteConversation_StampsTurnStartState(t *testing.T) {
	ctx := context.Background()

	newExecutor := func() *DefaultConversationExecutor {
		return &DefaultConversationExecutor{
			scriptedExecutor: &stampingTurnExecutor{},
			promptRegistry:   nil,
		}
	}
	newReq := func(store *statestore.ArenaStateStore, convID string) ConversationRequest {
		return ConversationRequest{
			Provider: &MockProvider{id: "mock"},
			Scenario: &arenaconfig.Scenario{
				ID: "sc",
				Turns: []arenaconfig.TurnDefinition{
					{Role: "user", Content: "turn one"},
					{Role: "user", Content: "turn two"},
				},
			},
			Config:           &arenaconfig.Config{},
			ConversationID:   convID,
			RunID:            convID,
			StateStoreConfig: &StateStoreConfig{Store: store},
		}
	}

	t.Run("stamps each turn with the state observed at turn start", func(t *testing.T) {
		store := statestore.NewArenaStateStore()
		convID := "conv-loop"
		require.NoError(t, store.Save(ctx, &runtimestore.ConversationState{ID: convID}))
		e := &Engine{stateStore: store}
		req := newReq(store, convID)

		// Simulate a state machine that advances between turns: turn 1 sees
		// "intake", turn 2 sees "analyzing". current_workflow_state is read at
		// turn start so each assistant message records the driving state.
		states := []string{"intake", "analyzing"}
		idx := 0
		req.CurrentWorkflowState = func() map[string]interface{} {
			st := states[idx]
			if idx < len(states)-1 {
				idx++
			}
			return map[string]interface{}{"current_state": st}
		}
		req.StampWorkflowState = func(meta map[string]interface{}) {
			e.stampCurrentWorkflowState(convID, meta)
		}

		res := newExecutor().ExecuteConversation(ctx, req)
		require.False(t, res.Failed, res.Error)

		got, err := store.Load(ctx, convID)
		require.NoError(t, err)

		var assistantStates []string
		for i := range got.Messages {
			if got.Messages[i].Role != "assistant" {
				continue
			}
			ws, ok := got.Messages[i].Meta["current_workflow_state"].(map[string]interface{})
			require.True(t, ok, "every assistant message must carry current_workflow_state")
			assistantStates = append(assistantStates, ws["current_state"].(string))
		}
		assert.Equal(t, []string{"intake", "analyzing"}, assistantStates)
	})

	t.Run("no workflow hooks adds no key", func(t *testing.T) {
		store := statestore.NewArenaStateStore()
		convID := "conv-noworkflow"
		require.NoError(t, store.Save(ctx, &runtimestore.ConversationState{ID: convID}))
		req := newReq(store, convID)
		// CurrentWorkflowState / StampWorkflowState left nil.

		res := newExecutor().ExecuteConversation(ctx, req)
		require.False(t, res.Failed, res.Error)

		got, err := store.Load(ctx, convID)
		require.NoError(t, err)
		for i := range got.Messages {
			assert.Nil(t, got.Messages[i].Meta["current_workflow_state"],
				"non-workflow runs must not stamp the key")
		}
	})
}
