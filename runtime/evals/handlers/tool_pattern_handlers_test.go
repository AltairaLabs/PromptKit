package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// --- ToolAntiPattern ---

func TestToolAntiPatternHandler_Type(t *testing.T) {
	h := &ToolAntiPatternHandler{}
	if h.Type() != "tool_anti_pattern" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestToolAntiPatternHandler_NoPatterns(t *testing.T) {
	h := &ToolAntiPatternHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsPassed() {
		t.Fatal("expected pass with no patterns")
	}
}

func TestToolAntiPatternHandler_Pass(t *testing.T) {
	h := &ToolAntiPatternHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, nil, ""),
			toolCall("confirm", nil, nil, ""),
			toolCall("delete", nil, nil, ""),
		},
	}
	// Anti-pattern: delete before confirm — not present here
	params := map[string]any{
		"patterns": []any{
			map[string]any{
				"sequence": []any{"delete", "confirm"},
				"message":  "must confirm before delete",
			},
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsPassed() {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestToolAntiPatternHandler_Fail(t *testing.T) {
	h := &ToolAntiPatternHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("delete", nil, nil, ""),
			toolCall("confirm", nil, nil, ""),
		},
	}
	params := map[string]any{
		"patterns": []any{
			map[string]any{
				"sequence": []any{"delete", "confirm"},
				"message":  "must confirm before delete",
			},
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsPassed() {
		t.Fatal("expected fail for detected anti-pattern")
	}
	if result.Details == nil {
		t.Fatal("expected details with violations")
	}
}

func TestToolAntiPatternHandler_MultiplePatterns(t *testing.T) {
	h := &ToolAntiPatternHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("delete", nil, nil, ""),
			toolCall("search", nil, nil, ""),
		},
	}
	params := map[string]any{
		"patterns": []any{
			map[string]any{"sequence": []any{"delete", "search"}},
			map[string]any{"sequence": []any{"search", "delete"}}, // not present
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsPassed() {
		t.Fatal("expected fail — first pattern matches")
	}
	violations := result.Details["violations"].([]map[string]any)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
}

// --- ToolNoRepeat ---

func TestToolNoRepeatHandler_Type(t *testing.T) {
	h := &ToolNoRepeatHandler{}
	if h.Type() != "tool_no_repeat" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestToolNoRepeatHandler_Pass(t *testing.T) {
	h := &ToolNoRepeatHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, nil, ""),
			toolCall("fetch", nil, nil, ""),
			toolCall("search", nil, nil, ""),
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsPassed() {
		t.Fatalf("expected pass — no consecutive repeats: %s", result.Explanation)
	}
}

func TestToolNoRepeatHandler_Fail(t *testing.T) {
	h := &ToolNoRepeatHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, nil, ""),
			toolCall("search", nil, nil, ""),
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsPassed() {
		t.Fatal("expected fail for consecutive repeats")
	}
}

func TestToolNoRepeatHandler_CustomMaxRepeats(t *testing.T) {
	h := &ToolNoRepeatHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, nil, ""),
			toolCall("search", nil, nil, ""),
			toolCall("search", nil, nil, ""),
		},
	}
	// Allow up to 2 consecutive calls
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"max_repeats": 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsPassed() {
		t.Fatal("expected fail — 3 consecutive exceeds max_repeats=2")
	}

	// Allow up to 3
	result, err = h.Eval(context.Background(), evalCtx, map[string]any{
		"max_repeats": 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsPassed() {
		t.Fatalf("expected pass — 3 consecutive within max_repeats=3: %s", result.Explanation)
	}
}

func TestToolNoRepeatHandler_ScopedTools(t *testing.T) {
	h := &ToolNoRepeatHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, nil, ""),
			toolCall("search", nil, nil, ""),
			toolCall("fetch", nil, nil, ""),
		},
	}
	// Only check "fetch" — should pass (search repeats are ignored)
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tools": []any{"fetch"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsPassed() {
		t.Fatalf("expected pass — search repeats not in scope: %s", result.Explanation)
	}
}

// --- ToolEfficiency ---

func TestToolEfficiencyHandler_Type(t *testing.T) {
	h := &ToolEfficiencyHandler{}
	if h.Type() != "tool_efficiency" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestToolEfficiencyHandler_NoThresholds(t *testing.T) {
	h := &ToolEfficiencyHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsPassed() {
		t.Fatal("expected fail with no thresholds")
	}
}

func TestToolEfficiencyHandler_MaxCalls_Pass(t *testing.T) {
	h := &ToolEfficiencyHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("a", nil, nil, ""),
			toolCall("b", nil, nil, ""),
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"max_calls": 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score <= 0 {
		t.Fatalf("expected positive score for passing efficiency check, got %v: %s", result.Score, result.Explanation)
	}
}

func TestToolEfficiencyHandler_MaxCalls_Fail(t *testing.T) {
	h := &ToolEfficiencyHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("a", nil, nil, ""),
			toolCall("b", nil, nil, ""),
			toolCall("c", nil, nil, ""),
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"max_calls": 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsPassed() {
		t.Fatal("expected fail — 3 calls exceed max_calls=2")
	}
}

func TestToolEfficiencyHandler_ErrorRate(t *testing.T) {
	h := &ToolEfficiencyHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("a", nil, nil, ""),
			toolCall("b", nil, nil, "timeout"),
			toolCall("c", nil, nil, ""),
			toolCall("d", nil, nil, "error"),
		},
	}
	// 2/4 = 50% error rate, max is 25%
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"max_error_rate": 0.25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsPassed() {
		t.Fatal("expected fail — 50% error rate exceeds 25%")
	}

	// max 60% — should pass
	result, err = h.Eval(context.Background(), evalCtx, map[string]any{
		"max_error_rate": 0.6,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsPassed() {
		t.Fatalf("expected pass — 50%% error rate within 60%%: %s", result.Explanation)
	}
}

// --- CostBudget ---

func TestCostBudgetHandler_Type(t *testing.T) {
	h := &CostBudgetHandler{}
	if h.Type() != "cost_budget" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestCostBudgetHandler_NoThresholds(t *testing.T) {
	h := &CostBudgetHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{
		Metadata: map[string]any{},
	}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsPassed() {
		t.Fatal("expected fail with no thresholds")
	}
}

func TestCostBudgetHandler_CostPass(t *testing.T) {
	h := &CostBudgetHandler{}
	evalCtx := &evals.EvalContext{
		Metadata: map[string]any{
			"total_cost":    0.05,
			"input_tokens":  500,
			"output_tokens": 200,
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"max_cost_usd": 0.10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score < 0.4 || *result.Score > 0.6 {
		t.Fatalf("expected score ~0.5, got %v", result.Score)
	}
}

func TestCostBudgetHandler_CostFail(t *testing.T) {
	h := &CostBudgetHandler{}
	evalCtx := &evals.EvalContext{
		Metadata: map[string]any{
			"total_cost": 0.15,
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"max_cost_usd": 0.10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsPassed() {
		t.Fatal("expected fail — cost exceeds budget")
	}
}

func TestCostBudgetHandler_TokenLimits(t *testing.T) {
	h := &CostBudgetHandler{}
	evalCtx := &evals.EvalContext{
		Metadata: map[string]any{
			"total_cost":    0.01,
			"input_tokens":  1000,
			"output_tokens": 500,
		},
	}
	// Total tokens = 1500, max = 1000
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"max_total_tokens": 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsPassed() {
		t.Fatal("expected fail — total tokens exceed limit")
	}
}
