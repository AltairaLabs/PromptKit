package validators

import (
	"regexp"

	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// ContentMatchesValidator checks that content matches a regex pattern
type ContentMatchesValidator struct {
	pattern    *regexp.Regexp
	rawPattern string
}

// NewContentMatchesValidator creates a new content_matches validator from params
func NewContentMatchesValidator(params map[string]interface{}) runtimeValidators.Validator {
	patternStr, ok := params["pattern"].(string)
	if !ok || patternStr == "" {
		// Return validator that always passes if no pattern
		return &ContentMatchesValidator{pattern: nil, rawPattern: ""}
	}

	// Try to compile the regex pattern
	pattern, err := regexp.Compile(patternStr)
	if err != nil {
		// Return validator that will fail with error message
		return &ContentMatchesValidator{pattern: nil, rawPattern: patternStr}
	}

	return &ContentMatchesValidator{pattern: pattern, rawPattern: patternStr}
}

// Validate checks if content matches the regex pattern
func (v *ContentMatchesValidator) Validate(content string, params map[string]interface{}) runtimeValidators.ValidationResult {
	// No pattern means nothing to validate
	if v.rawPattern == "" {
		return runtimeValidators.ValidationResult{
			OK: true,
			Details: map[string]interface{}{
				"matched": true,
			},
		}
	}

	// Invalid pattern
	if v.pattern == nil {
		return runtimeValidators.ValidationResult{
			OK: false,
			Details: map[string]interface{}{
				"matched": false,
				"error":   "invalid regex pattern: " + v.rawPattern,
			},
		}
	}

	// Check if pattern matches
	matched := v.pattern.MatchString(content)

	return runtimeValidators.ValidationResult{
		OK: matched,
		Details: map[string]interface{}{
			"matched": matched,
			"pattern": v.rawPattern,
		},
	}
}
