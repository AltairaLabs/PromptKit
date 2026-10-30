package handlers

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
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
			Score:       boolScore(false),
			Explanation: fmt.Sprintf("tool %q was not called", toolName),
			Details:     map[string]any{keyToolName: toolName},
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
		Score:       boolScore(passed),
		Explanation: explanation,
		Value: map[string]any{
			keyMatchingCalls:  len(matching),
			keyViolationCount: len(violations),
			keyToolName:       toolName,
		},
		Details: map[string]any{
			keyViolations:    violations,
			keyMatchingCalls: len(matching),
		},
	}, nil
}

func validateExactArgs(tc toolCallView, expectedArgs map[string]any) []map[string]any {
	var violations []map[string]any
	for argName, expectedValue := range expectedArgs {
		actualValue, exists := tc.Args[argName]
		if !exists {
			violations = append(violations, map[string]any{
				keyType: "missing_argument", valueKeyTool: tc.Name, keyArgument: argName,
			})
			continue
		}
		if expectedValue != nil && asString(actualValue) != asString(expectedValue) {
			violations = append(violations, map[string]any{
				keyType: "value_mismatch", valueKeyTool: tc.Name, keyArgument: argName,
				keyExpected: expectedValue, keyActual: actualValue,
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
				keyType: "missing_argument_for_pattern", valueKeyTool: tc.Name,
				keyArgument: argName, keyPattern: pattern,
			})
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			violations = append(violations, map[string]any{
				keyType: "invalid_pattern", valueKeyTool: tc.Name, keyArgument: argName,
				keyPattern: pattern, valueKeyError: err.Error(),
			})
			continue
		}
		if !re.MatchString(asString(actualValue)) {
			violations = append(violations, map[string]any{
				keyType: "pattern_mismatch", valueKeyTool: tc.Name, keyArgument: argName,
				keyPattern: pattern, keyActual: actualValue,
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
			keyType: "tool_error", valueKeyTool: tc.Name, valueKeyError: tc.Error,
		})
	}

	if len(resultIncludes) > 0 {
		resultLower := strings.ToLower(tc.Result)
		for _, pattern := range resultIncludes {
			if !strings.Contains(resultLower, strings.ToLower(pattern)) {
				violations = append(violations, map[string]any{
					keyType: "result_missing_pattern", valueKeyTool: tc.Name, keyPattern: pattern,
				})
			}
		}
	}

	if resultMatches != "" {
		re, err := regexp.Compile(resultMatches)
		if err != nil {
			violations = append(violations, map[string]any{
				keyType: "invalid_result_pattern", valueKeyTool: tc.Name,
				keyPattern: resultMatches, valueKeyError: err.Error(),
			})
		} else if !re.MatchString(tc.Result) {
			violations = append(violations, map[string]any{
				keyType: "result_pattern_mismatch", valueKeyTool: tc.Name, keyPattern: resultMatches,
			})
		}
	}

	return violations
}
