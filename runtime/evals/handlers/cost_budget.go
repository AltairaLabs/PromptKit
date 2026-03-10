package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// CostBudgetHandler checks conversation-level cost and token limits.
// Params:
//   - max_cost_usd: float64 — maximum total cost in USD (optional)
//   - max_input_tokens: int — maximum input tokens (optional)
//   - max_output_tokens: int — maximum output tokens (optional)
//   - max_total_tokens: int — maximum total tokens (optional)
type CostBudgetHandler struct{}

// Type returns the eval type identifier.
func (h *CostBudgetHandler) Type() string { return "cost_budget" }

// Eval checks cost and token limits.
func (h *CostBudgetHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	maxCostUSD := extractFloat64Ptr(params, "max_cost_usd")
	maxInputTokens := extractIntPtr(params, "max_input_tokens")
	maxOutputTokens := extractIntPtr(params, "max_output_tokens")
	maxTotalTokens := extractIntPtr(params, "max_total_tokens")

	if maxCostUSD == nil && maxInputTokens == nil && maxOutputTokens == nil && maxTotalTokens == nil {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "no budget thresholds specified",
		}, nil
	}

	totalCost, _ := extractFloat64(evalCtx.Metadata, "total_cost")
	inputTokens := extractIntFromMeta(evalCtx.Metadata, "input_tokens")
	outputTokens := extractIntFromMeta(evalCtx.Metadata, "output_tokens")
	totalTokens := inputTokens + outputTokens

	var failures []string

	if maxCostUSD != nil && totalCost > *maxCostUSD {
		failures = append(failures,
			fmt.Sprintf("cost $%.4f exceeds budget $%.4f", totalCost, *maxCostUSD))
	}
	if maxInputTokens != nil && inputTokens > *maxInputTokens {
		failures = append(failures,
			fmt.Sprintf("input tokens %d exceeds max %d", inputTokens, *maxInputTokens))
	}
	if maxOutputTokens != nil && outputTokens > *maxOutputTokens {
		failures = append(failures,
			fmt.Sprintf("output tokens %d exceeds max %d", outputTokens, *maxOutputTokens))
	}
	if maxTotalTokens != nil && totalTokens > *maxTotalTokens {
		failures = append(failures,
			fmt.Sprintf("total tokens %d exceeds max %d", totalTokens, *maxTotalTokens))
	}

	details := map[string]any{
		"total_cost_usd": totalCost,
		"input_tokens":   inputTokens,
		"output_tokens":  outputTokens,
		"total_tokens":   totalTokens,
	}

	value := map[string]any{
		"total_cost_usd": totalCost,
		"input_tokens":   inputTokens,
		"output_tokens":  outputTokens,
		"total_tokens":   totalTokens,
	}

	if len(failures) > 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Value:       value,
			Explanation: fmt.Sprintf("budget exceeded: %s", strings.Join(failures, "; ")),
			Details:     details,
		}, nil
	}

	// Score: ratio of budget used (lower is better, capped at 1.0)
	score := 1.0
	if maxCostUSD != nil && *maxCostUSD > 0 {
		score = 1.0 - totalCost / *maxCostUSD
		if score < 0 {
			score = 0
		}
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      true,
		Score:       &score,
		Value:       value,
		Explanation: fmt.Sprintf("within budget: $%.4f, %d tokens", totalCost, totalTokens),
		Details:     details,
	}, nil
}

// extractIntFromMeta extracts an int from metadata, handling float64 from JSON.
func extractIntFromMeta(m map[string]any, key string) int {
	v, exists := m[key]
	if !exists {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
