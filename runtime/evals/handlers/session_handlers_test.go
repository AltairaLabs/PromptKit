package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func assistantMsg(content string) types.Message {
	return types.Message{Role: "assistant", Content: content}
}

func userMsg(content string) types.Message {
	return types.Message{Role: "user", Content: content}
}

func TestContentExcludesHandler_Pass(t *testing.T) {
	h := &ContentExcludesHandler{}
	if h.Type() != "content_excludes" {
		t.Fatalf("unexpected type: %s", h.Type())
	}

	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			userMsg("hello"),
			assistantMsg("I can help with that"),
			assistantMsg("Here is the answer"),
		},
	}
	params := map[string]any{
		"patterns": []any{"forbidden", "secret"},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestContentExcludesHandler_Fail(t *testing.T) {
	h := &ContentExcludesHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			userMsg("hello"),
			assistantMsg("This is SECRET info"),
		},
	}
	params := map[string]any{
		"patterns": []any{"secret"},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for forbidden content")
	}
}

func TestContentExcludesHandler_IgnoresUserMessages(t *testing.T) {
	h := &ContentExcludesHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			userMsg("secret"),
			assistantMsg("I can help"),
		},
	}
	params := map[string]any{
		"patterns": []any{"secret"},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatal("should ignore user messages")
	}
}

func TestContentExcludesHandler_NoPatterns(t *testing.T) {
	h := &ContentExcludesHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			assistantMsg("anything"),
		},
	}

	result, err := h.Eval(ctx, evalCtx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatal("no patterns should pass")
	}
}

func TestContainsAnyHandler_Pass(t *testing.T) {
	h := &ContainsAnyHandler{}
	if h.Type() != "contains_any" {
		t.Fatalf("unexpected type: %s", h.Type())
	}

	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			userMsg("hi"),
			assistantMsg("The weather is nice"),
			assistantMsg("Have a good day"),
		},
	}
	params := map[string]any{
		"patterns": []any{"weather", "rain"},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestContainsAnyHandler_Fail(t *testing.T) {
	h := &ContainsAnyHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			userMsg("hi"),
			assistantMsg("nothing relevant"),
		},
	}
	params := map[string]any{
		"patterns": []any{"weather", "rain"},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail")
	}
}

func TestContainsAnyHandler_CaseInsensitive(t *testing.T) {
	h := &ContainsAnyHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			assistantMsg("The WEATHER is nice"),
		},
	}
	params := map[string]any{
		"patterns": []any{"weather"},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatal("case-insensitive match should pass")
	}
}

func TestContainsAnyHandler_NoPatterns(t *testing.T) {
	h := &ContainsAnyHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			assistantMsg("anything"),
		},
	}

	result, err := h.Eval(ctx, evalCtx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("no patterns should fail")
	}
}

func TestToolsCalledSessionHandler_Pass(t *testing.T) {
	h := &ToolsCalledSessionHandler{}
	if h.Type() != "tools_called_session" {
		t.Fatalf("unexpected type: %s", h.Type())
	}

	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "search", TurnIndex: 0},
			{ToolName: "fetch", TurnIndex: 1},
			{ToolName: "search", TurnIndex: 2},
		},
	}
	params := map[string]any{
		"tool_names": []any{"search", "fetch"},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestToolsCalledSessionHandler_Fail(t *testing.T) {
	h := &ToolsCalledSessionHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "search", TurnIndex: 0},
		},
	}
	params := map[string]any{
		"tool_names": []any{"search", "fetch"},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for missing fetch")
	}
}

func TestToolsCalledSessionHandler_MinCalls(t *testing.T) {
	h := &ToolsCalledSessionHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "search", TurnIndex: 0},
		},
	}
	params := map[string]any{
		"tool_names": []any{"search"},
		"min_calls":  float64(2),
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: only 1 call but need 2")
	}
}

func TestToolsNotCalledSessionHandler_Pass(t *testing.T) {
	h := &ToolsNotCalledSessionHandler{}
	if h.Type() != "tools_not_called_session" {
		t.Fatalf("unexpected type: %s", h.Type())
	}

	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "search", TurnIndex: 0},
		},
	}
	params := map[string]any{
		"tool_names": []any{"delete", "drop"},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestToolsNotCalledSessionHandler_Fail(t *testing.T) {
	h := &ToolsNotCalledSessionHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "search", TurnIndex: 0},
			{ToolName: "delete", TurnIndex: 1},
		},
	}
	params := map[string]any{
		"tool_names": []any{"delete"},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: delete was called")
	}
}

func TestToolArgsSessionHandler_Pass(t *testing.T) {
	h := &ToolArgsSessionHandler{}
	if h.Type() != "tool_args_session" {
		t.Fatalf("unexpected type: %s", h.Type())
	}

	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{
				ToolName:  "search",
				TurnIndex: 0,
				Arguments: map[string]any{
					"query": "golang",
					"limit": "10",
				},
			},
		},
	}
	params := map[string]any{
		"tool_name": "search",
		"expected_args": map[string]any{
			"query": "golang",
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

func TestToolArgsSessionHandler_Fail(t *testing.T) {
	h := &ToolArgsSessionHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{
				ToolName:  "search",
				TurnIndex: 0,
				Arguments: map[string]any{
					"query": "python",
				},
			},
		},
	}
	params := map[string]any{
		"tool_name": "search",
		"expected_args": map[string]any{
			"query": "golang",
		},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: args don't match")
	}
}

func TestToolArgsSessionHandler_ToolNotCalled(t *testing.T) {
	h := &ToolArgsSessionHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{},
	}
	params := map[string]any{
		"tool_name": "search",
		"expected_args": map[string]any{
			"query": "golang",
		},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: tool not called")
	}
}

func TestToolArgsSessionHandler_NoToolName(t *testing.T) {
	h := &ToolArgsSessionHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{}

	result, err := h.Eval(ctx, evalCtx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: no tool_name")
	}
}

func TestToolArgsExcludedSessionHandler_Pass(t *testing.T) {
	h := &ToolArgsExcludedSessionHandler{}
	if h.Type() != "tool_args_excluded_session" {
		t.Fatalf("unexpected type: %s", h.Type())
	}

	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{
				ToolName:  "search",
				TurnIndex: 0,
				Arguments: map[string]any{
					"query": "golang",
				},
			},
		},
	}
	params := map[string]any{
		"tool_name": "search",
		"excluded_args": map[string]any{
			"query": "drop tables",
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

func TestToolArgsExcludedSessionHandler_Fail(t *testing.T) {
	h := &ToolArgsExcludedSessionHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{
				ToolName:  "search",
				TurnIndex: 0,
				Arguments: map[string]any{
					"query": "drop tables",
				},
			},
		},
	}
	params := map[string]any{
		"tool_name": "search",
		"excluded_args": map[string]any{
			"query": "drop tables",
		},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: excluded args found")
	}
}

func TestToolArgsExcludedSessionHandler_NoToolName(t *testing.T) {
	h := &ToolArgsExcludedSessionHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{}

	result, err := h.Eval(ctx, evalCtx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: no tool_name")
	}
}

func TestToolArgsExcludedSessionHandler_NoExcludedArgs(t *testing.T) {
	h := &ToolArgsExcludedSessionHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{}

	result, err := h.Eval(ctx, evalCtx, map[string]any{
		"tool_name": "search",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatal("no excluded_args should pass")
	}
}
