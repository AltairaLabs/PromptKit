package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ToolsCalledHandler checks if specific tools were called successfully.
//
// By default, only counts tool calls that completed without error.
// Use ignore_validation: true to also count calls that failed argument validation.
//
// Params:
//   - tool_names/tools []string — required tool names
//   - min_calls int — minimum calls per tool (default 1)
//   - ignore_validation bool — count validation failures as successful (default false)
//   - require_args bool — only count calls with non-empty arguments (default false)
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
		toolNames = extractStringSlice(params, "tools")
	}
	if len(toolNames) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Explanation: "no tool_names specified",
		}, nil
	}

	minCalls := extractInt(params, "min_calls", 1)
	ignoreValidation := extractBool(params, "ignore_validation")
	requireArgs := extractBool(params, "require_args")
	callCounts := buildCallCounts(evalCtx.ToolCalls, ignoreValidation, requireArgs)

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

	found := len(toolNames) - len(missing)

	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       ratioScore(found, len(toolNames)),
		Explanation: explanation,
		Value: map[string]any{
			"found":    found,
			"expected": len(toolNames),
			"missing":  missing,
		},
	}, nil
}

// buildCallCounts counts how many times each tool was called successfully.
// A call is considered successful when it has no error, or when
// ignoreValidation is true and the error is a validation failure.
// When requireArgs is true, calls with nil/empty arguments are not counted.
func buildCallCounts(
	toolCalls []evals.ToolCallRecord, ignoreValidation, requireArgs bool,
) map[string]int {
	counts := make(map[string]int)
	for i := range toolCalls {
		if !shouldCountCall(&toolCalls[i], ignoreValidation, requireArgs) {
			continue
		}
		counts[toolCalls[i].ToolName]++
	}
	return counts
}

// shouldCountCall determines whether a tool call should be counted as successful.
func shouldCountCall(tc *evals.ToolCallRecord, ignoreValidation, requireArgs bool) bool {
	if tc.Error != "" && (!ignoreValidation || tc.ErrorType != types.ToolErrorValidation) {
		return false
	}
	if requireArgs && len(tc.Arguments) == 0 {
		return false
	}
	return true
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
