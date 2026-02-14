package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ToolsCalledHandler checks if specific tools were called.
// Params: tool_names []string, optional min_calls int.
type ToolsCalledHandler struct{}

// Type returns the eval type identifier.
func (h *ToolsCalledHandler) Type() string { return "tools_called" }

// Eval checks that all expected tools were called.
func (h *ToolsCalledHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (result *evals.EvalResult, err error) {
	toolNames := extractStringSlice(params, "tool_names")
	if len(toolNames) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "no tool_names specified",
		}, nil
	}

	minCalls := extractInt(params, "min_calls", 1)
	callCounts := buildCallCounts(evalCtx.ToolCalls)

	return h.checkToolCalls(toolNames, callCounts, minCalls)
}

// checkToolCalls verifies each tool was called the minimum number
// of times.
func (h *ToolsCalledHandler) checkToolCalls(
	toolNames []string,
	callCounts map[string]int,
	minCalls int,
) (result *evals.EvalResult, err error) {
	var missing []string
	for _, name := range toolNames {
		if callCounts[name] < minCalls {
			missing = append(missing, fmt.Sprintf(
				"%s (called %d, need %d)",
				name, callCounts[name], minCalls,
			))
		}
	}

	passed := len(missing) == 0
	explanation := "all expected tools were called"
	if !passed {
		explanation = fmt.Sprintf(
			"tools not called enough: %s",
			strings.Join(missing, ", "),
		)
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      passed,
		Explanation: explanation,
	}, nil
}

// buildCallCounts counts how many times each tool was called.
func buildCallCounts(
	toolCalls []evals.ToolCallRecord,
) map[string]int {
	counts := make(map[string]int)
	for i := range toolCalls {
		counts[toolCalls[i].ToolName]++
	}
	return counts
}

// extractInt extracts an int from params with a default value.
func extractInt(
	params map[string]any, key string, defaultVal int,
) int {
	v, ok := params[key]
	if !ok {
		return defaultVal
	}
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case int64:
		return int(val)
	default:
		return defaultVal
	}
}
