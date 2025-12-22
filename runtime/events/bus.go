// Package events provides a lightweight pub/sub event bus for runtime observability.
package events

import (
	"context"
	"sync"
)

// Listener is a function that handles events.
type Listener func(*Event)

// EventBus manages event distribution to listeners.
type EventBus struct {
	mu              sync.RWMutex
	listeners       map[EventType][]Listener
	globalListeners []Listener
	store           EventStore
}

// NewEventBus creates a new event bus.
func NewEventBus() *EventBus {
	return &EventBus{
		listeners: make(map[EventType][]Listener),
	}
}

// WithStore returns a new event bus that persists events to the given store.
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

// Subscribe registers a listener for a specific event type.
func (eb *EventBus) Subscribe(eventType EventType, listener Listener) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.listeners[eventType] = append(eb.listeners[eventType], listener)
}

// SubscribeAll registers a listener for all event types.
func (eb *EventBus) SubscribeAll(listener Listener) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.globalListeners = append(eb.globalListeners, listener)
}

// Publish sends an event to all registered listeners asynchronously.
// If a store is configured, the event is persisted before dispatch.
func (eb *EventBus) Publish(event *Event) {
	eb.mu.RLock()
	typeListeners := eb.listeners[event.Type]
	store := eb.store

	specificListeners := make([]Listener, len(typeListeners))
	copy(specificListeners, typeListeners)

	globalListeners := make([]Listener, len(eb.globalListeners))
	copy(globalListeners, eb.globalListeners)
	eb.mu.RUnlock()

	// Persist to store if configured (synchronous to ensure ordering)
	if store != nil && event.SessionID != "" {
		// Use background context for persistence - don't block on caller context
		_ = store.Append(context.Background(), event)
	}

	go func() {
		for _, listener := range specificListeners {
			safeInvoke(listener, event)
		}
		for _, listener := range globalListeners {
			safeInvoke(listener, event)
		}
	}()
}

// Clear removes all listeners (primarily for tests).
func (eb *EventBus) Clear() {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.listeners = make(map[EventType][]Listener)
	eb.globalListeners = nil
}

func safeInvoke(listener Listener, event *Event) {
	defer func() { _ = recover() }()
	listener(event)
}
