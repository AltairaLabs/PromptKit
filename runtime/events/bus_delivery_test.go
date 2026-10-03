package events

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Each listener runs on its own goroutine fed by one dispatcher, so it sees
// events in the order the bus accepted them. The old worker pool handed
// events to ten goroutines and made no ordering promise.
func TestEventBusDeliversInOrderPerListener(t *testing.T) {
	t.Parallel()

	const n = 2000
	bus := NewEventBus(WithEventBufferSize(n))
	defer bus.Close()

	var mu sync.Mutex
	var seqs []int64
	var wg sync.WaitGroup
	wg.Add(n)
	bus.SubscribeAll(func(e *Event) {
		mu.Lock()
		seqs = append(seqs, e.Sequence)
		mu.Unlock()
		wg.Done()
	})

	for range n {
		if !bus.Publish(&Event{Type: EventPipelineStarted}) {
			t.Fatal("publish dropped an event the buffer had room for")
		}
	}
	if !waitForWG(&wg, 5*time.Second) {
		t.Fatal("timed out waiting for every event")
	}

	mu.Lock()
	defer mu.Unlock()
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Fatalf("event %d arrived after event %d: delivery is out of publish order", seqs[i], seqs[i-1])
		}
	}
}

// A listener that blocks delays only itself: another listener still receives
// every event promptly. Under the worker pool a blocked listener held a worker
// for up to the subscriber timeout per event.
func TestEventBusSlowListenerDoesNotDelayOthers(t *testing.T) {
	t.Parallel()

	bus := NewEventBus(WithSubscriberTimeout(time.Hour))
	defer bus.Close()

	block := make(chan struct{})
	defer close(block) // runs before Close
	bus.SubscribeAll(func(*Event) { <-block })

	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	bus.SubscribeAll(func(*Event) { wg.Done() })

	for range n {
		bus.Publish(&Event{Type: EventPipelineStarted})
	}
	if !waitForWG(&wg, 2*time.Second) {
		t.Fatal("a blocked listener delayed delivery to another listener")
	}
}

// A listener whose calls keep exceeding the subscriber timeout is disabled
// after maxLeakCount of them, and its later events are dropped and counted,
// so it cannot accumulate an ever-growing backlog.
func TestEventBusDisablesChronicallySlowListener(t *testing.T) {
	t.Parallel()

	bus := NewEventBus(WithSubscriberTimeout(10 * time.Millisecond))
	defer bus.Close()

	var calls atomic.Int32
	bus.SubscribeAll(func(*Event) {
		calls.Add(1)
		time.Sleep(40 * time.Millisecond)
	})

	// Deliveries keep arriving while each slow call is in flight, which is
	// when the bus notices the call has run past the timeout.
	const n = 30
	for range n {
		bus.Publish(&Event{Type: EventPipelineStarted})
		time.Sleep(5 * time.Millisecond)
	}

	if got := waitForDropped(bus, 1, 2*time.Second); got == 0 {
		t.Fatal("a listener over the timeout on every call was never disabled")
	}
	if got := calls.Load(); got >= n {
		t.Fatalf("disabled listener was still called for every event (%d of %d)", got, n)
	}
}

// Clear stops every listener; an unsubscribe func called afterwards must be a
// no-op rather than stopping the listener a second time.
func TestEventBusUnsubscribeAfterClearDoesNotPanic(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	defer bus.Close()

	unsubscribe := bus.SubscribeAll(func(*Event) {})
	bus.Clear()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unsubscribe after Clear panicked: %v", r)
		}
	}()
	unsubscribe()
	unsubscribe()
}
