package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

func TestTransitionExecutor_DeferredCommit(t *testing.T) {
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "t", OnEvent: map[string]string{"Go": "b"}},
			"b": {PromptTask: "t"},
		},
	}
	sm := NewStateMachine(spec)
	exec := NewTransitionExecutor(sm, spec)

	// Execute stores pending but does NOT transition
	args, _ := json.Marshal(map[string]string{"event": "Go", "context": "test"})
	result, err := exec.Execute(context.Background(), nil, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// State should still be "a"
	if sm.CurrentState() != "a" {
		t.Fatalf("state should still be 'a' after Execute, got %q", sm.CurrentState())
	}

	// Result should indicate scheduled
	var resp map[string]string
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if resp["status"] != "transition_scheduled" {
		t.Errorf("status = %q, want transition_scheduled", resp["status"])
	}

	// Pending should be set
	pending := exec.Pending()
	if pending == nil || pending.Event != "Go" {
		t.Fatalf("pending = %v, want event=Go", pending)
	}

	// CommitPending applies the transition
	tr, err := exec.CommitPending()
	if err != nil {
		t.Fatalf("CommitPending: %v", err)
	}
	if tr.To != "b" {
		t.Errorf("transition to = %q, want 'b'", tr.To)
	}
	if sm.CurrentState() != "b" {
		t.Errorf("state after commit = %q, want 'b'", sm.CurrentState())
	}

	// Pending should be cleared
	if exec.Pending() != nil {
		t.Error("pending should be nil after commit")
	}
}

func TestTransitionExecutor_CommitPendingNoop(t *testing.T) {
	spec := &Spec{
		Version: 1,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "t"},
		},
	}
	sm := NewStateMachine(spec)
	exec := NewTransitionExecutor(sm, spec)

	// CommitPending with no pending transition is a no-op
	tr, err := exec.CommitPending()
	if err != nil {
		t.Fatalf("CommitPending: %v", err)
	}
	if tr != nil {
		t.Errorf("expected nil TransitionResult, got %+v", tr)
	}
}

func TestTransitionExecutor_CommitPendingError(t *testing.T) {
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "t", OnEvent: map[string]string{"Go": "b"}},
			"b": {PromptTask: "t"},
		},
	}
	sm := NewStateMachine(spec)
	exec := NewTransitionExecutor(sm, spec)

	// Store a bad event
	args, _ := json.Marshal(map[string]string{"event": "BadEvent", "context": ""})
	_, err := exec.Execute(context.Background(), nil, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// CommitPending should fail
	_, err = exec.CommitPending()
	if err == nil {
		t.Fatal("expected error for invalid event")
	}
	if !errors.Is(err, ErrInvalidEvent) {
		t.Errorf("expected ErrInvalidEvent, got: %v", err)
	}

	// Pending should be cleared even on error
	if exec.Pending() != nil {
		t.Error("pending should be cleared after failed commit")
	}
}

func TestTransitionExecutor_ClearPending(t *testing.T) {
	spec := &Spec{
		Version: 1,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "t", OnEvent: map[string]string{"Go": "b"}},
			"b": {PromptTask: "t"},
		},
	}
	sm := NewStateMachine(spec)
	exec := NewTransitionExecutor(sm, spec)

	args, _ := json.Marshal(map[string]string{"event": "Go", "context": ""})
	_, _ = exec.Execute(context.Background(), nil, args)

	exec.ClearPending()
	if exec.Pending() != nil {
		t.Error("pending should be nil after ClearPending")
	}
	if sm.CurrentState() != "a" {
		t.Error("state should remain 'a' after ClearPending")
	}
}

func TestTransitionExecutor_RegisterForState(t *testing.T) {
	spec := &Spec{
		Version: 1,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "t", OnEvent: map[string]string{"Go": "b"}},
			"b": {PromptTask: "t"},
		},
	}
	sm := NewStateMachine(spec)
	exec := NewTransitionExecutor(sm, spec)

	registry := newTestRegistry(t)
	exec.RegisterForState(registry, spec.States["a"])

	tool := registry.Get(TransitionToolName)
	if tool == nil {
		t.Fatal("transition tool should be registered")
	}
	if tool.Mode != TransitionExecutorMode {
		t.Errorf("tool Mode = %q, want %q", tool.Mode, TransitionExecutorMode)
	}
}

func TestTransitionExecutor_SkipsTerminalState(t *testing.T) {
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "t", Terminal: true},
		},
	}
	sm := NewStateMachine(spec)
	exec := NewTransitionExecutor(sm, spec)

	registry := newTestRegistry(t)
	exec.RegisterForState(registry, spec.States["a"])

	if registry.Get(TransitionToolName) != nil {
		t.Error("transition tool should not be registered for terminal state")
	}
}

func TestTransitionExecutor_MaxVisitsRedirect(t *testing.T) {
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "t", OnEvent: map[string]string{"Loop": "b"}},
			"b": {PromptTask: "t", MaxVisits: 1, OnMaxVisits: "done",
				OnEvent: map[string]string{"Loop": "b"}},
			"done": {PromptTask: "t"},
		},
	}
	sm := NewStateMachine(spec)
	exec := NewTransitionExecutor(sm, spec)

	// First: a → b (visit 1)
	args, _ := json.Marshal(map[string]string{"event": "Loop", "context": ""})
	_, _ = exec.Execute(context.Background(), nil, args)
	tr, err := exec.CommitPending()
	if err != nil {
		t.Fatalf("first commit: %v", err)
	}
	if tr.To != "b" {
		t.Fatalf("first commit to = %q, want 'b'", tr.To)
	}

	// Second: b → b, but max_visits=1, redirect to done
	args2, _ := json.Marshal(map[string]string{"event": "Loop", "context": ""})
	_, _ = exec.Execute(context.Background(), nil, args2)
	tr2, err := exec.CommitPending()
	if err != nil {
		t.Fatalf("second commit: %v", err)
	}
	if tr2.To != "done" {
		t.Errorf("second commit to = %q, want 'done'", tr2.To)
	}
	if !tr2.Redirected {
		t.Error("expected redirect")
	}
}

// newTestRegistry creates a minimal tools.Registry for testing.
func newTestRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	return tools.NewRegistry()
}
