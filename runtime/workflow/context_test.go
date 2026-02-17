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
