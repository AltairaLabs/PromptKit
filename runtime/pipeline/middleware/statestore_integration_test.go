package middleware

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestStateStoreMiddleware_Integration tests the complete load + save flow
func TestStateStoreMiddleware_Integration(t *testing.T) {
	store := statestore.NewMemoryStore()
	config := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "integration-test",
		UserID:         "user-integration",
		Metadata: map[string]interface{}{
			"session_type": "test",
		},
	}

	// First execution: Start new conversation
	t.Run("first_turn", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Context: context.Background(),
			Messages: []types.Message{
				{Role: "user", Content: "What is 2+2?"},
			},
			CostInfo: types.CostInfo{
				InputTokens:  5,
				OutputTokens: 3,
				TotalCost:    0.0001,
			},
		}

		// Build middleware chain: Load -> Save -> Provider
		loadMiddleware := StateStoreLoadMiddleware(config)
		saveMiddleware := StateStoreSaveMiddleware(config)

		// Execute pipeline with proper nesting
		err := loadMiddleware.Process(execCtx, func() error {
			return saveMiddleware.Process(execCtx, func() error {
				// Simulate provider adding assistant response
				execCtx.Messages = append(execCtx.Messages, types.Message{
					Role:    "assistant",
					Content: "The answer is 4.",
				})
				return nil
			})
		})

		if err != nil {
			t.Fatalf("first turn failed: %v", err)
		}

		// Verify state was saved
		state, err := store.Load(context.Background(), "integration-test")
		if err != nil {
			t.Fatalf("failed to load state: %v", err)
		}

		if len(state.Messages) != 2 {
			t.Errorf("expected 2 messages in state, got %d", len(state.Messages))
		}

		if state.Messages[0].Content != "What is 2+2?" {
			t.Error("first message not saved correctly")
		}

		if state.Messages[1].Content != "The answer is 4." {
			t.Error("assistant response not saved correctly")
		}
	})

	// Second execution: Continue conversation
	t.Run("second_turn", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Context: context.Background(),
			Messages: []types.Message{
				{Role: "user", Content: "What about 3+3?"},
			},
			CostInfo: types.CostInfo{
				InputTokens:  5,
				OutputTokens: 3,
				TotalCost:    0.0001,
			},
		}

		// Build middleware chain
		loadMiddleware := StateStoreLoadMiddleware(config)
		saveMiddleware := StateStoreSaveMiddleware(config)

		// Execute pipeline with proper nesting
		err := loadMiddleware.Process(execCtx, func() error {
			// At this point, execCtx.Messages should have history + new message
			if len(execCtx.Messages) != 3 {
				t.Errorf("expected 3 messages (2 history + 1 new), got %d", len(execCtx.Messages))
			}

			return saveMiddleware.Process(execCtx, func() error {
				// Simulate provider adding assistant response
				execCtx.Messages = append(execCtx.Messages, types.Message{
					Role:    "assistant",
					Content: "The answer is 6.",
				})
				return nil
			})
		})

		if err != nil {
			t.Fatalf("second turn failed: %v", err)
		}

		// Verify full conversation history
		state, err := store.Load(context.Background(), "integration-test")
		if err != nil {
			t.Fatalf("failed to load state: %v", err)
		}

		if len(state.Messages) != 4 {
			t.Errorf("expected 4 messages in state, got %d", len(state.Messages))
		}

		// Verify message order
		expectedContents := []string{
			"What is 2+2?",
			"The answer is 4.",
			"What about 3+3?",
			"The answer is 6.",
		}

		for i, expected := range expectedContents {
			if i >= len(state.Messages) {
				t.Errorf("message %d missing", i)
				continue
			}
			if state.Messages[i].Content != expected {
				t.Errorf("message %d: got %q, want %q", i, state.Messages[i].Content, expected)
			}
		}

		// Verify metadata was preserved
		if val, ok := state.Metadata["session_type"].(string); !ok || val != "test" {
			t.Error("session_type metadata not preserved")
		}
	})

	// Third execution: Verify metadata accumulation
	t.Run("third_turn_with_metadata", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Context: context.Background(),
			Messages: []types.Message{
				{Role: "user", Content: "Thanks!"},
			},
			Metadata: map[string]interface{}{
				"feedback_rating": 5,
			},
			CostInfo: types.CostInfo{
				InputTokens:  3,
				OutputTokens: 2,
				TotalCost:    0.00005,
			},
		}

		// Build middleware chain
		loadMiddleware := StateStoreLoadMiddleware(config)
		saveMiddleware := StateStoreSaveMiddleware(config)

		// Execute pipeline with proper nesting
		err := loadMiddleware.Process(execCtx, func() error {
			return saveMiddleware.Process(execCtx, func() error {
				// Add assistant response
				execCtx.Messages = append(execCtx.Messages, types.Message{
					Role:    "assistant",
					Content: "You're welcome!",
				})
				return nil
			})
		})

		if err != nil {
			t.Fatalf("third turn failed: %v", err)
		}

		// Verify metadata accumulation
		state, err := store.Load(context.Background(), "integration-test")
		if err != nil {
			t.Fatalf("failed to load state: %v", err)
		}

		if rating, ok := state.Metadata["feedback_rating"].(float64); !ok || rating != 5 {
			t.Error("runtime metadata not saved")
		}

		if len(state.Messages) != 6 {
			t.Errorf("expected 6 messages in state, got %d", len(state.Messages))
		}
	})
}

// TestStateStoreMiddleware_MultipleConversations tests isolation between conversations
func TestStateStoreMiddleware_MultipleConversations(t *testing.T) {
	store := statestore.NewMemoryStore()

	// Conversation 1
	config1 := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "conv-1",
		UserID:         "alice",
	}

	// Conversation 2
	config2 := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "conv-2",
		UserID:         "bob",
	}

	// Execute conversation 1
	t.Run("conversation_1", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Context:  context.Background(),
			Messages: []types.Message{{Role: "user", Content: "Alice says hello"}},
		}

		load := StateStoreLoadMiddleware(config1)
		save := StateStoreSaveMiddleware(config1)

		err := load.Process(execCtx, func() error {
			return save.Process(execCtx, func() error {
				execCtx.Messages = append(execCtx.Messages, types.Message{
					Role:    "assistant",
					Content: "Hello Alice!",
				})
				return nil
			})
		})

		if err != nil {
			t.Fatalf("conversation 1 failed: %v", err)
		}
	})

	// Execute conversation 2
	t.Run("conversation_2", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Context:  context.Background(),
			Messages: []types.Message{{Role: "user", Content: "Bob says hi"}},
		}

		load := StateStoreLoadMiddleware(config2)
		save := StateStoreSaveMiddleware(config2)

		err := load.Process(execCtx, func() error {
			return save.Process(execCtx, func() error {
				execCtx.Messages = append(execCtx.Messages, types.Message{
					Role:    "assistant",
					Content: "Hi Bob!",
				})
				return nil
			})
		})

		if err != nil {
			t.Fatalf("conversation 2 failed: %v", err)
		}
	})

	// Verify isolation
	t.Run("verify_isolation", func(t *testing.T) {
		state1, err := store.Load(context.Background(), "conv-1")
		if err != nil {
			t.Fatalf("failed to load conv-1: %v", err)
		}

		state2, err := store.Load(context.Background(), "conv-2")
		if err != nil {
			t.Fatalf("failed to load conv-2: %v", err)
		}

		// Verify state1 has Alice's messages
		if len(state1.Messages) != 2 {
			t.Errorf("conv-1: expected 2 messages, got %d", len(state1.Messages))
		}
		if state1.Messages[0].Content != "Alice says hello" {
			t.Error("conv-1: wrong message content")
		}

		// Verify state2 has Bob's messages
		if len(state2.Messages) != 2 {
			t.Errorf("conv-2: expected 2 messages, got %d", len(state2.Messages))
		}
		if state2.Messages[0].Content != "Bob says hi" {
			t.Error("conv-2: wrong message content")
		}

		// Verify no cross-contamination
		if state1.UserID == state2.UserID {
			t.Error("conversations should have different UserIDs")
		}
	})
}
