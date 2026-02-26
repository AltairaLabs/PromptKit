// Package events provides a lightweight pub/sub event bus for runtime observability.
package events

import (
	"context"
	"sync"
	"sync/atomic"
)

// Default configuration values for the event bus worker pool.
const (
	DefaultWorkerPoolSize  = 10
	DefaultEventBufferSize = 1000
)

// Listener is a function that handles events.
type Listener func(*Event)

// BusOption configures an EventBus during construction.
type BusOption func(*busConfig)

type busConfig struct {
	workerPoolSize  int
	eventBufferSize int
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

// listenerEntry holds a listener with a unique ID for unsubscription.
type listenerEntry struct {
	id       uint64
	listener Listener
}

// EventBus manages event distribution to listeners via a fixed-size worker pool.
type EventBus struct {
	mu              sync.RWMutex
	listeners       map[EventType][]listenerEntry
	globalListeners []listenerEntry
	store           EventStore
	nextID          atomic.Uint64

	eventCh chan *Event
	wg      sync.WaitGroup
	closed  atomic.Bool
}

// NewEventBus creates a new event bus with a worker pool.
// Options can be provided to configure pool size and buffer capacity.
// The zero-argument form uses sensible defaults and is fully backward-compatible.
func NewEventBus(opts ...BusOption) *EventBus {
	cfg := &busConfig{
		workerPoolSize:  DefaultWorkerPoolSize,
		eventBufferSize: DefaultEventBufferSize,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	eb := &EventBus{
		listeners: make(map[EventType][]listenerEntry),
		eventCh:   make(chan *Event, cfg.eventBufferSize),
	}

	eb.wg.Add(cfg.workerPoolSize)
	for range cfg.workerPoolSize {
		go eb.worker()
	}

	return eb
}

// worker processes events from the buffered channel.
func (eb *EventBus) worker() {
	defer eb.wg.Done()
	for event := range eb.eventCh {
		eb.dispatch(event)
	}
}

// dispatch delivers an event to all matching listeners.
func (eb *EventBus) dispatch(event *Event) {
	eb.mu.RLock()
	typeListeners := eb.listeners[event.Type]

	specificEntries := make([]listenerEntry, len(typeListeners))
	copy(specificEntries, typeListeners)

	globalEntries := make([]listenerEntry, len(eb.globalListeners))
	copy(globalEntries, eb.globalListeners)
	eb.mu.RUnlock()

	for _, entry := range specificEntries {
		safeInvoke(entry.listener, event)
	}
	for _, entry := range globalEntries {
		safeInvoke(entry.listener, event)
	}
}

// WithStore returns the event bus configured with the given store for persistence.
func (eb *EventBus) WithStore(store EventStore) *EventBus {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.store = store
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
func (eb *EventBus) Subscribe(eventType EventType, listener Listener) func() {
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
func (eb *EventBus) SubscribeAll(listener Listener) func() {
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
// synchronously before being queued for dispatch.
// Returns false if the bus has been closed.
func (eb *EventBus) Publish(event *Event) bool {
	if eb.closed.Load() {
		return false
	}

	eb.mu.RLock()
	store := eb.store
	eb.mu.RUnlock()

	// Persist to store if configured (synchronous to ensure ordering)
	if store != nil && event.SessionID != "" {
		// Use background context for persistence - don't block on caller context
		_ = store.Append(context.Background(), event)
	}

	// Non-blocking send: if the buffer is full, drop the event rather than blocking
	// the caller indefinitely. In practice, the buffer should be sized to handle bursts.
	select {
	case eb.eventCh <- event:
		return true
	default:
		return false
	}
}

// Close shuts down the event bus gracefully. It closes the event channel and
// waits for all workers to finish processing remaining events.
// After Close returns, Publish calls will return false.
func (eb *EventBus) Close() {
	if eb.closed.CompareAndSwap(false, true) {
		close(eb.eventCh)
		eb.wg.Wait()
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
