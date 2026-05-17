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
//   - max_calls int — maximum calls per tool (default unbounded, use 0 to forbid).
//     When any listed tool exceeds max_calls the assertion fails hard with
//     score 0, regardless of min_calls satisfaction.
//   - ignore_validation bool — count validation failures as successful (default false)
//   - require_args bool — only count calls with non-empty arguments (default false)
type ToolsCalledHandler struct{}

// maxCallsUnbounded is the sentinel meaning "no max_calls constraint."
// Chosen as -1 so callers can pass 0 to mean "tool must not be called."
const maxCallsUnbounded = -1

// Result-value map keys for the ToolsCalledHandler result.
const (
	keyFound     = "found"
	keyExpected  = "expected"
	keyMissing   = "missing"
	keyOverLimit = "over_limit"
)

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
	maxCalls := extractInt(params, "max_calls", maxCallsUnbounded)
	ignoreValidation := extractBool(params, "ignore_validation")
	requireArgs := extractBool(params, "require_args")
	callCounts := buildCallCounts(evalCtx.ToolCalls, ignoreValidation, requireArgs)

	return h.checkToolCalls(toolNames, callCounts, minCalls, maxCalls)
}

// checkToolCalls verifies each tool was called within the configured
// min/max bounds. min_calls is a ratio-scored check (partial credit);
// max_calls is a hard pass/fail (any exceeded tool → score 0).
func (h *ToolsCalledHandler) checkToolCalls(
	toolNames []string,
	callCounts map[string]int,
	minCalls, maxCalls int,
) (result *evals.EvalResult, err error) {
	var missing, overLimit []string
	for _, name := range toolNames {
		count := callCounts[name]
		if count < minCalls {
			missing = append(missing, fmt.Sprintf(
				"%s (called %d, need %d)",
				name, count, minCalls,
			))
		}
		if maxCalls >= 0 && count > maxCalls {
			overLimit = append(overLimit, fmt.Sprintf(
				"%s (called %d, max %d)",
				name, count, maxCalls,
			))
		}
	}

	explanation := "all expected tools were called within bounds"
	switch {
	case len(overLimit) > 0 && len(missing) > 0:
		explanation = fmt.Sprintf(
			"tools called outside bounds: under min: %s; over max: %s",
			strings.Join(missing, ", "), strings.Join(overLimit, ", "),
		)
	case len(overLimit) > 0:
		explanation = fmt.Sprintf(
			"tools called too many times: %s",
			strings.Join(overLimit, ", "),
		)
	case len(missing) > 0:
		explanation = fmt.Sprintf(
			"tools not called enough: %s",
			strings.Join(missing, ", "),
		)
	}

	found := len(toolNames) - len(missing)
	// Over-limit is a hard fail regardless of how many min_calls were
	// satisfied — exceeding the cap means the agent did the wrong thing,
	// not a partially-correct thing.
	score := ratioScore(found, len(toolNames))
	if len(overLimit) > 0 {
		score = boolScore(false)
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       score,
		Explanation: explanation,
		Value: map[string]any{
			keyFound:     found,
			keyExpected:  len(toolNames),
			keyMissing:   missing,
			keyOverLimit: overLimit,
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
