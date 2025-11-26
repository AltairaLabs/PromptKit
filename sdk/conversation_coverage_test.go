package sdk

import (
	"errors"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestNewConversationManager_MissingProvider covers validation error (improves from 77.8%)
func TestNewConversationManager_MissingProvider(t *testing.T) {
	_, err := NewConversationManager()

	if err == nil {
		t.Fatal("expected error for missing provider")
	}
	if err.Error() != "provider is required (use WithProvider option)" {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestHandleStreamError_WithError covers error path (improves from 50%)
func TestHandleStreamError_WithError(t *testing.T) {
	mockProvider := mock.NewProvider("test", "model", false)
	manager, _ := NewConversationManager(WithProvider(mockProvider))

	conv := &Conversation{
		manager: manager,
		state:   &statestore.ConversationState{},
	}

	eventChan := make(chan StreamEvent, 1)
	testError := errors.New("stream error")
	chunk := &providers.StreamChunk{
		Error: testError,
	}

	shouldStop := conv.handleStreamError(chunk, eventChan)

	if !shouldStop {
		t.Error("expected handleStreamError to return true")
	}

	select {
	case event := <-eventChan:
		if event.Type != "error" {
			t.Errorf("expected error event, got %s", event.Type)
		}
		if event.Error != testError {
			t.Errorf("expected error %v, got %v", testError, event.Error)
		}
	default:
		t.Fatal("expected error event in channel")
	}
}

// TestHandleToolCalls_MultipleToolCalls covers tool call path (improves from 50%)
func TestHandleToolCalls_MultipleToolCalls(t *testing.T) {
	mockProvider := mock.NewProvider("test", "model", false)
	manager, _ := NewConversationManager(WithProvider(mockProvider))

	conv := &Conversation{
		manager: manager,
		state:   &statestore.ConversationState{},
	}

	eventChan := make(chan StreamEvent, 3)
	chunk := &providers.StreamChunk{
		ToolCalls: []types.MessageToolCall{
			{ID: "call1", Name: "tool1"},
			{ID: "call2", Name: "tool2"},
		},
	}

	conv.handleToolCalls(chunk, eventChan)

	if len(eventChan) != 2 {
		t.Fatalf("expected 2 events, got %d", len(eventChan))
	}

	event1 := <-eventChan
	if event1.Type != "tool_call" || event1.ToolCall.ID != "call1" {
		t.Errorf("unexpected first event: %+v", event1)
	}

	event2 := <-eventChan
	if event2.Type != "tool_call" || event2.ToolCall.ID != "call2" {
		t.Errorf("unexpected second event: %+v", event2)
	}
}

// TestHandleStreamCompletion_NoFinishReason covers false path (improves from 55.6%)
func TestHandleStreamCompletion_NoFinishReason(t *testing.T) {
	mockProvider := mock.NewProvider("test", "model", false)
	manager, _ := NewConversationManager(WithProvider(mockProvider))

	conv := &Conversation{
		manager: manager,
		state:   &statestore.ConversationState{},
	}

	eventChan := make(chan StreamEvent, 1)
	var finalResult *pipeline.ExecutionResult
	chunk := &providers.StreamChunk{
		FinishReason: nil,
	}

	shouldStop := conv.handleStreamCompletion(chunk, eventChan, &finalResult, "content", time.Now())

	if shouldStop {
		t.Error("expected handleStreamCompletion to return false when no finish reason")
	}
}

// TestConvertToPendingToolsSlice_EmptyMetadata covers nil/empty cases (improves from 80%)
func TestConvertToPendingToolsSlice_EmptyMetadata(t *testing.T) {
	mockProvider := mock.NewProvider("test", "model", false)
	manager, _ := NewConversationManager(WithProvider(mockProvider))

	conv := &Conversation{
		manager: manager,
		state:   &statestore.ConversationState{},
	}

	// Test nil metadata
	result := conv.convertToPendingToolsSlice(nil)
	if result != nil {
		t.Errorf("expected nil for nil metadata, got %v", result)
	}

	// Test empty metadata
	result = conv.convertToPendingToolsSlice(map[string]interface{}{})
	if result != nil {
		t.Errorf("expected nil for empty metadata, got %v", result)
	}

	// Test invalid type
	metadata := map[string]interface{}{
		"pending_tools": "not a slice",
	}
	result = conv.convertToPendingToolsSlice(metadata)
	if result != nil {
		t.Errorf("expected nil for invalid type, got %v", result)
	}
}

// TestConvertInterfaceSliceToTools_AllInvalid covers error path (improves from 71.4%)
func TestConvertInterfaceSliceToTools_AllInvalid(t *testing.T) {
	mockProvider := mock.NewProvider("test", "model", false)
	manager, _ := NewConversationManager(WithProvider(mockProvider))

	conv := &Conversation{
		manager: manager,
		state:   &statestore.ConversationState{},
	}

	input := []interface{}{"invalid", 123, true}
	result := conv.convertInterfaceSliceToTools(input)

	if len(result) != 0 {
		t.Errorf("expected empty slice for all invalid types, got %d items", len(result))
	}
}

// TestExtractValidationsFromResult_NoValidation covers nil path (improves from 83.3%)
func TestExtractValidationsFromResult_NoValidation(t *testing.T) {
	mockProvider := mock.NewProvider("test", "model", false)
	manager, _ := NewConversationManager(WithProvider(mockProvider))

	conv := &Conversation{
		manager: manager,
		state:   &statestore.ConversationState{},
	}

	result := &pipeline.ExecutionResult{}

	validations := conv.extractValidationsFromResult(result)

	if validations != nil {
		t.Errorf("expected nil validations, got %+v", validations)
	}
}
