package events

import (
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestBaseEventData_EventData(t *testing.T) {
	// Test that baseEventData satisfies EventData interface
	var _ EventData = baseEventData{}

	// Test that it has the marker method
	bed := baseEventData{}
	bed.eventData() // Should not panic

	// Test that MessageCreatedData embeds baseEventData and satisfies EventData
	var _ EventData = &MessageCreatedData{}
	msgData := &MessageCreatedData{
		Role:    "user",
		Content: "test",
	}
	msgData.eventData() // Should not panic
}

func TestEventDataStructs(t *testing.T) {
	// Test that all event data structs satisfy EventData interface
	var _ EventData = &PipelineStartedData{}
	var _ EventData = &PipelineCompletedData{}
	var _ EventData = &PipelineFailedData{}
	var _ EventData = &MiddlewareStartedData{}
	var _ EventData = &MiddlewareCompletedData{}
	var _ EventData = &MiddlewareFailedData{}
	var _ EventData = &ProviderCallStartedData{}
	var _ EventData = &ProviderCallCompletedData{}
	var _ EventData = &ProviderCallFailedData{}
	var _ EventData = &ToolCallStartedData{}
	var _ EventData = &ToolCallCompletedData{}
	var _ EventData = &ToolCallFailedData{}
	var _ EventData = &ValidationStartedData{}
	var _ EventData = &ValidationPassedData{}
	var _ EventData = &ValidationFailedData{}
	var _ EventData = &ContextBuiltData{}
	var _ EventData = &TokenBudgetExceededData{}
	var _ EventData = &StateLoadedData{}
	var _ EventData = &StateSavedData{}
	var _ EventData = &StreamInterruptedData{}
	var _ EventData = &CustomEventData{}
}

func TestEvent_Creation(t *testing.T) {
	now := time.Now()
	event := &Event{
		Type:           EventPipelineStarted,
		Timestamp:      now,
		RunID:          "test-run",
		SessionID:      "test-session",
		ConversationID: "test-conversation",
		Data: &PipelineStartedData{
			MiddlewareCount: 5,
		},
	}

	if event.Type != EventPipelineStarted {
		t.Errorf("Event.Type = %v, want %v", event.Type, EventPipelineStarted)
	}
	if event.Timestamp != now {
		t.Errorf("Event.Timestamp = %v, want %v", event.Timestamp, now)
	}
	if event.RunID != "test-run" {
		t.Errorf("Event.RunID = %v, want test-run", event.RunID)
	}

	data, ok := event.Data.(*PipelineStartedData)
	if !ok {
		t.Fatalf("Event.Data type assertion failed")
	}
	if data.MiddlewareCount != 5 {
		t.Errorf("PipelineStartedData.MiddlewareCount = %v, want 5", data.MiddlewareCount)
	}
}

func TestEventTypes_Constants(t *testing.T) {
	// Test that event type constants have expected values
	tests := []struct {
		eventType EventType
		expected  string
	}{
		{EventPipelineStarted, "pipeline.started"},
		{EventPipelineCompleted, "pipeline.completed"},
		{EventPipelineFailed, "pipeline.failed"},
		{EventMiddlewareStarted, "middleware.started"},
		{EventMiddlewareCompleted, "middleware.completed"},
		{EventMiddlewareFailed, "middleware.failed"},
		{EventProviderCallStarted, "provider.call.started"},
		{EventProviderCallCompleted, "provider.call.completed"},
		{EventProviderCallFailed, "provider.call.failed"},
		{EventToolCallStarted, "tool.call.started"},
		{EventToolCallCompleted, "tool.call.completed"},
		{EventToolCallFailed, "tool.call.failed"},
		{EventValidationStarted, "validation.started"},
		{EventValidationPassed, "validation.passed"},
		{EventValidationFailed, "validation.failed"},
		{EventContextBuilt, "context.built"},
		{EventTokenBudgetExceeded, "context.token_budget_exceeded"},
		{EventStateLoaded, "state.loaded"},
		{EventStateSaved, "state.saved"},
		{EventStreamInterrupted, "stream.interrupted"},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			if string(tt.eventType) != tt.expected {
				t.Errorf("EventType = %v, want %v", tt.eventType, tt.expected)
			}
		})
	}
}

func TestConsolidatedTypes_Aliases(t *testing.T) {
	// Verify that type aliases resolve to the canonical consolidated types.
	// Because aliases are transparent, values constructed via old names
	// can be asserted with the new canonical name and vice versa.
	mw := &MiddlewareStartedData{Name: "auth", Index: 0}
	var mwCanonical *MiddlewareEventData = mw // alias identity
	if mwCanonical.Name != "auth" {
		t.Errorf("MiddlewareEventData.Name = %v, want auth", mwCanonical.Name)
	}

	stage := &StageCompletedData{Name: "provider", Index: 1, StageType: "generate"}
	var stCanonical *StageEventData = stage
	if stCanonical.Name != "provider" {
		t.Errorf("StageEventData.Name = %v, want provider", stCanonical.Name)
	}

	tc := &ToolCallFailedData{ToolName: "search", CallID: "c1"}
	var tcCanonical *ToolCallEventData = tc
	if tcCanonical.ToolName != "search" {
		t.Errorf("ToolCallEventData.ToolName = %v, want search", tcCanonical.ToolName)
	}

	val := &ValidationPassedData{ValidatorName: "guard", ValidatorType: "output"}
	var valCanonical *ValidationEventData = val
	if valCanonical.ValidatorName != "guard" {
		t.Errorf("ValidationEventData.ValidatorName = %v, want guard", valCanonical.ValidatorName)
	}

	st := &StateLoadedData{ConversationID: "conv", MessageCount: 3}
	var stateCanonical *StateEventData = st
	if stateCanonical.ConversationID != "conv" {
		t.Errorf("StateEventData.ConversationID = %v, want conv", stateCanonical.ConversationID)
	}

	ai := &AudioInputData{Actor: "user", Direction: "input"}
	var audioCanonical *AudioEventData = ai
	if audioCanonical.Actor != "user" {
		t.Errorf("AudioEventData.Actor = %v, want user", audioCanonical.Actor)
	}

	ii := &ImageOutputData{GeneratedFrom: "dalle", Direction: "output"}
	var imgCanonical *ImageEventData = ii
	if imgCanonical.GeneratedFrom != "dalle" {
		t.Errorf("ImageEventData.GeneratedFrom = %v, want dalle", imgCanonical.GeneratedFrom)
	}

	// All consolidated types satisfy EventData
	var _ EventData = &MiddlewareEventData{}
	var _ EventData = &StageEventData{}
	var _ EventData = &ToolCallEventData{}
	var _ EventData = &ValidationEventData{}
	var _ EventData = &StateEventData{}
	var _ EventData = &AudioEventData{}
	var _ EventData = &ImageEventData{}
}

func TestMessageCreatedData_Parts(t *testing.T) {
	// Test that MessageCreatedData can store multimodal content parts
	textContent := "Check out this image"
	imageURL := "https://example.com/image.jpg"

	msgData := &MessageCreatedData{
		Role:    "assistant",
		Content: textContent,
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeText,
				Text: &textContent,
			},
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					URL:      &imageURL,
					MIMEType: types.MIMETypeImageJPEG,
				},
			},
		},
	}

	// Ensure it satisfies EventData interface
	var _ EventData = msgData
	msgData.eventData() // Call the marker method

	if msgData.Role != "assistant" {
		t.Errorf("MessageCreatedData.Role = %v, want assistant", msgData.Role)
	}
	if msgData.Content != textContent {
		t.Errorf("MessageCreatedData.Content = %v, want %v", msgData.Content, textContent)
	}
	if len(msgData.Parts) != 2 {
		t.Fatalf("MessageCreatedData.Parts length = %v, want 2", len(msgData.Parts))
	}

	// Verify text part
	if msgData.Parts[0].Type != types.ContentTypeText {
		t.Errorf("Parts[0].Type = %v, want %v", msgData.Parts[0].Type, types.ContentTypeText)
	}
	if msgData.Parts[0].Text == nil || *msgData.Parts[0].Text != textContent {
		t.Errorf("Parts[0].Text = %v, want %v", msgData.Parts[0].Text, textContent)
	}

	// Verify image part
	if msgData.Parts[1].Type != types.ContentTypeImage {
		t.Errorf("Parts[1].Type = %v, want %v", msgData.Parts[1].Type, types.ContentTypeImage)
	}
	if msgData.Parts[1].Media == nil || msgData.Parts[1].Media.URL == nil || *msgData.Parts[1].Media.URL != imageURL {
		t.Errorf("Parts[1].Media.URL = %v, want %v", msgData.Parts[1].Media.URL, imageURL)
	}
}
