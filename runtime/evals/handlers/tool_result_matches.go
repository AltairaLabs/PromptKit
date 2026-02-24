package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ToolResultMatchesHandler checks that tool results match a regex pattern.
// Params: tool string, pattern string, occurrence int (optional, default 1).
type ToolResultMatchesHandler struct{}

// Type returns the eval type identifier.
func (h *ToolResultMatchesHandler) Type() string { return "tool_result_matches" }

// Eval checks a regex pattern on tool results.
func (h *ToolResultMatchesHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	views := viewsFromRecords(evalCtx.ToolCalls)
	tool, _ := params["tool"].(string)
	pattern, _ := params["pattern"].(string)
	occurrence := extractInt(params, "occurrence", 1)

	if pattern == "" {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "no pattern specified",
		}, nil
	}

	matchCount, err := coreToolResultMatches(views, tool, pattern)
	if err != nil {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: fmt.Sprintf("invalid regex: %v", err),
		}, nil
	}

	passed := matchCount >= occurrence
	explanation := fmt.Sprintf("%d call(s) matched pattern (required: %d)", matchCount, occurrence)
	if !passed {
		explanation = fmt.Sprintf("only %d call(s) matched pattern (required: %d)", matchCount, occurrence)
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      passed,
		Explanation: explanation,
		Details: map[string]any{
			"match_count": matchCount,
			"occurrence":  occurrence,
			"pattern":     pattern,
		},
	}, nil
}
