package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ToolCallCountHandler checks the count of tool calls within bounds.
// Params: tool string (optional), min int (optional), max int (optional).
type ToolCallCountHandler struct{}

// Type returns the eval type identifier.
func (h *ToolCallCountHandler) Type() string { return "tool_call_count" }

// Eval counts matching tool calls and checks min/max bounds.
func (h *ToolCallCountHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	views := viewsFromRecords(evalCtx.ToolCalls)
	tool, _ := params["tool"].(string)
	minCount := extractInt(params, "min", countNotSet)
	maxCount := extractInt(params, "max", countNotSet)

	count, violation := coreToolCallCount(views, tool, minCount, maxCount)

	value := map[string]any{"count": count, "tool": tool}

	if violation != "" {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Value:       value,
			Explanation: violation,
			Details:     map[string]any{"tool": tool, "count": count},
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       boolScore(true),
		Value:       value,
		Explanation: fmt.Sprintf("tool call count %d is within bounds", count),
		Details:     map[string]any{"tool": tool, "count": count},
	}, nil
}
