package assertions

import (
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// AgentResponseContainsValidator checks that a specific agent's response contains expected text.
// When an agent is invoked as a tool call, its response comes back as a tool result message
// (Role="tool") with a ToolResult containing the agent name and content.
// Params:
//   - agent: string - the agent name whose response to check
//   - contains: string - the substring to look for in the agent's response
type AgentResponseContainsValidator struct {
	agent    string
	contains string
}

// NewAgentResponseContainsValidator creates a new agent_response_contains validator from params.
func NewAgentResponseContainsValidator(params map[string]interface{}) runtimeValidators.Validator {
	agent, _ := params["agent"].(string)
	contains, _ := params["contains"].(string)
	return &AgentResponseContainsValidator{agent: agent, contains: contains}
}

// Validate checks if the specified agent's tool result contains the expected text.
func (v *AgentResponseContainsValidator) Validate(
	content string,
	params map[string]interface{},
) runtimeValidators.ValidationResult {
	// Look for tool result messages from the specified agent in turn messages
	if messages, ok := params["_turn_messages"].([]types.Message); ok {
		for i := range messages {
			msg := &messages[i]
			// Skip messages loaded from history
			if msg.Source == sourceStatestore {
				continue
			}

			// Tool result messages have Role="tool" and ToolResult containing the agent's response
			if msg.Role == "tool" && msg.ToolResult != nil && msg.ToolResult.Name == v.agent {
				if strings.Contains(msg.ToolResult.Content, v.contains) {
					return runtimeValidators.ValidationResult{
						Passed: true,
						Details: map[string]interface{}{
							"agent":         v.agent,
							"found_content": msg.ToolResult.Content,
						},
					}
				}
			}
		}
	}

	// Legacy: check _message_tool_calls for matching agent name
	// Tool results are not available in legacy params, so we cannot check content
	return runtimeValidators.ValidationResult{
		Passed: false,
		Details: map[string]interface{}{
			"agent":           v.agent,
			"expected_substr": v.contains,
			"reason":          "no matching agent response found containing expected text",
		},
	}
}
