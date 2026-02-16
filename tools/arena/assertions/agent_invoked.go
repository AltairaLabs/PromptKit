// Package assertions provides turn-level and conversation-level validators
// for Arena test scenarios.
package assertions

import (
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// AgentInvokedValidator checks that expected agents were invoked as tools in the response.
// In multi-agent packs, agent invocations appear as tool calls where the tool name
// matches the agent member name.
// Params: agents: []string - list of agent names that should have been called.
type AgentInvokedValidator struct {
	expectedAgents []string
}

// NewAgentInvokedValidator creates a new agent_invoked validator from params.
func NewAgentInvokedValidator(params map[string]interface{}) runtimeValidators.Validator {
	agents := extractStringSlice(params, "agents")
	return &AgentInvokedValidator{expectedAgents: agents}
}

// Validate checks if expected agents were invoked as tool calls.
func (v *AgentInvokedValidator) Validate(
	content string,
	params map[string]interface{},
) runtimeValidators.ValidationResult {
	toolCalls := resolveToolCalls(params)
	missing := findMissing(toolCalls, v.expectedAgents)

	return runtimeValidators.ValidationResult{
		Passed: len(missing) == 0,
		Details: map[string]interface{}{
			"missing_agents": missing,
		},
	}
}
