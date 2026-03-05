package events

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEventBusPublishesToSpecificAndGlobalListeners(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	defer bus.Close()

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
	defer bus.Close()

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

func TestEventBusUnsubscribeSpecific(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	defer bus.Close()

	var count atomic.Int32
	var wg sync.WaitGroup

	unsub := bus.Subscribe(EventPipelineStarted, func(*Event) {
		count.Add(1)
		wg.Done()
	})

	// First publish should reach the listener.
	wg.Add(1)
	bus.Publish(&Event{Type: EventPipelineStarted})
	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for first event")
	}

	if got := count.Load(); got != 1 {
		t.Fatalf("expected count 1 after first publish, got %d", got)
	}

	// Unsubscribe and publish again -- listener should NOT fire.
	unsub()

	// Subscribe a sentinel listener to know when the second event is processed.
	var wg2 sync.WaitGroup
	wg2.Add(1)
	bus.Subscribe(EventPipelineStarted, func(*Event) {
		wg2.Done()
	})
	bus.Publish(&Event{Type: EventPipelineStarted})
	if !waitForWG(&wg2, 200*time.Millisecond) {
		t.Fatal("timed out waiting for sentinel")
	}

	if got := count.Load(); got != 1 {
		t.Fatalf("expected count still 1 after unsubscribe, got %d", got)
	}
}

func TestEventBusUnsubscribeAll(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	defer bus.Close()

	var count atomic.Int32
	var wg sync.WaitGroup

	unsub := bus.SubscribeAll(func(*Event) {
		count.Add(1)
		wg.Done()
	})

	wg.Add(1)
	bus.Publish(&Event{Type: EventPipelineStarted})
	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for first event")
	}

	if got := count.Load(); got != 1 {
		t.Fatalf("expected count 1 after first publish, got %d", got)
	}

	unsub()

	// Subscribe a sentinel to know when the second event is processed.
	var wg2 sync.WaitGroup
	wg2.Add(1)
	bus.Subscribe(EventPipelineStarted, func(*Event) {
		wg2.Done()
	})
	bus.Publish(&Event{Type: EventPipelineStarted})
	if !waitForWG(&wg2, 200*time.Millisecond) {
		t.Fatal("timed out waiting for sentinel")
	}

	if got := count.Load(); got != 1 {
		t.Fatalf("expected count still 1 after unsubscribe, got %d", got)
	}
}

func TestEventBusClose(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()

	var count atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EventPipelineStarted, func(*Event) {
		count.Add(1)
		wg.Done()
	})

	bus.Publish(&Event{Type: EventPipelineStarted})
	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for event before close")
	}

	bus.Close()

	// Publish after close should return false.
	if bus.Publish(&Event{Type: EventPipelineStarted}) {
		t.Fatal("expected Publish to return false after Close")
	}

	if got := count.Load(); got != 1 {
		t.Fatalf("expected count 1, got %d", got)
	}
}

func TestEventBusCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	bus.Close()
	bus.Close() // should not panic
}

func TestEventBusCustomPoolSize(t *testing.T) {
	t.Parallel()

	bus := NewEventBus(WithWorkerPoolSize(2), WithEventBufferSize(5))
	defer bus.Close()

	var count atomic.Int32
	var wg sync.WaitGroup
	wg.Add(3)

	bus.Subscribe(EventPipelineStarted, func(*Event) {
		count.Add(1)
		wg.Done()
	})

	for range 3 {
		bus.Publish(&Event{Type: EventPipelineStarted})
	}

	if !waitForWG(&wg, 500*time.Millisecond) {
		t.Fatal("timed out waiting for events with custom pool")
	}

	if got := count.Load(); got != 3 {
		t.Fatalf("expected count 3, got %d", got)
	}
}

func TestEventBusCloseDrainsEvents(t *testing.T) {
	t.Parallel()

	// Use a single worker so events are serialized.
	bus := NewEventBus(WithWorkerPoolSize(1), WithEventBufferSize(100))

	var count atomic.Int32

	bus.Subscribe(EventPipelineStarted, func(*Event) {
		count.Add(1)
	})

	for range 50 {
		bus.Publish(&Event{Type: EventPipelineStarted})
	}

	// Close should wait for all queued events to be processed.
	bus.Close()

	if got := count.Load(); got != 50 {
		t.Fatalf("expected all 50 events drained, got %d", got)
	}
}

func TestEventBusPublishReturnsTrueWhenBufferAvailable(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	defer bus.Close()

	ok := bus.Publish(&Event{Type: EventPipelineStarted})
	if !ok {
		t.Fatal("expected Publish to return true")
	}
}

func TestEventBusInvalidOptionValues(t *testing.T) {
	t.Parallel()

	// Zero or negative values should be ignored, keeping defaults.
	bus := NewEventBus(WithWorkerPoolSize(0), WithEventBufferSize(-1))
	defer bus.Close()

	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EventPipelineStarted, func(*Event) {
		wg.Done()
	})

	bus.Publish(&Event{Type: EventPipelineStarted})

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out -- bus with default options should work")
	}
}

func TestEventBusClear(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	defer bus.Close()

	var count atomic.Int32

	bus.Subscribe(EventPipelineStarted, func(*Event) {
		count.Add(1)
	})
	bus.SubscribeAll(func(*Event) {
		count.Add(1)
	})

	bus.Clear()

	// Publish and wait for it to pass through the worker.
	var wg sync.WaitGroup
	wg.Add(1)
	bus.Subscribe(EventPipelineCompleted, func(*Event) {
		wg.Done()
	})
	bus.Publish(&Event{Type: EventPipelineCompleted})
	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for sentinel after clear")
	}

	// The cleared listeners for EventPipelineStarted should not have fired.
	if got := count.Load(); got != 0 {
		t.Fatalf("expected cleared listeners to not fire, got count %d", got)
	}
}

func TestEventBusDroppedCountIncrementsOnFullBuffer(t *testing.T) {
	t.Parallel()

	// Use 1 worker with a tiny buffer. Block the worker with a slow listener
	// so events pile up and get dropped.
	bus := NewEventBus(WithWorkerPoolSize(1), WithEventBufferSize(1))
	defer bus.Close()

	blockCh := make(chan struct{})

	// This listener blocks the single worker until we signal it.
	bus.Subscribe(EventPipelineStarted, func(*Event) {
		<-blockCh
	})

	// First publish fills the worker (blocks on listener).
	bus.Publish(&Event{Type: EventPipelineStarted})
	// Give the worker a moment to pick up the event and block.
	time.Sleep(20 * time.Millisecond)

	// Second publish fills the 1-slot buffer.
	bus.Publish(&Event{Type: EventPipelineStarted})

	// Now the buffer is full; subsequent publishes should drop.
	for range 10 {
		bus.Publish(&Event{Type: EventPipelineStarted})
	}

	if got := bus.DroppedCount(); got != 10 {
		t.Fatalf("expected 10 dropped events, got %d", got)
	}

	// Unblock the worker so Close() can drain.
	close(blockCh)
}

func TestEventBusDroppedCountZeroWhenNoDrops(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	defer bus.Close()

	if got := bus.DroppedCount(); got != 0 {
		t.Fatalf("expected 0 dropped events on new bus, got %d", got)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	bus.Subscribe(EventPipelineStarted, func(*Event) {
		wg.Done()
	})

	bus.Publish(&Event{Type: EventPipelineStarted})
	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for event")
	}

	if got := bus.DroppedCount(); got != 0 {
		t.Fatalf("expected 0 dropped events after successful publish, got %d", got)
	}
}

func TestEventBusSubscriberTimeoutSkipsSlowListener(t *testing.T) {
	t.Parallel()

	// Use a very short timeout so the test runs quickly.
	bus := NewEventBus(
		WithWorkerPoolSize(1),
		WithSubscriberTimeout(50*time.Millisecond),
	)
	defer bus.Close()

	var fastCalled atomic.Int32

	// Slow listener that blocks longer than the timeout.
	bus.Subscribe(EventPipelineStarted, func(*Event) {
		time.Sleep(5 * time.Second)
	})

	// Fast listener registered after the slow one.
	var wg sync.WaitGroup
	wg.Add(1)
	bus.Subscribe(EventPipelineStarted, func(*Event) {
		fastCalled.Add(1)
		wg.Done()
	})

	bus.Publish(&Event{Type: EventPipelineStarted})

	// The fast listener should still fire despite the slow one timing out.
	if !waitForWG(&wg, 2*time.Second) {
		t.Fatal("timed out waiting for fast listener — subscriber timeout may not be working")
	}

	if got := fastCalled.Load(); got != 1 {
		t.Fatalf("expected fast listener to be called once, got %d", got)
	}
}

func TestEventBusSubscriberTimeoutDoesNotAffectFastListeners(t *testing.T) {
	t.Parallel()

	bus := NewEventBus(
		WithWorkerPoolSize(1),
		WithSubscriberTimeout(500*time.Millisecond),
	)
	defer bus.Close()

	var count atomic.Int32
	var wg sync.WaitGroup
	wg.Add(2)

	// Two fast listeners — both should complete normally.
	bus.Subscribe(EventPipelineStarted, func(*Event) {
		count.Add(1)
		wg.Done()
	})
	bus.Subscribe(EventPipelineStarted, func(*Event) {
		count.Add(1)
		wg.Done()
	})

	bus.Publish(&Event{Type: EventPipelineStarted})

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for fast listeners")
	}

	if got := count.Load(); got != 2 {
		t.Fatalf("expected 2 listener calls, got %d", got)
	}
}

func TestWithSubscriberTimeoutOption(t *testing.T) {
	t.Parallel()

	bus := NewEventBus(WithSubscriberTimeout(10 * time.Second))
	defer bus.Close()

	if bus.subscriberTimeout != 10*time.Second {
		t.Fatalf("expected subscriber timeout 10s, got %s", bus.subscriberTimeout)
	}
}

func TestWithSubscriberTimeoutIgnoresInvalidValues(t *testing.T) {
	t.Parallel()

	bus := NewEventBus(WithSubscriberTimeout(0), WithSubscriberTimeout(-1*time.Second))
	defer bus.Close()

	if bus.subscriberTimeout != DefaultSubscriberTimeout {
		t.Fatalf("expected default subscriber timeout %s, got %s",
			DefaultSubscriberTimeout, bus.subscriberTimeout)
	}
}

func TestEventBusSubscriberTimeoutGlobalListener(t *testing.T) {
	t.Parallel()

	bus := NewEventBus(
		WithWorkerPoolSize(1),
		WithSubscriberTimeout(50*time.Millisecond),
	)
	defer bus.Close()

	var fastCalled atomic.Int32

	// Slow global listener.
	bus.SubscribeAll(func(*Event) {
		time.Sleep(5 * time.Second)
	})

	// Fast specific listener should still fire.
	var wg sync.WaitGroup
	wg.Add(1)
	bus.Subscribe(EventPipelineStarted, func(*Event) {
		fastCalled.Add(1)
		wg.Done()
	})

	bus.Publish(&Event{Type: EventPipelineStarted})

	if !waitForWG(&wg, 2*time.Second) {
		t.Fatal("timed out waiting for specific listener after slow global listener")
	}

	if got := fastCalled.Load(); got != 1 {
		t.Fatalf("expected specific listener called once, got %d", got)
	}
}

func TestEventBusDroppedCountRateLimitedLogging(t *testing.T) {
	t.Parallel()

	// Use 1 worker with a tiny buffer. Block the worker so events drop.
	bus := NewEventBus(WithWorkerPoolSize(1), WithEventBufferSize(1))
	defer bus.Close()

	blockCh := make(chan struct{})
	bus.Subscribe(EventPipelineStarted, func(*Event) {
		<-blockCh
	})

	// First publish occupies the worker.
	bus.Publish(&Event{Type: EventPipelineStarted})
	time.Sleep(20 * time.Millisecond)

	// Second publish fills the 1-slot buffer.
	bus.Publish(&Event{Type: EventPipelineStarted})

	// Drop 250 events.
	for range 250 {
		bus.Publish(&Event{Type: EventPipelineStarted})
	}

	if got := bus.DroppedCount(); got != 250 {
		t.Fatalf("expected 250 dropped events, got %d", got)
	}

	close(blockCh)
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
