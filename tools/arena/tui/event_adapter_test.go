package tui

import (
	"fmt"
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

func TestEventAdapter_Subscribe(t *testing.T) {
	t.Parallel()

	bus := events.NewEventBus()
	model := NewModel("cfg", 1)
	adapter := NewEventAdapterWithModel(model)

	// Subscribe adapter to bus
	adapter.Subscribe(bus)

	// Emit an event
	bus.Publish(&events.Event{
		Type:      events.EventProviderCallStarted,
		Timestamp: time.Now(),
		Data:      events.ProviderCallStartedData{Provider: "test-provider", Model: "test-model"},
	})

	// Give the event a moment to process
	time.Sleep(10 * time.Millisecond)

	// Verify the event was received by checking logs
	model.mu.Lock()
	logCount := len(model.logs)
	model.mu.Unlock()
	if logCount == 0 {
		t.Error("expected adapter to receive event after subscription")
	}
}

func TestEventAdapter_SubscribeNilBus(t *testing.T) {
	t.Parallel()

	adapter := NewEventAdapter(nil)
	// Should not panic
	adapter.Subscribe(nil)
}

func TestEventAdapter_HelperFunctions(t *testing.T) {
	t.Parallel()

	t.Run("readError with error value", func(t *testing.T) {
		testErr := fmt.Errorf("validation failed")
		data := events.CustomEventData{
			Data: map[string]interface{}{
				"error": testErr,
			},
		}
		err := readError(data, "error")
		if err == nil {
			t.Error("expected non-nil error")
		}
	})

	t.Run("readError with string value", func(t *testing.T) {
		data := events.CustomEventData{
			Data: map[string]interface{}{
				"error": "something went wrong",
			},
		}
		err := readError(data, "error")
		if err == nil {
			t.Error("expected non-nil error")
		}
		if err.Error() != "something went wrong" {
			t.Errorf("expected error message 'something went wrong', got '%s'", err.Error())
		}
	})

	t.Run("readError with nil data", func(t *testing.T) {
		data := events.CustomEventData{
			Data: map[string]interface{}{},
		}
		err := readError(data, "error")
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})

	t.Run("readInt", func(t *testing.T) {
		data := events.CustomEventData{
			Data: map[string]interface{}{
				"turn_index": 5,
			},
		}
		val := readInt(data, "turn_index")
		if val != 5 {
			t.Errorf("expected 5, got %d", val)
		}
	})

	t.Run("readInt missing key", func(t *testing.T) {
		data := events.CustomEventData{
			Data: map[string]interface{}{},
		}
		val := readInt(data, "missing")
		if val != 0 {
			t.Errorf("expected 0 for missing key, got %d", val)
		}
	})

	t.Run("ternary true", func(t *testing.T) {
		result := ternary(true, "yes", "no")
		if result != "yes" {
			t.Errorf("expected 'yes', got '%s'", result)
		}
	})

	t.Run("ternary false", func(t *testing.T) {
		result := ternary(false, "yes", "no")
		if result != "no" {
			t.Errorf("expected 'no', got '%s'", result)
		}
	})
}

func TestEventAdapter_ArenaCustomEvents(t *testing.T) {
	t.Parallel()

	model := NewModel("cfg", 1)
	adapter := NewEventAdapterWithModel(model)

	t.Run("arena run failed", func(t *testing.T) {
		evt := &events.Event{
			Type:      events.EventType("arena.run.failed"),
			RunID:     "run-fail",
			Timestamp: time.Now(),
			Data: events.CustomEventData{
				Data: map[string]interface{}{
					"error": "test failure",
				},
			},
		}
		adapter.HandleEvent(evt)
		if model.failedCount == 0 {
			t.Error("expected failed count to increment")
		}
	})

	t.Run("arena turn events", func(t *testing.T) {
		started := &events.Event{
			Type:      events.EventType("arena.turn.started"),
			RunID:     "run-1",
			Timestamp: time.Now(),
			Data: events.CustomEventData{
				Data: map[string]interface{}{
					"turn_index": 1,
					"role":       "user",
					"scenario":   "test-scenario",
				},
			},
		}
		adapter.HandleEvent(started)

		completed := &events.Event{
			Type:      events.EventType("arena.turn.completed"),
			RunID:     "run-1",
			Timestamp: time.Now(),
			Data: events.CustomEventData{
				Data: map[string]interface{}{
					"turn_index": 1,
					"role":       "user",
					"scenario":   "test-scenario",
				},
			},
		}
		// Should not panic
		adapter.HandleEvent(completed)
	})

	t.Run("unknown event type ignored", func(t *testing.T) {
		evt := &events.Event{
			Type:      events.EventType("unknown.event"),
			Timestamp: time.Now(),
			Data:      events.CustomEventData{Data: map[string]interface{}{}},
		}
		// Should not panic and should return early
		adapter.HandleEvent(evt)
	})
}

func TestEventAdapter_SendMethods(t *testing.T) {
	t.Parallel()

	t.Run("send with program", func(t *testing.T) {
		adapter := NewEventAdapter(nil)
		// Should not panic with nil program
		adapter.send(LogMsg{Message: "test"})
	})

	t.Run("send with model", func(t *testing.T) {
		model := NewModel("cfg", 1)
		adapter := NewEventAdapterWithModel(model)
		adapter.send(LogMsg{Level: "INFO", Message: "test message"})
		if len(model.logs) == 0 {
			t.Error("expected log message to be added to model")
		}
	})

	t.Run("send with neither", func(t *testing.T) {
		adapter := &EventAdapter{}
		// Should not panic
		adapter.send(LogMsg{Message: "test"})
	})
}

func TestEventAdapter_EventErrorExtraction(t *testing.T) {
	t.Parallel()

	testErr := fmt.Errorf("test error")

	testCases := []struct {
		name     string
		data     events.EventData
		expected error
	}{
		{
			name:     "ProviderCallFailedData",
			data:     events.ProviderCallFailedData{Error: testErr},
			expected: testErr,
		},
		{
			name:     "MiddlewareFailedData",
			data:     events.MiddlewareFailedData{Error: testErr},
			expected: testErr,
		},
		{
			name:     "ToolCallFailedData",
			data:     events.ToolCallFailedData{Error: testErr},
			expected: testErr,
		},
		{
			name:     "ValidationFailedData",
			data:     events.ValidationFailedData{Error: testErr},
			expected: testErr,
		},
		{
			name:     "other data type",
			data:     events.PipelineStartedData{},
			expected: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			evt := &events.Event{
				Type: events.EventPipelineFailed,
				Data: tc.data,
			}
			err := eventError(evt)
			if err != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, err)
			}
		})
	}
}

func TestEventAdapter_NameExtraction(t *testing.T) {
	t.Parallel()

	t.Run("providerName and providerModel", func(t *testing.T) {
		testCases := []struct {
			data          events.EventData
			expectedName  string
			expectedModel string
		}{
			{
				data:          events.ProviderCallStartedData{Provider: "openai", Model: "gpt-4"},
				expectedName:  "openai",
				expectedModel: "gpt-4",
			},
			{
				data:          events.ProviderCallCompletedData{Provider: "anthropic", Model: "claude-3"},
				expectedName:  "anthropic",
				expectedModel: "claude-3",
			},
			{
				data:          events.ProviderCallFailedData{Provider: "mock", Model: "test"},
				expectedName:  "mock",
				expectedModel: "test",
			},
			{
				data:          events.PipelineStartedData{},
				expectedName:  "",
				expectedModel: "",
			},
		}

		for _, tc := range testCases {
			evt := &events.Event{Data: tc.data}
			name := providerName(evt)
			model := providerModel(evt)
			if name != tc.expectedName {
				t.Errorf("expected provider name '%s', got '%s'", tc.expectedName, name)
			}
			if model != tc.expectedModel {
				t.Errorf("expected provider model '%s', got '%s'", tc.expectedModel, model)
			}
		}
	})

	t.Run("middlewareName", func(t *testing.T) {
		testCases := []struct {
			data         events.EventData
			expectedName string
		}{
			{data: events.MiddlewareStartedData{Name: "auth"}, expectedName: "auth"},
			{data: events.MiddlewareCompletedData{Name: "logging"}, expectedName: "logging"},
			{data: events.MiddlewareFailedData{Name: "validation"}, expectedName: "validation"},
			{data: events.PipelineStartedData{}, expectedName: ""},
		}

		for _, tc := range testCases {
			evt := &events.Event{Data: tc.data}
			name := middlewareName(evt)
			if name != tc.expectedName {
				t.Errorf("expected middleware name '%s', got '%s'", tc.expectedName, name)
			}
		}
	})

	t.Run("toolName", func(t *testing.T) {
		testCases := []struct {
			data         events.EventData
			expectedName string
		}{
			{data: events.ToolCallStartedData{ToolName: "calculator"}, expectedName: "calculator"},
			{data: events.ToolCallCompletedData{ToolName: "search"}, expectedName: "search"},
			{data: events.ToolCallFailedData{ToolName: "database"}, expectedName: "database"},
			{data: events.PipelineStartedData{}, expectedName: ""},
		}

		for _, tc := range testCases {
			evt := &events.Event{Data: tc.data}
			name := toolName(evt)
			if name != tc.expectedName {
				t.Errorf("expected tool name '%s', got '%s'", tc.expectedName, name)
			}
		}
	})

	t.Run("validationName", func(t *testing.T) {
		testCases := []struct {
			data         events.EventData
			expectedName string
		}{
			{data: events.ValidationStartedData{ValidatorName: "schema"}, expectedName: "schema"},
			{data: events.ValidationPassedData{ValidatorName: "format"}, expectedName: "format"},
			{data: events.ValidationFailedData{ValidatorName: "content"}, expectedName: "content"},
			{data: events.PipelineStartedData{}, expectedName: ""},
		}

		for _, tc := range testCases {
			evt := &events.Event{Data: tc.data}
			name := validationName(evt)
			if name != tc.expectedName {
				t.Errorf("expected validation name '%s', got '%s'", tc.expectedName, name)
			}
		}
	})
}
