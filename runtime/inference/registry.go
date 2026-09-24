package inference

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
)

// Registry holds named Provider instances keyed by an id supplied at config
// time (e.g. "hf", "openai-moderation"). Callers look up by id; the
// id-to-provider mapping is the only thing a handler config needs to know.
//
// Providers register themselves into a Registry at engine startup; the
// registry then travels with context.Context to handlers via WithRegistry /
// FromContext.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	defaultID string
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]Provider)}
}

// Register adds p under id. Duplicate ids return an error. The first
// registration made against this Registry becomes the default.
func (r *Registry) Register(id string, p Provider) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.providers[id]; ok {
		return fmt.Errorf("inference: provider %q already registered", id)
	}
	r.providers[id] = p
	if r.defaultID == "" {
		r.defaultID = id
	}
	return nil
}

// SetDefault names the provider used when a caller passes an empty id to
// Get. The id must already be registered.
func (r *Registry) SetDefault(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.providers[id]; !ok {
		return fmt.Errorf("inference: default provider %q not registered", id)
	}
	r.defaultID = id
	return nil
}

// Get resolves id, or the default when id is "". Returns a non-nil error
// when nothing matches.
func (r *Registry) Get(id string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if id == "" {
		id = r.defaultID
	}
	if id == "" {
		return nil, fmt.Errorf("inference: no provider id supplied and no default configured")
	}
	p, ok := r.providers[id]
	if !ok {
		return nil, fmt.Errorf("inference: provider %q not registered", id)
	}
	return p, nil
}

// IDs returns every registered provider id, sorted.
func (r *Registry) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// registryContextKey is the unexported key used to attach a Registry to a
// context.Context. The type-as-key idiom avoids collisions with other
// context values.
type registryContextKey struct{}

// WithRegistry returns ctx with the Registry attached.
func WithRegistry(ctx context.Context, r *Registry) context.Context {
	return context.WithValue(ctx, registryContextKey{}, r)
}

// FromContext returns the Registry attached to ctx, or nil if none. Callers
// that don't find a registry fall back to a "skipped" result rather than
// failing — inference is an optional feature of the runtime, not a hard
// dependency.
func FromContext(ctx context.Context) *Registry {
	if ctx == nil {
		return nil
	}
	if r, ok := ctx.Value(registryContextKey{}).(*Registry); ok {
		return r
	}
	return nil
}

// emitterContextKey is the unexported key used to attach an events.Emitter
// to a context.Context. The type-as-key idiom avoids collisions with other
// context values.
type emitterContextKey struct{}

// WithEmitter returns ctx with e attached. Instrument uses the attached
// emitter to publish inference.call.completed / inference.call.failed
// events; a Provider wrapped by Instrument is a no-op passthrough for
// telemetry when the context carries no emitter.
func WithEmitter(ctx context.Context, e *events.Emitter) context.Context {
	if e == nil {
		// Leave any emitter an outer caller attached in place.
		return ctx
	}
	return context.WithValue(ctx, emitterContextKey{}, e)
}

// emitterFromContext returns the events.Emitter attached to ctx, or nil if
// none. Returning nil is safe: every Emitter method used by Instrument is a
// nil-receiver no-op.
func emitterFromContext(ctx context.Context) *events.Emitter {
	if ctx == nil {
		return nil
	}
	e, _ := ctx.Value(emitterContextKey{}).(*events.Emitter)
	return e
}
