package workflow

import (
	"errors"
	"testing"
	"time"
)

func linearSpec() *Spec {
	return &Spec{
		Version: 1,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "task_a", OnEvent: map[string]string{"Next": "b"}},
			"b": {PromptTask: "task_b", OnEvent: map[string]string{"Next": "c"}},
			"c": {PromptTask: "task_c"},
		},
	}
}

func branchingSpec() *Spec {
	return &Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*State{
			"start": {PromptTask: "triage", OnEvent: map[string]string{
				"ToBilling":  "billing",
				"ToTechnical": "technical",
			}},
			"billing":   {PromptTask: "billing_task"},
			"technical": {PromptTask: "tech_task"},
		},
	}
}

func loopingSpec() *Spec {
	return &Spec{
		Version: 1,
		Entry:   "draft",
		States: map[string]*State{
			"draft": {PromptTask: "write", OnEvent: map[string]string{
				"Review": "review",
			}},
			"review": {PromptTask: "check", OnEvent: map[string]string{
				"Revise":  "draft",
				"Approve": "done",
			}},
			"done": {PromptTask: "final"},
		},
	}
}

func fixedTime() time.Time {
	return time.Date(2026, 2, 17, 12, 0, 0, 0, time.UTC)
}

func TestNewStateMachine(t *testing.T) {
	sm := NewStateMachine(linearSpec())
	if sm.CurrentState() != "a" {
		t.Errorf("CurrentState = %q, want %q", sm.CurrentState(), "a")
	}
	if sm.CurrentPromptTask() != "task_a" {
		t.Errorf("CurrentPromptTask = %q, want %q", sm.CurrentPromptTask(), "task_a")
	}
}

func TestLinearWorkflow(t *testing.T) {
	sm := NewStateMachine(linearSpec()).WithTimeFunc(func() time.Time { return fixedTime() })

	// a -> b
	if err := sm.ProcessEvent("Next"); err != nil {
		t.Fatalf("ProcessEvent(Next): %v", err)
	}
	if sm.CurrentState() != "b" {
		t.Errorf("CurrentState = %q, want %q", sm.CurrentState(), "b")
	}
	if sm.CurrentPromptTask() != "task_b" {
		t.Errorf("CurrentPromptTask = %q, want %q", sm.CurrentPromptTask(), "task_b")
	}
	if sm.IsTerminal() {
		t.Error("state b should not be terminal")
	}

	// b -> c
	if err := sm.ProcessEvent("Next"); err != nil {
		t.Fatalf("ProcessEvent(Next): %v", err)
	}
	if sm.CurrentState() != "c" {
		t.Errorf("CurrentState = %q, want %q", sm.CurrentState(), "c")
	}
	if !sm.IsTerminal() {
		t.Error("state c should be terminal")
	}

	// c is terminal — further events should fail
	err := sm.ProcessEvent("Next")
	if !errors.Is(err, ErrTerminalState) {
		t.Errorf("expected ErrTerminalState, got: %v", err)
	}
}

func TestBranchingWorkflow(t *testing.T) {
	sm := NewStateMachine(branchingSpec())

	if err := sm.ProcessEvent("ToBilling"); err != nil {
		t.Fatalf("ProcessEvent(ToBilling): %v", err)
	}
	if sm.CurrentState() != "billing" {
		t.Errorf("CurrentState = %q, want %q", sm.CurrentState(), "billing")
	}
	if !sm.IsTerminal() {
		t.Error("billing should be terminal")
	}

	// Start fresh and go the other way
	sm2 := NewStateMachine(branchingSpec())
	if err := sm2.ProcessEvent("ToTechnical"); err != nil {
		t.Fatalf("ProcessEvent(ToTechnical): %v", err)
	}
	if sm2.CurrentState() != "technical" {
		t.Errorf("CurrentState = %q, want %q", sm2.CurrentState(), "technical")
	}
}

func TestLoopingWorkflow(t *testing.T) {
	sm := NewStateMachine(loopingSpec())

	// draft -> review -> draft -> review -> done
	if err := sm.ProcessEvent("Review"); err != nil {
		t.Fatal(err)
	}
	if sm.CurrentState() != "review" {
		t.Fatalf("CurrentState = %q, want review", sm.CurrentState())
	}

	if err := sm.ProcessEvent("Revise"); err != nil {
		t.Fatal(err)
	}
	if sm.CurrentState() != "draft" {
		t.Fatalf("CurrentState = %q, want draft", sm.CurrentState())
	}

	if err := sm.ProcessEvent("Review"); err != nil {
		t.Fatal(err)
	}
	if err := sm.ProcessEvent("Approve"); err != nil {
		t.Fatal(err)
	}
	if sm.CurrentState() != "done" {
		t.Fatalf("CurrentState = %q, want done", sm.CurrentState())
	}
	if !sm.IsTerminal() {
		t.Error("done should be terminal")
	}

	ctx := sm.Context()
	if ctx.TransitionCount() != 4 {
		t.Errorf("TransitionCount = %d, want 4", ctx.TransitionCount())
	}
}

func TestInvalidEvent(t *testing.T) {
	sm := NewStateMachine(linearSpec())
	err := sm.ProcessEvent("Nonexistent")
	if !errors.Is(err, ErrInvalidEvent) {
		t.Errorf("expected ErrInvalidEvent, got: %v", err)
	}
	// State should not change on error
	if sm.CurrentState() != "a" {
		t.Errorf("state should not change on error, got %q", sm.CurrentState())
	}
}

func TestAvailableEvents(t *testing.T) {
	sm := NewStateMachine(branchingSpec())
	events := sm.AvailableEvents()
	if len(events) != 2 {
		t.Fatalf("AvailableEvents len = %d, want 2", len(events))
	}
	// Should be sorted
	if events[0] != "ToBilling" || events[1] != "ToTechnical" {
		t.Errorf("AvailableEvents = %v, want [ToBilling ToTechnical]", events)
	}

	// Terminal state has no events
	sm2 := NewStateMachine(linearSpec())
	_ = sm2.ProcessEvent("Next")
	_ = sm2.ProcessEvent("Next")
	if events := sm2.AvailableEvents(); events != nil {
		t.Errorf("terminal state AvailableEvents = %v, want nil", events)
	}
}

func TestContextSnapshot(t *testing.T) {
	sm := NewStateMachine(linearSpec()).WithTimeFunc(func() time.Time { return fixedTime() })
	_ = sm.ProcessEvent("Next")

	ctx := sm.Context()
	if ctx.CurrentState != "b" {
		t.Errorf("Context.CurrentState = %q, want %q", ctx.CurrentState, "b")
	}
	if ctx.TransitionCount() != 1 {
		t.Errorf("TransitionCount = %d, want 1", ctx.TransitionCount())
	}

	// Mutating the snapshot should not affect the machine
	ctx.CurrentState = "modified"
	if sm.CurrentState() != "b" {
		t.Error("Context snapshot should be independent of machine")
	}
}

func TestRestoreFromContext(t *testing.T) {
	spec := linearSpec()
	sm := NewStateMachine(spec).WithTimeFunc(func() time.Time { return fixedTime() })
	_ = sm.ProcessEvent("Next") // a -> b

	// Persist and restore
	savedCtx := sm.Context()
	sm2 := NewStateMachineFromContext(spec, savedCtx)

	if sm2.CurrentState() != "b" {
		t.Errorf("restored CurrentState = %q, want %q", sm2.CurrentState(), "b")
	}
	if sm2.CurrentPromptTask() != "task_b" {
		t.Errorf("restored CurrentPromptTask = %q, want %q", sm2.CurrentPromptTask(), "task_b")
	}

	// Continue from restored state
	if err := sm2.ProcessEvent("Next"); err != nil {
		t.Fatalf("ProcessEvent after restore: %v", err)
	}
	if sm2.CurrentState() != "c" {
		t.Errorf("state after continue = %q, want %q", sm2.CurrentState(), "c")
	}

	ctx2 := sm2.Context()
	if ctx2.TransitionCount() != 2 {
		t.Errorf("TransitionCount = %d, want 2", ctx2.TransitionCount())
	}
}

func TestTransitionHistoryTimestamps(t *testing.T) {
	callCount := 0
	times := []time.Time{
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}

	sm := NewStateMachine(linearSpec()).WithTimeFunc(func() time.Time {
		t := times[callCount]
		callCount++
		return t
	})

	_ = sm.ProcessEvent("Next")
	_ = sm.ProcessEvent("Next")

	ctx := sm.Context()
	if !ctx.History[0].Timestamp.Equal(times[0]) {
		t.Errorf("History[0].Timestamp = %v, want %v", ctx.History[0].Timestamp, times[0])
	}
	if !ctx.History[1].Timestamp.Equal(times[1]) {
		t.Errorf("History[1].Timestamp = %v, want %v", ctx.History[1].Timestamp, times[1])
	}
}
