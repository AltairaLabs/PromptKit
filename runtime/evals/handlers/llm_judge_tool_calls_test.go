package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

func TestLLMJudgeToolCallsHandler_Type(t *testing.T) {
	h := &LLMJudgeToolCallsHandler{}
	if h.Type() != "llm_judge_tool_calls" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestLLMJudgeToolCallsHandler_NoProvider(t *testing.T) {
	h := &LLMJudgeToolCallsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "search", Result: "found"},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"criteria": "check quality",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail when no judge provider")
	}
}

func TestLLMJudgeToolCallsHandler_NoToolCalls(t *testing.T) {
	h := &LLMJudgeToolCallsHandler{}
	evalCtx := &evals.EvalContext{
		Metadata: map[string]any{
			"judge_provider": &mockJudgeProvider{
				result: &JudgeResult{Score: 0.9, Passed: true},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"criteria": "check quality",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass (skip) with no tool calls: %s", result.Explanation)
	}
	if !result.Skipped {
		t.Fatal("expected skipped=true")
	}
}

func TestLLMJudgeToolCallsHandler_FilteredEmpty(t *testing.T) {
	h := &LLMJudgeToolCallsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "search", Result: "found"},
		},
		Metadata: map[string]any{
			"judge_provider": &mockJudgeProvider{
				result: &JudgeResult{Score: 0.9, Passed: true},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"criteria": "check quality",
		"tools":    []any{"nonexistent"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Skipped {
		t.Fatal("expected skipped for no matching tools")
	}
}

func TestLLMJudgeToolCallsHandler_WithProvider(t *testing.T) {
	h := &LLMJudgeToolCallsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "search", Arguments: map[string]any{"q": "test"}, Result: "found 5 items"},
		},
		Metadata: map[string]any{
			"judge_provider": &mockJudgeProvider{
				result: &JudgeResult{Score: 0.85, Passed: true, Reasoning: "good tool usage"},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"criteria": "check tool usage quality",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestFormatToolCallViews(t *testing.T) {
	views := []toolCallView{
		{Name: "search", Args: map[string]any{"q": "test"}, Result: "found", Index: 0},
		{Name: "fetch", Args: map[string]any{"id": "123"}, Error: "timeout", Index: 1},
	}
	text := formatToolCallViews(views)
	if text == "" {
		t.Fatal("expected non-empty output")
	}
	if !containsInsensitive(text, "search") || !containsInsensitive(text, "fetch") {
		t.Fatal("expected both tool names in output")
	}
	if !containsInsensitive(text, "timeout") {
		t.Fatal("expected error in output")
	}
}
