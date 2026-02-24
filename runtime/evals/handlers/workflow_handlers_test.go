package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// --- WorkflowComplete ---

func TestWorkflowCompleteHandler_Type(t *testing.T) {
	h := &WorkflowCompleteHandler{}
	if h.Type() != "workflow_complete" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestWorkflowCompleteHandler_Complete(t *testing.T) {
	h := &WorkflowCompleteHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{
			"workflow_complete":      true,
			"workflow_current_state": "resolved",
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestWorkflowCompleteHandler_NotComplete(t *testing.T) {
	h := &WorkflowCompleteHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{
			"workflow_complete": false,
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for incomplete workflow")
	}
}

func TestWorkflowCompleteHandler_Missing(t *testing.T) {
	h := &WorkflowCompleteHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{},
	}

	result, err := h.Eval(context.Background(), evalCtx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for missing data")
	}
}

// --- WorkflowStateIs ---

func TestWorkflowStateIsHandler_Type(t *testing.T) {
	h := &WorkflowStateIsHandler{}
	if h.Type() != "state_is" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestWorkflowStateIsHandler_Match(t *testing.T) {
	h := &WorkflowStateIsHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{
			"workflow_current_state": "processing",
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"state": "processing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestWorkflowStateIsHandler_Mismatch(t *testing.T) {
	h := &WorkflowStateIsHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{
			"workflow_current_state": "pending",
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"state": "processing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for state mismatch")
	}
}

func TestWorkflowStateIsHandler_NoState(t *testing.T) {
	h := &WorkflowStateIsHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{
		Extras: map[string]any{},
	}, map[string]any{"state": "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail when no state available")
	}
}

func TestWorkflowStateIsHandler_MissingParam(t *testing.T) {
	h := &WorkflowStateIsHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{
		Extras: map[string]any{},
	}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail with missing param")
	}
}

// --- WorkflowTransitionedTo ---

func TestWorkflowTransitionedToHandler_Type(t *testing.T) {
	h := &WorkflowTransitionedToHandler{}
	if h.Type() != "transitioned_to" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestWorkflowTransitionedToHandler_Found(t *testing.T) {
	h := &WorkflowTransitionedToHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{
			"workflow_transitions": []any{
				map[string]any{"from": "new", "to": "processing"},
				map[string]any{"from": "processing", "to": "resolved"},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"state": "resolved",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestWorkflowTransitionedToHandler_NotFound(t *testing.T) {
	h := &WorkflowTransitionedToHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{
			"workflow_transitions": []any{
				map[string]any{"from": "new", "to": "processing"},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"state": "resolved",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for missing transition")
	}
}

func TestWorkflowTransitionedToHandler_NoTransitions(t *testing.T) {
	h := &WorkflowTransitionedToHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"state": "resolved",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for no transitions")
	}
}
