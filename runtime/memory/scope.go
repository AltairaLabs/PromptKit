package memory

import "context"

type scopeCtxKey struct{}

// WithScope attaches a conversation's memory scope to the context. The
// [Executor] reads it on each Execute and reads and writes memories under it,
// falling back to the scope it was constructed with when absent.
//
// This is what lets a single memory executor, registered once into a shared
// tools.Registry, serve concurrent conversations without their memories
// crossing. The scope used to be captured at construction, and a Registry
// keeps exactly one executor per name, so the last conversation to register
// owned the memory tool for every conversation in flight -- one conversation's
// remember landed in another's scope and was invisible in its own. It mirrors
// tools.WithMCPRegistry, which exists for the same reason.
// See AltairaLabs/PromptKit#2011.
//
// An empty scope is ignored: installing one would silently widen or narrow
// every read rather than doing nothing.
func WithScope(ctx context.Context, scope map[string]string) context.Context {
	if len(scope) == 0 {
		return ctx
	}
	return context.WithValue(ctx, scopeCtxKey{}, scope)
}

// ScopeFromContext returns the scope attached by [WithScope], or nil when the
// context carries none.
func ScopeFromContext(ctx context.Context) map[string]string {
	if ctx == nil {
		return nil
	}
	scope, _ := ctx.Value(scopeCtxKey{}).(map[string]string)
	return scope
}
