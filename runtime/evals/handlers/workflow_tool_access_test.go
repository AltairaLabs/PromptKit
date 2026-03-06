package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

func TestWorkflowToolAccessHandler_Type(t *testing.T) {
	h := &WorkflowToolAccessHandler{}
	if h.Type() != "workflow_tool_access" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestWorkflowToolAccess_ToolAllowedInState(t *testing.T) {
	h := &WorkflowToolAccessHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{TurnIndex: 1, ToolName: "search"},
			{TurnIndex: 2, ToolName: "lookup"},
		},
		Extras: map[string]any{
			"workflow_transitions": []any{
				map[string]any{"from": "init", "to": "searching", "turn_index": 0},
			},
		},
	}
	params := map[string]any{
		"rules": []any{
			map[string]any{
				"state":   "searching",
				"allowed": []any{"search", "lookup", "fetch"},
			},
		},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestWorkflowToolAccess_ToolNotAllowed(t *testing.T) {
	h := &WorkflowToolAccessHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{TurnIndex: 1, ToolName: "delete"},
		},
		Extras: map[string]any{
			"workflow_transitions": []any{
				map[string]any{"from": "init", "to": "readonly", "turn_index": 0},
			},
		},
	}
	params := map[string]any{
		"rules": []any{
			map[string]any{
				"state":   "readonly",
				"allowed": []any{"search", "lookup"},
			},
		},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: delete is not allowed in readonly state")
	}
	if result.Details == nil {
		t.Fatal("expected details with violations")
	}
}

func TestWorkflowToolAccess_NoRulesForState(t *testing.T) {
	h := &WorkflowToolAccessHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{TurnIndex: 1, ToolName: "anything"},
		},
		Extras: map[string]any{
			"workflow_transitions": []any{
				map[string]any{"from": "init", "to": "freeform", "turn_index": 0},
			},
		},
	}
	params := map[string]any{
		"rules": []any{
			map[string]any{
				"state":   "locked",
				"allowed": []any{"read"},
			},
		},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass (open policy for state without rules): %s", result.Explanation)
	}
}

func TestWorkflowToolAccess_NoTransitionsData(t *testing.T) {
	h := &WorkflowToolAccessHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{TurnIndex: 0, ToolName: "search"},
		},
		Extras: map[string]any{},
	}
	params := map[string]any{
		"rules": []any{
			map[string]any{
				"state":   "locked",
				"allowed": []any{"read"},
			},
		},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	// No state info at all — open policy, should pass.
	if !result.Passed {
		t.Fatalf("expected pass when no transitions data: %s", result.Explanation)
	}
}

func TestWorkflowToolAccess_FallbackWorkflowState(t *testing.T) {
	h := &WorkflowToolAccessHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{TurnIndex: 0, ToolName: "delete"},
		},
		Extras: map[string]any{
			"workflow_state": "locked",
		},
	}
	params := map[string]any{
		"rules": []any{
			map[string]any{
				"state":   "locked",
				"allowed": []any{"read"},
			},
		},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: delete not allowed in locked state (fallback)")
	}
}

func TestWorkflowToolAccess_FallbackWorkflowStateAllowed(t *testing.T) {
	h := &WorkflowToolAccessHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{TurnIndex: 0, ToolName: "read"},
		},
		Extras: map[string]any{
			"workflow_state": "locked",
		},
	}
	params := map[string]any{
		"rules": []any{
			map[string]any{
				"state":   "locked",
				"allowed": []any{"read"},
			},
		},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: read is allowed in locked state: %s", result.Explanation)
	}
}

func TestWorkflowToolAccess_MultipleRulesMultipleTools(t *testing.T) {
	h := &WorkflowToolAccessHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{TurnIndex: 0, ToolName: "greet"},
			{TurnIndex: 1, ToolName: "search"},
			{TurnIndex: 2, ToolName: "search"},
			{TurnIndex: 3, ToolName: "delete"}, // violation: not allowed in "reviewing"
		},
		Extras: map[string]any{
			"workflow_transitions": []any{
				map[string]any{"from": "greeting", "to": "searching", "turn_index": 1},
				map[string]any{"from": "searching", "to": "reviewing", "turn_index": 3},
			},
		},
	}
	params := map[string]any{
		"rules": []any{
			map[string]any{
				"state":   "greeting",
				"allowed": []any{"greet"},
			},
			map[string]any{
				"state":   "searching",
				"allowed": []any{"search", "lookup"},
			},
			map[string]any{
				"state":   "reviewing",
				"allowed": []any{"approve", "reject"},
			},
		},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: delete not allowed in reviewing state")
	}
	violations := result.Details["violations"].([]map[string]any)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0]["tool"] != "delete" {
		t.Fatalf("expected violation for 'delete', got %v", violations[0]["tool"])
	}
}

func TestWorkflowToolAccess_NoRulesParam(t *testing.T) {
	h := &WorkflowToolAccessHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{TurnIndex: 0, ToolName: "anything"},
		},
	}

	result, err := h.Eval(ctx, evalCtx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass with no rules: %s", result.Explanation)
	}
}

func TestWorkflowToolAccess_TransitionsWithoutTurnIndex(t *testing.T) {
	h := &WorkflowToolAccessHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{TurnIndex: 0, ToolName: "delete"},
		},
		Extras: map[string]any{
			"workflow_transitions": []any{
				map[string]any{"from": "init", "to": "locked"},
			},
		},
	}
	params := map[string]any{
		"rules": []any{
			map[string]any{
				"state":   "locked",
				"allowed": []any{"read"},
			},
		},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	// Without turn_index, last "to" state is used for all turns.
	if result.Passed {
		t.Fatal("expected fail: delete not allowed in locked state (no turn_index)")
	}
}

func TestWorkflowToolAccess_EmptyTransitionsArray(t *testing.T) {
	h := &WorkflowToolAccessHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{TurnIndex: 0, ToolName: "anything"},
		},
		Extras: map[string]any{
			"workflow_transitions": []any{},
			"workflow_state":       "active",
		},
	}
	params := map[string]any{
		"rules": []any{
			map[string]any{
				"state":   "active",
				"allowed": []any{"anything"},
			},
		},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass with empty transitions falling back to workflow_state: %s", result.Explanation)
	}
}

func TestWorkflowToolAccess_InvalidRulesSkipped(t *testing.T) {
	h := &WorkflowToolAccessHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{TurnIndex: 0, ToolName: "search"},
		},
		Extras: map[string]any{
			"workflow_state": "active",
		},
	}
	params := map[string]any{
		"rules": []any{
			"not-a-map",
			map[string]any{"allowed": []any{"search"}}, // missing state
			map[string]any{
				"state":   "active",
				"allowed": []any{"search"},
			},
		},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestWorkflowToolAccess_InitialStateFromFirstTransition(t *testing.T) {
	h := &WorkflowToolAccessHandler{}
	ctx := context.Background()
	// Tool called at turn 0, before any transition (first transition at turn 2).
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{TurnIndex: 0, ToolName: "greet"},
		},
		Extras: map[string]any{
			"workflow_transitions": []any{
				map[string]any{"from": "greeting", "to": "active", "turn_index": 2},
			},
		},
	}
	params := map[string]any{
		"rules": []any{
			map[string]any{
				"state":   "greeting",
				"allowed": []any{"greet"},
			},
		},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: greet allowed in initial 'greeting' state: %s", result.Explanation)
	}
}

func TestWorkflowToolAccess_MultipleViolations(t *testing.T) {
	h := &WorkflowToolAccessHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{TurnIndex: 1, ToolName: "delete"},
			{TurnIndex: 2, ToolName: "drop"},
		},
		Extras: map[string]any{
			"workflow_state": "locked",
		},
	}
	params := map[string]any{
		"rules": []any{
			map[string]any{
				"state":   "locked",
				"allowed": []any{"read"},
			},
		},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: two violations")
	}
	violations := result.Details["violations"].([]map[string]any)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(violations))
	}
}

func TestWorkflowToolAccess_InvalidTransitionsType(t *testing.T) {
	h := &WorkflowToolAccessHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{TurnIndex: 0, ToolName: "search"},
		},
		Extras: map[string]any{
			"workflow_transitions": "not-a-slice",
			"workflow_state":       "active",
		},
	}
	params := map[string]any{
		"rules": []any{
			map[string]any{
				"state":   "active",
				"allowed": []any{"search"},
			},
		},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass with invalid transitions type falling back to workflow_state: %s", result.Explanation)
	}
}
