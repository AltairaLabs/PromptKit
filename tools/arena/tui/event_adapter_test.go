package tui

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/logging"
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
		adapter.send(logging.Msg{Message: "test"})
	})

	t.Run("send with model", func(t *testing.T) {
		model := NewModel("cfg", 1)
		adapter := NewEventAdapterWithModel(model)
		adapter.send(logging.Msg{Level: "INFO", Message: "test message"})
		if len(model.logs) == 0 {
			t.Error("expected log message to be added to model")
		}
	})

	t.Run("send with neither", func(t *testing.T) {
		adapter := &EventAdapter{}
		// Should not panic
		adapter.send(logging.Msg{Message: "test"})
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

// Tests for handleMessageCreated
func TestEventAdapter_HandleMessageCreated(t *testing.T) {
	t.Parallel()

	adapter := NewEventAdapter(nil)

	t.Run("basic message", func(t *testing.T) {
		evt := &events.Event{
			Type:           events.EventMessageCreated,
			ConversationID: "conv-1",
			Timestamp:      time.Now(),
			Data: events.MessageCreatedData{
				Role:    "assistant",
				Content: "Hello, how can I help?",
				Index:   0,
			},
		}

		msg := adapter.handleMessageCreated(evt)
		require.NotNil(t, msg)

		createdMsg, ok := msg.(MessageCreatedMsg)
		require.True(t, ok)
		assert.Equal(t, "conv-1", createdMsg.ConversationID)
		assert.Equal(t, "assistant", createdMsg.Role)
		assert.Equal(t, "Hello, how can I help?", createdMsg.Content)
		assert.Equal(t, 0, createdMsg.Index)
	})

	t.Run("with tool calls", func(t *testing.T) {
		evt := &events.Event{
			Type:           events.EventMessageCreated,
			ConversationID: "conv-1",
			Timestamp:      time.Now(),
			Data: events.MessageCreatedData{
				Role:    "assistant",
				Content: "",
				Index:   1,
				ToolCalls: []events.MessageToolCall{
					{ID: "call-1", Name: "get_weather", Args: `{"city": "NYC"}`},
					{ID: "call-2", Name: "get_time", Args: `{"zone": "EST"}`},
				},
			},
		}

		msg := adapter.handleMessageCreated(evt)
		require.NotNil(t, msg)

		createdMsg, ok := msg.(MessageCreatedMsg)
		require.True(t, ok)
		require.Len(t, createdMsg.ToolCalls, 2)
		assert.Equal(t, "get_weather", createdMsg.ToolCalls[0].Name)
		assert.Equal(t, "get_time", createdMsg.ToolCalls[1].Name)
	})

	t.Run("with tool result", func(t *testing.T) {
		evt := &events.Event{
			Type:           events.EventMessageCreated,
			ConversationID: "conv-1",
			Timestamp:      time.Now(),
			Data: events.MessageCreatedData{
				Role:    "tool",
				Content: "",
				Index:   2,
				ToolResult: &events.MessageToolResult{
					ID:        "call-1",
					Name:      "get_weather",
					Content:   `{"temp": 72, "conditions": "sunny"}`,
					LatencyMs: 150,
				},
			},
		}

		msg := adapter.handleMessageCreated(evt)
		require.NotNil(t, msg)

		createdMsg, ok := msg.(MessageCreatedMsg)
		require.True(t, ok)
		require.NotNil(t, createdMsg.ToolResult)
		assert.Equal(t, "get_weather", createdMsg.ToolResult.Name)
		assert.Equal(t, int64(150), createdMsg.ToolResult.LatencyMs)
	})

	t.Run("with tool result error", func(t *testing.T) {
		evt := &events.Event{
			Type:           events.EventMessageCreated,
			ConversationID: "conv-1",
			Timestamp:      time.Now(),
			Data: events.MessageCreatedData{
				Role:    "tool",
				Content: "",
				Index:   2,
				ToolResult: &events.MessageToolResult{
					ID:    "call-1",
					Name:  "get_weather",
					Error: "API rate limit exceeded",
				},
			},
		}

		msg := adapter.handleMessageCreated(evt)
		require.NotNil(t, msg)

		createdMsg, ok := msg.(MessageCreatedMsg)
		require.True(t, ok)
		require.NotNil(t, createdMsg.ToolResult)
		assert.Equal(t, "API rate limit exceeded", createdMsg.ToolResult.Error)
	})

	t.Run("wrong data type returns nil", func(t *testing.T) {
		evt := &events.Event{
			Type:           events.EventMessageCreated,
			ConversationID: "conv-1",
			Timestamp:      time.Now(),
			Data:           events.PipelineStartedData{}, // Wrong type
		}

		msg := adapter.handleMessageCreated(evt)
		assert.Nil(t, msg)
	})
}

// Tests for handleMessageUpdated
func TestEventAdapter_HandleMessageUpdated(t *testing.T) {
	t.Parallel()

	adapter := NewEventAdapter(nil)

	t.Run("basic update", func(t *testing.T) {
		evt := &events.Event{
			Type:           events.EventMessageUpdated,
			ConversationID: "conv-1",
			Timestamp:      time.Now(),
			Data: events.MessageUpdatedData{
				Index:        0,
				LatencyMs:    500,
				InputTokens:  100,
				OutputTokens: 50,
				TotalCost:    0.015,
			},
		}

		msg := adapter.handleMessageUpdated(evt)
		require.NotNil(t, msg)

		updatedMsg, ok := msg.(MessageUpdatedMsg)
		require.True(t, ok)
		assert.Equal(t, "conv-1", updatedMsg.ConversationID)
		assert.Equal(t, 0, updatedMsg.Index)
		assert.Equal(t, int64(500), updatedMsg.LatencyMs)
		assert.Equal(t, 100, updatedMsg.InputTokens)
		assert.Equal(t, 50, updatedMsg.OutputTokens)
		assert.Equal(t, 0.015, updatedMsg.TotalCost)
	})

	t.Run("wrong data type returns nil", func(t *testing.T) {
		evt := &events.Event{
			Type:           events.EventMessageUpdated,
			ConversationID: "conv-1",
			Timestamp:      time.Now(),
			Data:           events.PipelineStartedData{}, // Wrong type
		}

		msg := adapter.handleMessageUpdated(evt)
		assert.Nil(t, msg)
	})
}

// Tests for handleConversationStarted
func TestEventAdapter_HandleConversationStarted(t *testing.T) {
	t.Parallel()

	adapter := NewEventAdapter(nil)

	t.Run("basic conversation started", func(t *testing.T) {
		evt := &events.Event{
			Type:           events.EventConversationStarted,
			ConversationID: "conv-1",
			Timestamp:      time.Now(),
			Data: events.ConversationStartedData{
				SystemPrompt: "You are a helpful AI assistant.",
			},
		}

		msg := adapter.handleConversationStarted(evt)
		require.NotNil(t, msg)

		startedMsg, ok := msg.(ConversationStartedMsg)
		require.True(t, ok)
		assert.Equal(t, "conv-1", startedMsg.ConversationID)
		assert.Equal(t, "You are a helpful AI assistant.", startedMsg.SystemPrompt)
	})

	t.Run("empty system prompt", func(t *testing.T) {
		evt := &events.Event{
			Type:           events.EventConversationStarted,
			ConversationID: "conv-2",
			Timestamp:      time.Now(),
			Data: events.ConversationStartedData{
				SystemPrompt: "",
			},
		}

		msg := adapter.handleConversationStarted(evt)
		require.NotNil(t, msg)

		startedMsg, ok := msg.(ConversationStartedMsg)
		require.True(t, ok)
		assert.Equal(t, "", startedMsg.SystemPrompt)
	})

	t.Run("wrong data type returns nil", func(t *testing.T) {
		evt := &events.Event{
			Type:           events.EventConversationStarted,
			ConversationID: "conv-1",
			Timestamp:      time.Now(),
			Data:           events.PipelineStartedData{}, // Wrong type
		}

		msg := adapter.handleConversationStarted(evt)
		assert.Nil(t, msg)
	})
}

// Tests for mapEvent with new event types
func TestEventAdapter_MapEvent_NewEventTypes(t *testing.T) {
	t.Parallel()

	adapter := NewEventAdapter(nil)

	t.Run("message created event", func(t *testing.T) {
		evt := &events.Event{
			Type:           events.EventMessageCreated,
			ConversationID: "conv-1",
			Timestamp:      time.Now(),
			Data: events.MessageCreatedData{
				Role:    "assistant",
				Content: "Hello",
				Index:   0,
			},
		}

		msg := adapter.mapEvent(evt)
		require.NotNil(t, msg)
		_, ok := msg.(MessageCreatedMsg)
		assert.True(t, ok)
	})

	t.Run("message updated event", func(t *testing.T) {
		evt := &events.Event{
			Type:           events.EventMessageUpdated,
			ConversationID: "conv-1",
			Timestamp:      time.Now(),
			Data: events.MessageUpdatedData{
				Index:        0,
				LatencyMs:    100,
				InputTokens:  50,
				OutputTokens: 25,
				TotalCost:    0.01,
			},
		}

		msg := adapter.mapEvent(evt)
		require.NotNil(t, msg)
		_, ok := msg.(MessageUpdatedMsg)
		assert.True(t, ok)
	})

	t.Run("conversation started event", func(t *testing.T) {
		evt := &events.Event{
			Type:           events.EventConversationStarted,
			ConversationID: "conv-1",
			Timestamp:      time.Now(),
			Data: events.ConversationStartedData{
				SystemPrompt: "You are helpful.",
			},
		}

		msg := adapter.mapEvent(evt)
		require.NotNil(t, msg)
		_, ok := msg.(ConversationStartedMsg)
		assert.True(t, ok)
	})
}

// Test HandleEvent integration with new event types
func TestEventAdapter_HandleEvent_NewEventTypes(t *testing.T) {
	t.Parallel()

	model := NewModel("cfg", 1)
	adapter := NewEventAdapterWithModel(model)

	t.Run("message created flows to model", func(t *testing.T) {
		model.conversationMessages = make(map[string][]types.Message)

		evt := &events.Event{
			Type:           events.EventMessageCreated,
			ConversationID: "run-1",
			Timestamp:      time.Now(),
			Data: events.MessageCreatedData{
				Role:    "user",
				Content: "Hello",
				Index:   0,
			},
		}

		adapter.HandleEvent(evt)

		// Message should be cached in model
		model.mu.Lock()
		defer model.mu.Unlock()
		assert.Len(t, model.conversationMessages["run-1"], 1)
	})

	t.Run("conversation started flows to model", func(t *testing.T) {
		model.systemPrompts = make(map[string]string)

		evt := &events.Event{
			Type:           events.EventConversationStarted,
			ConversationID: "run-2",
			Timestamp:      time.Now(),
			Data: events.ConversationStartedData{
				SystemPrompt: "You are a helpful assistant.",
			},
		}

		adapter.HandleEvent(evt)

		// System prompt should be cached in model
		model.mu.Lock()
		defer model.mu.Unlock()
		assert.Equal(t, "You are a helpful assistant.", model.systemPrompts["run-2"])
	})
}
