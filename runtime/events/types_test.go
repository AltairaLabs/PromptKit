package events

import (
	"testing"
)

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
		{EventToolExcluded, "tool.excluded"},
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

func TestAncillaryEventTypes_Constants(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  string
	}{
		{EventImageGenCallStarted, "image_gen.call.started"},
		{EventImageGenCallCompleted, "image_gen.call.completed"},
		{EventImageGenCallFailed, "image_gen.call.failed"},
		{EventTTSCallStarted, "tts.call.started"},
		{EventTTSCallCompleted, "tts.call.completed"},
		{EventTTSCallFailed, "tts.call.failed"},
		{EventSTTCallStarted, "stt.call.started"},
		{EventSTTCallCompleted, "stt.call.completed"},
		{EventSTTCallFailed, "stt.call.failed"},
		{EventEmbeddingCallStarted, "embedding.call.started"},
		{EventEmbeddingCallCompleted, "embedding.call.completed"},
		{EventEmbeddingCallFailed, "embedding.call.failed"},
		{EventInferenceCallCompleted, "inference.call.completed"},
		{EventInferenceCallFailed, "inference.call.failed"},
	}
	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			if string(tt.eventType) != tt.expected {
				t.Errorf("EventType = %v, want %v", tt.eventType, tt.expected)
			}
		})
	}
}
