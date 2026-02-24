package hooks

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// Registry holds registered hooks and provides chain-execution methods.
// A nil *Registry is safe to use — all Run* methods return Allow / nil.
type Registry struct {
	providerHooks     []ProviderHook
	toolHooks         []ToolHook
	sessionHooks      []SessionHook
	chunkInterceptors []ChunkInterceptor // cached from providerHooks that implement ChunkInterceptor
}

// Option configures a Registry during construction.
type Option func(*Registry)

// WithProviderHook registers a provider hook.
func WithProviderHook(h ProviderHook) Option {
	return func(r *Registry) {
		r.providerHooks = append(r.providerHooks, h)
		if ci, ok := h.(ChunkInterceptor); ok {
			r.chunkInterceptors = append(r.chunkInterceptors, ci)
		}
	}
}

// WithToolHook registers a tool hook.
func WithToolHook(h ToolHook) Option {
	return func(r *Registry) {
		r.toolHooks = append(r.toolHooks, h)
	}
}

// WithSessionHook registers a session hook.
func WithSessionHook(h SessionHook) Option {
	return func(r *Registry) {
		r.sessionHooks = append(r.sessionHooks, h)
	}
}

// NewRegistry creates a Registry with the given options.
func NewRegistry(opts ...Option) *Registry {
	r := &Registry{}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// IsEmpty returns true if no hooks are registered.
func (r *Registry) IsEmpty() bool {
	if r == nil {
		return true
	}
	return len(r.providerHooks) == 0 && len(r.toolHooks) == 0 && len(r.sessionHooks) == 0
}

// --- Provider hooks ---

// RunBeforeProviderCall executes all provider hooks' BeforeCall in order.
// First deny wins and short-circuits.
func (r *Registry) RunBeforeProviderCall(ctx context.Context, req *ProviderRequest) Decision {
	if r == nil {
		return Allow
	}
	for _, h := range r.providerHooks {
		if d := h.BeforeCall(ctx, req); !d.Allow {
			return d
		}
	}
	return Allow
}

// RunAfterProviderCall executes all provider hooks' AfterCall in order.
// First deny wins and short-circuits.
func (r *Registry) RunAfterProviderCall(ctx context.Context, req *ProviderRequest, resp *ProviderResponse) Decision {
	if r == nil {
		return Allow
	}
	for _, h := range r.providerHooks {
		if d := h.AfterCall(ctx, req, resp); !d.Allow {
			return d
		}
	}
	return Allow
}

// HasChunkInterceptors returns true if any registered provider hook implements ChunkInterceptor.
func (r *Registry) HasChunkInterceptors() bool {
	if r == nil {
		return false
	}
	return len(r.chunkInterceptors) > 0
}

// RunOnChunk executes all chunk interceptors in order.
// First deny wins and short-circuits.
func (r *Registry) RunOnChunk(ctx context.Context, chunk *providers.StreamChunk) Decision {
	if r == nil {
		return Allow
	}
	for _, ci := range r.chunkInterceptors {
		if d := ci.OnChunk(ctx, chunk); !d.Allow {
			return d
		}
	}
	return Allow
}

// --- Tool hooks ---

// RunBeforeToolExecution executes all tool hooks' BeforeExecution in order.
// First deny wins and short-circuits.
func (r *Registry) RunBeforeToolExecution(ctx context.Context, req ToolRequest) Decision {
	if r == nil {
		return Allow
	}
	for _, h := range r.toolHooks {
		if d := h.BeforeExecution(ctx, req); !d.Allow {
			return d
		}
	}
	return Allow
}

// RunAfterToolExecution executes all tool hooks' AfterExecution in order.
// First deny wins and short-circuits.
func (r *Registry) RunAfterToolExecution(ctx context.Context, req ToolRequest, resp ToolResponse) Decision {
	if r == nil {
		return Allow
	}
	for _, h := range r.toolHooks {
		if d := h.AfterExecution(ctx, req, resp); !d.Allow {
			return d
		}
	}
	return Allow
}

// --- Session hooks ---

// RunSessionStart executes all session hooks' OnSessionStart in order.
// First error short-circuits.
func (r *Registry) RunSessionStart(ctx context.Context, event SessionEvent) error {
	if r == nil {
		return nil
	}
	for _, h := range r.sessionHooks {
		if err := h.OnSessionStart(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

// RunSessionUpdate executes all session hooks' OnSessionUpdate in order.
// First error short-circuits.
func (r *Registry) RunSessionUpdate(ctx context.Context, event SessionEvent) error {
	if r == nil {
		return nil
	}
	for _, h := range r.sessionHooks {
		if err := h.OnSessionUpdate(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

// RunSessionEnd executes all session hooks' OnSessionEnd in order.
// First error short-circuits.
func (r *Registry) RunSessionEnd(ctx context.Context, event SessionEvent) error {
	if r == nil {
		return nil
	}
	for _, h := range r.sessionHooks {
		if err := h.OnSessionEnd(ctx, event); err != nil {
			return err
		}
	}
	return nil
}
