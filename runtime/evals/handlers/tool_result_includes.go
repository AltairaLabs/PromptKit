package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ToolResultIncludesHandler checks that tool results contain expected substrings.
// Params: tool string, patterns []string, occurrence int (optional, default 1).
type ToolResultIncludesHandler struct{}

// Type returns the eval type identifier.
func (h *ToolResultIncludesHandler) Type() string { return "tool_result_includes" }

// Eval checks substring patterns in tool results.
func (h *ToolResultIncludesHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	views := viewsFromRecords(evalCtx.ToolCalls)
	tool, _ := params["tool"].(string)
	patterns := extractStringSlice(params, "patterns")
	occurrence := extractInt(params, "occurrence", 1)

	if len(patterns) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "no patterns specified",
		}, nil
	}

	matchCount, missingDetails := coreToolResultIncludes(views, tool, patterns)

	passed := matchCount >= occurrence
	explanation := fmt.Sprintf("%d call(s) matched all patterns (required: %d)", matchCount, occurrence)
	if !passed {
		explanation = fmt.Sprintf("only %d call(s) matched all patterns (required: %d)", matchCount, occurrence)
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      passed,
		Explanation: explanation,
		Details: map[string]any{
			"match_count":     matchCount,
			"occurrence":      occurrence,
			"missing_details": missingDetails,
		},
	}, nil
}
