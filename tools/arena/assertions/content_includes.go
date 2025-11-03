package assertions

import (
	"strings"

	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// ContentIncludesValidator checks that content includes all expected patterns
type ContentIncludesValidator struct {
	patterns []string
}

// NewContentIncludesValidator creates a new content_includes validator from params
func NewContentIncludesValidator(params map[string]interface{}) runtimeValidators.Validator {
	patterns := extractStringSlice(params, "patterns")
	return &ContentIncludesValidator{patterns: patterns}
}

// Validate checks if all patterns are present in content (case-insensitive)
func (v *ContentIncludesValidator) Validate(content string, params map[string]interface{}) runtimeValidators.ValidationResult {
	contentLower := strings.ToLower(content)

	var missing []string
	for _, pattern := range v.patterns {
		patternLower := strings.ToLower(pattern)
		if !strings.Contains(contentLower, patternLower) {
			missing = append(missing, pattern)
		}
	}

	return runtimeValidators.ValidationResult{
		Passed: len(missing) == 0,
		Details: map[string]interface{}{
			"missing_patterns": missing,
		},
	}
}
