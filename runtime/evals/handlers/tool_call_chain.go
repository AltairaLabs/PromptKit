package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ToolCallChainHandler checks a dependency chain of tool calls with per-step constraints.
// Params: steps []map — each with tool, result_includes, result_matches, args_match, no_error.
type ToolCallChainHandler struct{}

// Type returns the eval type identifier.
func (h *ToolCallChainHandler) Type() string { return "tool_call_chain" }

// Eval checks that the chain of tool calls satisfies all step constraints in order.
func (h *ToolCallChainHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	steps := parseChainSteps(params)

	if len(steps) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      true,
			Explanation: "empty chain always passes",
		}, nil
	}

	views := viewsFromRecords(evalCtx.ToolCalls)
	completed, failure := coreToolCallChain(views, steps)

	if failure != nil {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: fmt.Sprintf("%v", failure["message"]),
			Details:     failure,
		}, nil
	}

	if completed < len(steps) {
		return &evals.EvalResult{
			Type:   h.Type(),
			Passed: false,
			Explanation: fmt.Sprintf(
				"chain incomplete: satisfied %d/%d steps, missing %q",
				completed, len(steps), steps[completed].tool,
			),
			Details: map[string]any{
				"completed_steps": completed,
				"total_steps":     len(steps),
			},
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      true,
		Explanation: "chain fully satisfied",
		Details: map[string]any{
			"completed_steps": len(steps),
		},
	}, nil
}
