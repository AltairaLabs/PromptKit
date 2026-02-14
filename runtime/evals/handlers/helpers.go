package handlers

import (
	"fmt"
	"strings"
)

const roleAssistant = "assistant"

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
