package assertions

import (
	"context"
	"fmt"
)

// ToolResultIncludesConversationValidator checks that tool results contain expected substrings
// across the conversation.
// Params:
// - tool: string (optional) — if set, only check calls to this tool
// - patterns: []string — substrings that must all be present
// - occurrence: int (default 1) — minimum number of calls with all patterns
// Type: "tool_result_includes"
type ToolResultIncludesConversationValidator struct{}

// Type returns the validator type name.
func (v *ToolResultIncludesConversationValidator) Type() string { return "tool_result_includes" }

// NewToolResultIncludesConversationValidator constructs a conversation-level tool_result_includes validator.
func NewToolResultIncludesConversationValidator() ConversationValidator {
	return &ToolResultIncludesConversationValidator{}
}

// ValidateConversation checks substring patterns in tool results across the conversation.
func (v *ToolResultIncludesConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	tool, _ := params["tool"].(string)
	patterns := extractStringSlice(params, "patterns")
	occurrence := extractIntParam(params, "occurrence", 1)

	if len(patterns) == 0 {
		return ConversationValidationResult{
			Passed:  true,
			Message: "no patterns to check",
		}
	}

	views := toolCallViewsFromRecords(convCtx.ToolCalls)
	matchCount, missingDetails := coreToolResultIncludes(views, tool, patterns)

	if matchCount >= occurrence {
		return ConversationValidationResult{
			Passed:  true,
			Message: "patterns found in tool results",
			Details: map[string]interface{}{
				"match_count": matchCount,
			},
		}
	}

	violations := make([]ConversationViolation, len(missingDetails))
	for i, m := range missingDetails {
		turnIdx, _ := m["index"].(int)
		violations[i] = ConversationViolation{
			TurnIndex:   turnIdx,
			Description: "tool result missing patterns",
			Evidence: map[string]interface{}{
				"tool":             m["tool"],
				"missing_patterns": m["missing_patterns"],
			},
		}
	}

	return ConversationValidationResult{
		Passed: false,
		Message: fmt.Sprintf(
			"expected %d call(s) with all patterns, found %d",
			occurrence, matchCount,
		),
		Violations: violations,
	}
}
