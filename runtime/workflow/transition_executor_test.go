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

// TestTransitionExecutor_UnregistersOnTerminalEntry verifies that
// transitioning into a terminal state tears down the previous state's
// workflow__transition descriptor. Without this, the LLM would still
// see the stale tool (with the prior state's event enum) on the next
// turn and try to call it against a state with no exits.
func TestTransitionExecutor_UnregistersOnTerminalEntry(t *testing.T) {
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a":    {PromptTask: "t", OnEvent: map[string]string{"Finish": "done"}},
			"done": {PromptTask: "t", Terminal: true},
		},
	}
	sm := NewStateMachine(spec)
	exec := NewTransitionExecutor(sm, spec)
	registry := newTestRegistry(t)

	// First register for the entry state — descriptor lands in the registry.
	exec.RegisterForState(registry, spec.States["a"])
	if registry.Get(TransitionToolName) == nil {
		t.Fatal("transition tool should be registered for state a")
	}

	// Now register for the terminal state — the descriptor must be torn
	// down so the LLM can't see it on the next turn.
	exec.RegisterForState(registry, spec.States["done"])
	if registry.Get(TransitionToolName) != nil {
		t.Error("transition tool should be unregistered after entering terminal state")
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

func TestTransitionExecutor_AgentControlEagerCommit(t *testing.T) {
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "t-a", OnEvent: map[string]string{"Go": "b"}},
			"b": {
				PromptTask:  "t-b",
				Description: "B is agent-controlled — keep the turn",
				Control:     ControlModeAgent,
				OnEvent:     map[string]string{"Finish": "done"},
			},
			"done": {PromptTask: "t-done", Terminal: true},
		},
	}
	sm := NewStateMachine(spec)
	exec := NewTransitionExecutor(sm, spec)

	var committed []*TransitionResult
	exec.SetOnCommit(func(tr *TransitionResult) {
		committed = append(committed, tr)
	})

	args, _ := json.Marshal(map[string]string{"event": "Go", "context": "test"})
	raw, err := exec.Execute(context.Background(), nil, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if sm.CurrentState() != "b" {
		t.Errorf("state after eager commit = %q, want 'b'", sm.CurrentState())
	}
	if exec.Pending() != nil {
		t.Error("pending must be nil after eager commit")
	}
	if len(committed) != 1 || committed[0].To != "b" {
		t.Errorf("onCommit not fired correctly: %+v", committed)
	}

	// CommitPending must be a no-op now.
	tr, err := exec.CommitPending()
	if err != nil {
		t.Fatalf("CommitPending: %v", err)
	}
	if tr != nil {
		t.Errorf("CommitPending must return nil after eager commit, got %+v", tr)
	}

	// Response must include the new state's metadata so the LLM can act on it.
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "transitioned" {
		t.Errorf("status = %q, want 'transitioned'", resp["status"])
	}
	if resp["to"] != "b" {
		t.Errorf("to = %q, want 'b'", resp["to"])
	}
	if resp["prompt_task"] != "t-b" {
		t.Errorf("prompt_task = %v, want 't-b'", resp["prompt_task"])
	}
	if resp["description"] == nil {
		t.Errorf("description must be populated")
	}
	events, ok := resp["available_events"].([]any)
	if !ok || len(events) != 1 || events[0] != "Finish" {
		t.Errorf("available_events = %v, want ['Finish']", resp["available_events"])
	}
}

func TestTransitionExecutor_UserControlStillDefers(t *testing.T) {
	// Default Control ("") must behave exactly like the deferred-commit path.
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "t", OnEvent: map[string]string{"Go": "b"}},
			"b": {PromptTask: "t"}, // Control == "" → user (default)
		},
	}
	sm := NewStateMachine(spec)
	exec := NewTransitionExecutor(sm, spec)

	var committed int
	exec.SetOnCommit(func(*TransitionResult) { committed++ })

	args, _ := json.Marshal(map[string]string{"event": "Go", "context": ""})
	_, err := exec.Execute(context.Background(), nil, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if sm.CurrentState() != "a" {
		t.Errorf("state must not advance during deferred Execute")
	}
	if exec.Pending() == nil {
		t.Error("pending must be set for user-controlled target")
	}
	if committed != 0 {
		t.Errorf("onCommit must not fire on Execute for user-controlled targets, fired %d times", committed)
	}

	if _, err := exec.CommitPending(); err != nil {
		t.Fatalf("CommitPending: %v", err)
	}
	if committed != 1 {
		t.Errorf("onCommit must fire on CommitPending, fired %d times", committed)
	}
	if sm.CurrentState() != "b" {
		t.Errorf("state after CommitPending = %q, want 'b'", sm.CurrentState())
	}
}

func TestTransitionExecutor_OnCommitErrorFiresOnEagerFailure(t *testing.T) {
	// agent-controlled state "loop" with max_visits=1 (no on_max_visits).
	// Entry to "loop" via a→loop consumes the only visit; a second eager
	// loop→loop trips max_visits and surfaces via OnCommitError.
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "t", OnEvent: map[string]string{"Enter": "loop"}},
			"loop": {
				PromptTask: "t",
				MaxVisits:  1,
				Control:    ControlModeAgent,
				OnEvent:    map[string]string{"Again": "loop"},
			},
		},
	}
	sm := NewStateMachine(spec)
	exec := NewTransitionExecutor(sm, spec)

	type capture struct {
		event string
		err   error
	}
	var got []capture
	exec.SetOnCommitError(func(event string, err error) {
		got = append(got, capture{event, err})
	})

	enter, _ := json.Marshal(map[string]string{"event": "Enter"})
	if _, err := exec.Execute(context.Background(), nil, enter); err != nil {
		t.Fatalf("entering loop: %v", err)
	}
	// Second eager attempt loops back to "loop" — trips max_visits.
	again, _ := json.Marshal(map[string]string{"event": "Again"})
	if _, err := exec.Execute(context.Background(), nil, again); err == nil {
		t.Fatal("expected eager re-entry to fail")
	}
	if len(got) != 1 || got[0].event != "Again" {
		t.Fatalf("OnCommitError must fire once with event=Again, got %+v", got)
	}
	if !errors.Is(got[0].err, ErrMaxVisitsExceeded) {
		t.Errorf("expected ErrMaxVisitsExceeded, got %v", got[0].err)
	}
}

func TestTransitionExecutor_OnCommitErrorFiresOnDeferredFailure(t *testing.T) {
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

	var fired int
	var capturedEvent string
	exec.SetOnCommitError(func(event string, _ error) {
		fired++
		capturedEvent = event
	})

	// Store a bad event (deferred path — target lookup at commit time
	// would fail because "BadEvent" isn't in OnEvent).
	args, _ := json.Marshal(map[string]string{"event": "BadEvent"})
	if _, err := exec.Execute(context.Background(), nil, args); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, err := exec.CommitPending(); err == nil {
		t.Fatal("expected commit to fail for invalid event")
	}
	if fired != 1 || capturedEvent != "BadEvent" {
		t.Errorf("OnCommitError must fire once with event=BadEvent, got fired=%d event=%q", fired, capturedEvent)
	}
}

func TestTransitionExecutor_AgentControlChainedTransitions(t *testing.T) {
	// Two agent-controlled transitions in sequence — proves the executor stays
	// usable after an eager commit and the second call uses the post-commit
	// source state to resolve its target.
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "t", OnEvent: map[string]string{"ToB": "b"}},
			"b": {
				PromptTask: "t",
				Control:    ControlModeAgent,
				OnEvent:    map[string]string{"ToC": "c"},
			},
			"c": {
				PromptTask: "t",
				Control:    ControlModeAgent,
				OnEvent:    map[string]string{"ToDone": "done"},
			},
			"done": {PromptTask: "t", Terminal: true},
		},
	}
	sm := NewStateMachine(spec)
	exec := NewTransitionExecutor(sm, spec)

	first, _ := json.Marshal(map[string]string{"event": "ToB"})
	if _, err := exec.Execute(context.Background(), nil, first); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if sm.CurrentState() != "b" {
		t.Fatalf("after first eager commit, state = %q want 'b'", sm.CurrentState())
	}

	second, _ := json.Marshal(map[string]string{"event": "ToC"})
	if _, err := exec.Execute(context.Background(), nil, second); err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if sm.CurrentState() != "c" {
		t.Fatalf("after second eager commit, state = %q want 'c'", sm.CurrentState())
	}
	if exec.Pending() != nil {
		t.Error("pending must remain nil across chained eager commits")
	}
}

// TestTransitionExecutor_HostExtras_Deferred verifies that fields the LLM
// supplies beyond the typed schema (e.g. via sdk.WithToolDescriptorOverride)
// survive the deferred-commit path and reach the OnCommit callback on the
// TransitionResult.
func TestTransitionExecutor_HostExtras_Deferred(t *testing.T) {
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

	var observed *TransitionResult
	exec.SetOnCommit(func(tr *TransitionResult) { observed = tr })

	args := json.RawMessage(`{"event":"Go","context":"ctx","escalation_reason":"vip","priority":3}`)
	if _, err := exec.Execute(context.Background(), nil, args); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	pending := exec.Pending()
	if pending == nil {
		t.Fatal("expected pending transition")
	}
	if got := pending.HostExtras["escalation_reason"]; got != "vip" {
		t.Errorf("PendingTransition.HostExtras[escalation_reason] = %v, want vip", got)
	}

	if _, err := exec.CommitPending(); err != nil {
		t.Fatalf("CommitPending: %v", err)
	}
	if observed == nil {
		t.Fatal("OnCommit was not called")
	}
	if got := observed.HostExtras["escalation_reason"]; got != "vip" {
		t.Errorf("TransitionResult.HostExtras[escalation_reason] = %v, want vip", got)
	}
	if got, ok := observed.HostExtras["priority"].(float64); !ok || got != 3 {
		t.Errorf("TransitionResult.HostExtras[priority] = %v (%T), want 3", observed.HostExtras["priority"], observed.HostExtras["priority"])
	}
}

// TestTransitionExecutor_HostExtras_Eager verifies extras flow through the
// agent-controlled (eager) commit path and reach OnCommit on the same
// pipeline turn.
func TestTransitionExecutor_HostExtras_Eager(t *testing.T) {
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "t", OnEvent: map[string]string{"Go": "b"}},
			"b": {PromptTask: "t", Control: ControlModeAgent},
		},
	}
	sm := NewStateMachine(spec)
	exec := NewTransitionExecutor(sm, spec)

	var observed *TransitionResult
	exec.SetOnCommit(func(tr *TransitionResult) { observed = tr })

	args := json.RawMessage(`{"event":"Go","escalation_reason":"vip"}`)
	if _, err := exec.Execute(context.Background(), nil, args); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if observed == nil {
		t.Fatal("OnCommit was not called on eager path")
	}
	if got := observed.HostExtras["escalation_reason"]; got != "vip" {
		t.Errorf("eager TransitionResult.HostExtras[escalation_reason] = %v, want vip", got)
	}
}

// TestTransitionExecutor_HostExtras_NoneWhenAbsent verifies that args
// without extras leave HostExtras nil — the field is a passthrough channel,
// not a side-effect of every call.
func TestTransitionExecutor_HostExtras_NoneWhenAbsent(t *testing.T) {
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

	var observed *TransitionResult
	exec.SetOnCommit(func(tr *TransitionResult) { observed = tr })

	args, _ := json.Marshal(map[string]string{"event": "Go", "context": "ctx"})
	if _, err := exec.Execute(context.Background(), nil, args); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if exec.Pending().HostExtras != nil {
		t.Errorf("expected nil HostExtras when no extras present, got %v", exec.Pending().HostExtras)
	}
	if _, err := exec.CommitPending(); err != nil {
		t.Fatalf("CommitPending: %v", err)
	}
	if observed.HostExtras != nil {
		t.Errorf("expected nil HostExtras on TransitionResult, got %v", observed.HostExtras)
	}
}
