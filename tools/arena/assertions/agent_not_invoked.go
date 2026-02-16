package assertions

import (
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// AgentNotInvokedValidator checks that forbidden agents were NOT called in the response.
// In multi-agent packs, agent invocations appear as tool calls where the tool name
// matches the agent member name.
// Params: agents: []string - agent names that should not have been called.
type AgentNotInvokedValidator struct {
	forbiddenAgents []string
}

// NewAgentNotInvokedValidator creates a new agent_not_invoked validator from params.
func NewAgentNotInvokedValidator(params map[string]interface{}) runtimeValidators.Validator {
	agents := extractStringSlice(params, "agents")
	return &AgentNotInvokedValidator{forbiddenAgents: agents}
}

// Validate checks if any forbidden agents were invoked as tool calls.
func (v *AgentNotInvokedValidator) Validate(
	content string,
	params map[string]interface{},
) runtimeValidators.ValidationResult {
	toolCalls := resolveToolCalls(params)
	called := findForbiddenCalled(toolCalls, v.forbiddenAgents)

	return runtimeValidators.ValidationResult{
		Passed: len(called) == 0,
		Details: map[string]interface{}{
			"forbidden_agents_called": called,
		},
	}
}
