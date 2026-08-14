package hooks

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
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

// EmitterAware is implemented by hooks that report their own events. A hook
// cannot build an emitter for itself: the session and conversation IDs the
// events must carry are only known once the conversation exists, long after
// hooks are compiled from the pack.
//
// The provider stage constructor is the single place holding both the emitter
// and the hook registry, so that is where the two are joined — which is why
// SDK and Arena both get this without wiring anything themselves.
type EmitterAware interface {
	SetEmitter(*events.Emitter)
}

// SetEmitter hands the emitter to every registered provider hook that wants
// one. Hooks that do not implement EmitterAware are untouched. Nil-safe on both
// the receiver and the emitter.
func (r *Registry) SetEmitter(e *events.Emitter) {
	if r == nil || e == nil {
		return
	}
	for _, h := range r.providerHooks {
		if aware, ok := h.(EmitterAware); ok {
			aware.SetEmitter(e)
		}
	}
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

// Log messages for a non-Allow provider hook decision. Enforced and Deny are
// different outcomes and must not read alike: Enforced is a guardrail doing its
// job — the provider call is skipped (before-call) or the response is replaced
// (after-call), the round loop stops, and the pipeline continues. Deny aborts
// the pipeline with a HookDeniedError.
const (
	msgEnforcedBeforeCall = "provider hook enforced before-call: provider call skipped, canned turn substituted"
	msgDeniedBeforeCall   = "provider hook denied before-call"
	msgEnforcedAfterCall  = "provider hook enforced after-call: response replaced, pipeline continues"
	msgDeniedAfterCall    = "provider hook denied after-call"
)

// logProviderHookDecision logs a non-Allow provider hook decision at the level
// its outcome warrants. A graceful enforcement is routine policy work, so it
// logs at INFO; only a genuine denial — which aborts the pipeline — is a WARN.
func logProviderHookDecision(h ProviderHook, d Decision, enforcedMsg, deniedMsg string) {
	if d.Enforced {
		logger.Info(enforcedMsg, "hook", fmt.Sprintf("%T", h), "reason", d.Reason)
		return
	}
	logger.Warn(deniedMsg, "hook", fmt.Sprintf("%T", h), "reason", d.Reason)
}

// RunBeforeProviderCall executes all provider hooks' BeforeCall in order.
// The first non-Allow decision wins and short-circuits.
func (r *Registry) RunBeforeProviderCall(ctx context.Context, req *ProviderRequest) Decision {
	if r == nil {
		return Allow
	}
	for _, h := range r.providerHooks {
		if d := h.BeforeCall(ctx, req); !d.Allow {
			logProviderHookDecision(h, d, msgEnforcedBeforeCall, msgDeniedBeforeCall)
			return d
		}
	}
	return Allow
}

// RunAfterProviderCall executes all provider hooks' AfterCall in order.
// The first non-Allow decision wins and short-circuits.
func (r *Registry) RunAfterProviderCall(ctx context.Context, req *ProviderRequest, resp *ProviderResponse) Decision {
	if r == nil {
		return Allow
	}
	for _, h := range r.providerHooks {
		if d := h.AfterCall(ctx, req, resp); !d.Allow {
			logProviderHookDecision(h, d, msgEnforcedAfterCall, msgDeniedAfterCall)
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
			logger.Warn("chunk interceptor denied",
				"interceptor", fmt.Sprintf("%T", ci), "reason", d.Reason)
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
	var lastAllow Decision
	lastAllow.Allow = true
	for _, h := range r.toolHooks {
		d := h.BeforeExecution(ctx, req)
		if !d.Allow {
			logger.Warn("tool hook denied before-execution",
				"hook", fmt.Sprintf("%T", h), "tool", req.Name, "reason", d.Reason)
			return d
		}
		if d.Metadata != nil {
			lastAllow = d
		}
	}
	return lastAllow
}

// RunAfterToolExecution executes all tool hooks' AfterExecution in order.
// First deny wins and short-circuits.
func (r *Registry) RunAfterToolExecution(ctx context.Context, req ToolRequest, resp ToolResponse) Decision {
	if r == nil {
		return Allow
	}
	for _, h := range r.toolHooks {
		if d := h.AfterExecution(ctx, req, resp); !d.Allow {
			logger.Warn("tool hook denied after-execution",
				"hook", fmt.Sprintf("%T", h), "tool", req.Name, "reason", d.Reason)
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
