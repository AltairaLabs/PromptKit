package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// --- SkillActivated ---

func TestSkillActivatedHandler_Type(t *testing.T) {
	h := &SkillActivatedHandler{}
	if h.Type() != "skill_activated" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestSkillActivatedHandler_Pass(t *testing.T) {
	h := &SkillActivatedHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "billing"}},
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "shipping"}},
			{ToolName: "other_tool", Arguments: map[string]any{}},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"skill_names": []any{"billing", "shipping"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestSkillActivatedHandler_Missing(t *testing.T) {
	h := &SkillActivatedHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "billing"}},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"skill_names": []any{"billing", "shipping"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for missing skill")
	}
}

func TestSkillActivatedHandler_MinCalls(t *testing.T) {
	h := &SkillActivatedHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "billing"}},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"skill_names": []any{"billing"},
		"min_calls":   3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for insufficient calls")
	}
}

// --- SkillNotActivated ---

func TestSkillNotActivatedHandler_Type(t *testing.T) {
	h := &SkillNotActivatedHandler{}
	if h.Type() != "skill_not_activated" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestSkillNotActivatedHandler_Pass(t *testing.T) {
	h := &SkillNotActivatedHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "billing"}},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"skill_names": []any{"dangerous_skill"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestSkillNotActivatedHandler_Fail(t *testing.T) {
	h := &SkillNotActivatedHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "dangerous_skill"}},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"skill_names": []any{"dangerous_skill"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for forbidden skill")
	}
	if len(result.Violations) == 0 {
		t.Fatal("expected violations")
	}
}
