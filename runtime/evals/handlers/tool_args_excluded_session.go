package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ToolArgsExcludedSessionHandler checks that a tool was NOT called
// with specific argument values across the session.
// Params: tool_name string, excluded_args map[string]any.
type ToolArgsExcludedSessionHandler struct{}

// Type returns the eval type identifier.
func (h *ToolArgsExcludedSessionHandler) Type() string {
	return "tool_args_excluded_session"
}

// Eval ensures the tool was never called with excluded args.
func (h *ToolArgsExcludedSessionHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (_ *evals.EvalResult, _ error) {
	toolName, _ := params["tool_name"].(string)
	excludedArgs, _ := params["excluded_args"].(map[string]any)

	if toolName == "" {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "tool_name parameter is required",
		}, nil
	}

	if len(excludedArgs) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      true,
			Explanation: "no excluded_args to check",
		}, nil
	}

	var violations []string
	for i := range evalCtx.ToolCalls {
		tc := &evalCtx.ToolCalls[i]
		if tc.ToolName != toolName {
			continue
		}
		violations = append(
			violations,
			checkExcludedArgs(tc, excludedArgs)...,
		)
	}

	if len(violations) > 0 {
		return &evals.EvalResult{
			Type:   h.Type(),
			Passed: false,
			Explanation: fmt.Sprintf(
				"excluded args found: %s",
				strings.Join(violations, "; "),
			),
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      true,
		Explanation: "no excluded args detected",
	}, nil
}

// checkExcludedArgs returns descriptions for each excluded arg match
// found in a single tool call.
func checkExcludedArgs(
	tc *evals.ToolCallRecord,
	excluded map[string]any,
) []string {
	var results []string
	for k, ev := range excluded {
		av, ok := tc.Arguments[k]
		if !ok {
			continue
		}
		if asString(av) == asString(ev) {
			results = append(results, fmt.Sprintf(
				"turn %d: %s=%v", tc.TurnIndex, k, av,
			))
		}
	}
	return results
}
