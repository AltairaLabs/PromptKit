package engine

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
)

// sessionHookContextKey is a private type used to key session-hook data in a context.
// A private type prevents collisions with any other package's context keys.
type sessionHookContextKey struct{}

// sessionHookCtxValue bundles the hooks.Registry together with the stable
// session identifiers so per-turn callers can emit OnSessionUpdate without
// needing to thread extra parameters through the call stack.
type sessionHookCtxValue struct {
	registry  *hooks.Registry
	sessionID string
	meta      map[string]any
}

// withSessionHookContext returns a child context carrying the session hook
// registry, sessionID, and metadata. Callers downstream can retrieve these
// via sessionHookContextFrom to fire per-turn OnSessionUpdate events.
func withSessionHookContext(
	ctx context.Context,
	reg *hooks.Registry,
	sessionID string,
	meta map[string]any,
) context.Context {
	if reg == nil {
		return ctx
	}
	return context.WithValue(ctx, sessionHookContextKey{}, sessionHookCtxValue{
		registry:  reg,
		sessionID: sessionID,
		meta:      meta,
	})
}

// sessionHookContextFrom extracts the session hook registry, sessionID, and
// metadata from ctx. The bool return is false when no value was set (e.g.
// the Engine has no session hooks registered), allowing callers to skip
// the Update call entirely without any nil-guard boilerplate.
func sessionHookContextFrom(ctx context.Context) (reg *hooks.Registry, sessionID string, meta map[string]any, ok bool) {
	v, found := ctx.Value(sessionHookContextKey{}).(sessionHookCtxValue)
	if !found {
		return nil, "", nil, false
	}
	return v.registry, v.sessionID, v.meta, true
}
