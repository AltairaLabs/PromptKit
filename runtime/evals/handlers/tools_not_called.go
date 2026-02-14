package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ToolsNotCalledHandler checks that specific tools were NOT called.
// Params: tool_names []string.
type ToolsNotCalledHandler struct{}

// Type returns the eval type identifier.
func (h *ToolsNotCalledHandler) Type() string {
	return "tools_not_called"
}

// Eval checks that none of the forbidden tools were called.
func (h *ToolsNotCalledHandler) Eval(
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

	forbidden := make(map[string]bool, len(toolNames))
	for _, name := range toolNames {
		forbidden[name] = true
	}

	called := findForbiddenCalls(evalCtx.ToolCalls, forbidden)

	passed := len(called) == 0
	explanation := "none of the forbidden tools were called"
	if !passed {
		explanation = fmt.Sprintf(
			"forbidden tools were called: %s",
			strings.Join(called, ", "),
		)
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      passed,
		Explanation: explanation,
	}, nil
}

// findForbiddenCalls returns unique names of forbidden tools that
// were called.
func findForbiddenCalls(
	toolCalls []evals.ToolCallRecord,
	forbidden map[string]bool,
) []string {
	seen := make(map[string]bool)
	var called []string
	for i := range toolCalls {
		name := toolCalls[i].ToolName
		if forbidden[name] && !seen[name] {
			called = append(called, name)
			seen[name] = true
		}
	}
	return called
}
