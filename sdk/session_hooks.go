package sdk

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// sessionInfoFunc returns the dynamic session state needed to build a SessionEvent.
// It is called at dispatch time so the event always reflects the latest state.
type sessionInfoFunc func() (sessionID, conversationID string, messages []types.Message)

// sessionHookDispatcher encapsulates session lifecycle hook dispatching.
// It tracks the turn index and builds SessionEvent payloads via a callback,
// keeping the dispatch mechanics separate from Conversation business logic.
//
// All public methods are nil-receiver safe: calling any method on a nil
// *sessionHookDispatcher is a no-op (or returns a zero value).
type sessionHookDispatcher struct {
	registry *hooks.Registry
	info     sessionInfoFunc
	turns    int
}

// newSessionHookDispatcher creates a dispatcher that will call info() to gather
// session metadata whenever it needs to build a hooks.SessionEvent.
// Passing a nil registry is valid and produces a no-op dispatcher.
func newSessionHookDispatcher(registry *hooks.Registry, info sessionInfoFunc) *sessionHookDispatcher {
	return &sessionHookDispatcher{
		registry: registry,
		info:     info,
	}
}

// SessionStart dispatches OnSessionStart to all registered session hooks.
func (d *sessionHookDispatcher) SessionStart(ctx context.Context) {
	if d == nil {
		return
	}
	d.dispatch(ctx, func(ctx context.Context, e hooks.SessionEvent) error {
		return d.registry.RunSessionStart(ctx, e)
	})
}

// SessionUpdate dispatches OnSessionUpdate to all registered session hooks.
func (d *sessionHookDispatcher) SessionUpdate(ctx context.Context) {
	if d == nil {
		return
	}
	d.dispatch(ctx, func(ctx context.Context, e hooks.SessionEvent) error {
		return d.registry.RunSessionUpdate(ctx, e)
	})
}

// SessionEnd dispatches OnSessionEnd to all registered session hooks.
func (d *sessionHookDispatcher) SessionEnd(ctx context.Context) {
	if d == nil {
		return
	}
	d.dispatch(ctx, func(ctx context.Context, e hooks.SessionEvent) error {
		return d.registry.RunSessionEnd(ctx, e)
	})
}

// IncrementTurn advances the turn counter by one.
func (d *sessionHookDispatcher) IncrementTurn() {
	if d == nil {
		return
	}
	d.turns++
}

// TurnIndex returns the current turn count.
func (d *sessionHookDispatcher) TurnIndex() int {
	if d == nil {
		return 0
	}
	return d.turns
}

// dispatch builds a SessionEvent and calls fn. Errors from hooks are intentionally
// discarded (they are logged inside the registry) so that hook failures never block
// SDK operations.
func (d *sessionHookDispatcher) dispatch(
	ctx context.Context,
	fn func(context.Context, hooks.SessionEvent) error,
) {
	if d.registry == nil {
		return
	}
	_ = fn(ctx, d.buildEvent())
}

// buildEvent constructs a hooks.SessionEvent from the current dispatcher state
// plus the dynamic info callback.
func (d *sessionHookDispatcher) buildEvent() hooks.SessionEvent {
	event := hooks.SessionEvent{
		TurnIndex: d.turns,
	}
	if d.info != nil {
		event.SessionID, event.ConversationID, event.Messages = d.info()
	}
	return event
}
