package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestDirectional_Type(t *testing.T) {
	h := &DirectionalHandler{}
	if h.Type() != "directional" {
		t.Errorf("expected type %q, got %q", "directional", h.Type())
	}
}

func TestDirectional_MissingCheck(t *testing.T) {
	h := &DirectionalHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsPassed() {
		t.Error("expected fail when check is missing")
	}
}

func TestDirectional_UnknownCheck(t *testing.T) {
	h := &DirectionalHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{
		"check": "bogus",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsPassed() {
		t.Error("expected fail for unknown check")
	}
}

func TestDirectional_SameToolCalls_Match(t *testing.T) {
	h := &DirectionalHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []types.ToolCallRecord{
			{ToolName: "search"},
			{ToolName: "lookup"},
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"check":          "same_tool_calls",
		"baseline_tools": []any{"lookup", "search"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsPassed() {
		t.Errorf("expected pass: %s", result.Explanation)
	}
}

func TestDirectional_SameToolCalls_Mismatch(t *testing.T) {
	h := &DirectionalHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []types.ToolCallRecord{
			{ToolName: "search"},
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"check":          "same_tool_calls",
		"baseline_tools": []any{"search", "lookup"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsPassed() {
		t.Error("expected fail when tool sets differ")
	}
}

func TestDirectional_SameToolCalls_NoBaseline(t *testing.T) {
	h := &DirectionalHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{
		"check": "same_tool_calls",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsPassed() {
		t.Errorf("expected pass when no baseline: %s", result.Explanation)
	}
}

func TestDirectional_SameOutcome_Match(t *testing.T) {
	h := &DirectionalHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{"workflow_state": "completed"},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"check":          "same_outcome",
		"baseline_state": "completed",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsPassed() {
		t.Errorf("expected pass: %s", result.Explanation)
	}
}

func TestDirectional_SameOutcome_Mismatch(t *testing.T) {
	h := &DirectionalHandler{}
	evalCtx := &evals.EvalContext{
		Extras: map[string]any{"workflow_state": "pending"},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"check":          "same_outcome",
		"baseline_state": "completed",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsPassed() {
		t.Error("expected fail when states differ")
	}
}

func TestDirectional_SameOutcome_NoBaseline(t *testing.T) {
	h := &DirectionalHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{
		"check": "same_outcome",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsPassed() {
		t.Errorf("expected pass when no baseline: %s", result.Explanation)
	}
}

func TestDirectional_SameOutcome_MissingExtras(t *testing.T) {
	h := &DirectionalHandler{}
	evalCtx := &evals.EvalContext{Extras: map[string]any{}}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"check":          "same_outcome",
		"baseline_state": "completed",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsPassed() {
		t.Error("expected fail when workflow_state missing")
	}
}

func TestDirectional_SameOutcome_NilExtras(t *testing.T) {
	h := &DirectionalHandler{}
	evalCtx := &evals.EvalContext{}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"check":          "same_outcome",
		"baseline_state": "completed",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsPassed() {
		t.Error("expected fail when extras is nil")
	}
}

func TestDirectional_SimilarContent_AboveThreshold(t *testing.T) {
	h := &DirectionalHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "The quick brown fox jumps over the lazy dog",
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"check":            "similar_content",
		"baseline_content": "The quick brown fox leaps over the lazy dog",
		"threshold":        0.5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score == nil {
		t.Fatal("expected score to be set")
	}
	if *result.Score < 0.5 {
		t.Errorf("expected score >= 0.5, got %v: %s", *result.Score, result.Explanation)
	}
}

func TestDirectional_SimilarContent_BelowThreshold(t *testing.T) {
	h := &DirectionalHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "completely different output here",
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"check":            "similar_content",
		"baseline_content": "The quick brown fox jumps over the lazy dog",
		"threshold":        0.9,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsPassed() {
		t.Error("expected fail when content is very different")
	}
}

func TestDirectional_SimilarContent_NoBaseline(t *testing.T) {
	h := &DirectionalHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{
		"check": "similar_content",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsPassed() {
		t.Errorf("expected pass when no baseline: %s", result.Explanation)
	}
}

func TestDirectional_SimilarContent_DefaultThreshold(t *testing.T) {
	h := &DirectionalHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "hello world foo bar baz",
	}
	// 3 shared words out of 7 union = ~0.43 < default 0.5
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"check":            "similar_content",
		"baseline_content": "hello world qux quux corge",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With default threshold 0.5, low overlap should fail
	if result.IsPassed() {
		t.Errorf("expected fail with default threshold: score=%v", result.Score)
	}
}

func TestDirectional_SimilarContent_IdenticalContent(t *testing.T) {
	h := &DirectionalHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "exact same content",
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"check":            "similar_content",
		"baseline_content": "exact same content",
		"threshold":        1.0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsPassed() {
		t.Errorf("expected pass for identical content: %s", result.Explanation)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Errorf("expected score 1.0, got %v", result.Score)
	}
}

func TestDirectional_SimilarContent_EmptyBoth(t *testing.T) {
	h := &DirectionalHandler{}
	evalCtx := &evals.EvalContext{CurrentOutput: ""}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"check":            "similar_content",
		"baseline_content": " ", // whitespace-only becomes empty word set
		"threshold":        0.5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both empty word sets → score 1.0
	if !result.IsPassed() {
		t.Errorf("expected pass for empty content: %s", result.Explanation)
	}
}

func TestDirectional_SameToolCalls_StringSliceParam(t *testing.T) {
	h := &DirectionalHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []types.ToolCallRecord{
			{ToolName: "search"},
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"check":          "same_tool_calls",
		"baseline_tools": []string{"search"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsPassed() {
		t.Errorf("expected pass with []string param: %s", result.Explanation)
	}
}

func TestWordOverlap(t *testing.T) {
	tests := []struct {
		name     string
		a, b     string
		expected float64
	}{
		{"identical", "hello world", "hello world", 1.0},
		{"no overlap", "foo bar", "baz qux", 0.0},
		{"partial", "a b c", "b c d", 0.5}, // intersection=2, union=4
		{"both empty", "", "", 1.0},
		{"one empty", "hello", "", 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := wordOverlap(tt.a, tt.b)
			if score < tt.expected-0.01 || score > tt.expected+0.01 {
				t.Errorf("wordOverlap(%q, %q) = %f, want %f", tt.a, tt.b, score, tt.expected)
			}
		})
	}
}
