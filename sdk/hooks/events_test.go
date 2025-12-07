package hooks

import (
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEventSource implements EventSource for testing
type mockEventSource struct {
	bus *events.EventBus
}

func (m *mockEventSource) EventBus() *events.EventBus {
	return m.bus
}

func newMockSource() *mockEventSource {
	return &mockEventSource{bus: events.NewEventBus()}
}

func TestOnEvent(t *testing.T) {
	t.Run("subscribes to all events", func(t *testing.T) {
		source := newMockSource()
		var received []*events.Event
		var mu sync.Mutex

		OnEvent(source, func(e *events.Event) {
			mu.Lock()
			received = append(received, e)
			mu.Unlock()
		})

		// Publish different event types
		source.bus.Publish(&events.Event{Type: events.EventToolCallStarted})
		source.bus.Publish(&events.Event{Type: events.EventValidationFailed})

		// Wait for async delivery
		time.Sleep(50 * time.Millisecond)

		mu.Lock()
		assert.Len(t, received, 2)
		mu.Unlock()
	})

	t.Run("handles nil event bus", func(t *testing.T) {
		source := &mockEventSource{bus: nil}
		// Should not panic
		OnEvent(source, func(e *events.Event) {})
	})
}

func TestOn(t *testing.T) {
	t.Run("subscribes to specific event type", func(t *testing.T) {
		source := newMockSource()
		var received []*events.Event
		var mu sync.Mutex

		On(source, events.EventToolCallStarted, func(e *events.Event) {
			mu.Lock()
			received = append(received, e)
			mu.Unlock()
		})

		// Publish matching and non-matching events
		source.bus.Publish(&events.Event{Type: events.EventToolCallStarted})
		source.bus.Publish(&events.Event{Type: events.EventValidationFailed})
		source.bus.Publish(&events.Event{Type: events.EventToolCallStarted})

		// Wait for async delivery
		time.Sleep(50 * time.Millisecond)

		mu.Lock()
		assert.Len(t, received, 2)
		mu.Unlock()
	})

	t.Run("handles nil event bus", func(t *testing.T) {
		source := &mockEventSource{bus: nil}
		// Should not panic
		On(source, events.EventToolCallStarted, func(e *events.Event) {})
	})
}

func TestOnToolCall(t *testing.T) {
	t.Run("receives tool call data", func(t *testing.T) {
		source := newMockSource()
		var receivedName string
		var receivedArgs map[string]any
		var mu sync.Mutex

		OnToolCall(source, func(name string, args map[string]any) {
			mu.Lock()
			receivedName = name
			receivedArgs = args
			mu.Unlock()
		})

		source.bus.Publish(&events.Event{
			Type: events.EventToolCallStarted,
			Data: &events.ToolCallStartedData{
				ToolName: "get_weather",
				Args:     map[string]any{"city": "NYC"},
			},
		})

		time.Sleep(50 * time.Millisecond)

		mu.Lock()
		assert.Equal(t, "get_weather", receivedName)
		assert.Equal(t, "NYC", receivedArgs["city"])
		mu.Unlock()
	})
}

func TestOnValidationFailed(t *testing.T) {
	t.Run("receives validation failure data", func(t *testing.T) {
		source := newMockSource()
		var receivedValidator string
		var receivedErr error
		var mu sync.Mutex

		OnValidationFailed(source, func(validator string, err error) {
			mu.Lock()
			receivedValidator = validator
			receivedErr = err
			mu.Unlock()
		})

		testErr := assert.AnError
		source.bus.Publish(&events.Event{
			Type: events.EventValidationFailed,
			Data: &events.ValidationFailedData{
				ValidatorName: "profanity_filter",
				Error:         testErr,
			},
		})

		time.Sleep(50 * time.Millisecond)

		mu.Lock()
		assert.Equal(t, "profanity_filter", receivedValidator)
		assert.Equal(t, testErr, receivedErr)
		mu.Unlock()
	})
}

func TestOnProviderCall(t *testing.T) {
	t.Run("receives provider call data", func(t *testing.T) {
		source := newMockSource()
		var receivedModel string
		var receivedIn, receivedOut int
		var receivedCost float64
		var mu sync.Mutex

		OnProviderCall(source, func(model string, in, out int, cost float64) {
			mu.Lock()
			receivedModel = model
			receivedIn = in
			receivedOut = out
			receivedCost = cost
			mu.Unlock()
		})

		source.bus.Publish(&events.Event{
			Type: events.EventProviderCallCompleted,
			Data: &events.ProviderCallCompletedData{
				Model:        "gpt-4o",
				InputTokens:  100,
				OutputTokens: 50,
				Cost:         0.0045,
			},
		})

		time.Sleep(50 * time.Millisecond)

		mu.Lock()
		assert.Equal(t, "gpt-4o", receivedModel)
		assert.Equal(t, 100, receivedIn)
		assert.Equal(t, 50, receivedOut)
		assert.Equal(t, 0.0045, receivedCost)
		mu.Unlock()
	})
}

func TestOnPipelineComplete(t *testing.T) {
	t.Run("receives pipeline completion data", func(t *testing.T) {
		source := newMockSource()
		var receivedCost float64
		var receivedIn, receivedOut int
		var mu sync.Mutex

		OnPipelineComplete(source, func(cost float64, in, out int) {
			mu.Lock()
			receivedCost = cost
			receivedIn = in
			receivedOut = out
			mu.Unlock()
		})

		source.bus.Publish(&events.Event{
			Type: events.EventPipelineCompleted,
			Data: &events.PipelineCompletedData{
				TotalCost:    0.01,
				InputTokens:  200,
				OutputTokens: 100,
			},
		})

		time.Sleep(50 * time.Millisecond)

		mu.Lock()
		assert.Equal(t, 0.01, receivedCost)
		assert.Equal(t, 200, receivedIn)
		assert.Equal(t, 100, receivedOut)
		mu.Unlock()
	})
}

func TestEventSourceInterface(t *testing.T) {
	t.Run("mockEventSource implements EventSource", func(t *testing.T) {
		var _ EventSource = (*mockEventSource)(nil)
	})
}

func TestMultipleSubscribers(t *testing.T) {
	source := newMockSource()
	var count1, count2 int
	var mu sync.Mutex

	OnToolCall(source, func(name string, args map[string]any) {
		mu.Lock()
		count1++
		mu.Unlock()
	})

	OnToolCall(source, func(name string, args map[string]any) {
		mu.Lock()
		count2++
		mu.Unlock()
	})

	source.bus.Publish(&events.Event{
		Type: events.EventToolCallStarted,
		Data: &events.ToolCallStartedData{ToolName: "test"},
	})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	require.Equal(t, 1, count1)
	require.Equal(t, 1, count2)
	mu.Unlock()
}
