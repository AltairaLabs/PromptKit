package assertions

import (
	"context"
	"fmt"
)

// ToolResultMatchesConversationValidator checks that tool results match a regex pattern
// across the conversation.
// Params:
// - tool: string (optional) — if set, only check calls to this tool
// - pattern: string — regex pattern to match
// - occurrence: int (default 1) — minimum number of calls matching the pattern
// Type: "tool_result_matches"
type ToolResultMatchesConversationValidator struct{}

// Type returns the validator type name.
func (v *ToolResultMatchesConversationValidator) Type() string { return "tool_result_matches" }

// NewToolResultMatchesConversationValidator constructs a conversation-level tool_result_matches validator.
func NewToolResultMatchesConversationValidator() ConversationValidator {
	return &ToolResultMatchesConversationValidator{}
}

// ValidateConversation checks regex patterns on tool results across the conversation.
func (v *ToolResultMatchesConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	tool, _ := params["tool"].(string)
	pattern, _ := params["pattern"].(string)
	occurrence := extractIntParam(params, "occurrence", 1)

	if pattern == "" {
		return ConversationValidationResult{
			Passed:  true,
			Message: "no pattern to check",
		}
	}

	views := toolCallViewsFromRecords(convCtx.ToolCalls)
	matchCount, err := coreToolResultMatches(views, tool, pattern)

	if err != nil {
		return ConversationValidationResult{
			Passed:  false,
			Message: fmt.Sprintf("invalid regex: %s", err.Error()),
			Details: map[string]interface{}{
				"error":   "invalid_regex",
				"pattern": pattern,
			},
		}
	}

	if matchCount >= occurrence {
		return ConversationValidationResult{
			Passed:  true,
			Message: "pattern matched in tool results",
			Details: map[string]interface{}{
				"match_count": matchCount,
			},
		}
	}

	return ConversationValidationResult{
		Passed: false,
		Message: fmt.Sprintf(
			"expected %d call(s) matching pattern, found %d",
			occurrence, matchCount,
		),
		Details: map[string]interface{}{
			"pattern": pattern,
			"tool":    tool,
		},
	}
}
