package evals

import (
	"context"
	"testing"
)

// An on_workflow_step eval runs when a workflow step is dispatched, and only
// then: the turn, session and conversation dispatches must not pick it up, and
// the step dispatch must not pick up theirs.
func TestRunWorkflowStepEvals_RunsOnlyWorkflowStepTrigger(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)

	defs := []EvalDef{
		{ID: "step", Type: "test", Trigger: TriggerOnWorkflowStep},
		{ID: "turn", Type: "test", Trigger: TriggerEveryTurn},
		{ID: "session", Type: "test", Trigger: TriggerOnSessionComplete},
		{ID: "conversation", Type: "test", Trigger: TriggerOnConversationComplete},
	}
	evalCtx := &EvalContext{SessionID: "s1", TurnIndex: 3}

	results := runner.RunWorkflowStepEvals(context.Background(), defs, evalCtx)
	if len(results) != 1 || results[0].EvalID != "step" {
		t.Fatalf("workflow step dispatch ran %v, want only [step]", resultIDs(results))
	}
	if results[0].TurnIndex != 3 || results[0].SessionID != "s1" {
		t.Errorf("result attributed to session %q turn %d, want s1 turn 3",
			results[0].SessionID, results[0].TurnIndex)
	}

	for name, run := range map[string]func(context.Context, []EvalDef, *EvalContext) []EvalResult{
		"turn":         runner.RunTurnEvals,
		"session":      runner.RunSessionEvals,
		"conversation": runner.RunConversationEvals,
	} {
		for _, r := range run(context.Background(), defs, &EvalContext{SessionID: "s1"}) {
			if r.EvalID == "step" {
				t.Errorf("%s dispatch ran the on_workflow_step eval", name)
			}
		}
	}
}

// enabled:false disables a workflow-step eval like any other; a
// sample_percentage is ignored, as the PromptPack spec says it is for every
// trigger other than sample_turns and sample_sessions.
func TestRunWorkflowStepEvals_HonorsEnabledIgnoresSamplePercentage(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)

	disabled := false
	zero := 0.0
	defs := []EvalDef{
		{ID: "off", Type: "test", Trigger: TriggerOnWorkflowStep, Enabled: &disabled},
		{ID: "zero-pct", Type: "test", Trigger: TriggerOnWorkflowStep, SamplePercentage: &zero},
	}

	results := runner.RunWorkflowStepEvals(context.Background(), defs, &EvalContext{SessionID: "s1"})
	if len(results) != 1 || results[0].EvalID != "zero-pct" {
		t.Fatalf("got %v, want only [zero-pct]", resultIDs(results))
	}
}

func resultIDs(results []EvalResult) []string {
	ids := make([]string, len(results))
	for i := range results {
		ids[i] = results[i].EvalID
	}
	return ids
}
