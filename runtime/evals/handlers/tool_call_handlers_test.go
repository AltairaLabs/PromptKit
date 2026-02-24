package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func toolCall(name string, args map[string]any, result any, errStr string) types.ToolCallRecord {
	return types.ToolCallRecord{
		ToolName:  name,
		Arguments: args,
		Result:    result,
		Error:     errStr,
	}
}

// --- NoToolErrors ---

func TestNoToolErrorsHandler_Type(t *testing.T) {
	h := &NoToolErrorsHandler{}
	if h.Type() != "no_tool_errors" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestNoToolErrorsHandler_Pass(t *testing.T) {
	h := &NoToolErrorsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, "result", ""),
			toolCall("fetch", nil, "data", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestNoToolErrorsHandler_Fail(t *testing.T) {
	h := &NoToolErrorsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, "result", ""),
			toolCall("fetch", nil, nil, "timeout error"),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for tool error")
	}
}

func TestNoToolErrorsHandler_ScopedTools(t *testing.T) {
	h := &NoToolErrorsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, nil, "error"),
			toolCall("fetch", nil, "ok", ""),
		},
	}

	// Only check "fetch" — should pass
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tools": []any{"fetch"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatal("expected pass when scoped to fetch")
	}
}

// --- ToolCallCount ---

func TestToolCallCountHandler_Type(t *testing.T) {
	h := &ToolCallCountHandler{}
	if h.Type() != "tool_call_count" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestToolCallCountHandler_Pass(t *testing.T) {
	h := &ToolCallCountHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, "r", ""),
			toolCall("search", nil, "r", ""),
			toolCall("fetch", nil, "r", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool": "search", "min": 1, "max": 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestToolCallCountHandler_BelowMin(t *testing.T) {
	h := &ToolCallCountHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, "r", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool": "search", "min": 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail below min")
	}
}

func TestToolCallCountHandler_AboveMax(t *testing.T) {
	h := &ToolCallCountHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, "r", ""),
			toolCall("search", nil, "r", ""),
			toolCall("search", nil, "r", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool": "search", "max": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail above max")
	}
}

// --- ToolResultIncludes ---

func TestToolResultIncludesHandler_Type(t *testing.T) {
	h := &ToolResultIncludesHandler{}
	if h.Type() != "tool_result_includes" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestToolResultIncludesHandler_Pass(t *testing.T) {
	h := &ToolResultIncludesHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, "Found 3 results for query", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":     "search",
		"patterns": []any{"found", "results"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestToolResultIncludesHandler_MissingPattern(t *testing.T) {
	h := &ToolResultIncludesHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, "no matches", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":     "search",
		"patterns": []any{"results"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for missing pattern")
	}
}

func TestToolResultIncludesHandler_NoPatterns(t *testing.T) {
	h := &ToolResultIncludesHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail with no patterns")
	}
}

// --- ToolResultMatches ---

func TestToolResultMatchesHandler_Type(t *testing.T) {
	h := &ToolResultMatchesHandler{}
	if h.Type() != "tool_result_matches" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestToolResultMatchesHandler_Pass(t *testing.T) {
	h := &ToolResultMatchesHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, "Order #12345 confirmed", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":    "search",
		"pattern": `#\d{5}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestToolResultMatchesHandler_NoMatch(t *testing.T) {
	h := &ToolResultMatchesHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, "no order", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":    "search",
		"pattern": `#\d{5}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for no match")
	}
}

func TestToolResultMatchesHandler_InvalidRegex(t *testing.T) {
	h := &ToolResultMatchesHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{
		"pattern": `[invalid`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for invalid regex")
	}
}

func TestToolResultMatchesHandler_NoPattern(t *testing.T) {
	h := &ToolResultMatchesHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail with no pattern")
	}
}

// --- ToolCallSequence ---

func TestToolCallSequenceHandler_Type(t *testing.T) {
	h := &ToolCallSequenceHandler{}
	if h.Type() != "tool_call_sequence" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestToolCallSequenceHandler_Pass(t *testing.T) {
	h := &ToolCallSequenceHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, "", ""),
			toolCall("fetch", nil, "", ""),
			toolCall("process", nil, "", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{"search", "process"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestToolCallSequenceHandler_Fail(t *testing.T) {
	h := &ToolCallSequenceHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("process", nil, "", ""),
			toolCall("search", nil, "", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"sequence": []any{"search", "process"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// "process" appears before "search", so subsequence "search" → "process" only matches "search"
	// but then "process" has already passed, so it can't match. Actually wait — subsequence check:
	// we look for "search" first. tc[0] is "process" → no match. tc[1] is "search" → match (1/2).
	// Then we look for "process" — no more calls. So matched=1 < 2, fail.
	if result.Passed {
		t.Fatal("expected fail for wrong order")
	}
}

func TestToolCallSequenceHandler_EmptySequence(t *testing.T) {
	h := &ToolCallSequenceHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatal("empty sequence should pass")
	}
}

// --- ToolCallChain ---

func TestToolCallChainHandler_Type(t *testing.T) {
	h := &ToolCallChainHandler{}
	if h.Type() != "tool_call_chain" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestToolCallChainHandler_Pass(t *testing.T) {
	h := &ToolCallChainHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", map[string]any{"q": "test"}, "found items", ""),
			toolCall("fetch", map[string]any{"id": "123"}, "item details", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"steps": []any{
			map[string]any{"tool": "search", "result_includes": []any{"found"}},
			map[string]any{"tool": "fetch", "no_error": true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestToolCallChainHandler_IncompleteChain(t *testing.T) {
	h := &ToolCallChainHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, "found", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"steps": []any{
			map[string]any{"tool": "search"},
			map[string]any{"tool": "fetch"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for incomplete chain")
	}
}

func TestToolCallChainHandler_StepViolation(t *testing.T) {
	h := &ToolCallChainHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, "nothing", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"steps": []any{
			map[string]any{"tool": "search", "result_includes": []any{"found"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for step constraint violation")
	}
}

func TestToolCallChainHandler_EmptySteps(t *testing.T) {
	h := &ToolCallChainHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatal("empty chain should pass")
	}
}

// --- ToolCallsWithArgs ---

func TestToolCallsWithArgsHandler_Type(t *testing.T) {
	h := &ToolCallsWithArgsHandler{}
	if h.Type() != "tool_calls_with_args" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestToolCallsWithArgsHandler_ExactArgs(t *testing.T) {
	h := &ToolCallsWithArgsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", map[string]any{"query": "test", "limit": "10"}, "", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool_name":     "search",
		"expected_args": map[string]any{"query": "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestToolCallsWithArgsHandler_ArgMismatch(t *testing.T) {
	h := &ToolCallsWithArgsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", map[string]any{"query": "wrong"}, "", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool_name":     "search",
		"expected_args": map[string]any{"query": "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for arg mismatch")
	}
}

func TestToolCallsWithArgsHandler_PatternArgs(t *testing.T) {
	h := &ToolCallsWithArgsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", map[string]any{"query": "hello world"}, "", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool_name":  "search",
		"args_match": map[string]any{"query": "hello.*"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestToolCallsWithArgsHandler_ToolNotCalled(t *testing.T) {
	h := &ToolCallsWithArgsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("other", nil, "", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool_name": "search",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail when tool not called")
	}
}

func TestToolCallsWithArgsHandler_ResultConstraints(t *testing.T) {
	h := &ToolCallsWithArgsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, "found 5 items", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool_name":       "search",
		"result_includes": []any{"found"},
		"no_error":        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestToolCallsWithArgsHandler_ErrorViolation(t *testing.T) {
	h := &ToolCallsWithArgsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, "", "connection refused"),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool_name": "search",
		"no_error":  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for tool error")
	}
}

// --- validatePatternArgs additional coverage ---

func TestToolCallsWithArgsHandler_PatternMissing(t *testing.T) {
	h := &ToolCallsWithArgsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", map[string]any{}, "", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool_name":  "search",
		"args_match": map[string]any{"query": "hello.*"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for missing argument in pattern match")
	}
}

func TestToolCallsWithArgsHandler_PatternInvalid(t *testing.T) {
	h := &ToolCallsWithArgsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", map[string]any{"query": "test"}, "", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool_name":  "search",
		"args_match": map[string]any{"query": "[invalid"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for invalid pattern")
	}
}

func TestToolCallsWithArgsHandler_PatternMismatch(t *testing.T) {
	h := &ToolCallsWithArgsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", map[string]any{"query": "goodbye"}, "", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool_name":  "search",
		"args_match": map[string]any{"query": "^hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for pattern mismatch")
	}
}

// --- validateExactArgs additional coverage ---

func TestToolCallsWithArgsHandler_ExactArgMissing(t *testing.T) {
	h := &ToolCallsWithArgsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", map[string]any{}, "", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool_name":     "search",
		"expected_args": map[string]any{"query": "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for missing argument")
	}
}

func TestToolCallsWithArgsHandler_ExactArgNilExpected(t *testing.T) {
	// When expected value is nil, only existence matters.
	h := &ToolCallsWithArgsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", map[string]any{"query": "anything"}, "", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool_name":     "search",
		"expected_args": map[string]any{"query": nil},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass when expected is nil (existence check): %s", result.Explanation)
	}
}

// --- validateResultConstraints additional coverage ---

func TestToolCallsWithArgsHandler_ResultIncludesMissing(t *testing.T) {
	h := &ToolCallsWithArgsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, "some result", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool_name":       "search",
		"result_includes": []any{"missing_pattern"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for missing result pattern")
	}
}

func TestToolCallsWithArgsHandler_ResultMatchesInvalid(t *testing.T) {
	h := &ToolCallsWithArgsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, "result", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool_name":      "search",
		"result_matches": "[invalid",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for invalid result_matches regex")
	}
}

func TestToolCallsWithArgsHandler_ResultMatchesMismatch(t *testing.T) {
	h := &ToolCallsWithArgsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", nil, "some result", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool_name":      "search",
		"result_matches": "^no_match$",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail for result_matches mismatch")
	}
}

func TestToolCallsWithArgsHandler_NoToolNameMatchesAll(t *testing.T) {
	h := &ToolCallsWithArgsHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("search", map[string]any{"q": "test"}, "ok", ""),
			toolCall("fetch", map[string]any{"q": "test"}, "ok", ""),
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"expected_args": map[string]any{"q": "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass matching all tools: %s", result.Explanation)
	}
}
