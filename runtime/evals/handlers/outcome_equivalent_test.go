package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestOutcomeEquivalent_Type(t *testing.T) {
	h := &OutcomeEquivalentHandler{}
	if h.Type() != "outcome_equivalent" {
		t.Errorf("expected type %q, got %q", "outcome_equivalent", h.Type())
	}
}

func TestOutcomeEquivalent_MissingMetric(t *testing.T) {
	h := &OutcomeEquivalentHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Error("expected fail when metric is missing")
	}
	if result.Explanation != "missing required param 'metric'" {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
}

func TestOutcomeEquivalent_UnknownMetric(t *testing.T) {
	h := &OutcomeEquivalentHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{
		"metric": "unknown_metric",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Error("expected fail for unknown metric")
	}
	if result.Explanation == "" {
		t.Error("expected non-empty explanation")
	}
}

func TestOutcomeEquivalent_ToolCalls_Match(t *testing.T) {
	h := &OutcomeEquivalentHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []types.ToolCallRecord{
			{ToolName: "search"},
			{ToolName: "lookup"},
			{ToolName: "search"}, // duplicate — should be deduplicated
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"metric":         "tool_calls",
		"expected_tools": []any{"lookup", "search"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Errorf("expected pass, got fail: %s", result.Explanation)
	}
}

func TestOutcomeEquivalent_ToolCalls_Mismatch(t *testing.T) {
	h := &OutcomeEquivalentHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []types.ToolCallRecord{
			{ToolName: "search"},
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"metric":         "tool_calls",
		"expected_tools": []any{"search", "lookup"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Error("expected fail when tool sets differ")
	}
}

func TestOutcomeEquivalent_ToolCalls_NoExpected(t *testing.T) {
	h := &OutcomeEquivalentHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []types.ToolCallRecord{
			{ToolName: "search"},
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"metric": "tool_calls",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Errorf("expected pass when no expected_tools: %s", result.Explanation)
	}
}

func TestOutcomeEquivalent_ToolCalls_OrderIndependent(t *testing.T) {
	h := &OutcomeEquivalentHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []types.ToolCallRecord{
			{ToolName: "beta"},
			{ToolName: "alpha"},
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"metric":         "tool_calls",
		"expected_tools": []any{"alpha", "beta"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Errorf("expected pass (order independent): %s", result.Explanation)
	}
}

func TestOutcomeEquivalent_FinalState_Match(t *testing.T) {
	h := &OutcomeEquivalentHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{
			"workflow_state": "completed",
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"metric":         "final_state",
		"expected_state": "completed",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Errorf("expected pass: %s", result.Explanation)
	}
}

func TestOutcomeEquivalent_FinalState_Mismatch(t *testing.T) {
	h := &OutcomeEquivalentHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{
			"workflow_state": "pending",
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"metric":         "final_state",
		"expected_state": "completed",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Error("expected fail when states differ")
	}
}

func TestOutcomeEquivalent_FinalState_NoExpected(t *testing.T) {
	h := &OutcomeEquivalentHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{
			"workflow_state": "completed",
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"metric": "final_state",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Errorf("expected pass when no expected_state: %s", result.Explanation)
	}
}

func TestOutcomeEquivalent_FinalState_MissingExtras(t *testing.T) {
	h := &OutcomeEquivalentHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"metric":         "final_state",
		"expected_state": "completed",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Error("expected fail when workflow_state is missing from extras")
	}
}

func TestOutcomeEquivalent_FinalState_NilExtras(t *testing.T) {
	h := &OutcomeEquivalentHandler{}
	evalCtx := &evals.EvalContext{}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"metric":         "final_state",
		"expected_state": "completed",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Error("expected fail when extras is nil")
	}
}

func TestOutcomeEquivalent_ContentHash_Match(t *testing.T) {
	h := &OutcomeEquivalentHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "Hello, world!",
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"metric":           "content_hash",
		"expected_content": "Hello, world!",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Errorf("expected pass: %s", result.Explanation)
	}
}

func TestOutcomeEquivalent_ContentHash_Mismatch(t *testing.T) {
	h := &OutcomeEquivalentHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "Hello, world!",
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"metric":           "content_hash",
		"expected_content": "Goodbye, world!",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Error("expected fail when content differs")
	}
}

func TestOutcomeEquivalent_ContentHash_NoExpected(t *testing.T) {
	h := &OutcomeEquivalentHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "Hello, world!",
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"metric": "content_hash",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Errorf("expected pass when no expected_content: %s", result.Explanation)
	}
}

func TestOutcomeEquivalent_ToolCalls_EmptyActualAndExpected(t *testing.T) {
	h := &OutcomeEquivalentHandler{}
	evalCtx := &evals.EvalContext{}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"metric":         "tool_calls",
		"expected_tools": []any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty expected_tools slice has length 0, so treated as "no expected"
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Errorf("expected pass for empty expected_tools: %s", result.Explanation)
	}
}

func TestOutcomeEquivalent_ToolCalls_StringSliceParam(t *testing.T) {
	h := &OutcomeEquivalentHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []types.ToolCallRecord{
			{ToolName: "search"},
		},
	}
	// Test with []string (extractStringSlice handles both []string and []any)
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"metric":         "tool_calls",
		"expected_tools": []string{"search"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Errorf("expected pass with []string param: %s", result.Explanation)
	}
}

func TestOutcomeEquivalent_ContentHash_EmptyContent(t *testing.T) {
	h := &OutcomeEquivalentHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "",
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"metric":           "content_hash",
		"expected_content": "something",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Error("expected fail when actual is empty but expected is not")
	}
}
