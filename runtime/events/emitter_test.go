package events

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestEmitterPublishesSharedContext(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-1", "session-1", "conv-1")

	var got *Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EventPipelineStarted, func(e *Event) {
		got = e
		wg.Done()
	})

	emitter.PipelineStarted(3)

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for pipeline started event")
	}

	if got.RunID != "run-1" || got.SessionID != "session-1" || got.ConversationID != "conv-1" {
		t.Fatalf("unexpected context: %+v", got)
	}

	data, ok := got.Data.(PipelineStartedData)
	if !ok {
		t.Fatalf("unexpected data type: %T", got.Data)
	}

	if data.MiddlewareCount != 3 {
		t.Fatalf("unexpected middleware count: %d", data.MiddlewareCount)
	}
}

func TestEmitterPublishesVariousEvents(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	emitter := NewEmitter(bus, "run-2", "session-2", "conv-2")

	var seen []EventType
	var mu sync.Mutex
	var wg sync.WaitGroup

	bus.SubscribeAll(func(e *Event) {
		mu.Lock()
		seen = append(seen, e.Type)
		mu.Unlock()
		wg.Done()
	})

	tests := []func(){
		func() { emitter.PipelineCompleted(time.Second, 1.23, 10, 20, 1) },
		func() { emitter.PipelineFailed(errors.New("boom"), time.Second) },
		func() { emitter.MiddlewareStarted("mw", 0) },
		func() { emitter.MiddlewareCompleted("mw", 0, time.Millisecond) },
		func() { emitter.MiddlewareFailed("mw", 0, errors.New("oops"), time.Millisecond) },
		func() { emitter.ProviderCallStarted("provider", "model", 2, 1) },
		func() {
			emitter.ProviderCallCompleted(&ProviderCallCompletedData{
				Provider:      "provider",
				Model:         "model",
				Duration:      time.Millisecond,
				InputTokens:   5,
				OutputTokens:  6,
				CachedTokens:  0,
				Cost:          0.1,
				FinishReason:  "stop",
				ToolCallCount: 0,
			})
		},
		func() { emitter.ProviderCallFailed("provider", "model", errors.New("fail"), time.Millisecond) },
		func() { emitter.ToolCallStarted("tool", "call", map[string]interface{}{"k": "v"}) },
		func() { emitter.ToolCallCompleted("tool", "call", time.Millisecond, "success") },
		func() { emitter.ToolCallFailed("tool", "call", errors.New("fail"), time.Millisecond) },
		func() { emitter.ValidationStarted("validator", "input") },
		func() { emitter.ValidationPassed("validator", "input", time.Millisecond) },
		func() {
			emitter.ValidationFailed("validator", "input", errors.New("fail"), time.Millisecond, []string{"x"})
		},
		func() { emitter.ContextBuilt(1, 2, 3, false) },
		func() { emitter.TokenBudgetExceeded(5, 3, 2) },
		func() { emitter.StateLoaded("conv", 1) },
		func() { emitter.StateSaved("conv", 1) },
		func() { emitter.StreamInterrupted("reason") },
		func() {
			emitter.EmitCustom(EventType("middleware.custom.event"), "mw", "custom", map[string]interface{}{"a": 1}, "msg")
		},
	}

	wg.Add(len(tests))
	for _, fn := range tests {
		fn()
	}

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatalf("timed out waiting for %d events, saw %d", len(tests), len(seen))
	}

	if len(seen) != len(tests) {
		t.Fatalf("expected %d events, got %d", len(tests), len(seen))
	}
}

func TestEmitterHandlesNilBus(t *testing.T) {
	t.Parallel()

	emitter := NewEmitter(nil, "run", "session", "conv")
	// Should not panic even without a bus.
	emitter.PipelineStarted(1)
}
