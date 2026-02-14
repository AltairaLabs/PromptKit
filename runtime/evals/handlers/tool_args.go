package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ToolArgsHandler checks that a tool was called with specific args.
// Params: tool_name string, expected_args map[string]any.
type ToolArgsHandler struct{}

// Type returns the eval type identifier.
func (h *ToolArgsHandler) Type() string { return "tool_args" }

// Eval checks that the specified tool was called with matching args.
func (h *ToolArgsHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (result *evals.EvalResult, err error) {
	toolName, _ := params["tool_name"].(string)
	if toolName == "" {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "no tool_name specified",
		}, nil
	}

	expectedArgs, _ := params["expected_args"].(map[string]any)
	if len(expectedArgs) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "no expected_args specified",
		}, nil
	}

	matching := findMatchingCalls(evalCtx.ToolCalls, toolName)
	if len(matching) == 0 {
		return &evals.EvalResult{
			Type:   h.Type(),
			Passed: false,
			Explanation: fmt.Sprintf(
				"tool %q was not called", toolName,
			),
		}, nil
	}

	return h.checkArgs(matching, expectedArgs, toolName)
}

// checkArgs verifies at least one matching call has the expected
// arguments.
func (h *ToolArgsHandler) checkArgs(
	matching []evals.ToolCallRecord,
	expectedArgs map[string]any,
	toolName string,
) (result *evals.EvalResult, err error) {
	for i := range matching {
		if argsMatch(matching[i].Arguments, expectedArgs) {
			return &evals.EvalResult{
				Type:   h.Type(),
				Passed: true,
				Explanation: fmt.Sprintf(
					"tool %q called with expected args",
					toolName,
				),
			}, nil
		}
	}

	violations := buildArgViolations(
		matching[len(matching)-1], expectedArgs,
	)

	return &evals.EvalResult{
		Type:   h.Type(),
		Passed: false,
		Explanation: fmt.Sprintf(
			"tool %q args mismatch: %s",
			toolName, strings.Join(violations, "; "),
		),
	}, nil
}

// buildArgViolations reports mismatches from a single tool call
// against expected args.
func buildArgViolations(
	call evals.ToolCallRecord, expectedArgs map[string]any,
) []string {
	var violations []string
	for k, expected := range expectedArgs {
		actual, exists := call.Arguments[k]
		if !exists {
			violations = append(violations, fmt.Sprintf(
				"missing arg %q", k,
			))
		} else if asString(actual) != asString(expected) {
			violations = append(violations, fmt.Sprintf(
				"arg %q: got %v, want %v",
				k, actual, expected,
			))
		}
	}
	return violations
}

// findMatchingCalls returns tool call records matching the given
// tool name.
func findMatchingCalls(
	toolCalls []evals.ToolCallRecord, toolName string,
) []evals.ToolCallRecord {
	var matching []evals.ToolCallRecord
	for i := range toolCalls {
		if toolCalls[i].ToolName == toolName {
			matching = append(matching, toolCalls[i])
		}
	}
	return matching
}

// argsMatch checks if actual arguments contain all expected args
// with matching string representations.
func argsMatch(
	actual map[string]any, expected map[string]any,
) bool {
	for k, expectedVal := range expected {
		actualVal, exists := actual[k]
		if !exists {
			return false
		}
		if asString(actualVal) != asString(expectedVal) {
			return false
		}
	}
	return true
}
