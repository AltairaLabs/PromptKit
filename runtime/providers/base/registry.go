package base

import (
	"context"
	"fmt"
	"sync"
)

type registryKey struct {
	name string
	typ  ProviderType
}

// Registry stores providers keyed by (name, capability).
type Registry struct {
	mu       sync.RWMutex
	entries  map[registryKey]Provider
	resolver PricingResolver
}

// NewRegistry creates an empty registry with an InlinePricingResolver.
func NewRegistry() *Registry {
	return &Registry{
		entries:  make(map[registryKey]Provider),
		resolver: NewInlinePricingResolver(),
	}
}

// SetPricingResolver injects a non-default resolver (used by tests / future remote impl).
func (r *Registry) SetPricingResolver(res PricingResolver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resolver = res
}

// PricingResolver returns the registry's resolver (used by provider constructors at registration time).
func (r *Registry) PricingResolver() PricingResolver {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.resolver
}

// Register adds a provider. Returns an error if (name, capability) is already registered.
func (r *Registry) Register(p Provider) error {
	if p == nil {
		return fmt.Errorf("nil provider")
	}
	k := registryKey{name: p.Name(), typ: p.Type()}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.entries[k]; exists {
		return fmt.Errorf("provider %q (type=%s) already registered", k.name, k.typ)
	}
	r.entries[k] = p
	return nil
}

// Get returns the provider registered under (name, capability), or an error.
func (r *Registry) Get(name string, typ ProviderType) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if p, ok := r.entries[registryKey{name: name, typ: typ}]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("no provider %q registered for capability %q", name, typ)
}

// GetAll returns every provider of the given capability.
func (r *Registry) GetAll(typ ProviderType) []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Provider, 0)
	for k, v := range r.entries {
		if k.typ == typ {
			out = append(out, v)
		}
	}
	return out
}

// InitAll calls Init on every registered provider; aborts on first error.
func (r *Registry) InitAll(ctx context.Context) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for k, p := range r.entries {
		if err := p.Init(ctx); err != nil {
			return fmt.Errorf("init %q (%s): %w", k.name, k.typ, err)
		}
	}
	return nil
}

// CloseAll calls Close on every registered provider, returning the first error encountered.
func (r *Registry) CloseAll() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var firstErr error
	for k, p := range r.entries {
		if err := p.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close %q (%s): %w", k.name, k.typ, err)
		}
	}
	return firstErr
}

// GetTyped is a generic helper that returns a provider as a specific interface type T.
// Returns an error if the registered provider doesn't satisfy T.
func GetTyped[T Provider](r *Registry, name string, typ ProviderType) (T, error) {
	var zero T
	p, err := r.Get(name, typ)
	if err != nil {
		return zero, err
	}
	t, ok := p.(T)
	if !ok {
		return zero, fmt.Errorf("provider %q (type=%s) does not satisfy requested interface", name, typ)
	}
	return t, nil
}
