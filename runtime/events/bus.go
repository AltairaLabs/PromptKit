// Package events provides a lightweight pub/sub event bus for runtime observability.
package events

import (
	"context"
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
)

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

// EventBus manages event distribution to listeners via a fixed-size worker pool.
// Workers are started lazily on the first Subscribe/SubscribeAll call to avoid
// spawning goroutines when no one is listening.
type EventBus struct {
	mu              sync.RWMutex
	listeners       map[EventType][]listenerEntry
	globalListeners []listenerEntry
	store           EventStore
	nextID          atomic.Uint64

	eventCh           chan *Event
	wg                sync.WaitGroup
	closed            atomic.Bool
	started           atomic.Bool // true once workers have been launched
	droppedCount      atomic.Int64
	subscriberTimeout time.Duration

	// Saved config for lazy worker startup.
	workerPoolSize int
}

// NewEventBus creates a new event bus with a worker pool.
// Options can be provided to configure pool size and buffer capacity.
// The zero-argument form uses sensible defaults and is fully backward-compatible.
//
// Workers are started lazily: goroutines are only spawned when the first
// subscriber is added via Subscribe or SubscribeAll.
func NewEventBus(opts ...BusOption) *EventBus {
	cfg := &busConfig{
		workerPoolSize:    DefaultWorkerPoolSize,
		eventBufferSize:   DefaultEventBufferSize,
		subscriberTimeout: DefaultSubscriberTimeout,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	eb := &EventBus{
		listeners:         make(map[EventType][]listenerEntry),
		eventCh:           make(chan *Event, cfg.eventBufferSize),
		subscriberTimeout: cfg.subscriberTimeout,
		workerPoolSize:    cfg.workerPoolSize,
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
	// Persist to store if configured (asynchronous — runs in worker goroutine)
	eb.mu.RLock()
	store := eb.store
	eb.mu.RUnlock()

	if store != nil && event.SessionID != "" {
		if err := store.Append(context.Background(), event); err != nil {
			logger.Warn("event store append failed",
				"event_type", string(event.Type),
				"session_id", event.SessionID,
				"error", err,
			)
		}
	}

	eb.mu.RLock()
	typeListeners := eb.listeners[event.Type]

	specificEntries := make([]listenerEntry, len(typeListeners))
	copy(specificEntries, typeListeners)

	globalEntries := make([]listenerEntry, len(eb.globalListeners))
	copy(globalEntries, eb.globalListeners)
	eb.mu.RUnlock()

	for _, entry := range specificEntries {
		eb.invokeWithTimeout(entry.listener, event)
	}
	for _, entry := range globalEntries {
		eb.invokeWithTimeout(entry.listener, event)
	}
}

// invokeWithTimeout runs a listener with the configured subscriber timeout.
// If the listener does not complete in time, a warning is logged and the call is skipped.
// A second warning is logged at 2x timeout if the goroutine is still running.
func (eb *EventBus) invokeWithTimeout(listener Listener, event *Event) {
	done := make(chan struct{}, 1)
	go func() {
		safeInvoke(listener, event)
		done <- struct{}{}
	}()

	timeout := eb.subscriberTimeout
	select {
	case <-done:
		return
	case <-time.After(timeout):
		logger.Warn("event subscriber timed out",
			"event_type", string(event.Type),
			"timeout", timeout.String(),
		)
	}

	// Monitor for goroutine leak: if listener hasn't returned after 2x timeout, log a warning.
	go func() {
		select {
		case <-done:
			// Listener eventually completed.
		case <-time.After(timeout):
			logger.Warn("event subscriber goroutine still running after 2x timeout",
				"event_type", string(event.Type),
				"elapsed", (timeout + timeout).String(),
			)
		}
	}()
}

// WithStore returns the event bus configured with the given store for persistence.
// If a non-nil store is provided, workers are started immediately since store
// writes happen in the dispatch worker goroutine.
func (eb *EventBus) WithStore(store EventStore) *EventBus {
	eb.mu.Lock()
	eb.store = store
	eb.mu.Unlock()

	if store != nil {
		eb.ensureStarted()
	}
	return eb
}

// Store returns the configured event store, or nil if none.
func (eb *EventBus) Store() EventStore {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return eb.store
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
	if eb.closed.Load() {
		return false
	}

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
		close(eb.eventCh)
		if eb.started.Load() {
			eb.wg.Wait()
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
