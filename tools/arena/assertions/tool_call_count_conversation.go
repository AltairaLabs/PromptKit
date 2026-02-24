package assertions

import (
	"context"
)

// ToolCallCountConversationValidator checks count constraints on tool calls across the conversation.
// Params:
// - tool: string (optional) — if set, only count calls to this tool
// - min: int (optional) — minimum number of calls
// - max: int (optional) — maximum number of calls
// Type: "tool_call_count"
type ToolCallCountConversationValidator struct{}

// Type returns the validator type name.
func (v *ToolCallCountConversationValidator) Type() string { return "tool_call_count" }

// NewToolCallCountConversationValidator constructs a conversation-level tool_call_count validator.
func NewToolCallCountConversationValidator() ConversationValidator {
	return &ToolCallCountConversationValidator{}
}

// ValidateConversation checks tool call count constraints across the conversation.
func (v *ToolCallCountConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	tool, _ := params["tool"].(string)
	minCount := extractIntParam(params, "min", countNotSet)
	maxCount := extractIntParam(params, "max", countNotSet)

	views := toolCallViewsFromRecords(convCtx.ToolCalls)
	count, violation := coreToolCallCount(views, tool, minCount, maxCount)

	details := map[string]interface{}{
		"count": count,
	}
	if tool != "" {
		details["tool"] = tool
	}

	if violation != "" {
		details["message"] = violation
		return ConversationValidationResult{
			Passed:  false,
			Message: violation,
			Details: details,
		}
	}

	details["message"] = countWithinBound
	return ConversationValidationResult{
		Passed:  true,
		Message: countWithinBound,
		Details: details,
	}
}
