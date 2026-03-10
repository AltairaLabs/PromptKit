package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ToolsNotCalledSessionHandler checks that specific tools were NOT
// called anywhere in the session.
// Params: tool_names []string.
type ToolsNotCalledSessionHandler struct{}

// Type returns the eval type identifier.
func (h *ToolsNotCalledSessionHandler) Type() string {
	return "tools_not_called_session"
}

// Eval ensures forbidden tools were never called across the session.
func (h *ToolsNotCalledSessionHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (_ *evals.EvalResult, _ error) {
	forbidden := extractStringSlice(params, "tool_names")
	if len(forbidden) == 0 {
		forbidden = extractStringSlice(params, "tools")
	}
	forbiddenSet := make(map[string]struct{}, len(forbidden))
	for _, n := range forbidden {
		forbiddenSet[n] = struct{}{}
	}

	var found []string
	for i := range evalCtx.ToolCalls {
		tc := &evalCtx.ToolCalls[i]
		if _, bad := forbiddenSet[tc.ToolName]; bad {
			found = append(found, fmt.Sprintf(
				"%s at turn %d", tc.ToolName, tc.TurnIndex,
			))
		}
	}

	passed := len(found) == 0

	if !passed {
		return &evals.EvalResult{
			Type:   h.Type(),
			Passed: false,
			Score:  boolScore(false),
			Explanation: fmt.Sprintf(
				"forbidden tools were called: %s",
				strings.Join(found, "; "),
			),
			Value: map[string]any{
				"forbidden_called": found,
			},
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      true,
		Score:       boolScore(true),
		Explanation: "no forbidden tools called",
		Value: map[string]any{
			"forbidden_called": []string{},
		},
	}, nil
}
