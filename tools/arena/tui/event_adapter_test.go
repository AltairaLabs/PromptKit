package tui

import (
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

func TestEventAdapterHandlesRunLifecycle(t *testing.T) {
	t.Parallel()

	model := NewModel("cfg", 1)
	adapter := NewEventAdapterWithModel(model)

	start := &events.Event{
		Type:      events.EventType("arena.run.started"),
		RunID:     "run-1",
		Timestamp: time.Now(),
		Data: events.CustomEventData{
			Data: map[string]interface{}{
				"scenario": "sc-1",
				"provider": "prov-1",
				"region":   "us-east",
			},
		},
	}
	complete := &events.Event{
		Type:      events.EventType("arena.run.completed"),
		RunID:     "run-1",
		Timestamp: time.Now(),
		Data: events.CustomEventData{
			Data: map[string]interface{}{
				"duration": time.Second,
				"cost":     0.1,
			},
		},
	}

	adapter.HandleEvent(start)
	adapter.HandleEvent(complete)

	if len(model.activeRuns) == 0 {
		t.Fatalf("expected active run to be created from events")
	}
	if model.completedCount == 0 {
		t.Fatalf("expected run completion to increment completed count")
	}
}

func TestEventAdapterLogsProviderEvents(t *testing.T) {
	t.Parallel()

	model := NewModel("cfg", 1)
	adapter := NewEventAdapterWithModel(model)

	adapter.HandleEvent(&events.Event{
		Type:      events.EventProviderCallStarted,
		Timestamp: time.Now(),
		Data: events.ProviderCallStartedData{
			Provider: "prov",
			Model:    "model",
		},
	})

	if len(model.logs) == 0 {
		t.Fatalf("expected logs to receive provider event")
	}
}

func TestEventAdapter_AllEventTypes(t *testing.T) {
	t.Parallel()

	adapter := NewEventAdapter(nil)

	testCases := []struct {
		name      string
		eventType events.EventType
		data      events.EventData
	}{
		{
			name:      "pipeline started",
			eventType: events.EventPipelineStarted,
			data:      &events.PipelineStartedData{MiddlewareCount: 5},
		},
		{
			name:      "pipeline completed",
			eventType: events.EventPipelineCompleted,
			data:      &events.PipelineCompletedData{Duration: time.Second},
		},
		{
			name:      "pipeline failed",
			eventType: events.EventPipelineFailed,
			data:      &events.PipelineFailedData{Error: nil},
		},
		{
			name:      "middleware started",
			eventType: events.EventMiddlewareStarted,
			data:      &events.MiddlewareStartedData{Name: "test"},
		},
		{
			name:      "middleware completed",
			eventType: events.EventMiddlewareCompleted,
			data:      &events.MiddlewareCompletedData{Name: "test"},
		},
		{
			name:      "middleware failed",
			eventType: events.EventMiddlewareFailed,
			data:      &events.MiddlewareFailedData{Name: "test"},
		},
		{
			name:      "provider call started",
			eventType: events.EventProviderCallStarted,
			data:      &events.ProviderCallStartedData{Provider: "test"},
		},
		{
			name:      "provider call completed",
			eventType: events.EventProviderCallCompleted,
			data:      &events.ProviderCallCompletedData{Provider: "test"},
		},
		{
			name:      "provider call failed",
			eventType: events.EventProviderCallFailed,
			data:      &events.ProviderCallFailedData{Provider: "test"},
		},
		{
			name:      "tool call started",
			eventType: events.EventToolCallStarted,
			data:      &events.ToolCallStartedData{ToolName: "test"},
		},
		{
			name:      "tool call completed",
			eventType: events.EventToolCallCompleted,
			data:      &events.ToolCallCompletedData{ToolName: "test"},
		},
		{
			name:      "tool call failed",
			eventType: events.EventToolCallFailed,
			data:      &events.ToolCallFailedData{ToolName: "test"},
		},
		{
			name:      "validation started",
			eventType: events.EventValidationStarted,
			data:      &events.ValidationStartedData{ValidatorName: "test"},
		},
		{
			name:      "validation passed",
			eventType: events.EventValidationPassed,
			data:      &events.ValidationPassedData{ValidatorName: "test"},
		},
		{
			name:      "validation failed",
			eventType: events.EventValidationFailed,
			data:      &events.ValidationFailedData{ValidatorName: "test"},
		},
		{
			name:      "context built",
			eventType: events.EventContextBuilt,
			data:      &events.ContextBuiltData{MessageCount: 5},
		},
		{
			name:      "token budget exceeded",
			eventType: events.EventTokenBudgetExceeded,
			data:      &events.TokenBudgetExceededData{Budget: 1000},
		},
		{
			name:      "state loaded",
			eventType: events.EventStateLoaded,
			data:      &events.StateLoadedData{ConversationID: "conv-1"},
		},
		{
			name:      "state saved",
			eventType: events.EventStateSaved,
			data:      &events.StateSavedData{ConversationID: "conv-1"},
		},
		{
			name:      "stream interrupted",
			eventType: events.EventStreamInterrupted,
			data:      &events.StreamInterruptedData{Reason: "error"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			evt := &events.Event{
				Type:      tc.eventType,
				Timestamp: time.Now(),
				Data:      tc.data,
			}

			// Should not panic
			adapter.HandleEvent(evt)
		})
	}
}
