package validators

import (
	"testing"
)

func TestContentMatchesValidator(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		content string
		wantOK  bool
	}{
		{
			name:    "Exact match",
			pattern: "account status",
			content: "Your account status is active",
			wantOK:  true,
		},
		{
			name:    "Regex pattern match",
			pattern: `cannot.*access.*another.*account`,
			content: "Sorry, you cannot access another user's account for privacy reasons",
			wantOK:  true,
		},
		{
			name:    "Pattern not found",
			pattern: "password reset",
			content: "Your account is active",
			wantOK:  false,
		},
		{
			name:    "Case insensitive match",
			pattern: `(?i)ACCOUNT`,
			content: "your account is ready",
			wantOK:  true,
		},
		{
			name:    "Complex regex",
			pattern: `\b(error|failure|problem)\b`,
			content: "We encountered an error processing your request",
			wantOK:  true,
		},
		{
			name:    "Complex regex no match",
			pattern: `\b(error|failure|problem)\b`,
			content: "Everything is working correctly",
			wantOK:  false,
		},
		{
			name:    "Empty pattern",
			pattern: "",
			content: "any content",
			wantOK:  true,
		},
		{
			name:    "Empty content",
			pattern: "something",
			content: "",
			wantOK:  false,
		},
		{
			name:    "Start of line anchor",
			pattern: `^Sorry`,
			content: "Sorry, I cannot help with that",
			wantOK:  true,
		},
		{
			name:    "End of line anchor",
			pattern: `account$`,
			content: "Please check your account",
			wantOK:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create validator with factory pattern
			params := map[string]interface{}{
				"pattern": tt.pattern,
			}
			validator := NewContentMatchesValidator(params)

			// Validate
			result := validator.Validate(tt.content, params)

			if result.OK != tt.wantOK {
				t.Errorf("Validate() OK = %v, want %v (pattern: %q, content: %q)",
					result.OK, tt.wantOK, tt.pattern, tt.content)
			}

			// Check details
			details, ok := result.Details.(map[string]interface{})
			if !ok {
				t.Fatalf("Expected details to be map[string]interface{}, got %T", result.Details)
			}

			if result.OK {
				matched, ok := details["matched"].(bool)
				if !ok || !matched {
					t.Errorf("Expected matched=true in details when OK=true")
				}
			} else {
				matched, ok := details["matched"].(bool)
				if !ok || matched {
					t.Errorf("Expected matched=false in details when OK=false")
				}
			}
		})
	}
}

func TestContentMatchesValidator_InvalidRegex(t *testing.T) {
	// Test that invalid regex patterns are handled gracefully
	params := map[string]interface{}{
		"pattern": `[invalid(`,
	}

	// Should not panic during creation
	validator := NewContentMatchesValidator(params)

	// Validation should fail with invalid regex
	result := validator.Validate("any content", params)

	if result.OK {
		t.Error("Expected validation to fail with invalid regex pattern")
	}
}

func TestContentMatchesValidator_MissingPattern(t *testing.T) {
	params := map[string]interface{}{}
	validator := NewContentMatchesValidator(params)

	// Should pass when no pattern specified (nothing to match)
	result := validator.Validate("any content", params)

	if !result.OK {
		t.Error("Expected OK=true when no pattern specified")
	}
}
