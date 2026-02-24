package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// AgentNotInvokedHandler checks that forbidden agents were NOT called.
// Params: agents []string — agent names that should not have been called.
type AgentNotInvokedHandler struct{}

// Type returns the eval type identifier.
func (h *AgentNotInvokedHandler) Type() string { return "agent_not_invoked" }

// Eval checks if any forbidden agents were invoked.
func (h *AgentNotInvokedHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	agents := extractStringSlice(params, "agents")
	if len(agents) == 0 {
		return &evals.EvalResult{
			Type: h.Type(), Passed: true,
			Explanation: "no agents specified to exclude",
		}, nil
	}

	forbiddenSet := make(map[string]bool)
	for _, a := range agents {
		forbiddenSet[a] = true
	}

	var called []string
	for _, tc := range evalCtx.ToolCalls {
		if forbiddenSet[tc.ToolName] {
			called = append(called, tc.ToolName)
		}
	}

	if len(called) > 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: fmt.Sprintf("forbidden agents called: %s", strings.Join(called, ", ")),
			Details:     map[string]any{"forbidden_agents_called": called},
		}, nil
	}

	return &evals.EvalResult{
		Type: h.Type(), Passed: true,
		Explanation: "no forbidden agents were invoked",
	}, nil
}
