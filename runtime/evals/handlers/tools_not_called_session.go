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

	if len(found) > 0 {
		return &evals.EvalResult{
			Type:   h.Type(),
			Passed: false,
			Explanation: fmt.Sprintf(
				"forbidden tools were called: %s",
				strings.Join(found, "; "),
			),
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      true,
		Explanation: "no forbidden tools called",
	}, nil
}
