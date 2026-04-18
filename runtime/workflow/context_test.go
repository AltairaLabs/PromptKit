package workflow

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewContext(t *testing.T) {
	now := time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC)
	ctx := NewContext("intake", now)

	if ctx.CurrentState != "intake" {
		t.Errorf("CurrentState = %q, want %q", ctx.CurrentState, "intake")
	}
	if len(ctx.History) != 0 {
		t.Errorf("History should be empty, got %d", len(ctx.History))
	}
	if ctx.Metadata == nil {
		t.Error("Metadata should be initialized")
	}
	if !ctx.StartedAt.Equal(now) {
		t.Errorf("StartedAt = %v, want %v", ctx.StartedAt, now)
	}
	if !ctx.UpdatedAt.Equal(now) {
		t.Errorf("UpdatedAt = %v, want %v", ctx.UpdatedAt, now)
	}
}

func TestRecordTransition(t *testing.T) {
	now := time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC)
	ctx := NewContext("intake", now)

	t1 := now.Add(time.Minute)
	ctx.RecordTransition("intake", "solving", "IssueUnderstood", t1)

	if ctx.CurrentState != "solving" {
		t.Errorf("CurrentState = %q, want %q", ctx.CurrentState, "solving")
	}
	if len(ctx.History) != 1 {
		t.Fatalf("History len = %d, want 1", len(ctx.History))
	}
	if ctx.History[0].From != "intake" || ctx.History[0].To != "solving" {
		t.Errorf("transition = %v, want intake->solving", ctx.History[0])
	}
	if !ctx.UpdatedAt.Equal(t1) {
		t.Errorf("UpdatedAt = %v, want %v", ctx.UpdatedAt, t1)
	}

	// Record a second transition
	t2 := now.Add(2 * time.Minute)
	ctx.RecordTransition("solving", "confirmation", "SolutionAccepted", t2)

	if ctx.CurrentState != "confirmation" {
		t.Errorf("CurrentState = %q, want %q", ctx.CurrentState, "confirmation")
	}
	if len(ctx.History) != 2 {
		t.Fatalf("History len = %d, want 2", len(ctx.History))
	}
}

func TestClone(t *testing.T) {
	now := time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC)
	ctx := NewContext("intake", now)
	ctx.Metadata["key"] = "value"
	ctx.RecordTransition("intake", "solving", "IssueUnderstood", now.Add(time.Minute))

	clone := ctx.Clone()

	// Verify equal values
	if clone.CurrentState != ctx.CurrentState {
		t.Errorf("CurrentState = %q, want %q", clone.CurrentState, ctx.CurrentState)
	}
	if len(clone.History) != len(ctx.History) {
		t.Errorf("History len = %d, want %d", len(clone.History), len(ctx.History))
	}
	if clone.Metadata["key"] != "value" {
		t.Errorf("Metadata[key] = %v, want %q", clone.Metadata["key"], "value")
	}

	// Verify independence — mutating clone doesn't affect original
	clone.CurrentState = "modified"
	clone.Metadata["key"] = "changed"
	clone.History = append(clone.History, StateTransition{From: "x", To: "y", Event: "E"})

	if ctx.CurrentState != "solving" {
		t.Error("original CurrentState was mutated")
	}
	if ctx.Metadata["key"] != "value" {
		t.Error("original Metadata was mutated")
	}
	if len(ctx.History) != 1 {
		t.Error("original History was mutated")
	}
}

func TestCloneNilFields(t *testing.T) {
	ctx := &Context{CurrentState: "s"}
	clone := ctx.Clone()

	if clone.History != nil {
		t.Error("History should be nil when original is nil")
	}
	if clone.Metadata != nil {
		t.Error("Metadata should be nil when original is nil")
	}
}

func TestTransitionCount(t *testing.T) {
	now := time.Now()
	ctx := NewContext("a", now)
	if ctx.TransitionCount() != 0 {
		t.Errorf("TransitionCount = %d, want 0", ctx.TransitionCount())
	}

	ctx.RecordTransition("a", "b", "E1", now)
	ctx.RecordTransition("b", "c", "E2", now)
	if ctx.TransitionCount() != 2 {
		t.Errorf("TransitionCount = %d, want 2", ctx.TransitionCount())
	}
}

func TestLastTransition(t *testing.T) {
	now := time.Now()
	ctx := NewContext("a", now)

	if ctx.LastTransition() != nil {
		t.Error("LastTransition should be nil with no history")
	}

	ctx.RecordTransition("a", "b", "E1", now)
	ctx.RecordTransition("b", "c", "E2", now.Add(time.Second))

	last := ctx.LastTransition()
	if last == nil {
		t.Fatal("LastTransition should not be nil")
	}
	if last.From != "b" || last.To != "c" || last.Event != "E2" {
		t.Errorf("LastTransition = %+v, want b->c via E2", last)
	}

	// Verify returned value is a copy
	last.From = "modified"
	if ctx.History[1].From != "b" {
		t.Error("LastTransition should return a copy")
	}
}

func TestCloneDeepCopiesVisitCounts(t *testing.T) {
	ctx := NewContext("a", time.Now())
	ctx.VisitCounts["a"] = 3
	ctx.VisitCounts["b"] = 7

	clone := ctx.Clone()

	if clone.VisitCounts["a"] != 3 || clone.VisitCounts["b"] != 7 {
		t.Errorf("VisitCounts not copied: got %+v", clone.VisitCounts)
	}

	// Mutate clone, verify original is untouched.
	clone.VisitCounts["a"] = 99
	clone.VisitCounts["c"] = 1
	if ctx.VisitCounts["a"] != 3 {
		t.Errorf("original VisitCounts[a] = %d, want 3", ctx.VisitCounts["a"])
	}
	if _, ok := ctx.VisitCounts["c"]; ok {
		t.Error("original VisitCounts should not have key c")
	}
}

func TestCloneDeepCopiesArtifacts(t *testing.T) {
	ctx := NewContext("a", time.Now())
	ctx.Artifacts = map[string]string{
		"findings": "initial research",
		"plan":     "step one",
	}

	clone := ctx.Clone()

	if clone.Artifacts["findings"] != "initial research" {
		t.Errorf("Artifacts[findings] = %q", clone.Artifacts["findings"])
	}

	// Mutate clone, verify independence.
	clone.Artifacts["findings"] = "changed"
	clone.Artifacts["new"] = "added"
	if ctx.Artifacts["findings"] != "initial research" {
		t.Error("original Artifacts[findings] was mutated")
	}
	if _, ok := ctx.Artifacts["new"]; ok {
		t.Error("original Artifacts should not have key new")
	}
}

func TestCloneDeepCopiesArtifactHistoryWithNestedValues(t *testing.T) {
	now := time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC)
	ctx := NewContext("a", now)
	ctx.ArtifactHistory = []ArtifactSnapshot{
		{
			FromState: "a",
			ToState:   "b",
			Event:     "Next",
			Timestamp: now,
			Values:    map[string]string{"k1": "v1", "k2": "v2"},
		},
		{
			FromState: "b",
			ToState:   "c",
			Event:     "Done",
			Timestamp: now.Add(time.Minute),
			Values:    nil, // explicitly nil to exercise the nil branch
		},
	}

	clone := ctx.Clone()

	if len(clone.ArtifactHistory) != 2 {
		t.Fatalf("ArtifactHistory len = %d, want 2", len(clone.ArtifactHistory))
	}
	if clone.ArtifactHistory[0].Values["k1"] != "v1" {
		t.Errorf("snapshot[0].Values[k1] = %q", clone.ArtifactHistory[0].Values["k1"])
	}
	if clone.ArtifactHistory[1].Values != nil {
		t.Errorf("snapshot[1].Values should be nil, got %+v", clone.ArtifactHistory[1].Values)
	}

	// Mutate nested map on clone — original should not be affected.
	clone.ArtifactHistory[0].Values["k1"] = "changed"
	clone.ArtifactHistory[0].Values["k3"] = "added"
	if ctx.ArtifactHistory[0].Values["k1"] != "v1" {
		t.Error("original snapshot[0].Values[k1] was mutated")
	}
	if _, ok := ctx.ArtifactHistory[0].Values["k3"]; ok {
		t.Error("original snapshot[0].Values should not have k3")
	}
}

func TestCloneDeepCopiesNestedMetadata(t *testing.T) {
	now := time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC)
	ctx := NewContext("intake", now)
	ctx.Metadata["nested"] = map[string]any{
		"inner_key": "inner_value",
		"numbers":   []any{1.0, 2.0, 3.0},
	}
	ctx.Metadata["list"] = []any{"a", "b", map[string]any{"deep": true}}

	clone := ctx.Clone()

	// Verify values are equal.
	nestedOrig := ctx.Metadata["nested"].(map[string]any)
	nestedClone := clone.Metadata["nested"].(map[string]any)
	if nestedClone["inner_key"] != "inner_value" {
		t.Errorf("nested inner_key = %v, want inner_value", nestedClone["inner_key"])
	}

	// Mutate the clone's nested map and verify original is unaffected.
	nestedClone["inner_key"] = "changed"
	if nestedOrig["inner_key"] != "inner_value" {
		t.Error("original nested map was mutated via clone")
	}

	// Mutate nested slice in clone.
	cloneList := clone.Metadata["list"].([]any)
	cloneList[0] = "z"
	origList := ctx.Metadata["list"].([]any)
	if origList[0] != "a" {
		t.Error("original slice was mutated via clone")
	}

	// Mutate deeply nested map inside slice.
	cloneDeep := cloneList[2].(map[string]any)
	cloneDeep["deep"] = false
	origDeep := origList[2].(map[string]any)
	if origDeep["deep"] != true {
		t.Error("original deeply nested map in slice was mutated via clone")
	}
}

func TestHistoryCap(t *testing.T) {
	now := time.Now()
	ctx := NewContext("s0", now)

	// Record more than MaxHistoryLength transitions.
	for i := 0; i < MaxHistoryLength+100; i++ {
		from := "s0"
		to := "s1"
		if i%2 == 1 {
			from = "s1"
			to = "s0"
		}
		ctx.RecordTransition(from, to, "E", now.Add(time.Duration(i)*time.Millisecond))
	}

	if len(ctx.History) != MaxHistoryLength {
		t.Errorf("History len = %d, want %d", len(ctx.History), MaxHistoryLength)
	}

	// Verify the oldest entries were trimmed (first kept entry should be index 100 of the original).
	// The 100th transition (0-indexed) has index 100 which is even -> from=s0, to=s1.
	first := ctx.History[0]
	if first.From != "s0" || first.To != "s1" {
		t.Errorf("first kept transition = %s->%s, want s0->s1", first.From, first.To)
	}
}

func TestContextRoundTripThroughJSON(t *testing.T) {
	now := time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC)
	ctx := NewContext("intake", now)
	ctx.Metadata["count"] = float64(42)
	ctx.RecordTransition("intake", "solving", "IssueUnderstood", now.Add(time.Minute))

	data, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored Context
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if restored.CurrentState != ctx.CurrentState {
		t.Errorf("CurrentState = %q, want %q", restored.CurrentState, ctx.CurrentState)
	}
	if restored.TransitionCount() != ctx.TransitionCount() {
		t.Errorf("TransitionCount = %d, want %d", restored.TransitionCount(), ctx.TransitionCount())
	}
	if restored.Metadata["count"] != float64(42) {
		t.Errorf("Metadata[count] = %v, want 42", restored.Metadata["count"])
	}
	if !restored.StartedAt.Equal(ctx.StartedAt) {
		t.Errorf("StartedAt = %v, want %v", restored.StartedAt, ctx.StartedAt)
	}
}
