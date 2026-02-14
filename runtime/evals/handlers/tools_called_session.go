package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ToolsCalledSessionHandler checks that specific tools were called
// across the full session.
// Params: tool_names []string, min_calls int (optional, default 1).
type ToolsCalledSessionHandler struct{}

// Type returns the eval type identifier.
func (h *ToolsCalledSessionHandler) Type() string {
	return "tools_called_session"
}

// Eval checks that all required tools were called at least
// min_calls times.
func (h *ToolsCalledSessionHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (_ *evals.EvalResult, _ error) {
	required := extractStringSlice(params, "tool_names")
	minCalls := 1
	if m, ok := params["min_calls"].(int); ok && m > 0 {
		minCalls = m
	}
	if mf, ok := params["min_calls"].(float64); ok && mf > 0 {
		minCalls = int(mf)
	}

	counts := make(map[string]int)
	for i := range evalCtx.ToolCalls {
		counts[evalCtx.ToolCalls[i].ToolName]++
	}

	var missing []string
	for _, name := range required {
		if counts[name] < minCalls {
			missing = append(missing, fmt.Sprintf(
				"%s (got %d, need %d)",
				name, counts[name], minCalls,
			))
		}
	}

	if len(missing) > 0 {
		return &evals.EvalResult{
			Type:   h.Type(),
			Passed: false,
			Explanation: fmt.Sprintf(
				"missing required tools: %s",
				strings.Join(missing, "; "),
			),
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      true,
		Explanation: "all required tools were called",
	}, nil
}
