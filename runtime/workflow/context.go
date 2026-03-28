package workflow

import (
	"time"
)

// MaxHistoryLength is the maximum number of state transitions retained in context history.
// When exceeded, the oldest transitions are discarded.
const MaxHistoryLength = 1000

// NewContext creates a new Context initialized at the given entry state.
func NewContext(entryState string, now time.Time) *Context {
	return &Context{
		CurrentState: entryState,
		History:      []StateTransition{},
		Metadata:     map[string]any{},
		VisitCounts:  map[string]int{entryState: 1},
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
	if len(ctx.History) > MaxHistoryLength {
		trimmed := make([]StateTransition, MaxHistoryLength)
		copy(trimmed, ctx.History[len(ctx.History)-MaxHistoryLength:])
		ctx.History = trimmed
	}
	if ctx.VisitCounts == nil {
		ctx.VisitCounts = make(map[string]int)
	}
	ctx.VisitCounts[to]++
	ctx.CurrentState = to
	ctx.UpdatedAt = ts
}

// Clone returns a deep copy of the Context.
func (ctx *Context) Clone() *Context {
	c := &Context{
		CurrentState:   ctx.CurrentState,
		TotalToolCalls: ctx.TotalToolCalls,
		StartedAt:      ctx.StartedAt,
		UpdatedAt:      ctx.UpdatedAt,
	}
	if ctx.History != nil {
		c.History = make([]StateTransition, len(ctx.History))
		copy(c.History, ctx.History)
	}
	if ctx.Metadata != nil {
		c.Metadata = deepCopyMap(ctx.Metadata)
	}
	if ctx.VisitCounts != nil {
		c.VisitCounts = make(map[string]int, len(ctx.VisitCounts))
		for k, v := range ctx.VisitCounts {
			c.VisitCounts[k] = v
		}
	}
	return c
}

// deepCopyMap performs a deep copy of a map[string]any, recursively copying
// nested maps and slices to prevent shared references between original and clone.
func deepCopyMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = deepCopyValue(v)
	}
	return result
}

// deepCopyValue deep-copies a single value, handling nested maps and slices.
func deepCopyValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return deepCopyMap(val)
	case []any:
		cp := make([]any, len(val))
		for i, item := range val {
			cp[i] = deepCopyValue(item)
		}
		return cp
	default:
		return v
	}
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

// TotalVisits returns the sum of all per-state visit counts.
func (ctx *Context) TotalVisits() int {
	total := 0
	for _, v := range ctx.VisitCounts {
		total += v
	}
	return total
}

// IncrementToolCalls adds n to the workflow-wide tool call counter.
func (ctx *Context) IncrementToolCalls(n int) {
	ctx.TotalToolCalls += n
}
