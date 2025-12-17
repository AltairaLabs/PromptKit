package statestore

import (
	"context"
	"testing"

	runtimestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestUpdateLastAssistantMessage(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	// Create initial state with a conversation
	convID := "test-conv"
	state := &runtimestore.ConversationState{
		ID: convID,
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
		},
	}

	// Save initial state
	err := store.Save(ctx, state)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Update the last assistant message
	updatedMsg := &types.Message{
		Role:    "assistant",
		Content: "Hi there!",
		Meta: map[string]interface{}{
			"assertions": map[string]interface{}{
				"passed": true,
			},
		},
	}

	store.UpdateLastAssistantMessage(updatedMsg)

	// Load and verify
	loaded, err := store.Load(ctx, convID)
	if err != nil {
		t.Fatalf("Failed to load state: %v", err)
	}

	if len(loaded.Messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(loaded.Messages))
	}

	lastMsg := loaded.Messages[1]
	if lastMsg.Meta == nil {
		t.Fatal("Expected Meta to be set on last message")
	}

	assertions, ok := lastMsg.Meta["assertions"]
	if !ok {
		t.Error("Expected assertions in Meta")
	}

	assertionsMap, ok := assertions.(map[string]interface{})
	if !ok {
		t.Error("Expected assertions to be a map")
	}

	if assertionsMap["passed"] != true {
		t.Error("Expected assertions.passed to be true")
	}
}

func TestUpdateLastAssistantMessageMultipleAssistants(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	// Create conversation with multiple assistant messages
	convID := "test-conv-2"
	state := &runtimestore.ConversationState{
		ID: convID,
		Messages: []types.Message{
			{Role: "user", Content: "First question"},
			{Role: "assistant", Content: "First answer"},
			{Role: "user", Content: "Second question"},
			{Role: "assistant", Content: "Second answer"},
		},
	}

	err := store.Save(ctx, state)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Update should affect only the last assistant message
	updatedMsg := &types.Message{
		Role:    "assistant",
		Content: "Second answer",
		Meta: map[string]interface{}{
			"test_key": "test_value",
		},
	}

	store.UpdateLastAssistantMessage(updatedMsg)

	// Verify
	loaded, err := store.Load(ctx, convID)
	if err != nil {
		t.Fatalf("Failed to load state: %v", err)
	}

	// First assistant message should not be affected
	if loaded.Messages[1].Meta != nil {
		if _, ok := loaded.Messages[1].Meta["test_key"]; ok {
			t.Error("First assistant message should not be updated")
		}
	}

	// Last assistant message should have the update
	if loaded.Messages[3].Meta == nil {
		t.Fatal("Expected Meta on last assistant message")
	}

	if loaded.Messages[3].Meta["test_key"] != "test_value" {
		t.Error("Expected test_key to be set on last assistant message")
	}
}

func TestUpdateLastAssistantMessageNoMatch(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	// Create conversation with only user messages
	convID := "test-conv-3"
	state := &runtimestore.ConversationState{
		ID: convID,
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	err := store.Save(ctx, state)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Try to update non-existent assistant message
	updatedMsg := &types.Message{
		Role:    "assistant",
		Content: "This doesn't exist",
		Meta: map[string]interface{}{
			"test": "value",
		},
	}

	// Should not panic, just be a no-op
	store.UpdateLastAssistantMessage(updatedMsg)

	// Verify state unchanged
	loaded, err := store.Load(ctx, convID)
	if err != nil {
		t.Fatalf("Failed to load state: %v", err)
	}

	if len(loaded.Messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(loaded.Messages))
	}
}
