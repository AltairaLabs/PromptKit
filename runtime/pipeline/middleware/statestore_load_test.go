package middleware

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestStateStoreLoadMiddleware(t *testing.T) {
	tests := []struct {
		name            string
		config          *pipeline.StateStoreConfig
		existingState   *statestore.ConversationState
		initialMessages []types.Message
		wantMessages    int
		wantMetadata    map[string]interface{}
		wantErr         bool
	}{
		{
			name:            "no config provided (no-op)",
			config:          nil,
			initialMessages: []types.Message{{Role: "user", Content: "test"}},
			wantMessages:    1,
			wantMetadata:    nil,
			wantErr:         false,
		},
		{
			name: "conversation not found (starts fresh)",
			config: &pipeline.StateStoreConfig{
				Store:          statestore.NewMemoryStore(),
				ConversationID: "new-conv",
				UserID:         "user-123",
			},
			existingState:   nil,
			initialMessages: []types.Message{{Role: "user", Content: "new message"}},
			wantMessages:    1,
			wantMetadata: map[string]interface{}{
				"conversation_id": "new-conv",
				"user_id":         "user-123",
			},
			wantErr: false,
		},
		{
			name: "load existing conversation history",
			config: &pipeline.StateStoreConfig{
				Store:          statestore.NewMemoryStore(),
				ConversationID: "existing-conv",
				UserID:         "user-456",
			},
			existingState: &statestore.ConversationState{
				ID:     "existing-conv",
				UserID: "user-456",
				Messages: []types.Message{
					{Role: "user", Content: "previous message 1"},
					{Role: "assistant", Content: "previous response 1"},
				},
				Metadata: map[string]interface{}{"context": "test"},
			},
			initialMessages: []types.Message{{Role: "user", Content: "new message"}},
			wantMessages:    3, // 2 history + 1 new
			wantMetadata: map[string]interface{}{
				"conversation_id": "existing-conv",
				"user_id":         "user-456",
			},
			wantErr: false,
		},
		{
			name: "load with config metadata",
			config: &pipeline.StateStoreConfig{
				Store:          statestore.NewMemoryStore(),
				ConversationID: "conv-with-meta",
				UserID:         "user-789",
				Metadata: map[string]interface{}{
					"custom_key": "custom_value",
					"session_id": "sess-123",
				},
			},
			existingState:   nil,
			initialMessages: []types.Message{{Role: "user", Content: "test"}},
			wantMessages:    1,
			wantMetadata: map[string]interface{}{
				"conversation_id": "conv-with-meta",
				"user_id":         "user-789",
				"custom_key":      "custom_value",
				"session_id":      "sess-123",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup: Save existing state if provided
			if tt.existingState != nil && tt.config != nil {
				store := tt.config.Store.(statestore.Store)
				err := store.Save(context.Background(), tt.existingState)
				if err != nil {
					t.Fatalf("failed to setup test state: %v", err)
				}
			}

			// Create execution context
			execCtx := &pipeline.ExecutionContext{
				Context:  context.Background(),
				Messages: tt.initialMessages,
				Metadata: make(map[string]interface{}),
			}

			// Create middleware
			middleware := StateStoreLoadMiddleware(tt.config)

			// Execute
			err := middleware.Process(execCtx, func() error { return nil })

			// Verify
			if (err != nil) != tt.wantErr {
				t.Errorf("StateStoreLoadMiddleware() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			// Verify message count
			if len(execCtx.Messages) != tt.wantMessages {
				t.Errorf("got %d messages, want %d", len(execCtx.Messages), tt.wantMessages)
			}

			// Verify metadata if expected
			if tt.wantMetadata != nil {
				for key, expectedVal := range tt.wantMetadata {
					actualVal, ok := execCtx.Metadata[key]
					if !ok {
						t.Errorf("expected metadata key %q not found", key)
						continue
					}
					if actualVal != expectedVal {
						t.Errorf("metadata[%q] = %v, want %v", key, actualVal, expectedVal)
					}
				}
			}

			// Verify message order (history should come first)
			if tt.existingState != nil && len(tt.existingState.Messages) > 0 {
				if execCtx.Messages[0].Content != tt.existingState.Messages[0].Content {
					t.Error("history messages should be prepended before new messages")
				}
			}
		})
	}
}

func TestStateStoreLoadMiddleware_InvalidStore(t *testing.T) {
	config := &pipeline.StateStoreConfig{
		Store:          "invalid-type", // Not a statestore.Store
		ConversationID: "test",
	}

	execCtx := &pipeline.ExecutionContext{
		Context:  context.Background(),
		Messages: []types.Message{{Role: "user", Content: "test"}},
	}

	middleware := StateStoreLoadMiddleware(config)

	err := middleware.Process(execCtx, func() error { return nil })

	if err == nil {
		t.Error("expected error for invalid store type, got nil")
	}
}

func TestStateStoreLoadMiddleware_SetsSourceField(t *testing.T) {
	// Setup: Create store with existing conversation
	store := statestore.NewMemoryStore()
	existingState := &statestore.ConversationState{
		ID:     "test-conv",
		UserID: "user-123",
		Messages: []types.Message{
			{Role: "user", Content: "previous user message"},
			{Role: "assistant", Content: "previous assistant response"},
		},
	}
	err := store.Save(context.Background(), existingState)
	if err != nil {
		t.Fatalf("failed to setup test state: %v", err)
	}

	// Create execution context with new message (no Source set yet)
	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Messages: []types.Message{
			{Role: "user", Content: "new user message"},
		},
	}

	// Create and execute middleware
	config := &pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: "test-conv",
		UserID:         "user-123",
	}
	middleware := StateStoreLoadMiddleware(config)
	err = middleware.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify: All loaded messages should have Source="statestore"
	// First 2 messages are from StateStore
	if len(execCtx.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(execCtx.Messages))
	}

	for i := 0; i < 2; i++ {
		if execCtx.Messages[i].Source != "statestore" {
			t.Errorf("message[%d] (loaded from StateStore) Source = %q, want %q",
				i, execCtx.Messages[i].Source, "statestore")
		}
	}

	// Last message should have empty Source (not set by load middleware)
	if execCtx.Messages[2].Source != "" {
		t.Errorf("message[2] (new message) Source = %q, want empty string", execCtx.Messages[2].Source)
	}
}
