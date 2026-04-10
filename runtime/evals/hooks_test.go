package evals

import (
	"context"
	"sync"
	"testing"
)

// recordingHook captures every result it observes for assertions.
type recordingHook struct {
	name    string
	mu      sync.Mutex
	results []EvalResult
	defs    []string
}

func (h *recordingHook) Name() string { return h.name }

func (h *recordingHook) OnEvalResult(
	_ context.Context, def *EvalDef, _ *EvalContext, result *EvalResult,
) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.defs = append(h.defs, def.ID)
	h.results = append(h.results, *result)
}

// mutatingHook annotates the result's Details map to prove hooks can
// observe and mutate results (for redaction / enrichment use cases).
type mutatingHook struct{}

func (m *mutatingHook) Name() string { return "mutator" }

func (m *mutatingHook) OnEvalResult(
	_ context.Context, _ *EvalDef, _ *EvalContext, result *EvalResult,
) {
	if result.Details == nil {
		result.Details = map[string]any{}
	}
	result.Details["hooked"] = true
}

func TestEvalHook_InvokedOnEachResult(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	hook := &recordingHook{name: "rec"}
	runner := NewEvalRunner(reg, WithEvalHook(hook))

	defs := []EvalDef{
		{ID: "e1", Type: "test", Trigger: TriggerEveryTurn},
		{ID: "e2", Type: "test", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	results := runner.RunTurnEvals(context.Background(), defs, evalCtx)

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if len(hook.results) != 2 {
		t.Fatalf("hook saw %d results, want 2", len(hook.results))
	}
	if hook.defs[0] != "e1" || hook.defs[1] != "e2" {
		t.Errorf("hook saw defs %v, want [e1 e2]", hook.defs)
	}
}

func TestEvalHook_MultipleHooksInvokedInOrder(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	h1 := &recordingHook{name: "h1"}
	h2 := &recordingHook{name: "h2"}
	runner := NewEvalRunner(reg, WithEvalHook(h1), WithEvalHook(h2))

	defs := []EvalDef{{ID: "e1", Type: "test", Trigger: TriggerEveryTurn}}
	evalCtx := &EvalContext{SessionID: "s1"}

	_ = runner.RunTurnEvals(context.Background(), defs, evalCtx)

	if len(h1.results) != 1 {
		t.Errorf("h1 saw %d results, want 1", len(h1.results))
	}
	if len(h2.results) != 1 {
		t.Errorf("h2 saw %d results, want 1", len(h2.results))
	}
}

func TestEvalHook_CanMutateResult(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg, WithEvalHook(&mutatingHook{}))

	defs := []EvalDef{{ID: "e1", Type: "test", Trigger: TriggerEveryTurn}}
	evalCtx := &EvalContext{SessionID: "s1"}

	results := runner.RunTurnEvals(context.Background(), defs, evalCtx)

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if got, _ := results[0].Details["hooked"].(bool); !got {
		t.Errorf("expected Details[hooked]=true on returned result, got %v", results[0].Details)
	}
}

func TestEvalHook_InvokedForSessionEvals(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	hook := &recordingHook{name: "rec"}
	runner := NewEvalRunner(reg, WithEvalHook(hook))

	defs := []EvalDef{
		{ID: "e1", Type: "test", Trigger: TriggerOnSessionComplete},
	}
	evalCtx := &EvalContext{SessionID: "s1", TurnIndex: 3}

	_ = runner.RunSessionEvals(context.Background(), defs, evalCtx)

	if len(hook.results) != 1 {
		t.Fatalf("hook saw %d results for session evals, want 1", len(hook.results))
	}
}

// panicHook panics unconditionally — used to prove the runner recovers.
type panicHook struct{}

func (p *panicHook) Name() string { return "panicker" }
func (p *panicHook) OnEvalResult(_ context.Context, _ *EvalDef, _ *EvalContext, _ *EvalResult) {
	panic("boom from hook")
}

func TestEvalHook_PanicIsRecovered(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	observer := &recordingHook{name: "observer"}
	// Place the panicking hook first; observer must still be invoked,
	// and the runner must still return the result.
	runner := NewEvalRunner(reg, WithEvalHook(&panicHook{}), WithEvalHook(observer))

	defs := []EvalDef{{ID: "e1", Type: "test", Trigger: TriggerEveryTurn}}
	evalCtx := &EvalContext{SessionID: "s1"}

	results := runner.RunTurnEvals(context.Background(), defs, evalCtx)
	if len(results) != 1 {
		t.Fatalf("panicking hook should not drop the result, got %d results", len(results))
	}
	if len(observer.results) != 1 {
		t.Errorf("observer hook should still run after earlier hook panics, got %d", len(observer.results))
	}
}

func TestEvalHook_NilHookListIsNoOp(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg) // no hooks

	defs := []EvalDef{{ID: "e1", Type: "test", Trigger: TriggerEveryTurn}}
	evalCtx := &EvalContext{SessionID: "s1"}

	results := runner.RunTurnEvals(context.Background(), defs, evalCtx)
	if len(results) != 1 {
		t.Errorf("got %d results, want 1", len(results))
	}
}
