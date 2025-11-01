package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestStateStoreSaveMiddleware(t *testing.T) {
	tests := []struct {
		name          string
		config        *pipeline.StateStoreConfig
		messages      []types.Message
		costInfo      types.CostInfo
		metadata      map[string]interface{}
		nextErr       error
		wantSaved     bool
		wantErr       bool
		wantSaveErr   bool
		validateState func(t *testing.T, state *statestore.ConversationState)
	}{
		{
			name:      "no config provided (no-op)",
			config:    nil,
			messages:  []types.Message{{Role: "user", Content: "test"}},
			wantSaved: false,
			wantErr:   false,
		},
		{
			name: "save new conversation",
			config: &pipeline.StateStoreConfig{
				Store:          statestore.NewMemoryStore(),
				ConversationID: "new-conv",
				UserID:         "user-123",
			},
			messages: []types.Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi there"},
			},
			costInfo: types.CostInfo{
				InputTokens:  10,
				OutputTokens: 5,
				TotalCost:    0.001,
			},
			wantSaved: true,
			wantErr:   false,
			validateState: func(t *testing.T, state *statestore.ConversationState) {
				if state.ID != "new-conv" {
					t.Errorf("ID = %q, want %q", state.ID, "new-conv")
				}
				if state.UserID != "user-123" {
					t.Errorf("UserID = %q, want %q", state.UserID, "user-123")
				}
				if len(state.Messages) != 2 {
					t.Errorf("got %d messages, want 2", len(state.Messages))
				}
				if cost, ok := state.Metadata["total_cost_usd"].(float64); ok {
					if cost != 0.001 {
						t.Errorf("total_cost_usd = %f, want 0.001", cost)
					}
				}
				if tokens, ok := state.Metadata["total_tokens"].(int); ok {
					if tokens != 15 {
						t.Errorf("total_tokens = %d, want 15", tokens)
					}
				}
			},
		},
		{
			name: "update existing conversation",
			config: &pipeline.StateStoreConfig{
				Store:          statestore.NewMemoryStore(),
				ConversationID: "existing-conv",
				UserID:         "user-456",
			},
			messages: []types.Message{
				{Role: "user", Content: "first"},
				{Role: "assistant", Content: "response"},
				{Role: "user", Content: "second"},
				{Role: "assistant", Content: "another response"},
			},
			costInfo: types.CostInfo{
				InputTokens:  20,
				OutputTokens: 15,
				TotalCost:    0.002,
			},
			wantSaved: true,
			wantErr:   false,
			validateState: func(t *testing.T, state *statestore.ConversationState) {
				if len(state.Messages) != 4 {
					t.Errorf("got %d messages, want 4", len(state.Messages))
				}
			},
		},
		{
			name: "save with custom metadata",
			config: &pipeline.StateStoreConfig{
				Store:          statestore.NewMemoryStore(),
				ConversationID: "meta-conv",
				UserID:         "user-789",
				Metadata: map[string]interface{}{
					"initial_key": "initial_value",
				},
			},
			messages: []types.Message{{Role: "user", Content: "test"}},
			metadata: map[string]interface{}{
				"runtime_key": "runtime_value",
				"session_id":  "sess-456",
			},
			wantSaved: true,
			wantErr:   false,
			validateState: func(t *testing.T, state *statestore.ConversationState) {
				if val, ok := state.Metadata["runtime_key"].(string); !ok || val != "runtime_value" {
					t.Error("expected runtime_key to be saved in metadata")
				}
				if val, ok := state.Metadata["session_id"].(string); !ok || val != "sess-456" {
					t.Error("expected session_id to be saved in metadata")
				}
			},
		},
		{
			name: "save even when execution fails",
			config: &pipeline.StateStoreConfig{
				Store:          statestore.NewMemoryStore(),
				ConversationID: "fail-conv",
				UserID:         "user-999",
			},
			messages:  []types.Message{{Role: "user", Content: "failed request"}},
			nextErr:   errors.New("execution failed"),
			wantSaved: true,
			wantErr:   true, // Should return the execution error
			validateState: func(t *testing.T, state *statestore.ConversationState) {
				if len(state.Messages) != 1 {
					t.Error("state should be saved even when execution fails")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create execution context
			execCtx := &pipeline.ExecutionContext{
				Context:  context.Background(),
				Messages: tt.messages,
				CostInfo: tt.costInfo,
				Metadata: tt.metadata,
			}
			if execCtx.Metadata == nil {
				execCtx.Metadata = make(map[string]interface{})
			}

			// Create middleware
			middleware := StateStoreSaveMiddleware(tt.config)

			// Execute with next() returning nextErr if provided
			err := middleware.Process(execCtx, func() error {
				if tt.nextErr != nil {
					return tt.nextErr
				}
				return nil
			})

			// Verify error handling
			if (err != nil) != tt.wantErr {
				t.Errorf("StateStoreSaveMiddleware() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Verify state was saved (or not)
			if tt.wantSaved && tt.config != nil {
				store := tt.config.Store.(statestore.Store)
				state, err := store.Load(context.Background(), tt.config.ConversationID)
				if err != nil {
					t.Fatalf("failed to load saved state: %v", err)
				}
				if state == nil {
					t.Fatal("expected state to be saved, but got nil")
				}

				// Run custom validation if provided
				if tt.validateState != nil {
					tt.validateState(t, state)
				}
			}
		})
	}
}

func TestStateStoreSaveMiddleware_InvalidStore(t *testing.T) {
	config := &pipeline.StateStoreConfig{
		Store:          "invalid-type", // Not a statestore.Store
		ConversationID: "test",
	}

	execCtx := &pipeline.ExecutionContext{
		Context:  context.Background(),
		Messages: []types.Message{{Role: "user", Content: "test"}},
	}

	middleware := StateStoreSaveMiddleware(config)

	err := middleware.Process(execCtx, func() error { return nil })

	if err == nil {
		t.Error("expected error for invalid store type, got nil")
	}
}

func TestStateStoreSaveMiddleware_PreservesExecutionError(t *testing.T) {
	config := &pipeline.StateStoreConfig{
		Store:          statestore.NewMemoryStore(),
		ConversationID: "test-conv",
	}

	execCtx := &pipeline.ExecutionContext{
		Context:  context.Background(),
		Messages: []types.Message{{Role: "user", Content: "test"}},
	}

	expectedErr := errors.New("execution failed")

	middleware := StateStoreSaveMiddleware(config)

	// Before phase - pass execution error through next()
	err := middleware.Process(execCtx, func() error {
		// Simulate execution error from next middleware
		return expectedErr
	})

	// Should return the execution error, not a save error
	if err != expectedErr {
		t.Errorf("expected execution error to be preserved, got %v", err)
	}

	// But state should still be saved
	store := config.Store.(statestore.Store)
	state, loadErr := store.Load(context.Background(), "test-conv")
	if loadErr != nil {
		t.Fatalf("state should be saved even when execution fails: %v", loadErr)
	}
	if state == nil {
		t.Error("expected state to be saved")
	}
}
