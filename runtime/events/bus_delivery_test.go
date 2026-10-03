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
// after maxLeakCount of them and receives no further events, so it cannot
// accumulate an ever-growing backlog. (The old bus did this too; the test
// keeps the behaviour through the rewrite.)
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

	// Let it work through whatever it was given before being disabled.
	time.Sleep(500 * time.Millisecond)
	if got := calls.Load(); got > maxLeakCount+2 {
		t.Fatalf("listener over the timeout on every call was called %d times; want it disabled after about %d",
			got, maxLeakCount)
	}
	if got := bus.DroppedCount(); got != 0 {
		t.Fatalf("events withheld from a disabled listener were counted as %d drops; "+
			"DroppedCount reports backpressure only", got)
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

// Unsubscribing stops delivery: the listener may finish the call it is in,
// but is never called again, even with events still in its queue. Its
// goroutine sees both the queue and the stop signal ready, and select picks
// between them at random, so this repeats to catch a missing check.
func TestEventBusUnsubscribedListenerIsNotCalledAgain(t *testing.T) {
	t.Parallel()

	for range 20 {
		bus := NewEventBus()
		inCall := make(chan struct{})
		release := make(chan struct{})
		var calls atomic.Int32
		unsubscribe := bus.SubscribeAll(func(*Event) {
			if calls.Add(1) == 1 {
				close(inCall)
				<-release
			}
		})

		for range 50 {
			bus.Publish(&Event{Type: EventPipelineStarted})
		}
		<-inCall
		unsubscribe()
		close(release)
		time.Sleep(20 * time.Millisecond)
		bus.Close()

		if got := calls.Load(); got != 1 {
			t.Fatalf("listener was called %d times; want only the call in progress at unsubscribe", got)
		}
	}
}

// Close must not wait out closeTimeout for a listener stuck in a call: once
// every listener still running has been in its call past the subscriber
// timeout, Close gives up on it.
func TestEventBusCloseDoesNotWaitOnStuckListener(t *testing.T) {
	t.Parallel()

	bus := NewEventBus(WithSubscriberTimeout(50 * time.Millisecond))
	stuck := make(chan struct{})
	defer close(stuck)
	inCall := make(chan struct{})
	var once sync.Once
	bus.SubscribeAll(func(*Event) {
		once.Do(func() { close(inCall) })
		<-stuck
	})
	bus.Publish(&Event{Type: EventPipelineStarted})
	<-inCall

	start := time.Now()
	bus.Close()
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Close waited %s for a listener stuck past the 50ms subscriber timeout", elapsed)
	}
}

// A listener hung in one call fills its queue. That is one timed-out call, so
// one strike, but it must still be disabled once its queue is full past the
// timeout; otherwise every later event counts as a drop forever and the
// backpressure metric climbs on a bus that is not saturated.
func TestEventBusDisablesHungListenerWithFullQueue(t *testing.T) {
	t.Parallel()

	bus := NewEventBus(WithEventBufferSize(4), WithSubscriberTimeout(20*time.Millisecond))
	hung := make(chan struct{})
	defer bus.Close()
	defer close(hung) // runs before Close
	inCall := make(chan struct{})
	var once sync.Once
	bus.SubscribeAll(func(*Event) {
		once.Do(func() { close(inCall) })
		<-hung
	})

	bus.Publish(&Event{Type: EventPipelineStarted})
	<-inCall
	time.Sleep(40 * time.Millisecond) // the call is now past the timeout

	// Paced so the bus buffer never overflows: these reach the listener's
	// queue, fill it, and then find it full behind a hung call.
	for range 100 {
		bus.Publish(&Event{Type: EventPipelineStarted})
		time.Sleep(200 * time.Microsecond)
	}
	time.Sleep(20 * time.Millisecond)

	if got := bus.DroppedCount(); got > 1 {
		t.Fatalf("a hung listener's withheld events were counted as %d drops; it should have been disabled", got)
	}
}
