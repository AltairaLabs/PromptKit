// Package events provides a lightweight pub/sub event bus for runtime observability.
package events

import (
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
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
func WithWorkerPoolSize(size int) BusOption {
	return func(c *busConfig) {
		if size > 0 {
			c.workerPoolSize = size
		}
	}
}

// WithEventBufferSize sets the capacity of the buffered event channel.
// Defaults to DefaultEventBufferSize (1000).
func WithEventBufferSize(size int) BusOption {
	return func(c *busConfig) {
		if size > 0 {
			c.eventBufferSize = size
		}
	}
}

// WithSubscriberTimeout sets the maximum duration a listener is allowed to run
// before it is considered timed out and skipped. Defaults to DefaultSubscriberTimeout (5s).
func WithSubscriberTimeout(d time.Duration) BusOption {
	return func(c *busConfig) {
		if d > 0 {
			c.subscriberTimeout = d
		}
	}
}

// listenerEntry holds a listener with a unique ID for unsubscription.
type listenerEntry struct {
	id       uint64
	listener Listener
}

// maxLeakCount is the number of timeout strikes before a listener is skipped.
const maxLeakCount = 3

// Bus is the interface for publishing and subscribing to runtime events.
// The default implementation is EventBus (in-process, goroutine-pool based).
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

// EventBus manages event distribution to listeners via a fixed-size worker pool.
// Workers are started lazily on the first Subscribe/SubscribeAll call to avoid
// spawning goroutines when no one is listening.
type EventBus struct {
	mu              sync.RWMutex
	listeners       map[EventType][]listenerEntry
	globalListeners []listenerEntry
	nextID          atomic.Uint64
	seq             atomic.Int64 // monotonic sequence counter for events

	publishMu         sync.RWMutex // guards eventCh send (RLock) vs close (Lock)
	eventCh           chan *Event
	wg                sync.WaitGroup
	closed            atomic.Bool
	started           atomic.Bool // true once workers have been launched
	droppedCount      atomic.Int64
	subscriberTimeout time.Duration

	// leakCount tracks how many times each listener has timed out.
	// Protected by mu.
	leakCount map[uint64]int

	// done is closed by Close() to cancel orphaned invokeWithTimeout goroutines.
	done chan struct{}

	// Saved config for lazy worker startup.
	workerPoolSize int
}

// NewEventBus creates a new event bus with a worker pool.
//
// Configuration precedence (highest first):
//
//  1. Explicit options passed as BusOption arguments (WithWorkerPoolSize,
//     WithEventBufferSize, WithSubscriberTimeout). Tests and programmatic
//     callers that want deterministic behavior should use these.
//  2. Environment variables (PROMPTKIT_EVENT_BUS_BUFFER_SIZE,
//     PROMPTKIT_EVENT_BUS_WORKER_POOL_SIZE,
//     PROMPTKIT_EVENT_BUS_SUBSCRIBER_TIMEOUT). Invalid values are logged
//     and ignored. This is the operator knob for tuning without code
//     changes — see AltairaLabs/PromptKit#853.
//  3. Package defaults (DefaultEventBufferSize etc.).
//
// The zero-argument form reads env vars and falls back to defaults, so
// existing call sites pick up operator configuration automatically.
//
// Workers are started lazily: goroutines are only spawned when the first
// subscriber is added via Subscribe or SubscribeAll.
func NewEventBus(opts ...BusOption) *EventBus {
	cfg := envDefaultBusConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	eb := &EventBus{
		listeners:         make(map[EventType][]listenerEntry),
		eventCh:           make(chan *Event, cfg.eventBufferSize),
		subscriberTimeout: cfg.subscriberTimeout,
		workerPoolSize:    cfg.workerPoolSize,
		leakCount:         make(map[uint64]int),
		done:              make(chan struct{}),
	}

	return eb
}

// ensureStarted launches workers if they haven't been started yet.
// Caller must NOT hold eb.mu (this method is lock-free via atomic CAS).
func (eb *EventBus) ensureStarted() {
	if eb.started.CompareAndSwap(false, true) {
		eb.wg.Add(eb.workerPoolSize)
		for range eb.workerPoolSize {
			go eb.worker()
		}
	}
}

// worker processes events from the buffered channel.
func (eb *EventBus) worker() {
	defer eb.wg.Done()
	for event := range eb.eventCh {
		eb.dispatch(event)
	}
}

// dispatch delivers an event to all matching listeners.
// If an event store is configured, the event is persisted asynchronously here
// (in the worker goroutine) rather than in the Publish() caller's goroutine,
// keeping the publish path fast.
// Each listener is invoked with a timeout; if a listener exceeds the subscriber
// timeout it is skipped and a warning is logged.
func (eb *EventBus) dispatch(event *Event) {
	eb.mu.RLock()
	typeListeners := eb.listeners[event.Type]

	specificEntries := make([]listenerEntry, len(typeListeners))
	copy(specificEntries, typeListeners)

	globalEntries := make([]listenerEntry, len(eb.globalListeners))
	copy(globalEntries, eb.globalListeners)
	eb.mu.RUnlock()

	for _, entry := range specificEntries {
		eb.invokeWithTimeout(entry.id, entry.listener, event)
	}
	for _, entry := range globalEntries {
		eb.invokeWithTimeout(entry.id, entry.listener, event)
	}
}

// invokeWithTimeout runs a listener with the configured subscriber timeout.
// If the listener does not complete in time, a warning is logged and the call is skipped.
// A second warning is logged at 2x timeout if the goroutine is still running.
// Listeners that have timed out more than maxLeakCount times are skipped entirely.
func (eb *EventBus) invokeWithTimeout(id uint64, listener Listener, event *Event) {
	// Check leak count and skip chronically leaking listeners.
	eb.mu.RLock()
	count := eb.leakCount[id]
	eb.mu.RUnlock()
	if count >= maxLeakCount {
		logger.Warn("skipping chronically leaking listener",
			"listener_id", id,
			"event_type", string(event.Type),
			"leak_count", count,
		)
		return
	}

	done := make(chan struct{}, 1)
	go func() {
		safeInvoke(listener, event)
		done <- struct{}{}
	}()

	// Fast path: try non-blocking receive before starting timeout machinery.
	select {
	case <-done:
		return
	default:
	}

	timeout := eb.subscriberTimeout
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-done:
		return
	case <-timer.C:
		eb.mu.Lock()
		eb.leakCount[id]++
		eb.mu.Unlock()

		logger.Warn("event subscriber timed out",
			"event_type", string(event.Type),
			"timeout", timeout.String(),
			"listener_id", id,
		)
	}

	// Monitor for goroutine leak: exit when listener completes, bus closes, or 2x timeout.
	go func() {
		leakTimer := time.NewTimer(timeout)
		defer leakTimer.Stop()
		select {
		case <-done:
			// Listener eventually completed.
		case <-eb.done:
			// Bus is closing, stop monitoring.
		case <-leakTimer.C:
			logger.Warn("event subscriber goroutine still running after 2x timeout",
				"event_type", string(event.Type),
				"elapsed", (timeout + timeout).String(),
				"listener_id", id,
			)
		}
	}()
}

// Subscribe registers a listener for a specific event type and returns
// an unsubscribe function that removes the listener when called.
// On the first call, workers are started lazily.
func (eb *EventBus) Subscribe(eventType EventType, listener Listener) func() {
	eb.ensureStarted()

	eb.mu.Lock()
	defer eb.mu.Unlock()

	id := eb.nextID.Add(1)
	eb.listeners[eventType] = append(eb.listeners[eventType], listenerEntry{
		id:       id,
		listener: listener,
	})

	return func() {
		eb.mu.Lock()
		defer eb.mu.Unlock()
		entries := eb.listeners[eventType]
		for i, entry := range entries {
			if entry.id == id {
				eb.listeners[eventType] = append(entries[:i], entries[i+1:]...)
				return
			}
		}
	}
}

// SubscribeAll registers a listener for all event types and returns
// an unsubscribe function that removes the listener when called.
// On the first call, workers are started lazily.
func (eb *EventBus) SubscribeAll(listener Listener) func() {
	eb.ensureStarted()

	eb.mu.Lock()
	defer eb.mu.Unlock()

	id := eb.nextID.Add(1)
	eb.globalListeners = append(eb.globalListeners, listenerEntry{
		id:       id,
		listener: listener,
	})

	return func() {
		eb.mu.Lock()
		defer eb.mu.Unlock()
		for i, entry := range eb.globalListeners {
			if entry.id == id {
				eb.globalListeners = append(eb.globalListeners[:i], eb.globalListeners[i+1:]...)
				return
			}
		}
	}
}

// Publish sends an event to the worker pool for asynchronous delivery to all
// registered listeners. If a store is configured, the event is persisted
// asynchronously in the dispatch worker (not in the caller's goroutine).
// Returns false if the bus has been closed.
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

// DroppedCount returns the total number of events dropped due to a full buffer.
func (eb *EventBus) DroppedCount() int64 {
	return eb.droppedCount.Load()
}

// Close shuts down the event bus gracefully. It closes the event channel and
// waits for all workers to finish processing remaining events.
// After Close returns, Publish calls will return false.
// If no workers were ever started (no subscribers), Close simply marks the bus
// as closed and drains any buffered events.
func (eb *EventBus) Close() {
	if eb.closed.CompareAndSwap(false, true) {
		// Lock publishMu to ensure no Publish is mid-send before closing channels.
		eb.publishMu.Lock()
		close(eb.done)    // cancel orphaned invokeWithTimeout goroutines
		close(eb.eventCh) // signal workers to drain and exit
		eb.publishMu.Unlock()
		if eb.started.Load() {
			// Wait for workers with a hard deadline. Workers may be blocked
			// on slow listener timeouts — don't let that hang the process.
			done := make(chan struct{})
			go func() {
				eb.wg.Wait()
				close(done)
			}()
			select {
			case <-done:
				// Workers drained cleanly.
			case <-time.After(closeTimeout):
				logger.Warn("event bus close timed out, abandoning remaining events",
					"timeout", closeTimeout.String())
			}
		} else {
			// Drain any buffered events that were published before Close
			// when no workers were started.
			for range eb.eventCh { //nolint:revive // intentional drain
			}
		}
	}
}

// Clear removes all listeners (primarily for tests).
func (eb *EventBus) Clear() {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.listeners = make(map[EventType][]listenerEntry)
	eb.globalListeners = nil
}

func safeInvoke(listener Listener, event *Event) {
	defer func() { _ = recover() }()
	listener(event)
}
