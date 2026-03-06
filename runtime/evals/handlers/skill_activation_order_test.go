package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

func TestSkillActivationOrderHandler_Type(t *testing.T) {
	h := &SkillActivationOrderHandler{}
	if h.Type() != "skill_activation_order" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestSkillActivationOrderHandler_ExactMatch(t *testing.T) {
	h := &SkillActivationOrderHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "billing"}},
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "refund"}},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{"billing", "refund"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestSkillActivationOrderHandler_SubsequenceWithIntervening(t *testing.T) {
	h := &SkillActivationOrderHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "billing"}},
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "shipping"}},
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "refund"}},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{"billing", "refund"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass with intervening skills: %s", result.Explanation)
	}
}

func TestSkillActivationOrderHandler_WrongOrder(t *testing.T) {
	h := &SkillActivationOrderHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "refund"}},
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "billing"}},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{"billing", "refund"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for wrong order")
	}
}

func TestSkillActivationOrderHandler_MissingSkill(t *testing.T) {
	h := &SkillActivationOrderHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "billing"}},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{"billing", "refund"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for missing skill")
	}
}

func TestSkillActivationOrderHandler_NoActivations(t *testing.T) {
	h := &SkillActivationOrderHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "other_tool", Arguments: map[string]any{"name": "billing"}},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{"billing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail with no skill activations")
	}
}

func TestSkillActivationOrderHandler_EmptySequence(t *testing.T) {
	h := &SkillActivationOrderHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "billing"}},
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
}

func TestSkillActivationOrderHandler_SingleSkillPresent(t *testing.T) {
	h := &SkillActivationOrderHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "billing"}},
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "refund"}},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{"refund"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass for single skill present: %s", result.Explanation)
	}
}

func TestSkillActivationOrderHandler_NilArguments(t *testing.T) {
	h := &SkillActivationOrderHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "skill__activate", Arguments: nil},
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "billing"}},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{"billing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass skipping nil args: %s", result.Explanation)
	}
}

func TestSkillActivationOrderHandler_NoSequenceParam(t *testing.T) {
	h := &SkillActivationOrderHandler{}
	evalCtx := &evals.EvalContext{}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail with no sequence param")
	}
}

func TestSkillActivationOrderHandler_NonToolCallsIgnored(t *testing.T) {
	h := &SkillActivationOrderHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "other_tool", Arguments: map[string]any{"name": "billing"}},
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "billing"}},
			{ToolName: "other_tool", Arguments: map[string]any{"name": "refund"}},
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "refund"}},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{"billing", "refund"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass ignoring non-skill tool calls: %s", result.Explanation)
	}
}

func TestSkillActivationOrderHandler_EmptyNameSkipped(t *testing.T) {
	h := &SkillActivationOrderHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "skill__activate", Arguments: map[string]any{"name": ""}},
			{ToolName: "skill__activate", Arguments: map[string]any{"name": "billing"}},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{"billing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass skipping empty name: %s", result.Explanation)
	}
}
