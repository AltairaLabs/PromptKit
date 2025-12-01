package events

import (
	"sync"
	"testing"
	"time"
)

func TestEventBusPublishesToSpecificAndGlobalListeners(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	event := &Event{Type: EventPipelineStarted, Data: PipelineStartedData{MiddlewareCount: 1}}

	var mu sync.Mutex
	var received []EventType
	var wg sync.WaitGroup
	wg.Add(2)

	bus.Subscribe(EventPipelineStarted, func(e *Event) {
		mu.Lock()
		received = append(received, e.Type)
		mu.Unlock()
		wg.Done()
	})

	bus.SubscribeAll(func(e *Event) {
		mu.Lock()
		received = append(received, e.Type)
		mu.Unlock()
		wg.Done()
	})

	bus.Publish(event)

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for listeners")
	}

	if len(received) != 2 {
		t.Fatalf("expected 2 events, got %d", len(received))
	}
}

func TestEventBusRecoversFromPanic(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	event := &Event{Type: EventMiddlewareFailed}

	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EventMiddlewareFailed, func(*Event) {
		panic("listener panic")
	})

	// This listener should still fire even if another panics.
	bus.Subscribe(EventMiddlewareFailed, func(*Event) {
		wg.Done()
	})

	bus.Publish(event)

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("listener after panic did not fire")
	}
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
