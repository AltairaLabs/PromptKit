package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// LatencyBudgetHandler checks Metadata["latency_ms"] against a max.
// Params: max_ms float64.
type LatencyBudgetHandler struct{}

// Type returns the eval type identifier.
func (h *LatencyBudgetHandler) Type() string {
	return "latency_budget"
}

// Eval checks that the latency is within budget.
func (h *LatencyBudgetHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (result *evals.EvalResult, err error) {
	maxMs, ok := extractFloat64(params, "max_ms")
	if !ok {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "no max_ms specified",
		}, nil
	}

	latencyMs, ok := extractFloat64(
		evalCtx.Metadata, "latency_ms",
	)
	if !ok {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "latency_ms not found in metadata",
		}, nil
	}

	passed := latencyMs <= maxMs
	score := 1.0
	if latencyMs > 0 {
		score = maxMs / latencyMs
		if score > 1.0 {
			score = 1.0
		}
	}

	explanation := fmt.Sprintf(
		"latency %.1fms vs budget %.1fms", latencyMs, maxMs,
	)

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      passed,
		Score:       &score,
		Explanation: explanation,
	}, nil
}

// extractFloat64 extracts a float64 from a map, handling int and
// float types.
func extractFloat64(
	m map[string]any, key string,
) (val float64, ok bool) {
	v, exists := m[key]
	if !exists {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
