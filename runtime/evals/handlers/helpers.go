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
