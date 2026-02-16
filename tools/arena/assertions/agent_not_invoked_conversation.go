package assertions

import (
	"context"
)

// AgentNotInvokedConversationValidator checks that specific agents were NOT invoked
// anywhere in the conversation.
// In multi-agent packs, agent invocations appear as tool calls where the tool name
// matches the agent member name.
// Params:
//   - agent_names: []string forbidden agent names
//
// Type: "agent_not_invoked"
type AgentNotInvokedConversationValidator struct{}

// Type returns the validator type name.
func (v *AgentNotInvokedConversationValidator) Type() string { return "agent_not_invoked" }

// NewAgentNotInvokedConversationValidator constructs a conversation-level agent_not_invoked validator.
func NewAgentNotInvokedConversationValidator() ConversationValidator {
	return &AgentNotInvokedConversationValidator{}
}

// ValidateConversation ensures forbidden agents were never invoked across the conversation.
func (v *AgentNotInvokedConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	forbidden := extractStringSlice(params, "agent_names")
	forbiddenSet := make(map[string]struct{}, len(forbidden))
	for _, n := range forbidden {
		forbiddenSet[n] = struct{}{}
	}

	var violations []ConversationViolation
	for _, tc := range convCtx.ToolCalls {
		if _, bad := forbiddenSet[tc.ToolName]; bad {
			violations = append(violations, ConversationViolation{
				TurnIndex:   tc.TurnIndex,
				Description: "forbidden agent was invoked",
				Evidence: map[string]interface{}{
					"agent":     tc.ToolName,
					"arguments": tc.Arguments,
				},
			})
		}
	}

	if len(violations) > 0 {
		return ConversationValidationResult{
			Passed:     false,
			Message:    "forbidden agents were invoked",
			Violations: violations,
		}
	}

	return ConversationValidationResult{Passed: true, Message: "no forbidden agents invoked"}
}
