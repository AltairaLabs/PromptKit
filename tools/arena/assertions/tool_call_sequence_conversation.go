package assertions

import (
	"context"
	"fmt"
	"strings"
)

// ToolCallSequenceConversationValidator checks that tool calls across the conversation
// follow a specified order (subsequence matching).
// Params:
// - sequence: []string — expected tool call order
// Type: "tool_call_sequence"
type ToolCallSequenceConversationValidator struct{}

// Type returns the validator type name.
func (v *ToolCallSequenceConversationValidator) Type() string { return "tool_call_sequence" }

// NewToolCallSequenceConversationValidator constructs a conversation-level tool_call_sequence validator.
func NewToolCallSequenceConversationValidator() ConversationValidator {
	return &ToolCallSequenceConversationValidator{}
}

// ValidateConversation checks tool call ordering across the entire conversation.
func (v *ToolCallSequenceConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	sequence := extractStringSlice(params, "sequence")

	if len(sequence) == 0 {
		return ConversationValidationResult{
			Passed:  true,
			Message: "empty sequence always passes",
		}
	}

	views := toolCallViewsFromRecords(convCtx.ToolCalls)
	matched, actual := coreToolCallSequence(views, sequence)

	if matched < len(sequence) {
		return ConversationValidationResult{
			Passed: false,
			Message: fmt.Sprintf(
				"sequence not satisfied: matched %d/%d steps, stuck at %q",
				matched, len(sequence), sequence[matched],
			),
			Details: map[string]interface{}{
				"expected_sequence": sequence,
				"actual_tools":      strings.Join(actual, " → "),
				"matched_steps":     matched,
			},
		}
	}

	return ConversationValidationResult{
		Passed:  true,
		Message: "sequence satisfied",
		Details: map[string]interface{}{
			"matched_steps": len(sequence),
			"total_calls":   len(views),
		},
	}
}
