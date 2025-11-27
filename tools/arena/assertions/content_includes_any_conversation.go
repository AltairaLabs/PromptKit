package assertions

import (
	"context"
	"strings"
)

// ContentIncludesAnyConversationValidator ensures at least one assistant message
// contains any of the provided patterns.
// Params:
// - patterns: []string
// - case_sensitive: bool (optional, default false)
// Type: "content_includes_any"
type ContentIncludesAnyConversationValidator struct{}

// Type returns the validator type name.
func (v *ContentIncludesAnyConversationValidator) Type() string { return "content_includes_any" }

// NewContentIncludesAnyConversationValidator constructs validator instance.
func NewContentIncludesAnyConversationValidator() ConversationValidator {
	return &ContentIncludesAnyConversationValidator{}
}

// ValidateConversation checks if any assistant response contains any pattern.
func (v *ContentIncludesAnyConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	patterns := extractStringSlice(params, "patterns")
	caseSensitive, _ := params["case_sensitive"].(bool)

	for i := range convCtx.AllTurns {
		msg := convCtx.AllTurns[i]
		if !strings.EqualFold(msg.Role, roleAssistant) {
			continue
		}
		content := msg.GetContent()
		for _, p := range patterns {
			if contains(content, p, caseSensitive) {
				return ConversationValidationResult{
					Passed:  true,
					Message: "at least one response contains required pattern",
					Details: map[string]interface{}{
						"turn":    i,
						"pattern": p,
					},
				}
			}
		}
	}
	return ConversationValidationResult{Passed: false, Message: "no response contained required patterns"}
}
