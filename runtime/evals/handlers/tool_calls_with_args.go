package handlers

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ToolCallsWithArgsHandler checks that a tool was called with expected arguments.
// Supports exact value matching (expected_args), regex pattern matching (args_match),
// and result-level constraints (result_includes, result_matches, no_error).
type ToolCallsWithArgsHandler struct{}

// Type returns the eval type identifier.
func (h *ToolCallsWithArgsHandler) Type() string { return "tool_calls_with_args" }

// Eval checks tool calls for argument and result constraints.
func (h *ToolCallsWithArgsHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	views := viewsFromRecords(evalCtx.ToolCalls)
	toolName, _ := params["tool_name"].(string)
	expectedArgs := extractMapAny(params, "expected_args")
	argsMatch := extractMapStringString(params, "args_match")
	resultIncludes := extractStringSlice(params, "result_includes")
	resultMatches, _ := params["result_matches"].(string)
	noError := extractBool(params, "no_error")

	// Find matching tool calls
	var matching []toolCallView
	for _, v := range views {
		if toolName == "" || v.Name == toolName {
			matching = append(matching, v)
		}
	}

	if toolName != "" && len(matching) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: fmt.Sprintf("tool %q was not called", toolName),
			Details:     map[string]any{"tool_name": toolName},
		}, nil
	}

	var violations []map[string]any

	for _, tc := range matching {
		violations = append(violations, validateExactArgs(tc, expectedArgs)...)
		violations = append(violations, validatePatternArgs(tc, argsMatch)...)
		violations = append(violations, validateResultConstraints(tc, resultIncludes, resultMatches, noError)...)
	}

	passed := len(violations) == 0
	explanation := fmt.Sprintf("%d matching call(s), %d violation(s)", len(matching), len(violations))

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      passed,
		Explanation: explanation,
		Details: map[string]any{
			"violations":     violations,
			"matching_calls": len(matching),
		},
	}, nil
}

func validateExactArgs(tc toolCallView, expectedArgs map[string]any) []map[string]any {
	var violations []map[string]any
	for argName, expectedValue := range expectedArgs {
		actualValue, exists := tc.Args[argName]
		if !exists {
			violations = append(violations, map[string]any{
				"type": "missing_argument", "tool": tc.Name, "argument": argName,
			})
			continue
		}
		if expectedValue != nil && asString(actualValue) != asString(expectedValue) {
			violations = append(violations, map[string]any{
				"type": "value_mismatch", "tool": tc.Name, "argument": argName,
				"expected": expectedValue, "actual": actualValue,
			})
		}
	}
	return violations
}

func validatePatternArgs(tc toolCallView, argsMatch map[string]string) []map[string]any {
	var violations []map[string]any
	for argName, pattern := range argsMatch {
		actualValue, exists := tc.Args[argName]
		if !exists {
			violations = append(violations, map[string]any{
				"type": "missing_argument_for_pattern", "tool": tc.Name,
				"argument": argName, "pattern": pattern,
			})
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			violations = append(violations, map[string]any{
				"type": "invalid_pattern", "tool": tc.Name, "argument": argName,
				"pattern": pattern, "error": err.Error(),
			})
			continue
		}
		if !re.MatchString(asString(actualValue)) {
			violations = append(violations, map[string]any{
				"type": "pattern_mismatch", "tool": tc.Name, "argument": argName,
				"pattern": pattern, "actual": actualValue,
			})
		}
	}
	return violations
}

func validateResultConstraints(
	tc toolCallView, resultIncludes []string, resultMatches string, noError bool,
) []map[string]any {
	var violations []map[string]any

	if noError && tc.Error != "" {
		violations = append(violations, map[string]any{
			"type": "tool_error", "tool": tc.Name, "error": tc.Error,
		})
	}

	if len(resultIncludes) > 0 {
		resultLower := strings.ToLower(tc.Result)
		for _, pattern := range resultIncludes {
			if !strings.Contains(resultLower, strings.ToLower(pattern)) {
				violations = append(violations, map[string]any{
					"type": "result_missing_pattern", "tool": tc.Name, "pattern": pattern,
				})
			}
		}
	}

	if resultMatches != "" {
		re, err := regexp.Compile(resultMatches)
		if err != nil {
			violations = append(violations, map[string]any{
				"type": "invalid_result_pattern", "tool": tc.Name,
				"pattern": resultMatches, "error": err.Error(),
			})
		} else if !re.MatchString(tc.Result) {
			violations = append(violations, map[string]any{
				"type": "result_pattern_mismatch", "tool": tc.Name, "pattern": resultMatches,
			})
		}
	}

	return violations
}
