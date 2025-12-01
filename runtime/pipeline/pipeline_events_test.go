package pipeline

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

type stubMiddleware struct {
	err error
}

func (m *stubMiddleware) Process(ctx *ExecutionContext, next func() error) error {
	if m.err != nil {
		return m.err
	}
	return next()
}

func (m *stubMiddleware) StreamChunk(*ExecutionContext, *providers.StreamChunk) error {
	return nil
}

func TestPipelineEmitsLifecycleEvents(t *testing.T) {
	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run-123", "sess-123", "conv-123")

	var seen []events.EventType
	var mu sync.Mutex
	var wg sync.WaitGroup

	expectedEvents := 4 // pipeline started, middleware started, middleware completed, pipeline completed
	wg.Add(expectedEvents)

	bus.SubscribeAll(func(e *events.Event) {
		mu.Lock()
		seen = append(seen, e.Type)
		mu.Unlock()
		wg.Done()
	})

	p := NewPipeline(&stubMiddleware{})
	_, err := p.ExecuteWithOptions(&ExecutionOptions{
		Context:      context.Background(),
		EventEmitter: emitter,
	}, "user", "hi")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatalf("timed out waiting for events, saw %d/%d", len(seen), expectedEvents)
	}

	verifyEvents(t, seen, map[events.EventType]int{
		events.EventPipelineStarted:     1,
		events.EventMiddlewareStarted:   1,
		events.EventMiddlewareCompleted: 1,
		events.EventPipelineCompleted:   1,
	})
}

func TestPipelineEmitsFailureEvents(t *testing.T) {
	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run-err", "sess-err", "conv-err")

	var seen []events.EventType
	var mu sync.Mutex
	var wg sync.WaitGroup

	expectedEvents := 4 // pipeline started, middleware started, middleware failed, pipeline failed
	wg.Add(expectedEvents)

	bus.SubscribeAll(func(e *events.Event) {
		mu.Lock()
		seen = append(seen, e.Type)
		mu.Unlock()
		wg.Done()
	})

	p := NewPipeline(&stubMiddleware{err: errors.New("middleware failure")})
	_, err := p.ExecuteWithOptions(&ExecutionOptions{
		Context:      context.Background(),
		EventEmitter: emitter,
	}, "user", "hi")

	if err == nil {
		t.Fatal("expected error from middleware")
	}

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatalf("timed out waiting for events, saw %d/%d", len(seen), expectedEvents)
	}

	verifyEvents(t, seen, map[events.EventType]int{
		events.EventPipelineStarted:   1,
		events.EventMiddlewareStarted: 1,
		events.EventMiddlewareFailed:  1,
		events.EventPipelineFailed:    1,
	})
}

func waitForWG(wg *sync.WaitGroup, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func verifyEvents(t *testing.T, seen []events.EventType, expected map[events.EventType]int) {
	t.Helper()
	counts := make(map[events.EventType]int)
	for _, eventType := range seen {
		counts[eventType]++
	}

	for eventType, expectedCount := range expected {
		if counts[eventType] != expectedCount {
			t.Fatalf("expected %d occurrences of %s, got %d", expectedCount, eventType, counts[eventType])
		}
	}
}
