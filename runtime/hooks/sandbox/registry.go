package sandbox

import (
	"fmt"
	"sync"
)

// globalRegistry holds the process-wide factory registry. It's the
// default lookup path for RuntimeConfig-based sandbox resolution; SDK
// callers that want per-conversation scoping should pass a Registry
// explicitly via the SDK option instead of registering globally.
var globalRegistry = NewRegistry()

// Registry maps mode names to sandbox factories. It is safe for
// concurrent use.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

// Register adds a factory under the given mode name. An empty name or a
// nil factory is rejected. Duplicate registrations fail rather than
// silently overwriting — use Replace if overwrite is intentional.
func (r *Registry) Register(mode string, f Factory) error {
	if mode == "" {
		return fmt.Errorf("sandbox: mode name must not be empty")
	}
	if f == nil {
		return fmt.Errorf("sandbox: factory for mode %q must not be nil", mode)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[mode]; exists {
		return fmt.Errorf("sandbox: mode %q is already registered", mode)
	}
	r.factories[mode] = f
	return nil
}

// Replace is like Register but unconditionally overwrites any existing
// factory registered under mode. Use sparingly; the normal path is
// Register.
func (r *Registry) Replace(mode string, f Factory) error {
	if mode == "" {
		return fmt.Errorf("sandbox: mode name must not be empty")
	}
	if f == nil {
		return fmt.Errorf("sandbox: factory for mode %q must not be nil", mode)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[mode] = f
	return nil
}

// Lookup returns the factory registered under mode, or an error
// describing the available modes when the name is unknown.
func (r *Registry) Lookup(mode string) (Factory, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.factories[mode]
	if !ok {
		return nil, fmt.Errorf("sandbox: no factory registered for mode %q (known modes: %v)",
			mode, r.knownModesLocked())
	}
	return f, nil
}

// knownModesLocked returns the registered mode names, sorted by insertion.
// Caller must hold r.mu.
func (r *Registry) knownModesLocked() []string {
	names := make([]string, 0, len(r.factories))
	for k := range r.factories {
		names = append(names, k)
	}
	return names
}

// RegisterFactory registers f against the given mode in the process-wide
// registry. Typically called from an init function by consumers bringing
// their own sandbox implementations.
func RegisterFactory(mode string, f Factory) error {
	return globalRegistry.Register(mode, f)
}

// ReplaceFactory is the process-wide equivalent of Registry.Replace.
func ReplaceFactory(mode string, f Factory) error {
	return globalRegistry.Replace(mode, f)
}

// LookupFactory returns the factory registered against mode in the
// process-wide registry.
func LookupFactory(mode string) (Factory, error) {
	return globalRegistry.Lookup(mode)
}

// GlobalRegistry returns the process-wide registry, primarily for
// package tests and the SDK option that seeds per-conversation overrides.
func GlobalRegistry() *Registry { return globalRegistry }
