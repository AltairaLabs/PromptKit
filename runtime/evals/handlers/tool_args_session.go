package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ToolArgsSessionHandler checks that a tool was called with specific
// arguments across the session.
// Params: tool_name string, expected_args map[string]any.
type ToolArgsSessionHandler struct{}

// Type returns the eval type identifier.
func (h *ToolArgsSessionHandler) Type() string {
	return "tool_args_session"
}

// Eval checks tool calls for expected arguments.
func (h *ToolArgsSessionHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (_ *evals.EvalResult, _ error) {
	toolName, _ := params["tool_name"].(string)
	expectedArgs, _ := params["expected_args"].(map[string]any)

	if toolName == "" {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "tool_name parameter is required",
		}, nil
	}

	if len(expectedArgs) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      true,
			Explanation: "no expected_args to check",
		}, nil
	}

	matched := false
	var mismatches []string
	for i := range evalCtx.ToolCalls {
		tc := &evalCtx.ToolCalls[i]
		if tc.ToolName != toolName {
			continue
		}
		if argsMatch(tc.Arguments, expectedArgs) {
			matched = true
			break
		}
		mismatches = append(mismatches, fmt.Sprintf(
			"turn %d: args=%v", tc.TurnIndex, tc.Arguments,
		))
	}

	if matched {
		return &evals.EvalResult{
			Type:   h.Type(),
			Passed: true,
			Explanation: fmt.Sprintf(
				"%s was called with expected args", toolName,
			),
		}, nil
	}

	if len(mismatches) == 0 {
		return &evals.EvalResult{
			Type:   h.Type(),
			Passed: false,
			Explanation: fmt.Sprintf(
				"tool %q was never called", toolName,
			),
		}, nil
	}

	return &evals.EvalResult{
		Type:   h.Type(),
		Passed: false,
		Explanation: fmt.Sprintf(
			"%s called but args did not match: %s",
			toolName, strings.Join(mismatches, "; "),
		),
	}, nil
}
