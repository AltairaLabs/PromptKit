package generate

import (
	"fmt"
	"sync"
)

// Registry manages named SessionSourceAdapter instances.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]SessionSourceAdapter
}

// NewRegistry creates a new empty adapter registry.
func NewRegistry() *Registry {
	return &Registry{
		adapters: make(map[string]SessionSourceAdapter),
	}
}

// Register adds an adapter to the registry, keyed by its Name().
func (r *Registry) Register(adapter SessionSourceAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[adapter.Name()] = adapter
}

// Get returns the adapter with the given name, or an error if not found.
func (r *Registry) Get(name string) (SessionSourceAdapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	adapter, ok := r.adapters[name]
	if !ok {
		return nil, fmt.Errorf("unknown session source adapter: %q", name)
	}
	return adapter, nil
}

// Names returns the sorted list of registered adapter names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		names = append(names, name)
	}
	return names
}
