package assertions

import (
	"testing"
)

func TestRoleIntegrityValidator_Validate(t *testing.T) {
	validator := NewRoleIntegrityValidator()

	tests := []struct {
		name       string
		content    string
		wantPassed bool
		wantLen    int // expected number of violations
	}{
		{
			name:       "valid user message",
			content:    "I need help with my account please",
			wantPassed: true,
			wantLen:    0,
		},
		{
			name:       "contains 'here's your plan'",
			content:    "Here's your plan for fixing this issue",
			wantPassed: false,
			wantLen:    1,
		},
		{
			name:       "contains 'step 1:'",
			content:    "Step 1: First, you should check your settings",
			wantPassed: false,
			wantLen:    1,
		},
		{
			name:       "contains 'as an ai'",
			content:    "As an AI, I can help you with that",
			wantPassed: false,
			wantLen:    1,
		},
		{
			name:       "multiple violations",
			content:    "Here's what you should do. Step 1: first, you should check. I recommend this approach.",
			wantPassed: false,
			wantLen:    4,
		},
		{
			name:       "case insensitive check",
			content:    "HERE'S YOUR PLAN for success",
			wantPassed: false,
			wantLen:    1,
		},
		{
			name:       "valid question",
			content:    "Can you help me understand this?",
			wantPassed: true,
			wantLen:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.Validate(tt.content, nil)

			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() Passed = %v, want %v", result.Passed, tt.wantPassed)
			}

			details, ok := result.Details.(map[string]interface{})
			if !ok {
				t.Fatalf("Validate() Details is not a map")
			}

			violations, ok := details["violations"].([]string)
			if !ok {
				t.Fatalf("Validate() violations not found in Details")
			}

			if len(violations) != tt.wantLen {
				t.Errorf("Validate() violations count = %d, want %d. Violations: %v",
					len(violations), tt.wantLen, violations)
			}
		})
	}
}

func TestRoleIntegrityValidator_SupportsStreaming(t *testing.T) {
	validator := NewRoleIntegrityValidator()
	if validator.SupportsStreaming() {
		t.Error("RoleIntegrityValidator should not support streaming")
	}
}

func TestQuestionCapValidator_Validate(t *testing.T) {
	validator := NewQuestionCapValidator()

	tests := []struct {
		name           string
		content        string
		wantPassed     bool
		wantCount      int
		wantMaxAllowed int
	}{
		{
			name:           "no questions",
			content:        "I need help with this.",
			wantPassed:     true,
			wantCount:      0,
			wantMaxAllowed: 1,
		},
		{
			name:           "one question",
			content:        "Can you help me?",
			wantPassed:     true,
			wantCount:      1,
			wantMaxAllowed: 1,
		},
		{
			name:           "two questions",
			content:        "Can you help me? Is this correct?",
			wantPassed:     false,
			wantCount:      2,
			wantMaxAllowed: 1,
		},
		{
			name:           "multiple questions",
			content:        "What is this? How does it work? Why is it broken?",
			wantPassed:     false,
			wantCount:      3,
			wantMaxAllowed: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.Validate(tt.content, nil)

			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() Passed = %v, want %v", result.Passed, tt.wantPassed)
			}

			details, ok := result.Details.(map[string]interface{})
			if !ok {
				t.Fatalf("Validate() Details is not a map")
			}

			count, ok := details["question_count"].(int)
			if !ok {
				t.Fatalf("Validate() question_count not found in Details")
			}

			if count != tt.wantCount {
				t.Errorf("Validate() question_count = %d, want %d", count, tt.wantCount)
			}

			maxAllowed, ok := details["max_allowed"].(int)
			if !ok {
				t.Fatalf("Validate() max_allowed not found in Details")
			}

			if maxAllowed != tt.wantMaxAllowed {
				t.Errorf("Validate() max_allowed = %d, want %d", maxAllowed, tt.wantMaxAllowed)
			}
		})
	}
}

func TestQuestionCapValidator_SupportsStreaming(t *testing.T) {
	validator := NewQuestionCapValidator()
	if validator.SupportsStreaming() {
		t.Error("QuestionCapValidator should not support streaming")
	}
}

func TestLengthCapValidator_Validate(t *testing.T) {
	validator := NewLengthCapValidator()

	tests := []struct {
		name           string
		content        string
		wantPassed     bool
		wantCount      int
		wantMaxAllowed int
	}{
		{
			name:           "one sentence",
			content:        "I need help.",
			wantPassed:     true,
			wantCount:      1,
			wantMaxAllowed: 2,
		},
		{
			name:           "two sentences",
			content:        "I need help. Can you assist me?",
			wantPassed:     true,
			wantCount:      2,
			wantMaxAllowed: 2,
		},
		{
			name:           "three sentences",
			content:        "I need help. Can you assist me? This is urgent.",
			wantPassed:     false,
			wantCount:      3,
			wantMaxAllowed: 2,
		},
		{
			name:           "exclamation marks",
			content:        "Help! This is broken!",
			wantPassed:     true,
			wantCount:      2,
			wantMaxAllowed: 2,
		},
		{
			name:           "mixed punctuation",
			content:        "What is this? It doesn't work! Please help.",
			wantPassed:     false,
			wantCount:      3,
			wantMaxAllowed: 2,
		},
		{
			name:           "multiple punctuation in sequence",
			content:        "Help!!! Please!!!",
			wantPassed:     true,
			wantCount:      2,
			wantMaxAllowed: 2,
		},
		{
			name:           "empty after split",
			content:        "One sentence.",
			wantPassed:     true,
			wantCount:      1,
			wantMaxAllowed: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.Validate(tt.content, nil)

			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() Passed = %v, want %v", result.Passed, tt.wantPassed)
			}

			details, ok := result.Details.(map[string]interface{})
			if !ok {
				t.Fatalf("Validate() Details is not a map")
			}

			count, ok := details["sentence_count"].(int)
			if !ok {
				t.Fatalf("Validate() sentence_count not found in Details")
			}

			if count != tt.wantCount {
				t.Errorf("Validate() sentence_count = %d, want %d", count, tt.wantCount)
			}

			maxAllowed, ok := details["max_allowed"].(int)
			if !ok {
				t.Fatalf("Validate() max_allowed not found in Details")
			}

			if maxAllowed != tt.wantMaxAllowed {
				t.Errorf("Validate() max_allowed = %d, want %d", maxAllowed, tt.wantMaxAllowed)
			}
		})
	}
}

func TestLengthCapValidator_SupportsStreaming(t *testing.T) {
	validator := NewLengthCapValidator()
	if validator.SupportsStreaming() {
		t.Error("LengthCapValidator should not support streaming")
	}
}
