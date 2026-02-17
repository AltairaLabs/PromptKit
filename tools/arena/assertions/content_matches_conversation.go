package assertions

import (
	"context"
	"regexp"
	"strings"
)

// ContentMatchesConversationValidator checks that at least one assistant message
// matches a regex pattern. Works at both turn level (workflow steps) and
// conversation level.
// Params:
//   - pattern: string (regex pattern)
//
// Type: "content_matches"
type ContentMatchesConversationValidator struct{}

// Type returns the validator type name.
func (v *ContentMatchesConversationValidator) Type() string { return "content_matches" }

// NewContentMatchesConversationValidator constructs a validator instance.
func NewContentMatchesConversationValidator() ConversationValidator {
	return &ContentMatchesConversationValidator{}
}

// ValidateConversation checks if any assistant response matches the regex pattern.
func (v *ContentMatchesConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	patternStr, _ := params["pattern"].(string)
	if patternStr == "" {
		return ConversationValidationResult{
			Passed:  true,
			Message: "no pattern specified",
		}
	}

	re, err := regexp.Compile(patternStr)
	if err != nil {
		return ConversationValidationResult{
			Passed:  false,
			Message: "invalid regex pattern: " + patternStr,
			Details: map[string]interface{}{"error": err.Error()},
		}
	}

	for i := range convCtx.AllTurns {
		msg := convCtx.AllTurns[i]
		if !strings.EqualFold(msg.Role, roleAssistant) {
			continue
		}
		content := msg.GetContent()
		if re.MatchString(content) {
			return ConversationValidationResult{
				Passed:  true,
				Message: "response matches pattern",
				Details: map[string]interface{}{
					"turn":    i,
					"pattern": patternStr,
				},
			}
		}
	}

	return ConversationValidationResult{
		Passed:  false,
		Message: "no response matched pattern",
		Details: map[string]interface{}{"pattern": patternStr},
	}
}
