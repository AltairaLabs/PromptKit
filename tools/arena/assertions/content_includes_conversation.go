package assertions

import (
	"context"
	"strings"
)

// ContentIncludesConversationValidator checks that at least one assistant message
// contains all specified patterns. Works at both turn level (workflow steps) and
// conversation level.
// Params:
//   - patterns: []string
//
// Type: "content_includes"
type ContentIncludesConversationValidator struct{}

// Type returns the validator type name.
func (v *ContentIncludesConversationValidator) Type() string { return "content_includes" }

// NewContentIncludesConversationValidator constructs a validator instance.
func NewContentIncludesConversationValidator() ConversationValidator {
	return &ContentIncludesConversationValidator{}
}

// ValidateConversation checks if any assistant response contains all patterns.
func (v *ContentIncludesConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	patterns := extractStringSlice(params, "patterns")
	if len(patterns) == 0 {
		return ConversationValidationResult{
			Passed:  true,
			Message: "no patterns specified",
		}
	}

	for i := range convCtx.AllTurns {
		msg := convCtx.AllTurns[i]
		if !strings.EqualFold(msg.Role, roleAssistant) {
			continue
		}
		content := strings.ToLower(msg.GetContent())

		allFound := true
		for _, p := range patterns {
			if !strings.Contains(content, strings.ToLower(p)) {
				allFound = false
				break
			}
		}
		if allFound {
			return ConversationValidationResult{
				Passed:  true,
				Message: "response contains all patterns",
				Details: map[string]interface{}{
					"turn":     i,
					"patterns": patterns,
				},
			}
		}
	}

	return ConversationValidationResult{
		Passed:  false,
		Message: "no response contained all patterns",
		Details: map[string]interface{}{"patterns": patterns},
	}
}
