package handlers

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

const (
	roleAssistant = "assistant"
	roleTool      = "tool"
)

// scanTarget is one piece of content a content-matching handler should examine,
// paired with the turn index used in explanations.
type scanTarget struct {
	turn    int
	content string
}

// contentUnderTest returns the content a content-matching handler should scan,
// honoring the caller's declared EvalContext.ContentScope.
//
// ContentScopeCurrent (set by the guardrail adapter) means examine only
// CurrentOutput — the one message being judged. That is required for an input
// guardrail, whose content under test is the *user's* message: a scan filtered
// to assistant role would never see it, which made `direction: input`
// guardrails silent no-ops (#1679).
//
// The default, ContentScopeTranscript, scans every assistant message. Evals and
// assertions rely on this to answer "was this ever said" across the whole
// conversation.
//
// Scope is deliberately explicit rather than inferred from CurrentOutput being
// set: BuildEvalContext always populates that field, so inferring would
// silently narrow every eval and assertion to the last assistant turn.
func contentUnderTest(evalCtx *evals.EvalContext) []scanTarget {
	if evalCtx == nil {
		return nil
	}

	if evalCtx.ContentScope == evals.ContentScopeCurrent && evalCtx.CurrentOutput != "" {
		// Attribute it to the last message, which is the turn the caller is
		// evaluating in both the input and output directions.
		turn := len(evalCtx.Messages) - 1
		if turn < 0 {
			turn = 0
		}
		return []scanTarget{{turn: turn, content: evalCtx.CurrentOutput}}
	}

	targets := make([]scanTarget, 0, len(evalCtx.Messages))
	for i := range evalCtx.Messages {
		msg := &evalCtx.Messages[i]
		if !strings.EqualFold(msg.Role, roleAssistant) {
			continue
		}
		targets = append(targets, scanTarget{turn: i, content: msg.GetContent()})
	}
	return targets
}

// extractStringSlice safely extracts a string slice from params.
func extractStringSlice(params map[string]any, key string) []string {
	value, exists := params[key]
	if !exists {
		return nil
	}

	if strSlice, ok := value.([]string); ok {
		return strSlice
	}

	if ifaceSlice, ok := value.([]any); ok {
		result := make([]string, 0, len(ifaceSlice))
		for _, item := range ifaceSlice {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	}

	return nil
}

// containsInsensitive checks if text contains pattern
// (case-insensitive).
func containsInsensitive(text, pattern string) bool {
	return strings.Contains(
		strings.ToLower(text),
		strings.ToLower(pattern),
	)
}

// asString converts any value to its string representation.
func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// extractFloat64Ptr extracts an optional float64 param, handling int->float64.
func extractFloat64Ptr(params map[string]any, key string) *float64 {
	val, ok := params[key]
	if !ok {
		return nil
	}
	switch v := val.(type) {
	case float64:
		return &v
	case int:
		f := float64(v)
		return &f
	default:
		return nil
	}
}

// extractIntPtr extracts an optional integer param.
func extractIntPtr(params map[string]any, key string) *int {
	val, ok := params[key]
	if !ok {
		return nil
	}
	switch v := val.(type) {
	case int:
		return &v
	case float64:
		i := int(v)
		return &i
	default:
		return nil
	}
}

// extractBool extracts a boolean param from a map.
func extractBool(params map[string]any, key string) bool {
	val, ok := params[key].(bool)
	return ok && val
}

// extractMapStringString extracts a map[string]string from params.
func extractMapStringString(params map[string]any, key string) map[string]string {
	value, exists := params[key]
	if !exists {
		return nil
	}

	if m, ok := value.(map[string]any); ok {
		result := make(map[string]string, len(m))
		for k, v := range m {
			if s, ok := v.(string); ok {
				result[k] = s
			}
		}
		return result
	}

	if m, ok := value.(map[string]string); ok {
		return m
	}

	return nil
}

// extractMapAny extracts a map[string]any from params.
func extractMapAny(params map[string]any, key string) map[string]any {
	value, exists := params[key]
	if !exists {
		return nil
	}
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return nil
}
