package workflow

import (
	"maps"
	"time"
)

// NewContext creates a new Context initialized at the given entry state.
func NewContext(entryState string, now time.Time) *Context {
	return &Context{
		CurrentState: entryState,
		History:      []StateTransition{},
		Metadata:     map[string]any{},
		StartedAt:    now,
		UpdatedAt:    now,
	}
}

// RecordTransition records a state transition and updates the current state.
func (ctx *Context) RecordTransition(from, to, event string, ts time.Time) {
	ctx.History = append(ctx.History, StateTransition{
		From:      from,
		To:        to,
		Event:     event,
		Timestamp: ts,
	})
	ctx.CurrentState = to
	ctx.UpdatedAt = ts
}

// Clone returns a deep copy of the Context.
func (ctx *Context) Clone() *Context {
	c := &Context{
		CurrentState: ctx.CurrentState,
		StartedAt:    ctx.StartedAt,
		UpdatedAt:    ctx.UpdatedAt,
	}
	if ctx.History != nil {
		c.History = make([]StateTransition, len(ctx.History))
		copy(c.History, ctx.History)
	}
	if ctx.Metadata != nil {
		c.Metadata = make(map[string]any, len(ctx.Metadata))
		maps.Copy(c.Metadata, ctx.Metadata)
	}
	return c
}

// TransitionCount returns the number of transitions recorded.
func (ctx *Context) TransitionCount() int {
	return len(ctx.History)
}

// LastTransition returns the most recent transition, or nil if none.
func (ctx *Context) LastTransition() *StateTransition {
	if len(ctx.History) == 0 {
		return nil
	}
	t := ctx.History[len(ctx.History)-1]
	return &t
}
