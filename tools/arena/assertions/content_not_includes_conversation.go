package assertions

import (
	"context"
	"fmt"
	"strings"
)

// ContentNotIncludesConversationValidator ensures assistant messages do NOT include any forbidden patterns.
// Params:
// - patterns: []string
// - case_sensitive: bool (optional, default false)
// Type: "content_not_includes"
type ContentNotIncludesConversationValidator struct{}

// Type returns the validator type name.
func (v *ContentNotIncludesConversationValidator) Type() string { return "content_not_includes" }

// NewContentNotIncludesConversationValidator constructs validator instance.
func NewContentNotIncludesConversationValidator() ConversationValidator {
	return &ContentNotIncludesConversationValidator{}
}

// ValidateConversation scans assistant messages for forbidden substrings.
func (v *ContentNotIncludesConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	patterns := extractStringSlice(params, "patterns")
	caseSensitive, _ := params["case_sensitive"].(bool)

	var violations []ConversationViolation
	for i := range convCtx.AllTurns {
		msg := convCtx.AllTurns[i]
		if !strings.EqualFold(msg.Role, roleAssistant) {
			continue
		}
		content := msg.GetContent()
		for _, p := range patterns {
			if contains(content, p, caseSensitive) {
				violations = append(violations, ConversationViolation{
					TurnIndex:   i,
					Description: fmt.Sprintf("response contains forbidden pattern: %s", p),
					Evidence: map[string]interface{}{
						"pattern": p,
						"snippet": snippet(content, p),
					},
				})
			}
		}
	}

	if len(violations) > 0 {
		return ConversationValidationResult{Passed: false, Message: "forbidden content detected", Violations: violations}
	}
	return ConversationValidationResult{Passed: true, Message: "no forbidden content"}
}

func contains(text, pat string, caseSensitive bool) bool {
	if !caseSensitive {
		text = strings.ToLower(text)
		pat = strings.ToLower(pat)
	}
	return strings.Contains(text, pat)
}

const snippetContext = 20

func snippet(text, pat string) string {
	idx := strings.Index(strings.ToLower(text), strings.ToLower(pat))
	if idx < 0 {
		return ""
	}
	start := idx - snippetContext
	if start < 0 {
		start = 0
	}
	end := idx + len(pat) + snippetContext
	if end > len(text) {
		end = len(text)
	}
	return text[start:end]
}
