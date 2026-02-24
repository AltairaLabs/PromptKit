package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// AgentResponseContainsHandler checks that a specific agent's response
// contains expected text. Agent responses appear as tool-result messages
// where the tool name matches the agent name.
// Params: agent string, contains string.
type AgentResponseContainsHandler struct{}

// Type returns the eval type identifier.
func (h *AgentResponseContainsHandler) Type() string { return "agent_response_contains" }

// Eval checks if the specified agent's tool result contains the expected text.
func (h *AgentResponseContainsHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	agent, _ := params["agent"].(string)
	contains, _ := params["contains"].(string)

	if agent == "" {
		return &evals.EvalResult{
			Type: h.Type(), Passed: false,
			Explanation: "no agent specified",
		}, nil
	}

	// Check tool call records for matching agent
	for _, tc := range evalCtx.ToolCalls {
		if tc.ToolName != agent {
			continue
		}
		resultStr := asString(tc.Result)
		if strings.Contains(resultStr, contains) {
			return &evals.EvalResult{
				Type: h.Type(), Passed: true,
				Explanation: fmt.Sprintf("agent %q response contains expected text", agent),
			}, nil
		}
	}

	// Also check messages for tool result messages
	for i := range evalCtx.Messages {
		msg := &evalCtx.Messages[i]
		if msg.Role == "tool" && msg.ToolResult != nil && msg.ToolResult.Name == agent {
			if strings.Contains(msg.ToolResult.Content, contains) {
				return &evals.EvalResult{
					Type: h.Type(), Passed: true,
					Explanation: fmt.Sprintf("agent %q response contains expected text", agent),
				}, nil
			}
		}
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      false,
		Explanation: fmt.Sprintf("no response from agent %q containing %q", agent, contains),
		Details:     map[string]any{"agent": agent, "expected_substr": contains},
	}, nil
}
