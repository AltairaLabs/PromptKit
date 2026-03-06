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

// TestLoad_DeepClone verifies that Load returns a deep copy (C4 fix)
func TestLoad_DeepClone(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	txt := "hello"
	state := &runtimestore.ConversationState{
		ID: "deep-clone-test",
		Messages: []types.Message{
			{
				Role:    "assistant",
				Content: "response",
				Parts: []types.ContentPart{
					{Type: "text", Text: &txt},
				},
				Meta: map[string]interface{}{
					"key": "value",
				},
			},
		},
		Metadata: map[string]interface{}{
			"session": "data",
		},
	}

	if err := store.Save(ctx, state); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load and mutate the returned state
	loaded, err := store.Load(ctx, "deep-clone-test")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	loaded.Messages[0].Content = "mutated"
	loaded.Messages[0].Meta["key"] = "mutated"
	loaded.Metadata["session"] = "mutated"

	// Load again and verify internal state is unchanged
	loaded2, err := store.Load(ctx, "deep-clone-test")
	if err != nil {
		t.Fatalf("Second Load failed: %v", err)
	}
	if loaded2.Messages[0].Content != "response" {
		t.Errorf("Expected original content 'response', got %q", loaded2.Messages[0].Content)
	}
	if loaded2.Messages[0].Meta["key"] != "value" {
		t.Errorf("Expected original meta 'value', got %v", loaded2.Messages[0].Meta["key"])
	}
	if loaded2.Metadata["session"] != "data" {
		t.Errorf("Expected original metadata 'data', got %v", loaded2.Metadata["session"])
	}
}

// TestDeepCloneContentPart verifies deep clone of ContentPart including Media (C5 fix)
func TestDeepCloneContentPart(t *testing.T) {
	store := NewArenaStateStore()

	txt := "hello"
	data := "base64data"
	filePath := "/tmp/file.png"
	width := 100
	height := 200
	sizeKB := int64(50)

	part := types.ContentPart{
		Type: "image",
		Text: &txt,
		Media: &types.MediaContent{
			Data:     &data,
			FilePath: &filePath,
			MIMEType: "image/png",
			Width:    &width,
			Height:   &height,
			SizeKB:   &sizeKB,
		},
	}

	cloned := store.deepCloneContentPart(part)

	// Verify values are equal
	if *cloned.Text != txt {
		t.Errorf("Expected text %q, got %q", txt, *cloned.Text)
	}
	if *cloned.Media.Data != data {
		t.Errorf("Expected data %q, got %q", data, *cloned.Media.Data)
	}
	if *cloned.Media.Width != width {
		t.Errorf("Expected width %d, got %d", width, *cloned.Media.Width)
	}

	// Verify pointers are different (deep copy)
	if cloned.Text == part.Text {
		t.Error("Text pointer should be different after deep clone")
	}
	if cloned.Media == part.Media {
		t.Error("Media pointer should be different after deep clone")
	}
	if cloned.Media.Data == part.Media.Data {
		t.Error("Media.Data pointer should be different after deep clone")
	}
	if cloned.Media.Width == part.Media.Width {
		t.Error("Media.Width pointer should be different after deep clone")
	}

	// Mutate original and verify clone is unaffected
	*part.Text = "mutated"
	*part.Media.Data = "mutated"
	*part.Media.Width = 999
	if *cloned.Text != "hello" {
		t.Error("Clone text was affected by original mutation")
	}
	if *cloned.Media.Data != "base64data" {
		t.Error("Clone media data was affected by original mutation")
	}
	if *cloned.Media.Width != 100 {
		t.Error("Clone media width was affected by original mutation")
	}
}

// TestGetArenaState_DeepClone verifies GetArenaState returns a deep copy (H10 fix)
func TestGetArenaState_DeepClone(t *testing.T) {
	store := NewArenaStateStore()
	ctx := context.Background()

	state := &runtimestore.ConversationState{
		ID: "arena-state-clone-test",
		Messages: []types.Message{
			{Role: "user", Content: "hello"},
		},
	}

	if err := store.Save(ctx, state); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Save metadata
	meta := &RunMetadata{
		RunID:      "run-1",
		ScenarioID: "scenario-1",
		ProviderID: "provider-1",
		Params: map[string]interface{}{
			"temp": 0.7,
		},
	}
	if err := store.SaveMetadata(ctx, "arena-state-clone-test", meta); err != nil {
		t.Fatalf("SaveMetadata failed: %v", err)
	}

	// Get arena state and mutate it
	arenaState, err := store.GetArenaState(ctx, "arena-state-clone-test")
	if err != nil {
		t.Fatalf("GetArenaState failed: %v", err)
	}
	arenaState.Messages[0].Content = "mutated"
	arenaState.RunMetadata.ScenarioID = "mutated"

	// Get again and verify internal state is unchanged
	arenaState2, err := store.GetArenaState(ctx, "arena-state-clone-test")
	if err != nil {
		t.Fatalf("Second GetArenaState failed: %v", err)
	}
	if arenaState2.Messages[0].Content != "hello" {
		t.Errorf("Expected 'hello', got %q", arenaState2.Messages[0].Content)
	}
	if arenaState2.RunMetadata.ScenarioID != "scenario-1" {
		t.Errorf("Expected 'scenario-1', got %q", arenaState2.RunMetadata.ScenarioID)
	}
}

// TestDeepCloneMediaContent_NilFields verifies deep clone handles nil fields
func TestDeepCloneMediaContent_NilFields(t *testing.T) {
	store := NewArenaStateStore()

	// Test with nil media
	result := store.deepCloneMediaContent(nil)
	if result != nil {
		t.Error("Expected nil for nil media input")
	}

	// Test with media that has only MIMEType set (all pointers nil)
	m := &types.MediaContent{MIMEType: "audio/mp3"}
	cloned := store.deepCloneMediaContent(m)
	if cloned.MIMEType != "audio/mp3" {
		t.Errorf("Expected MIMEType 'audio/mp3', got %q", cloned.MIMEType)
	}
	if cloned.Data != nil || cloned.FilePath != nil || cloned.URL != nil {
		t.Error("Expected nil pointer fields to remain nil")
	}
}

// TestCloneHelpers verifies the pointer clone helper functions
func TestCloneHelpers(t *testing.T) {
	t.Run("cloneStringPtr", func(t *testing.T) {
		if cloneStringPtr(nil) != nil {
			t.Error("Expected nil")
		}
		s := "test"
		c := cloneStringPtr(&s)
		if c == &s {
			t.Error("Expected different pointer")
		}
		if *c != "test" {
			t.Errorf("Expected 'test', got %q", *c)
		}
	})

	t.Run("cloneIntPtr", func(t *testing.T) {
		if cloneIntPtr(nil) != nil {
			t.Error("Expected nil")
		}
		i := 42
		c := cloneIntPtr(&i)
		if c == &i {
			t.Error("Expected different pointer")
		}
		if *c != 42 {
			t.Errorf("Expected 42, got %d", *c)
		}
	})

	t.Run("cloneInt64Ptr", func(t *testing.T) {
		if cloneInt64Ptr(nil) != nil {
			t.Error("Expected nil")
		}
		i := int64(999)
		c := cloneInt64Ptr(&i)
		if c == &i {
			t.Error("Expected different pointer")
		}
		if *c != 999 {
			t.Errorf("Expected 999, got %d", *c)
		}
	})
}
