package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

func TestWorkflowTransitionOrderHandler_Type(t *testing.T) {
	h := &WorkflowTransitionOrderHandler{}
	if h.Type() != "workflow_transition_order" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestWorkflowTransitionOrderHandler_ExactMatch(t *testing.T) {
	h := &WorkflowTransitionOrderHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{
			"workflow_transitions": []any{
				map[string]any{"from": "new", "to": "processing"},
				map[string]any{"from": "processing", "to": "complete"},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{"processing", "complete"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass for exact match: %s", result.Explanation)
	}
	if result.Details["matched_steps"] != 2 {
		t.Fatalf("expected 2 matched steps, got %v", result.Details["matched_steps"])
	}
}

func TestWorkflowTransitionOrderHandler_SubsequenceMatch(t *testing.T) {
	h := &WorkflowTransitionOrderHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{
			"workflow_transitions": []any{
				map[string]any{"from": "new", "to": "processing"},
				map[string]any{"from": "processing", "to": "review"},
				map[string]any{"from": "review", "to": "approved"},
				map[string]any{"from": "approved", "to": "complete"},
			},
		},
	}

	// Subsequence skipping "review" and "approved"
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{"processing", "complete"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass for subsequence match: %s", result.Explanation)
	}
}

func TestWorkflowTransitionOrderHandler_WrongOrder(t *testing.T) {
	h := &WorkflowTransitionOrderHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{
			"workflow_transitions": []any{
				map[string]any{"from": "new", "to": "processing"},
				map[string]any{"from": "processing", "to": "complete"},
			},
		},
	}

	// Reversed order should fail
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{"complete", "processing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for wrong order")
	}
	if result.Details["matched_steps"] != 1 {
		t.Fatalf("expected 1 matched step, got %v", result.Details["matched_steps"])
	}
}

func TestWorkflowTransitionOrderHandler_MissingState(t *testing.T) {
	h := &WorkflowTransitionOrderHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{
			"workflow_transitions": []any{
				map[string]any{"from": "new", "to": "processing"},
				map[string]any{"from": "processing", "to": "complete"},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{"processing", "review", "complete"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for missing state 'review'")
	}
}

func TestWorkflowTransitionOrderHandler_NoTransitions(t *testing.T) {
	h := &WorkflowTransitionOrderHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{"processing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for no transitions in context")
	}
	if result.Explanation != "no workflow transitions available in context" {
		t.Fatalf("unexpected explanation: %s", result.Explanation)
	}
}

func TestWorkflowTransitionOrderHandler_EmptySequence(t *testing.T) {
	h := &WorkflowTransitionOrderHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{
			"workflow_transitions": []any{},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for empty sequence")
	}
	if result.Explanation != "missing or empty required param 'sequence'" {
		t.Fatalf("unexpected explanation: %s", result.Explanation)
	}
}

func TestWorkflowTransitionOrderHandler_MissingSequenceParam(t *testing.T) {
	h := &WorkflowTransitionOrderHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for missing sequence param")
	}
}

func TestWorkflowTransitionOrderHandler_SingleState(t *testing.T) {
	h := &WorkflowTransitionOrderHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{
			"workflow_transitions": []any{
				map[string]any{"from": "new", "to": "processing"},
				map[string]any{"from": "processing", "to": "complete"},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{"complete"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass for single state present: %s", result.Explanation)
	}
}

func TestWorkflowTransitionOrderHandler_SingleStateMissing(t *testing.T) {
	h := &WorkflowTransitionOrderHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{
			"workflow_transitions": []any{
				map[string]any{"from": "new", "to": "processing"},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{"complete"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for single state not present")
	}
}

func TestWorkflowTransitionOrderHandler_InvalidTransitionsData(t *testing.T) {
	h := &WorkflowTransitionOrderHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{
			"workflow_transitions": "not-a-slice",
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{"processing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for invalid transitions data")
	}
	if result.Explanation != "invalid transitions data in context" {
		t.Fatalf("unexpected explanation: %s", result.Explanation)
	}
}

func TestWorkflowTransitionOrderHandler_StringSliceParam(t *testing.T) {
	h := &WorkflowTransitionOrderHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{
			"workflow_transitions": []any{
				map[string]any{"from": "new", "to": "processing"},
				map[string]any{"from": "processing", "to": "complete"},
			},
		},
	}

	// Test with native []string param (not []any)
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []string{"processing", "complete"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass with []string param: %s", result.Explanation)
	}
}

func TestWorkflowTransitionOrderHandler_MalformedTransitionEntries(t *testing.T) {
	h := &WorkflowTransitionOrderHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{
			"workflow_transitions": []any{
				"not-a-map",
				map[string]any{"from": "new", "to": "processing"},
				42,
				map[string]any{"from": "processing", "to": "complete"},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{"processing", "complete"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass, skipping malformed entries: %s", result.Explanation)
	}
}
