package assertions

import (
	"context"
	"fmt"
)

// NoToolErrorsConversationValidator checks that no tool calls across the conversation returned errors.
// Params:
// - tools: []string (optional) — if set, only check these tool names
// Type: "no_tool_errors"
type NoToolErrorsConversationValidator struct{}

// Type returns the validator type name.
func (v *NoToolErrorsConversationValidator) Type() string { return "no_tool_errors" }

// NewNoToolErrorsConversationValidator constructs a conversation-level no_tool_errors validator.
func NewNoToolErrorsConversationValidator() ConversationValidator {
	return &NoToolErrorsConversationValidator{}
}

// ValidateConversation checks that no tool calls returned errors across the conversation.
func (v *NoToolErrorsConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	tools := extractStringSlice(params, "tools")
	views := toolCallViewsFromRecords(convCtx.ToolCalls)
	errors := coreNoToolErrors(views, tools)

	if len(errors) > 0 {
		violations := make([]ConversationViolation, len(errors))
		for i, e := range errors {
			turnIdx, _ := e["index"].(int)
			violations[i] = ConversationViolation{
				TurnIndex:   turnIdx,
				Description: "tool call returned error",
				Evidence: map[string]interface{}{
					"tool":  e["tool"],
					"error": e["error"],
				},
			}
		}
		return ConversationValidationResult{
			Passed:     false,
			Message:    fmt.Sprintf("%d tool call(s) returned errors", len(errors)),
			Violations: violations,
		}
	}

	return ConversationValidationResult{
		Passed:  true,
		Message: "all tool calls succeeded",
	}
}
