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

	store.UpdateLastAssistantMessage(convID, updatedMsg)

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

	store.UpdateLastAssistantMessage(convID, updatedMsg)

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
	store.UpdateLastAssistantMessage(convID, updatedMsg)

	// Verify state unchanged
	loaded, err := store.Load(ctx, convID)
	if err != nil {
		t.Fatalf("Failed to load state: %v", err)
	}

	if len(loaded.Messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(loaded.Messages))
	}
}

func TestUpdateLastAssistantMessageDirectLookup(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	// Create two conversations with the same assistant message content
	conv1 := "conv-1"
	conv2 := "conv-2"
	sharedContent := "Same answer"

	state1 := &runtimestore.ConversationState{
		ID: conv1,
		Messages: []types.Message{
			{Role: "user", Content: "Q1"},
			{Role: "assistant", Content: sharedContent},
		},
	}
	state2 := &runtimestore.ConversationState{
		ID: conv2,
		Messages: []types.Message{
			{Role: "user", Content: "Q2"},
			{Role: "assistant", Content: sharedContent},
		},
	}

	if err := store.Save(ctx, state1); err != nil {
		t.Fatalf("Failed to save state1: %v", err)
	}
	if err := store.Save(ctx, state2); err != nil {
		t.Fatalf("Failed to save state2: %v", err)
	}

	// Update only conv2 using direct conversationID lookup
	updatedMsg := &types.Message{
		Role:    "assistant",
		Content: sharedContent,
		Meta: map[string]interface{}{
			"targeted": true,
		},
	}
	store.UpdateLastAssistantMessage(conv2, updatedMsg)

	// conv2 should be updated
	loaded2, err := store.Load(ctx, conv2)
	if err != nil {
		t.Fatalf("Failed to load conv2: %v", err)
	}
	if loaded2.Messages[1].Meta == nil || loaded2.Messages[1].Meta["targeted"] != true {
		t.Error("Expected conv2 assistant message to be updated")
	}

	// conv1 should NOT be updated
	loaded1, err := store.Load(ctx, conv1)
	if err != nil {
		t.Fatalf("Failed to load conv1: %v", err)
	}
	if loaded1.Messages[1].Meta != nil {
		if _, exists := loaded1.Messages[1].Meta["targeted"]; exists {
			t.Error("Expected conv1 assistant message to remain untouched")
		}
	}
}

func TestUpdateLastAssistantMessageFallbackWithEmptyID(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	convID := "conv-fallback"
	state := &runtimestore.ConversationState{
		ID: convID,
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
		},
	}
	if err := store.Save(ctx, state); err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Empty conversationID should still work via fallback scan
	updatedMsg := &types.Message{
		Role:    "assistant",
		Content: "Hi there!",
		Meta: map[string]interface{}{
			"fallback": true,
		},
	}
	store.UpdateLastAssistantMessage("", updatedMsg)

	loaded, err := store.Load(ctx, convID)
	if err != nil {
		t.Fatalf("Failed to load state: %v", err)
	}
	if loaded.Messages[1].Meta == nil || loaded.Messages[1].Meta["fallback"] != true {
		t.Error("Expected fallback scan to update the message")
	}
}
