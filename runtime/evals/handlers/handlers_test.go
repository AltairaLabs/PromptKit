package handlers

import (
	"context"
	"math"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// --- Contains ---

func TestContainsHandler_Type(t *testing.T) {
	h := &ContainsHandler{}
	if h.Type() != "contains" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestContainsHandler_AllFound(t *testing.T) {
	h := &ContainsHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		CurrentOutput: "Hello World, this is a Test",
	}
	params := map[string]any{
		"patterns": []any{"hello", "world", "test"},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestContainsHandler_Missing(t *testing.T) {
	h := &ContainsHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		CurrentOutput: "Hello World",
	}
	params := map[string]any{
		"patterns": []any{"hello", "missing"},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for missing pattern")
	}
}

func TestContainsHandler_NoPatterns(t *testing.T) {
	h := &ContainsHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{CurrentOutput: "test"}

	result, err := h.Eval(ctx, evalCtx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail with no patterns")
	}
}

// --- Regex ---

func TestRegexHandler_Type(t *testing.T) {
	h := &RegexHandler{}
	if h.Type() != "regex" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestRegexHandler_Match(t *testing.T) {
	h := &RegexHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		CurrentOutput: "Order #12345 confirmed",
	}
	params := map[string]any{
		"pattern": `#\d{5}`,
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected match: %s", result.Explanation)
	}
}

func TestRegexHandler_NoMatch(t *testing.T) {
	h := &RegexHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		CurrentOutput: "no numbers here",
	}
	params := map[string]any{"pattern": `\d+`}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected no match")
	}
}

func TestRegexHandler_InvalidPattern(t *testing.T) {
	h := &RegexHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{CurrentOutput: "test"}
	params := map[string]any{"pattern": `[invalid`}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Error == "" {
		t.Fatal("expected error for invalid regex")
	}
}

func TestRegexHandler_NoPattern(t *testing.T) {
	h := &RegexHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{CurrentOutput: "test"}

	result, err := h.Eval(ctx, evalCtx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail with no pattern")
	}
}

// --- JSONValid ---

func TestJSONValidHandler_Type(t *testing.T) {
	h := &JSONValidHandler{}
	if h.Type() != "json_valid" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestJSONValidHandler_Valid(t *testing.T) {
	h := &JSONValidHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		CurrentOutput: `{"key": "value", "num": 42}`,
	}

	result, err := h.Eval(ctx, evalCtx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestJSONValidHandler_Invalid(t *testing.T) {
	h := &JSONValidHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		CurrentOutput: `not json at all`,
	}

	result, err := h.Eval(ctx, evalCtx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for invalid JSON")
	}
}

func TestJSONValidHandler_Array(t *testing.T) {
	h := &JSONValidHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		CurrentOutput: `[1, 2, 3]`,
	}

	result, err := h.Eval(ctx, evalCtx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatal("JSON array should be valid")
	}
}

// --- JSONSchema ---

func TestJSONSchemaHandler_Type(t *testing.T) {
	h := &JSONSchemaHandler{}
	if h.Type() != "json_schema" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestJSONSchemaHandler_Valid(t *testing.T) {
	h := &JSONSchemaHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		CurrentOutput: `{"name": "Alice", "age": 30}`,
	}
	params := map[string]any{
		"schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"age":  map[string]any{"type": "integer"},
			},
			"required": []any{"name", "age"},
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

func TestJSONSchemaHandler_Invalid(t *testing.T) {
	h := &JSONSchemaHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		CurrentOutput: `{"name": 123}`,
	}
	params := map[string]any{
		"schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
			"required": []any{"name"},
		},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for schema violation")
	}
}

func TestJSONSchemaHandler_NotJSON(t *testing.T) {
	h := &JSONSchemaHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{CurrentOutput: "not json"}
	params := map[string]any{
		"schema": map[string]any{"type": "object"},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for non-JSON")
	}
}

func TestJSONSchemaHandler_NoSchema(t *testing.T) {
	h := &JSONSchemaHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{CurrentOutput: `{}`}

	result, err := h.Eval(ctx, evalCtx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail with no schema")
	}
}

// --- JSONValid allow_wrapped / extract_json ---

func TestJSONValidHandler_AllowWrapped(t *testing.T) {
	h := &JSONValidHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		CurrentOutput: "Here is the result:\n```json\n{\"name\": \"Alice\"}\n```\nDone.",
	}

	// Without allow_wrapped, should fail (raw text is not JSON).
	result, err := h.Eval(ctx, evalCtx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail without allow_wrapped")
	}

	// With allow_wrapped, should pass.
	result, err = h.Eval(ctx, evalCtx, map[string]any{"allow_wrapped": true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass with allow_wrapped, got: %s", result.Explanation)
	}
}

func TestJSONValidHandler_ExtractJSON(t *testing.T) {
	h := &JSONValidHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		CurrentOutput: `Some text before {"key": "value"} and after`,
	}

	result, err := h.Eval(ctx, evalCtx, map[string]any{"extract_json": true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass with extract_json, got: %s", result.Explanation)
	}
}

// --- JSONSchema allow_wrapped / extract_json ---

func TestJSONSchemaHandler_AllowWrapped(t *testing.T) {
	h := &JSONSchemaHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		CurrentOutput: "Here is the data:\n```json\n{\"name\": \"Bob\", \"age\": 30}\n```\nEnd.",
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"age":  map[string]any{"type": "integer"},
		},
		"required": []any{"name", "age"},
	}

	// Without allow_wrapped, should fail.
	result, err := h.Eval(ctx, evalCtx, map[string]any{"schema": schema})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail without allow_wrapped")
	}

	// With allow_wrapped, should pass.
	result, err = h.Eval(ctx, evalCtx, map[string]any{
		"schema":        schema,
		"allow_wrapped": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass with allow_wrapped, got: %s", result.Explanation)
	}
}

func TestJSONSchemaHandler_ExtractJSON(t *testing.T) {
	h := &JSONSchemaHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		CurrentOutput: `The result is {"name": "Eve", "age": 25} as expected.`,
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"age":  map[string]any{"type": "integer"},
		},
		"required": []any{"name", "age"},
	}

	result, err := h.Eval(ctx, evalCtx, map[string]any{
		"schema":       schema,
		"extract_json": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass with extract_json, got: %s", result.Explanation)
	}
}

// --- ToolsCalled ---

func TestToolsCalledHandler_Type(t *testing.T) {
	h := &ToolsCalledHandler{}
	if h.Type() != "tools_called" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestToolsCalledHandler_AllCalled(t *testing.T) {
	h := &ToolsCalledHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "search"},
			{ToolName: "fetch"},
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

func TestToolsCalledHandler_Missing(t *testing.T) {
	h := &ToolsCalledHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "search"},
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

func TestToolsCalledHandler_MinCalls(t *testing.T) {
	h := &ToolsCalledHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "search"},
		},
	}
	params := map[string]any{
		"tool_names": []any{"search"},
		"min_calls":  float64(3),
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: only 1 call, need 3")
	}
}

func TestToolsCalledHandler_NoToolNames(t *testing.T) {
	h := &ToolsCalledHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{}

	result, err := h.Eval(ctx, evalCtx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail with no tool_names")
	}
}

func TestToolsCalledHandler_LegacyToolsParam(t *testing.T) {
	h := &ToolsCalledHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "search"},
			{ToolName: "calculate"},
		},
	}
	params := map[string]any{
		"tools": []any{"search", "calculate"},
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass with legacy 'tools' param: %s", result.Explanation)
	}
}

// --- ToolsNotCalled ---

func TestToolsNotCalledHandler_Type(t *testing.T) {
	h := &ToolsNotCalledHandler{}
	if h.Type() != "tools_not_called" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestToolsNotCalledHandler_Pass(t *testing.T) {
	h := &ToolsNotCalledHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "search"},
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

func TestToolsNotCalledHandler_Fail(t *testing.T) {
	h := &ToolsNotCalledHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "delete"},
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

func TestToolsNotCalledHandler_LegacyToolsParam(t *testing.T) {
	h := &ToolsNotCalledHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "search"},
		},
	}
	params := map[string]any{
		"tools": []any{"delete", "drop"},
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass with legacy 'tools' param: %s", result.Explanation)
	}
}

func TestToolsNotCalledHandler_IgnoresOtherTurns(t *testing.T) {
	h := &ToolsNotCalledHandler{}
	ctx := context.Background()
	// Tool calls from turn 0, but we're evaluating turn 1
	evalCtx := &evals.EvalContext{
		TurnIndex: 1,
		ToolCalls: []evals.ToolCallRecord{
			{TurnIndex: 0, ToolName: "search"},
			{TurnIndex: 0, ToolName: "calculate"},
		},
	}
	params := map[string]any{
		"tools": []any{"search", "calculate"},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: tools were called on a different turn, got: %s", result.Explanation)
	}
}

func TestToolsCalledHandler_FiltersByTurn(t *testing.T) {
	h := &ToolsCalledHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		TurnIndex: 0,
		ToolCalls: []evals.ToolCallRecord{
			{TurnIndex: 0, ToolName: "search"},
			{TurnIndex: 1, ToolName: "calculate"},
		},
	}
	params := map[string]any{
		"tools": []any{"search"},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: search was called on turn 0, got: %s", result.Explanation)
	}

	// calculate is on turn 1, so shouldn't be found on turn 0
	params2 := map[string]any{
		"tools": []any{"calculate"},
	}
	result2, err := h.Eval(ctx, evalCtx, params2)
	if err != nil {
		t.Fatal(err)
	}
	if result2.Passed {
		t.Fatal("expected fail: calculate was called on turn 1, not turn 0")
	}
}

func TestToolArgsExcludedSession_ForbiddenArgs(t *testing.T) {
	h := &ToolArgsExcludedSessionHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "mock_tool", Arguments: map[string]any{"mode": "debug"}},
		},
	}
	// Legacy forbidden_args format: map of arg name → list of forbidden values
	params := map[string]any{
		"tool_name": "mock_tool",
		"forbidden_args": map[string]any{
			"mode": []any{"debug", "unsafe"},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: mode=debug is forbidden")
	}
}

func TestToolArgsExcludedSession_ForbiddenArgsPass(t *testing.T) {
	h := &ToolArgsExcludedSessionHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "mock_tool", Arguments: map[string]any{"mode": "normal"}},
		},
	}
	params := map[string]any{
		"tool_name": "mock_tool",
		"forbidden_args": map[string]any{
			"mode": []any{"debug", "unsafe"},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: mode=normal is not forbidden: %s", result.Explanation)
	}
}

func TestToolArgsExcludedSession_ExcludedArgs(t *testing.T) {
	h := &ToolArgsExcludedSessionHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "api", Arguments: map[string]any{"key": "secret"}},
		},
	}
	params := map[string]any{
		"tool_name":     "api",
		"excluded_args": map[string]any{"key": "secret"},
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: key=secret is excluded")
	}
}

func TestToolArgsExcludedSession_ForbiddenArgsStringSlice(t *testing.T) {
	h := &ToolArgsExcludedSessionHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "cmd", Arguments: map[string]any{"flag": "verbose"}},
		},
	}
	// Legacy format with []string values (from Go-typed callers)
	params := map[string]any{
		"tool_name": "cmd",
		"forbidden_args": map[string]any{
			"flag": []string{"verbose", "debug"},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: flag=verbose is forbidden")
	}
}

func TestToolArgsExcludedSession_ForbiddenArgsSingleValue(t *testing.T) {
	h := &ToolArgsExcludedSessionHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "cmd", Arguments: map[string]any{"flag": "safe"}},
		},
	}
	// Legacy format with single (non-slice) value
	params := map[string]any{
		"tool_name": "cmd",
		"forbidden_args": map[string]any{
			"flag": "safe",
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: flag=safe is forbidden")
	}
}

func TestToolArgsExcludedSession_ForbiddenArgsMapSliceAny(t *testing.T) {
	h := &ToolArgsExcludedSessionHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{ToolName: "cmd", Arguments: map[string]any{"mode": "debug"}},
		},
	}
	// map[string][]any typed path (from Go callers, not YAML)
	params := map[string]any{
		"tool_name":      "cmd",
		"forbidden_args": map[string][]any{"mode": {"debug", "unsafe"}},
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: mode=debug is forbidden")
	}
}

func TestToolArgsExcludedSession_NoToolName(t *testing.T) {
	h := &ToolArgsExcludedSessionHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: no tool_name")
	}
}

func TestToolArgsExcludedSession_NoForbiddenArgs(t *testing.T) {
	h := &ToolArgsExcludedSessionHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{
		"tool_name": "cmd",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatal("expected pass: no excluded or forbidden args")
	}
}

func TestToolsNotCalledWithArgs_AliasResolution(t *testing.T) {
	r := evals.NewEvalTypeRegistry()
	h, err := r.Get("tools_not_called_with_args")
	if err != nil {
		t.Fatalf("alias not registered: %v", err)
	}
	if h.Type() != "tool_args_excluded_session" {
		t.Fatalf("expected alias to resolve to tool_args_excluded_session, got %s", h.Type())
	}
}

func TestLLMJudgeConversation_AliasResolution(t *testing.T) {
	r := evals.NewEvalTypeRegistry()
	h, err := r.Get("llm_judge_conversation")
	if err != nil {
		t.Fatalf("alias not registered: %v", err)
	}
	if h.Type() != "llm_judge_session" {
		t.Fatalf("expected alias to resolve to llm_judge_session, got %s", h.Type())
	}
}

// --- ToolArgs ---

func TestToolArgsHandler_Type(t *testing.T) {
	h := &ToolArgsHandler{}
	if h.Type() != "tool_args" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestToolArgsHandler_Match(t *testing.T) {
	h := &ToolArgsHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{
				ToolName: "search",
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

func TestToolArgsHandler_Mismatch(t *testing.T) {
	h := &ToolArgsHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			{
				ToolName:  "search",
				Arguments: map[string]any{"query": "python"},
			},
		},
	}
	params := map[string]any{
		"tool_name":     "search",
		"expected_args": map[string]any{"query": "golang"},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: args don't match")
	}
}

func TestToolArgsHandler_ToolNotCalled(t *testing.T) {
	h := &ToolArgsHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{ToolCalls: nil}
	params := map[string]any{
		"tool_name":     "search",
		"expected_args": map[string]any{"q": "test"},
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: tool not called")
	}
}

func TestToolArgsHandler_NoToolName(t *testing.T) {
	h := &ToolArgsHandler{}
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

// --- LatencyBudget ---

func TestLatencyBudgetHandler_Type(t *testing.T) {
	h := &LatencyBudgetHandler{}
	if h.Type() != "latency_budget" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestLatencyBudgetHandler_WithinBudget(t *testing.T) {
	h := &LatencyBudgetHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		Metadata: map[string]any{"latency_ms": 150.0},
	}
	params := map[string]any{"max_ms": 200.0}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Fatalf("expected score 1.0, got %v", result.Score)
	}
}

func TestLatencyBudgetHandler_OverBudget(t *testing.T) {
	h := &LatencyBudgetHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		Metadata: map[string]any{"latency_ms": 500.0},
	}
	params := map[string]any{"max_ms": 200.0}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: over budget")
	}
	if result.Score == nil {
		t.Fatal("expected score to be set")
	}
	if *result.Score >= 1.0 {
		t.Fatalf("expected score < 1.0, got %f", *result.Score)
	}
}

func TestLatencyBudgetHandler_NoMaxMs(t *testing.T) {
	h := &LatencyBudgetHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		Metadata: map[string]any{"latency_ms": 100.0},
	}

	result, err := h.Eval(ctx, evalCtx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: no max_ms")
	}
}

func TestLatencyBudgetHandler_NoLatencyInMetadata(t *testing.T) {
	h := &LatencyBudgetHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		Metadata: map[string]any{},
	}
	params := map[string]any{"max_ms": 200.0}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: no latency_ms in metadata")
	}
}

// --- CosineSimilarity ---

func TestCosineSimilarityHandler_Type(t *testing.T) {
	h := &CosineSimilarityHandler{}
	if h.Type() != "cosine_similarity" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestCosineSimilarityHandler_Identical(t *testing.T) {
	h := &CosineSimilarityHandler{}
	ctx := context.Background()
	ref := []float64{1.0, 0.0, 0.0}
	evalCtx := &evals.EvalContext{
		Metadata: map[string]any{
			"embedding": []float64{1.0, 0.0, 0.0},
		},
	}
	params := map[string]any{
		"reference":      ref,
		"min_similarity": 0.9,
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
	if result.Score == nil {
		t.Fatal("expected score")
	}
	if math.Abs(*result.Score-1.0) > 1e-9 {
		t.Fatalf("expected score ~1.0, got %f", *result.Score)
	}
}

func TestCosineSimilarityHandler_Orthogonal(t *testing.T) {
	h := &CosineSimilarityHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		Metadata: map[string]any{
			"embedding": []float64{0.0, 1.0, 0.0},
		},
	}
	params := map[string]any{
		"reference":      []float64{1.0, 0.0, 0.0},
		"min_similarity": 0.5,
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: orthogonal vectors")
	}
}

func TestCosineSimilarityHandler_DimensionMismatch(t *testing.T) {
	h := &CosineSimilarityHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		Metadata: map[string]any{
			"embedding": []float64{1.0, 0.0},
		},
	}
	params := map[string]any{
		"reference":      []float64{1.0, 0.0, 0.0},
		"min_similarity": 0.5,
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: dimension mismatch")
	}
}

func TestCosineSimilarityHandler_NoReference(t *testing.T) {
	h := &CosineSimilarityHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		Metadata: map[string]any{
			"embedding": []float64{1.0},
		},
	}

	result, err := h.Eval(ctx, evalCtx, map[string]any{
		"min_similarity": 0.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: no reference")
	}
}

func TestCosineSimilarityHandler_NoEmbedding(t *testing.T) {
	h := &CosineSimilarityHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		Metadata: map[string]any{},
	}
	params := map[string]any{
		"reference":      []float64{1.0, 0.0},
		"min_similarity": 0.5,
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: no embedding in metadata")
	}
}

func TestCosineSimilarityHandler_AnySlice(t *testing.T) {
	h := &CosineSimilarityHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		Metadata: map[string]any{
			"embedding": []any{1.0, 0.0, 0.0},
		},
	}
	params := map[string]any{
		"reference":      []any{1.0, 0.0, 0.0},
		"min_similarity": 0.9,
	}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass with []any: %s", result.Explanation)
	}
}

// --- MinLength ---

func TestMinLengthHandler_Type(t *testing.T) {
	h := &MinLengthHandler{}
	if h.Type() != "min_length" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestMinLengthHandler_Pass(t *testing.T) {
	h := &MinLengthHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		CurrentOutput: "Hello, this is a reasonably long response.",
	}
	params := map[string]any{"min": float64(10)}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestMinLengthHandler_Fail(t *testing.T) {
	h := &MinLengthHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		CurrentOutput: "short",
	}
	params := map[string]any{"min": float64(100)}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: output too short")
	}
}

func TestMinLengthHandler_ZeroMin(t *testing.T) {
	h := &MinLengthHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{CurrentOutput: ""}

	result, err := h.Eval(ctx, evalCtx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatal("expected pass: zero min allows empty output")
	}
}

func TestMinLengthHandler_ExactLength(t *testing.T) {
	h := &MinLengthHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{CurrentOutput: "12345"}
	params := map[string]any{"min": float64(5)}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatal("expected pass: exact length matches min")
	}
}

func TestMinLengthHandler_MinCharacters(t *testing.T) {
	h := &MinLengthHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{CurrentOutput: "Hello, world!"}
	params := map[string]any{"min_characters": float64(5)}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass with min_characters: %s", result.Explanation)
	}
}

// --- MaxLength ---

func TestMaxLengthHandler_Type(t *testing.T) {
	h := &MaxLengthHandler{}
	if h.Type() != "max_length" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestMaxLengthHandler_Pass(t *testing.T) {
	h := &MaxLengthHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		CurrentOutput: "short",
	}
	params := map[string]any{"max": float64(100)}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestMaxLengthHandler_Fail(t *testing.T) {
	h := &MaxLengthHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{
		CurrentOutput: "This is a very long response that exceeds the maximum length.",
	}
	params := map[string]any{"max": float64(10)}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: output too long")
	}
}

func TestMaxLengthHandler_ZeroMax(t *testing.T) {
	h := &MaxLengthHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{CurrentOutput: "test"}

	result, err := h.Eval(ctx, evalCtx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: missing max param")
	}
}

func TestMaxLengthHandler_ExactLength(t *testing.T) {
	h := &MaxLengthHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{CurrentOutput: "12345"}
	params := map[string]any{"max": float64(5)}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatal("expected pass: exact length matches max")
	}
}

func TestMaxLengthHandler_MaxCharacters(t *testing.T) {
	h := &MaxLengthHandler{}
	ctx := context.Background()
	evalCtx := &evals.EvalContext{CurrentOutput: "short"}
	params := map[string]any{"max_characters": float64(100)}

	result, err := h.Eval(ctx, evalCtx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass with max_characters: %s", result.Explanation)
	}
}

// --- Registration ---

func TestRegisterInit(t *testing.T) {
	// Verify that init() registered all handlers
	expected := []string{
		// Turn-level deterministic
		"contains",
		"regex",
		"json_valid",
		"json_schema",
		"tools_called",
		"tools_not_called",
		"tool_args",
		"latency_budget",
		"cosine_similarity",

		// Session-level deterministic
		"contains_any",
		"content_excludes",
		"tools_called_session",
		"tools_not_called_session",
		"tool_args_session",
		"tool_args_excluded_session",

		// Tool call handlers (Batch 1)
		"no_tool_errors",
		"tool_call_count",
		"tool_result_includes",
		"tool_result_matches",
		"tool_result_has_media",
		"tool_result_media_type",
		"tool_call_sequence",
		"tool_call_chain",
		"tool_calls_with_args",

		// Tool pattern and efficiency handlers (Batch 1b)
		"tool_anti_pattern",
		"tool_no_repeat",
		"tool_efficiency",
		"cost_budget",

		// JSON path, agent, guardrail handlers (Batch 2)
		"json_path",
		"agent_invoked",
		"agent_not_invoked",
		"agent_response_contains",
		"guardrail_triggered",

		// Property invariant validators (Phase 3)
		"invariant_fields_preserved",

		// Workflow and skill handlers (Batch 3)
		"workflow_complete",
		"state_is",
		"transitioned_to",
		"workflow_transition_order",
		"workflow_tool_access",
		"skill_activated",
		"skill_not_activated",
		"skill_activation_order",

		// Media handlers (Batch 4)
		"image_format",
		"image_dimensions",
		"audio_format",
		"audio_duration",
		"video_duration",
		"video_resolution",

		// LLM judge handlers
		"llm_judge",
		"llm_judge_session",
		"llm_judge_tool_calls",

		// External eval handlers
		"rest_eval",
		"rest_eval_session",
		"a2a_eval",
		"a2a_eval_session",

		// Length validation handlers
		"min_length",
		"max_length",

		// Behavioral testing handlers (Phase 6)
		"outcome_equivalent",
		"directional",

		// Arena assertion type aliases
		"content_includes",
		"content_includes_any",
		"content_matches",
		"content_not_includes",
		"is_valid_json",
		"valid_json",
		"tool_called",
		"tools_not_called_with_args",
		"llm_judge_conversation",
	}

	r := evals.NewEvalTypeRegistry()
	types := r.Types()

	if len(types) != len(expected) {
		t.Fatalf(
			"expected %d types, got %d: %v",
			len(expected), len(types), types,
		)
	}

	for _, e := range expected {
		if !r.Has(e) {
			t.Errorf("missing handler: %s", e)
		}
	}
}
