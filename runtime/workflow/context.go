package workflow

import (
	"maps"
	"slices"
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

	// Snapshot current artifact values at this transition
	if len(ctx.Artifacts) > 0 {
		snapshot := ArtifactSnapshot{
			FromState: from,
			ToState:   to,
			Event:     event,
			Values:    make(map[string]string, len(ctx.Artifacts)),
			Timestamp: ts,
		}
		for k, v := range ctx.Artifacts {
			snapshot.Values[k] = v
		}
		ctx.ArtifactHistory = append(ctx.ArtifactHistory, snapshot)
	}

	ctx.CurrentState = to
	ctx.UpdatedAt = ts
}

// Clone returns a deep copy of the Context. Nil collections on the source are
// preserved as nil on the clone so callers can distinguish "absent" from
// "present but empty" across the round-trip.
func (ctx *Context) Clone() *Context {
	return &Context{
		CurrentState:    ctx.CurrentState,
		TotalToolCalls:  ctx.TotalToolCalls,
		StartedAt:       ctx.StartedAt,
		UpdatedAt:       ctx.UpdatedAt,
		History:         slices.Clone(ctx.History),
		Metadata:        cloneMetadataMap(ctx.Metadata),
		VisitCounts:     maps.Clone(ctx.VisitCounts),
		Artifacts:       maps.Clone(ctx.Artifacts),
		ArtifactHistory: cloneArtifactHistory(ctx.ArtifactHistory),
	}
}

// cloneMetadataMap is the entry point for deep-copying Metadata. It preserves
// nil input as nil output rather than producing an empty map, to keep Clone's
// input-output shape identical.
func cloneMetadataMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	return deepCopyMap(m)
}

// cloneArtifactHistory deep-copies the slice, ensuring each snapshot's
// Values map is also an independent copy so callers can mutate the clone
// without affecting the original.
func cloneArtifactHistory(src []ArtifactSnapshot) []ArtifactSnapshot {
	if src == nil {
		return nil
	}
	dst := make([]ArtifactSnapshot, len(src))
	for i, s := range src {
		dst[i] = s
		dst[i].Values = maps.Clone(s.Values)
	}
	return dst
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

// SetArtifact sets an artifact value, respecting the mode (replace or append).
func (ctx *Context) SetArtifact(name, value, mode string) {
	if ctx.Artifacts == nil {
		ctx.Artifacts = make(map[string]string)
	}
	if mode == "append" {
		ctx.Artifacts[name] += value
	} else {
		ctx.Artifacts[name] = value
	}
}

// GetArtifact returns an artifact value, or empty string if not set.
func (ctx *Context) GetArtifact(name string) string {
	return ctx.Artifacts[name]
}
