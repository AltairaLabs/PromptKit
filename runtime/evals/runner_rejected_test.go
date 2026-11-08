package evals

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
)

// countingStub counts its runs.
type countingStub struct{ runs int }

func (h *countingStub) Type() string { return "counting_stub" }
func (h *countingStub) Eval(context.Context, *EvalContext, map[string]any) (*EvalResult, error) {
	h.runs++
	s := 1.0
	return &EvalResult{Score: &s}, nil
}

// A rejected def is an error result in def order with its role, its handler
// never runs, and the trigger and sampling filters still apply to it.
func TestRunner_WithRejectedDefs(t *testing.T) {
	h := &countingStub{}
	reg := newTestRegistry(h)
	reg.Register(&AssertionEvalHandler{registry: reg})
	zero := 0.0
	runner := NewEvalRunner(reg, WithRejectedDefs(map[string]string{
		"bad": "unknown param", "bad-assert": "bad threshold", "bad-session": "x", "bad-unsampled": "x",
	}))
	defs := []EvalDef{
		{ID: "bad", Type: "counting_stub", Trigger: TriggerEveryTurn},
		{ID: "good", Type: "counting_stub", Trigger: TriggerEveryTurn},
		{ID: "bad-assert", Type: WrapperTypeAssertion, Trigger: TriggerEveryTurn},
		{ID: "bad-session", Type: "counting_stub", Trigger: TriggerOnSessionComplete},
		{ID: "bad-unsampled", Type: "counting_stub", Trigger: TriggerSampleTurns, SamplePercentage: &zero},
	}
	results := runner.RunTurnEvals(context.Background(), defs, &EvalContext{SessionID: "s"})
	if got := resultIDs(results); len(got) != 3 || got[0] != "bad" || got[1] != "good" || got[2] != "bad-assert" {
		t.Fatalf("results %v, want [bad good bad-assert]", got)
	}
	if results[0].Error != "unknown param" || results[0].Kind != events.EvalKindEval {
		t.Errorf("bad: %+v", results[0])
	}
	if results[2].Kind != events.EvalKindAssertion || results[2].Error != "bad threshold" {
		t.Errorf("bad-assert: %+v", results[2])
	}
	if h.runs != 1 {
		t.Errorf("handler ran %d times, want 1", h.runs)
	}
	if runner.Clone().rejected["bad"] == "" {
		t.Error("Clone dropped the rejected defs")
	}
}
