package assertions

import (
	"context"
	"strings"
)

// AgentInvokedConversationValidator checks that specific agents were invoked
// at least a minimum number of times across the full conversation.
// In multi-agent packs, agent invocations appear as tool calls where the tool name
// matches the agent member name.
// Params:
//   - agent_names: []string required agent names
//   - min_calls: int optional (default 1) minimum calls per agent
//
// Type: "agent_invoked"
type AgentInvokedConversationValidator struct{}

// Type returns the validator type name.
func (v *AgentInvokedConversationValidator) Type() string { return "agent_invoked" }

// NewAgentInvokedConversationValidator constructs a conversation-level agent_invoked validator.
func NewAgentInvokedConversationValidator() ConversationValidator {
	return &AgentInvokedConversationValidator{}
}

// ValidateConversation evaluates whether all required agents were invoked
// at least the minimum number of times across the conversation.
func (v *AgentInvokedConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	// Extract params
	required := extractStringSlice(params, "agent_names")
	minCalls := 1
	if m, ok := params["min_calls"].(int); ok && m > 0 {
		minCalls = m
	}

	// Count calls by tool name (agent invocations are tool calls)
	counts := make(map[string]int)
	for _, tc := range convCtx.ToolCalls {
		counts[tc.ToolName]++
	}

	// Find missing agents w.r.t minCalls
	var missing []string
	requirements := make([]map[string]interface{}, 0, len(required))
	for _, name := range required {
		requirements = append(requirements, map[string]interface{}{
			"agent":         name,
			"calls":         counts[name],
			"requiredCalls": minCalls,
		})
		if counts[name] < minCalls {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		return ConversationValidationResult{
			Passed:  false,
			Message: "missing required agent invocations: " + strings.Join(missing, ", "),
			Details: map[string]interface{}{
				"requirements": requirements,
				"counts":       counts,
			},
		}
	}

	return ConversationValidationResult{
		Passed:  true,
		Message: "all required agents were invoked",
		Details: map[string]interface{}{
			"requirements": requirements,
			"counts":       counts,
		},
	}
}
