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

// Unsubscribing is ordered with publishing: the listener receives every event
// published before the unsubscribe and none published after, even though the
// earlier ones are still queued behind a call in progress when it returns.
func TestEventBusUnsubscribeDeliversEarlierEventsOnly(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	defer bus.Close()
	inCall := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	unsubscribe := bus.SubscribeAll(func(*Event) {
		if calls.Add(1) == 1 {
			close(inCall)
			<-release
		}
	})

	const before = 50
	for range before {
		bus.Publish(&Event{Type: EventPipelineStarted})
	}
	<-inCall
	unsubscribe()
	for range 50 {
		bus.Publish(&Event{Type: EventPipelineStarted})
	}
	// Release only once the unsubscribe has taken effect, so the queued
	// events are delivered after the listener left the set, not before.
	deadline := time.Now().Add(2 * time.Second)
	for len(bus.listeners.Load().all()) > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(release)

	deadline = time.Now().Add(2 * time.Second)
	for calls.Load() < before && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // room for any wrongly delivered later event
	if got := calls.Load(); got != before {
		t.Fatalf("listener was called %d times; want exactly the %d events published before unsubscribe", got, before)
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

// An unsubscribed listener leaves the set but still drains what it was
// given, so Close must wait for it like any other listener rather than
// treating an empty set as "only stuck listeners remain".
func TestEventBusCloseWaitsForUnsubscribedListenerBacklog(t *testing.T) {
	t.Parallel()

	bus := NewEventBus(WithSubscriberTimeout(time.Second))
	var calls atomic.Int32
	unsubscribe := bus.SubscribeAll(func(*Event) {
		time.Sleep(2 * time.Millisecond)
		calls.Add(1)
	})
	const published = 100
	for range published {
		if !bus.Publish(&Event{Type: EventPipelineStarted}) {
			t.Fatal("publish dropped an event the buffer had room for")
		}
	}
	unsubscribe()
	bus.Close()

	if got := calls.Load(); got != published {
		t.Fatalf("Close returned after %d of %d events reached the unsubscribed listener", got, published)
	}
}

// A listener stuck in one call past the timeout fills its queue. The events it
// misses meanwhile are reported once, not counted as drops (that would climb
// forever on a bus that is not saturated), and one slow call does not
// disable it: when the call returns it receives events again.
func TestEventBusStalledListenerMissesAreNotCountedAndItRecovers(t *testing.T) {
	t.Parallel()

	bus := NewEventBus(WithEventBufferSize(4), WithSubscriberTimeout(20*time.Millisecond))
	defer bus.Close()
	hung := make(chan struct{})
	inCall := make(chan struct{})
	var once sync.Once
	var calls atomic.Int32
	bus.SubscribeAll(func(e *Event) {
		calls.Add(1)
		once.Do(func() { close(inCall) })
		if e.Type == EventPipelineStarted {
			<-hung
		}
	})

	bus.Publish(&Event{Type: EventPipelineStarted})
	<-inCall
	time.Sleep(40 * time.Millisecond) // the call is now past the timeout

	// Paced so the bus buffer never overflows: these fill the listener's
	// queue and then find it full behind the stalled call.
	for range 100 {
		bus.Publish(&Event{Type: EventPipelineCompleted})
		time.Sleep(200 * time.Microsecond)
	}
	time.Sleep(20 * time.Millisecond)
	if got := bus.DroppedCount(); got > 1 {
		t.Fatalf("a stalled listener's missed events were counted as %d drops", got)
	}

	// Release it and let it work through the backlog its queue held.
	close(hung)
	waitForCalls := func(want int32) int32 {
		deadline := time.Now().Add(2 * time.Second)
		for calls.Load() < want && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		return calls.Load()
	}
	drained := waitForCalls(5) // the stalled call plus a full queue of 4
	time.Sleep(10 * time.Millisecond)

	bus.Publish(&Event{Type: EventPipelineCompleted})
	if got := waitForCalls(drained + 1); got <= drained {
		t.Fatalf("listener did not receive events after its slow call returned (calls %d)", got)
	}
}
