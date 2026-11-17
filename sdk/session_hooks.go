package sdk

import (
	"context"
	"sync/atomic"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// sessionInfoFunc returns the dynamic session state needed to build a
// SessionEvent: the session and conversation IDs, and the transcript when
// withMessages is set. It is called at dispatch time so the event always
// reflects the latest state.
type sessionInfoFunc func(withMessages bool) (sessionID, conversationID string, messages []types.Message)

// sessionHookDispatcher encapsulates session lifecycle hook dispatching.
// It tracks the turn index and builds SessionEvent payloads via a callback,
// keeping the dispatch mechanics separate from Conversation business logic.
//
// All public methods are nil-receiver safe: calling any method on a nil
// *sessionHookDispatcher is a no-op (or returns a zero value).
type sessionHookDispatcher struct {
	registry *hooks.Registry
	info     sessionInfoFunc
	caller   types.Caller
	turns    atomic.Int32
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
	if d.dispatches() {
		_ = d.registry.RunSessionStart(ctx, d.buildEvent(nil))
	}
}

// SessionUpdate dispatches OnSessionUpdate to all registered session hooks.
func (d *sessionHookDispatcher) SessionUpdate(ctx context.Context) {
	if d.dispatches() {
		_ = d.registry.RunSessionUpdate(ctx, d.buildEvent(nil))
	}
}

// SessionUpdateWith dispatches OnSessionUpdate with messages as the event's
// transcript, for a caller that has loaded it already.
func (d *sessionHookDispatcher) SessionUpdateWith(ctx context.Context, messages []types.Message) {
	if d.dispatches() {
		_ = d.registry.RunSessionUpdate(ctx, d.buildEvent(&messages))
	}
}

// SessionEnd dispatches OnSessionEnd to all registered session hooks.
func (d *sessionHookDispatcher) SessionEnd(ctx context.Context) {
	if d.dispatches() {
		_ = d.registry.RunSessionEnd(ctx, d.buildEvent(nil))
	}
}

// dispatches reports whether a session event would reach a registry. The
// registry has already logged each failing hook by name at WARN and run the
// remaining hooks, so a dispatch discards the error: hook failures never
// block SDK operations.
func (d *sessionHookDispatcher) dispatches() bool {
	return d != nil && d.registry != nil
}

// SetTurn sets the turn the next events report. Safe for concurrent use.
func (d *sessionHookDispatcher) SetTurn(n int) {
	if d == nil {
		return
	}
	d.turns.Store(int32(n)) //nolint:gosec // a turn number fits
}

// TurnIndex returns the current turn count. Safe for concurrent use.
func (d *sessionHookDispatcher) TurnIndex() int {
	if d == nil {
		return 0
	}
	return int(d.turns.Load())
}

// buildEvent constructs a hooks.SessionEvent from the current dispatcher
// state plus the info callback. messages, when set, is the transcript, and
// info then loads none.
func (d *sessionHookDispatcher) buildEvent(messages *[]types.Message) hooks.SessionEvent {
	event := hooks.SessionEvent{
		TurnIndex: int(d.turns.Load()),
		Caller:    d.caller,
	}
	if d.info != nil {
		event.SessionID, event.ConversationID, event.Messages = d.info(messages == nil)
	}
	if messages != nil {
		event.Messages = *messages
	}
	return event
}
