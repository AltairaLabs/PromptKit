// Package events provides a lightweight pub/sub event bus for runtime observability.
package events

import (
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// Default configuration values for the event bus worker pool.
const (
	DefaultWorkerPoolSize    = 10
	DefaultEventBufferSize   = 1000
	DefaultSubscriberTimeout = 5 * time.Second
	dropLogRateLimit         = 100 // log every Nth drop to avoid spam
	closeTimeout             = 10 * time.Second
)

// Environment variable names for operator-level event bus tuning. These
// are read by NewEventBus when an option is not supplied, so existing
// call sites (arena, SDK, examples) pick up operator config without any
// code changes.
//
// See AltairaLabs/PromptKit#853: the capability-matrix run dropped 201
// events due to worker-pool throughput saturation under bursty load.
// Operators can raise these values to match their expected concurrency
// without a code change.
const (
	// EnvEventBusBufferSize overrides DefaultEventBufferSize. Invalid
	// or non-positive values are ignored and the default is used.
	EnvEventBusBufferSize = "PROMPTKIT_EVENT_BUS_BUFFER_SIZE"
	// EnvEventBusWorkerPoolSize overrides DefaultWorkerPoolSize.
	// Invalid or non-positive values are ignored.
	//
	// Deprecated: the bus no longer uses a worker pool (each listener runs on
	// its own goroutine), so this has no effect. It is still read so existing
	// deployments that set it keep working.
	EnvEventBusWorkerPoolSize = "PROMPTKIT_EVENT_BUS_WORKER_POOL_SIZE"
	// EnvEventBusSubscriberTimeout overrides DefaultSubscriberTimeout.
	// Value is a Go duration string (e.g. "10s", "2m"). Invalid or
	// non-positive values are ignored.
	EnvEventBusSubscriberTimeout = "PROMPTKIT_EVENT_BUS_SUBSCRIBER_TIMEOUT"
)

// envDefaultBusConfig returns a busConfig seeded from environment
// variables, falling back to the package defaults when a variable is
// unset, malformed, or non-positive. Malformed values log a warning
// once per NewEventBus call so operators see their typos without
// spamming logs for every event.
func envDefaultBusConfig() *busConfig {
	cfg := &busConfig{
		workerPoolSize:    DefaultWorkerPoolSize,
		eventBufferSize:   DefaultEventBufferSize,
		subscriberTimeout: DefaultSubscriberTimeout,
	}
	if v := os.Getenv(EnvEventBusBufferSize); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.eventBufferSize = n
		} else {
			logger.Warn("ignoring invalid event bus buffer size from env",
				"env", EnvEventBusBufferSize,
				"value", v,
			)
		}
	}
	if v := os.Getenv(EnvEventBusWorkerPoolSize); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.workerPoolSize = n
		} else {
			logger.Warn("ignoring invalid event bus worker pool size from env",
				"env", EnvEventBusWorkerPoolSize,
				"value", v,
			)
		}
	}
	if v := os.Getenv(EnvEventBusSubscriberTimeout); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.subscriberTimeout = d
		} else {
			logger.Warn("ignoring invalid event bus subscriber timeout from env",
				"env", EnvEventBusSubscriberTimeout,
				"value", v,
			)
		}
	}
	return cfg
}

// Listener is a function that handles events.
type Listener func(*Event)

// BusOption configures an EventBus during construction.
type BusOption func(*busConfig)

type busConfig struct {
	workerPoolSize    int
	eventBufferSize   int
	subscriberTimeout time.Duration
}

// WithWorkerPoolSize sets the number of worker goroutines that process events.
// Defaults to DefaultWorkerPoolSize (10).
//
// Deprecated: the bus no longer uses a worker pool. One dispatcher hands each
// event to every matching listener's own queue, and each listener runs on its
// own goroutine, so this has no effect. It is kept for compatibility.
func WithWorkerPoolSize(size int) BusOption {
	return func(c *busConfig) {
		if size > 0 {
			c.workerPoolSize = size
		}
	}
}

// WithEventBufferSize sets the capacity of the buffered event channel, and of
// each listener's queue. Defaults to DefaultEventBufferSize (1000).
func WithEventBufferSize(size int) BusOption {
	return func(c *busConfig) {
		if size > 0 {
			c.eventBufferSize = size
		}
	}
}

// WithSubscriberTimeout sets how long one listener call may run before it is
// reported as timed out. A listener whose calls time out maxLeakCount times
// is disabled: its events are dropped from then on. A slow listener only ever
// delays itself — it runs on its own goroutine. Defaults to
// DefaultSubscriberTimeout (5s).
func WithSubscriberTimeout(d time.Duration) BusOption {
	return func(c *busConfig) {
		if d > 0 {
			c.subscriberTimeout = d
		}
	}
}

// maxLeakCount is the number of timed-out calls after which a listener is
// disabled and its events are dropped.
const maxLeakCount = 3

// Bus is the interface for publishing and subscribing to runtime events.
// The default implementation is EventBus (in-process, one goroutine per listener).
// Implement this interface to bridge events to an external transport
// such as NATS JetStream, Kafka, or Redis Streams.
type Bus interface {
	// Publish sends an event for delivery to all registered listeners.
	// Returns false if the bus has been closed or the event was dropped.
	Publish(event *Event) bool

	// Subscribe registers a listener for a specific event type.
	// Returns an unsubscribe function.
	Subscribe(eventType EventType, listener Listener) func()

	// SubscribeAll registers a listener for all event types.
	// Returns an unsubscribe function.
	SubscribeAll(listener Listener) func()

	// Close shuts down the bus and waits for pending events to drain.
	Close()
}

// listenerEntry is one subscribed listener and the goroutine that runs it.
//
// Delivery costs one non-blocking channel send per listener per event. The
// previous design started a goroutine and a timer, and made a channel, for
// every delivery — per streamed token, for every listener (#2146).
type listenerEntry struct {
	id       uint64
	listener Listener
	queue    chan *Event   // filled by the dispatcher, drained by run
	stop     chan struct{} // closed on unsubscribe: run exits without draining

	// Slow-call detection, read by the dispatcher on delivery: a timer per
	// delivery is the cost being removed, so there is no timer at all.
	callStart atomic.Int64  // UnixNano when the call in progress began; 0 when idle
	callSeq   atomic.Uint64 // number of calls started
	reported  atomic.Uint64 // callSeq last reported as timed out
	strikes   atomic.Int32  // timed-out calls so far
	disabled  atomic.Bool   // set after maxLeakCount strikes; events are dropped
	exited    atomic.Bool   // set when run returns
}

// listenerSet is an immutable snapshot of the subscribed listeners. The
// dispatcher reads it through an atomic pointer with no lock and no copy;
// Subscribe, unsubscribe and Clear replace it (copy-on-write).
type listenerSet struct {
	byType map[EventType][]*listenerEntry
	global []*listenerEntry
}

// with returns a copy of s with fn applied to the copy's fields.
func (s *listenerSet) with(fn func(byType map[EventType][]*listenerEntry, global *[]*listenerEntry)) *listenerSet {
	byType := make(map[EventType][]*listenerEntry, len(s.byType))
	for t, entries := range s.byType {
		byType[t] = append([]*listenerEntry(nil), entries...)
	}
	global := append([]*listenerEntry(nil), s.global...)
	fn(byType, &global)
	return &listenerSet{byType: byType, global: global}
}

// all returns every entry in the set.
func (s *listenerSet) all() []*listenerEntry {
	out := append([]*listenerEntry(nil), s.global...)
	for _, entries := range s.byType {
		out = append(out, entries...)
	}
	return out
}

// contains reports whether the set holds the entry with the given id.
func (s *listenerSet) contains(id uint64) bool {
	for _, e := range s.all() {
		if e.id == id {
			return true
		}
	}
	return false
}

// without removes the entry with the given id from entries.
func without(entries []*listenerEntry, id uint64) []*listenerEntry {
	for i, e := range entries {
		if e.id == id {
			return append(entries[:i:i], entries[i+1:]...)
		}
	}
	return entries
}

// EventBus delivers events to listeners in process.
//
// Publish hands an event to a buffered channel and returns. One dispatcher
// goroutine takes each event, in publish order, and offers it to every
// matching listener's own bounded queue; each listener runs on its own
// goroutine. So every listener sees events in publish order, a slow listener
// delays only itself, and a listener whose queue is full has the event
// dropped and counted (DroppedCount) rather than stalling the others.
//
// The dispatcher starts lazily on the first Subscribe/SubscribeAll, so a bus
// nobody listens to runs no goroutines.
type EventBus struct {
	mu        sync.Mutex // serializes changes to listeners, and Subscribe vs shutdown
	listeners atomic.Pointer[listenerSet]
	nextID    atomic.Uint64
	seq       atomic.Int64 // monotonic sequence counter for events

	publishMu         sync.RWMutex // guards eventCh send (RLock) vs close (Lock)
	eventCh           chan *Event
	queueSize         int
	listenerWG        sync.WaitGroup // one per listener goroutine
	dispatched        chan struct{}  // closed when the dispatcher has exited
	closed            atomic.Bool
	started           atomic.Bool // true once the dispatcher has been launched
	droppedCount      atomic.Int64
	subscriberTimeout time.Duration
}

// NewEventBus creates a new event bus.
//
// Configuration precedence (highest first):
//
//  1. Explicit options passed as BusOption arguments (WithEventBufferSize,
//     WithSubscriberTimeout). Tests and programmatic callers that want
//     deterministic behavior should use these.
//  2. Environment variables (PROMPTKIT_EVENT_BUS_BUFFER_SIZE,
//     PROMPTKIT_EVENT_BUS_SUBSCRIBER_TIMEOUT). Invalid values are logged
//     and ignored. This is the operator knob for tuning without code
//     changes — see AltairaLabs/PromptKit#853.
//  3. Package defaults (DefaultEventBufferSize etc.).
//
// The zero-argument form reads env vars and falls back to defaults, so
// existing call sites pick up operator configuration automatically.
//
// Goroutines are started lazily: the dispatcher when the first subscriber is
// added, and one goroutine per subscribed listener.
func NewEventBus(opts ...BusOption) *EventBus {
	cfg := envDefaultBusConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	eb := &EventBus{
		eventCh:           make(chan *Event, cfg.eventBufferSize),
		queueSize:         cfg.eventBufferSize,
		dispatched:        make(chan struct{}),
		subscriberTimeout: cfg.subscriberTimeout,
	}
	eb.listeners.Store(&listenerSet{})
	return eb
}

// ensureStarted launches the dispatcher if it hasn't been started yet.
func (eb *EventBus) ensureStarted() {
	if eb.started.CompareAndSwap(false, true) {
		go eb.dispatch()
	}
}

// dispatch hands each published event, in publish order, to every matching
// listener's queue. When Close closes eventCh it closes every listener's
// queue, so each listener drains what it was given and exits.
func (eb *EventBus) dispatch() {
	defer close(eb.dispatched)
	for event := range eb.eventCh {
		eb.deliver(event)
	}
	eb.mu.Lock()
	for _, e := range eb.listeners.Load().all() {
		close(e.queue)
	}
	eb.mu.Unlock()
}

// deliver offers event to every matching listener. It never blocks and does
// not allocate.
func (eb *EventBus) deliver(event *Event) {
	set := eb.listeners.Load()
	for _, e := range set.byType[event.Type] {
		eb.enqueue(e, event)
	}
	for _, e := range set.global {
		eb.enqueue(e, event)
	}
}

// enqueue offers event to one listener's queue, dropping it if the listener
// is disabled or its queue is full.
func (eb *EventBus) enqueue(e *listenerEntry, event *Event) {
	eb.checkSlowCall(e, event)
	if e.disabled.Load() {
		// Logged once when disabled. Not a drop: DroppedCount (and the
		// eventbus_events_dropped_total metric) reports backpressure, and a
		// disabled listener would otherwise add to it on every event forever.
		return
	}
	select {
	case e.queue <- event:
	default:
		eb.recordDrop(e, event, "listener queue full")
	}
}

// checkSlowCall reports a listener call that has run past the subscriber
// timeout, once per call, and disables the listener after maxLeakCount such
// calls. It runs on delivery, so a stuck listener is noticed when events for
// it arrive — which is when its backlog starts to matter.
func (eb *EventBus) checkSlowCall(e *listenerEntry, event *Event) {
	start := e.callStart.Load()
	if start == 0 {
		return
	}
	elapsed := time.Duration(time.Now().UnixNano() - start)
	if elapsed < eb.subscriberTimeout {
		return
	}
	call := e.callSeq.Load()
	if e.reported.Swap(call) == call {
		return // this call was already reported
	}
	strikes := e.strikes.Add(1)
	logger.Warn("event subscriber timed out",
		"event_type", string(event.Type),
		"timeout", eb.subscriberTimeout.String(),
		"elapsed", elapsed.String(),
		"listener_id", e.id,
		"strikes", strikes,
	)
	if strikes >= maxLeakCount && e.disabled.CompareAndSwap(false, true) {
		logger.Warn("disabling chronically slow event subscriber; it receives no further events",
			"listener_id", e.id,
			"strikes", strikes,
		)
	}
}

// recordDrop counts a delivery dropped because a listener's queue was full,
// and logs every dropLogRateLimit-th.
func (eb *EventBus) recordDrop(e *listenerEntry, event *Event, reason string) {
	dropped := eb.droppedCount.Add(1)
	if dropped%dropLogRateLimit == 1 {
		logger.Warn("event dropped",
			"reason", reason,
			"event_type", string(event.Type),
			"listener_id", e.id,
			"total_dropped", dropped,
		)
	}
}

// run calls the listener for each event in its queue, in order, until the
// queue is closed (bus Close) or the listener is unsubscribed.
func (eb *EventBus) run(e *listenerEntry) {
	defer eb.listenerWG.Done()
	defer e.exited.Store(true)
	for {
		select {
		case event, ok := <-e.queue:
			if !ok {
				return
			}
			// select picks at random when both are ready, so check stop
			// again: an unsubscribed listener must not be called again.
			select {
			case <-e.stop:
				return
			default:
			}
			if e.disabled.Load() {
				continue // disabled for timing out: its backlog is skipped too
			}
			e.callSeq.Add(1)
			e.callStart.Store(time.Now().UnixNano())
			safeInvoke(e.listener, event)
			e.callStart.Store(0)
		case <-e.stop:
			return
		}
	}
}

// add subscribes listener to eventType, or to every type when global, and
// returns its unsubscribe function. A bus that is already closed accepts the
// subscription and never calls the listener.
func (eb *EventBus) add(eventType EventType, global bool, listener Listener) func() {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	if eb.closed.Load() {
		return func() {}
	}
	e := &listenerEntry{
		id:       eb.nextID.Add(1),
		listener: listener,
		queue:    make(chan *Event, eb.queueSize),
		stop:     make(chan struct{}),
	}
	eb.listeners.Store(eb.listeners.Load().with(func(byType map[EventType][]*listenerEntry, all *[]*listenerEntry) {
		if global {
			*all = append(*all, e)
		} else {
			byType[eventType] = append(byType[eventType], e)
		}
	}))
	eb.listenerWG.Add(1)
	go eb.run(e)
	eb.ensureStarted()

	var once sync.Once
	return func() { once.Do(func() { eb.remove(e) }) }
}

// remove unsubscribes e and stops its goroutine. Events already in its queue
// are not delivered. A listener Clear already removed is left alone: Clear
// stopped it, and stopping it twice would panic.
func (eb *EventBus) remove(e *listenerEntry) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	if !eb.listeners.Load().contains(e.id) {
		return
	}
	eb.listeners.Store(eb.listeners.Load().with(func(byType map[EventType][]*listenerEntry, all *[]*listenerEntry) {
		*all = without(*all, e.id)
		for t, entries := range byType {
			byType[t] = without(entries, e.id)
		}
	}))
	close(e.stop)
}

// Subscribe registers a listener for a specific event type and returns
// an unsubscribe function that removes the listener when called.
// The listener runs on its own goroutine and sees events in publish order.
func (eb *EventBus) Subscribe(eventType EventType, listener Listener) func() {
	return eb.add(eventType, false, listener)
}

// SubscribeAll registers a listener for all event types and returns
// an unsubscribe function that removes the listener when called.
// The listener runs on its own goroutine and sees events in publish order.
func (eb *EventBus) SubscribeAll(listener Listener) func() {
	return eb.add("", true, listener)
}

// Publish sends an event for asynchronous delivery to all registered
// listeners. It never blocks: if the bus's buffer is full the event is
// dropped. Returns false if the bus has been closed or the event was dropped.
func (eb *EventBus) Publish(event *Event) bool {
	// RLock allows concurrent publishes but blocks during Close.
	eb.publishMu.RLock()
	defer eb.publishMu.RUnlock()

	if eb.closed.Load() {
		return false
	}

	// Stamp monotonic sequence for consumer-side ordering.
	event.Sequence = eb.seq.Add(1)

	// Non-blocking send: if the buffer is full, drop the event rather than blocking
	// the caller indefinitely. In practice, the buffer should be sized to handle bursts.
	select {
	case eb.eventCh <- event:
		return true
	default:
		dropped := eb.droppedCount.Add(1)
		if dropped%dropLogRateLimit == 1 {
			logger.Warn("event dropped: buffer full",
				"event_type", string(event.Type),
				"total_dropped", dropped,
			)
		}
		return false
	}
}

// DroppedCount returns the number of drops caused by backpressure: events
// Publish dropped because the bus's buffer was full, plus deliveries a
// listener missed because its own queue was full (one per listener per
// event). A listener disabled for timing out receives nothing further, and
// that is not counted here.
func (eb *EventBus) DroppedCount() int64 {
	return eb.droppedCount.Load()
}

// Close shuts down the event bus gracefully: Publish returns false from then
// on, and Close waits for every listener to finish the events it was given,
// up to closeTimeout. If no subscriber was ever added, Close just discards the
// buffered events.
func (eb *EventBus) Close() {
	if !eb.closed.CompareAndSwap(false, true) {
		return
	}
	// Lock publishMu so no Publish is mid-send when eventCh closes, and mu
	// so no Subscribe is between its closed check and starting the dispatcher.
	eb.publishMu.Lock()
	close(eb.eventCh)
	eb.publishMu.Unlock()
	eb.mu.Lock()
	started := eb.started.Load()
	eb.mu.Unlock()

	if !started {
		for range eb.eventCh { //nolint:revive // intentional drain
		}
		return
	}
	// Wait for the dispatcher to hand over every event, then for every
	// listener to finish its queue. Stop waiting once every listener still
	// running is stuck in a call past the subscriber timeout — it may never
	// return — and in any case after closeTimeout.
	drained := make(chan struct{})
	go func() {
		<-eb.dispatched
		eb.listenerWG.Wait()
		close(drained)
	}()
	deadline := time.After(closeTimeout)
	poll := time.NewTicker(closeStuckPoll)
	defer poll.Stop()
	for {
		select {
		case <-drained:
			return
		case <-poll.C:
			if eb.onlyStuckListenersRemain() {
				logger.Warn("event bus closed with a listener stuck past the subscriber timeout; abandoning its events",
					"timeout", eb.subscriberTimeout.String())
				return
			}
		case <-deadline:
			logger.Warn("event bus close timed out, abandoning remaining events",
				"timeout", closeTimeout.String())
			return
		}
	}
}

// closeStuckPoll is how often Close checks whether only stuck listeners remain.
const closeStuckPoll = 10 * time.Millisecond

// onlyStuckListenersRemain reports whether every listener that has not exited
// is in a call that has run past the subscriber timeout. Unsubscribed
// listeners are not in the set, and exit on their own.
func (eb *EventBus) onlyStuckListenersRemain() bool {
	now := time.Now().UnixNano()
	for _, e := range eb.listeners.Load().all() {
		if e.exited.Load() {
			continue
		}
		start := e.callStart.Load()
		if start == 0 || time.Duration(now-start) < eb.subscriberTimeout {
			return false // idle with events to drain, or in a call still within the timeout
		}
	}
	return true
}

// Clear removes all listeners (primarily for tests).
func (eb *EventBus) Clear() {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	for _, e := range eb.listeners.Load().all() {
		close(e.stop)
	}
	eb.listeners.Store(&listenerSet{})
}

func safeInvoke(listener Listener, event *Event) {
	defer func() { _ = recover() }()
	listener(event)
}
