package assertions

import (
	"context"
)

// ToolsNotCalledConversationValidator checks that specific tools were NOT called
// anywhere in the conversation.
// Params:
// - tool_names: []string forbidden tools
// Type: "tools_not_called"
type ToolsNotCalledConversationValidator struct{}

// Type returns the validator type name.
func (v *ToolsNotCalledConversationValidator) Type() string { return "tools_not_called" }

// NewToolsNotCalledConversationValidator constructs a conversation-level tools_not_called validator.
func NewToolsNotCalledConversationValidator() ConversationValidator {
	return &ToolsNotCalledConversationValidator{}
}

// ValidateConversation ensures forbidden tools were never called across the conversation.
func (v *ToolsNotCalledConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	forbidden := extractStringSlice(params, "tool_names")
	forbiddenSet := make(map[string]struct{}, len(forbidden))
	for _, n := range forbidden {
		forbiddenSet[n] = struct{}{}
	}

	var violations []ConversationViolation
	for _, tc := range convCtx.ToolCalls {
		if _, bad := forbiddenSet[tc.ToolName]; bad {
			violations = append(violations, ConversationViolation{
				TurnIndex:   tc.TurnIndex,
				Description: "forbidden tool was called",
				Evidence: map[string]interface{}{
					"tool":      tc.ToolName,
					"arguments": tc.Arguments,
				},
			})
		}
	}

	if len(violations) > 0 {
		return ConversationValidationResult{
			Passed:     false,
			Message:    "forbidden tools were called",
			Violations: violations,
		}
	}

	return ConversationValidationResult{Passed: true, Message: "no forbidden tools called"}
}
