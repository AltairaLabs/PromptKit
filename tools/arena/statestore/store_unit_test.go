package statestore

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	runtimestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestSaveWithTrace(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	state := &runtimestore.ConversationState{
		ID: "test-conversation",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there"},
		},
	}

	trace := &pipeline.ExecutionTrace{
		LLMCalls: []pipeline.LLMCall{
			{
				Sequence:     1,
				MessageIndex: 1,
				StartedAt:    time.Now(),
				Duration:     100 * time.Millisecond,
				Cost: types.CostInfo{
					InputTokens:  10,
					OutputTokens: 20,
				},
			},
		},
	}

	err := store.SaveWithTrace(ctx, state, trace)
	if err != nil {
		t.Fatalf("SaveWithTrace failed: %v", err)
	}

	// Verify the trace was attached
	arenaState, err := store.GetArenaState(ctx, "test-conversation")
	if err != nil {
		t.Fatalf("GetArenaState failed: %v", err)
	}

	if len(arenaState.Messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(arenaState.Messages))
	}

	// Check that trace was attached to message
	assistantMsg := arenaState.Messages[1]
	if assistantMsg.Meta == nil {
		t.Fatal("Expected Meta to be set on assistant message")
	}

	traceData, ok := assistantMsg.Meta["_llm_trace"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected _llm_trace in message Meta")
	}

	if traceData["sequence"] != 1 {
		t.Errorf("Expected sequence 1, got %v", traceData["sequence"])
	}
}

func TestSaveWithTrace_NilState(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	err := store.SaveWithTrace(ctx, nil, nil)
	if err == nil {
		t.Error("Expected error for nil state")
	}
}

func TestSaveWithTrace_NoTrace(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	state := &runtimestore.ConversationState{
		ID: "test-no-trace",
		Messages: []types.Message{
			{Role: "user", Content: "Test"},
		},
	}

	err := store.SaveWithTrace(ctx, state, nil)
	if err != nil {
		t.Fatalf("SaveWithTrace with nil trace failed: %v", err)
	}

	arenaState, _ := store.GetArenaState(ctx, "test-no-trace")
	if arenaState.Messages[0].Meta != nil {
		t.Error("Expected no Meta when trace is nil")
	}
}

func TestDelete(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	// Add a conversation
	state := &runtimestore.ConversationState{
		ID:       "test-delete",
		Messages: []types.Message{{Role: "user", Content: "Test"}},
	}

	err := store.Save(ctx, state)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify it exists
	_, err = store.GetArenaState(ctx, "test-delete")
	if err != nil {
		t.Fatalf("GetArenaState failed: %v", err)
	}

	// Delete it
	err = store.Delete(ctx, "test-delete")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it's gone
	_, err = store.GetArenaState(ctx, "test-delete")
	if err == nil {
		t.Error("Expected error after deleting conversation")
	}
}

func TestAttachTraceToMessages(t *testing.T) {
	store := NewArenaStateStore()

	state := &runtimestore.ConversationState{
		ID: "test",
		Messages: []types.Message{
			{Role: "user", Content: "Question"},
			{Role: "assistant", Content: "Answer"},
			{Role: "user", Content: "Follow-up"},
		},
	}

	trace := &pipeline.ExecutionTrace{
		LLMCalls: []pipeline.LLMCall{
			{
				Sequence:     1,
				MessageIndex: 1,
				StartedAt:    time.Now(),
				Duration:     50 * time.Millisecond,
				Cost: types.CostInfo{
					InputTokens:  5,
					OutputTokens: 10,
				},
				Request:     map[string]interface{}{"req": "data"},
				RawResponse: map[string]interface{}{"resp": "data"},
			},
		},
	}

	store.attachTraceToMessages(state, trace)

	// Check that trace was attached to the right message
	msg := state.Messages[1]
	if msg.Meta == nil {
		t.Fatal("Expected Meta on message 1")
	}

	traceData, ok := msg.Meta["_llm_trace"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected _llm_trace in Meta")
	}

	if traceData["message_index"] != 1 {
		t.Errorf("Expected message_index 1, got %v", traceData["message_index"])
	}

	// Check that raw request/response were attached
	if _, ok := msg.Meta["_llm_raw_request"]; !ok {
		t.Error("Expected _llm_raw_request in Meta")
	}

	if _, ok := msg.Meta["_llm_raw_response"]; !ok {
		t.Error("Expected _llm_raw_response in Meta")
	}

	// Check that other messages were not affected
	if state.Messages[0].Meta != nil {
		t.Error("Expected message 0 to have no Meta")
	}
}

func TestAttachTraceToMessages_OutOfBounds(t *testing.T) {
	store := NewArenaStateStore()

	state := &runtimestore.ConversationState{
		ID: "test",
		Messages: []types.Message{
			{Role: "user", Content: "Question"},
		},
	}

	trace := &pipeline.ExecutionTrace{
		LLMCalls: []pipeline.LLMCall{
			{
				Sequence:     1,
				MessageIndex: 5, // Out of bounds
				StartedAt:    time.Now(),
				Duration:     50 * time.Millisecond,
			},
		},
	}

	// Should not panic
	store.attachTraceToMessages(state, trace)

	// Original message should be unaffected
	if state.Messages[0].Meta != nil {
		t.Error("Expected message to remain unaffected")
	}
}

func TestSaveWithTrace_EmptyLLMCalls(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	state := &runtimestore.ConversationState{
		ID: "test-empty-calls",
		Messages: []types.Message{
			{Role: "user", Content: "Test"},
		},
	}

	trace := &pipeline.ExecutionTrace{
		LLMCalls: []pipeline.LLMCall{}, // Empty
	}

	err := store.SaveWithTrace(ctx, state, trace)
	if err != nil {
		t.Fatalf("SaveWithTrace failed: %v", err)
	}

	// Should save without attaching trace
	arenaState, _ := store.GetArenaState(ctx, "test-empty-calls")
	if arenaState.Messages[0].Meta != nil {
		t.Error("Expected no Meta when LLMCalls is empty")
	}
}

