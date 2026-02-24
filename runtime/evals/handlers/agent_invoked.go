package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// AgentInvokedHandler checks that expected agents were invoked as tool calls.
// Params: agents []string — list of agent names that should have been called.
type AgentInvokedHandler struct{}

// Type returns the eval type identifier.
func (h *AgentInvokedHandler) Type() string { return "agent_invoked" }

// Eval checks if expected agents were invoked.
func (h *AgentInvokedHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	agents := extractStringSlice(params, "agents")
	if len(agents) == 0 {
		return &evals.EvalResult{
			Type: h.Type(), Passed: false,
			Explanation: "no agents specified",
		}, nil
	}

	calledSet := make(map[string]bool)
	for _, tc := range evalCtx.ToolCalls {
		calledSet[tc.ToolName] = true
	}

	var missing []string
	for _, agent := range agents {
		if !calledSet[agent] {
			missing = append(missing, agent)
		}
	}

	if len(missing) > 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: fmt.Sprintf("missing agents: %s", strings.Join(missing, ", ")),
			Details:     map[string]any{"missing_agents": missing},
		}, nil
	}

	return &evals.EvalResult{
		Type: h.Type(), Passed: true,
		Explanation: "all expected agents were invoked",
	}, nil
}
