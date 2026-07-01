package statestore

import (
	"context"
	"testing"

	runtimestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ---- MessageLog tests (Piece 1) ----

func TestLogAppend_EmptyStore_ReturnsLen(t *testing.T) {
	s := NewArenaStateStore()
	ctx := context.Background()
	msgs := []types.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
	}
	n, err := s.LogAppend(ctx, "conv1", 0, msgs)
	if err != nil {
		t.Fatalf("LogAppend error: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2, got %d", n)
	}
}

func TestLogAppend_IdempotentReplay_NoDuplication(t *testing.T) {
	s := NewArenaStateStore()
	ctx := context.Background()
	msgs := []types.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	if _, err := s.LogAppend(ctx, "conv1", 0, msgs); err != nil {
		t.Fatalf("first LogAppend error: %v", err)
	}
	// Replay from seq=1 (idempotent: skip first 2 already-persisted, append 3rd)
	replayMsgs := []types.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	n, err := s.LogAppend(ctx, "conv1", 0, replayMsgs)
	if err != nil {
		t.Fatalf("replay LogAppend error: %v", err)
	}
	// No growth: startSeq(0) < current(3), all 3 inputs are dupes, new total stays 3
	if n != 3 {
		t.Errorf("expected 3 after idempotent replay, got %d", n)
	}
	// Verify actual stored count doesn't grow
	all, err := s.LogLoad(ctx, "conv1", 0)
	if err != nil {
		t.Fatalf("LogLoad error: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 messages stored (no duplication), got %d", len(all))
	}
}

func TestLogAppend_PartialReplay_AppendsTail(t *testing.T) {
	s := NewArenaStateStore()
	ctx := context.Background()
	first := []types.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
	}
	if _, err := s.LogAppend(ctx, "conv1", 0, first); err != nil {
		t.Fatalf("first LogAppend error: %v", err)
	}
	// Append from seq=1: skip first 1 already-persisted (current=2 - startSeq=1 = 1 to skip)
	second := []types.Message{
		{Role: "user", Content: "hello"},   // already persisted, skip
		{Role: "assistant", Content: "hi"}, // new
	}
	n, err := s.LogAppend(ctx, "conv1", 1, second)
	if err != nil {
		t.Fatalf("second LogAppend error: %v", err)
	}
	if n != 3 {
		t.Errorf("expected 3 total after partial replay, got %d", n)
	}
}

func TestLogLoad_RecentNMessages(t *testing.T) {
	s := NewArenaStateStore()
	ctx := context.Background()
	msgs := []types.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
	}
	if _, err := s.LogAppend(ctx, "conv1", 0, msgs); err != nil {
		t.Fatalf("LogAppend error: %v", err)
	}

	// recent=0 → all
	all, err := s.LogLoad(ctx, "conv1", 0)
	if err != nil {
		t.Fatalf("LogLoad all error: %v", err)
	}
	if len(all) != 5 {
		t.Errorf("expected 5 for recent=0, got %d", len(all))
	}

	// recent=2 → last 2
	last2, err := s.LogLoad(ctx, "conv1", 2)
	if err != nil {
		t.Fatalf("LogLoad recent=2 error: %v", err)
	}
	if len(last2) != 2 {
		t.Errorf("expected 2 for recent=2, got %d", len(last2))
	}
	if last2[0].Content != "u2" || last2[1].Content != "a2" {
		t.Errorf("expected last 2 messages [u2, a2], got %v %v", last2[0].Content, last2[1].Content)
	}
}

func TestLogLoad_MissingConversation_ReturnsEmptySlice(t *testing.T) {
	s := NewArenaStateStore()
	ctx := context.Background()
	msgs, err := s.LogLoad(ctx, "missing", 0)
	if err != nil {
		t.Fatalf("expected no error for missing conv, got %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected empty slice for missing conv, got %d messages", len(msgs))
	}
}

func TestLogLen_MissingConversation_ReturnsZero(t *testing.T) {
	s := NewArenaStateStore()
	ctx := context.Background()
	n, err := s.LogLen(ctx, "missing")
	if err != nil {
		t.Fatalf("expected no error for missing conv, got %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 for missing conv, got %d", n)
	}
}

func TestLogLen_AfterAppend(t *testing.T) {
	s := NewArenaStateStore()
	ctx := context.Background()
	msgs := []types.Message{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
	}
	if _, err := s.LogAppend(ctx, "conv1", 0, msgs); err != nil {
		t.Fatalf("LogAppend error: %v", err)
	}
	n, err := s.LogLen(ctx, "conv1")
	if err != nil {
		t.Fatalf("LogLen error: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2, got %d", n)
	}
}

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

// TestDeepCloneMessage_PreservesReasoning guards the bug where the arena store's
// field-by-field clone dropped Message.Reasoning, so reasoning never reached
// reports (RunResult.Messages is loaded back from the store).
func TestDeepCloneMessage_PreservesReasoning(t *testing.T) {
	s := NewArenaStateStore()
	msg := &types.Message{
		Role:    "assistant",
		Content: "ANSWER: 16",
		Reasoning: &types.ReasoningTrace{
			Text:   "step-by-step thinking",
			Opaque: []types.OpaqueReasoning{{Provider: "gemini", Kind: "thought_signature", Data: "sig"}},
		},
	}
	cloned := s.deepCloneMessage(msg)
	if cloned.Reasoning == nil {
		t.Fatal("clone dropped Message.Reasoning")
	}
	if cloned.Reasoning.Text != "step-by-step thinking" {
		t.Fatalf("reasoning text = %q", cloned.Reasoning.Text)
	}
	if len(cloned.Reasoning.Opaque) != 1 || cloned.Reasoning.Opaque[0].Data != "sig" {
		t.Fatalf("opaque reasoning not deep-copied: %+v", cloned.Reasoning.Opaque)
	}
	// Mutating the clone's Opaque must not touch the original (deep copy).
	cloned.Reasoning.Opaque[0].Data = "changed"
	if msg.Reasoning.Opaque[0].Data != "sig" {
		t.Fatal("clone shares Opaque backing array with the original")
	}
}
