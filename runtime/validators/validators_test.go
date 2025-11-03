package validators

import (
	"testing"
)

func TestBannedWordsValidator(t *testing.T) {
	bannedWords := []string{"badword", "offensive", "inappropriate"}
	validator := NewBannedWordsValidator(bannedWords)

	tests := []struct {
		name    string
		content string
		wantPassed  bool
	}{
		{
			name:    "No banned words",
			content: "This is a clean and appropriate message",
			wantPassed:  true,
		},
		{
			name:    "Contains banned word",
			content: "This message contains a badword in it",
			wantPassed:  false,
		},
		{
			name:    "Case insensitive match",
			content: "This message contains OFFENSIVE content",
			wantPassed:  false,
		},
		{
			name:    "Multiple banned words",
			content: "This is badword and also offensive",
			wantPassed:  false,
		},
		{
			name:    "Partial word match should not trigger",
			content: "This is good behavior",
			wantPassed:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.Validate(tt.content, nil)
			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() Passed = %v, want %v", result.Passed, tt.wantPassed)
			}
		})
	}
}

func TestMaxSentencesValidator(t *testing.T) {
	validator := NewMaxSentencesValidator()

	tests := []struct {
		name    string
		content string
		params  map[string]interface{}
		wantPassed  bool
	}{
		{
			name:    "Under limit",
			content: "This is sentence one. This is sentence two.",
			params:  map[string]interface{}{"max_sentences": 3},
			wantPassed:  true,
		},
		{
			name:    "At limit",
			content: "Sentence one. Sentence two. Sentence three.",
			params:  map[string]interface{}{"max_sentences": 3},
			wantPassed:  true,
		},
		{
			name:    "Over limit",
			content: "One. Two. Three. Four.",
			params:  map[string]interface{}{"max_sentences": 3},
			wantPassed:  false,
		},
		{
			name:    "No params",
			content: "Any content here.",
			params:  map[string]interface{}{},
			wantPassed:  true,
		},
		{
			name:    "Invalid param type",
			content: "Any content.",
			params:  map[string]interface{}{"max_sentences": "not an int"},
			wantPassed:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.Validate(tt.content, tt.params)
			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() Passed = %v, want %v, details = %v", result.Passed, tt.wantPassed, result.Details)
			}
		})
	}
}

func TestRequiredFieldsValidator(t *testing.T) {
	validator := NewRequiredFieldsValidator()

	tests := []struct {
		name    string
		content string
		params  map[string]interface{}
		wantPassed  bool
	}{
		{
			name:    "All fields present",
			content: "Response includes name and email and phone",
			params:  map[string]interface{}{"required_fields": []string{"name", "email"}},
			wantPassed:  true,
		},
		{
			name:    "Missing one field",
			content: "Response includes name only",
			params:  map[string]interface{}{"required_fields": []string{"name", "email"}},
			wantPassed:  false,
		},
		{
			name:    "No required fields param",
			content: "Any content",
			params:  map[string]interface{}{},
			wantPassed:  true,
		},
		{
			name:    "Invalid param type",
			content: "Any content",
			params:  map[string]interface{}{"required_fields": "not a slice"},
			wantPassed:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.Validate(tt.content, tt.params)
			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() Passed = %v, want %v, details = %v", result.Passed, tt.wantPassed, result.Details)
			}
		})
	}
}

func TestCommitValidator(t *testing.T) {
	validator := NewCommitValidator()

	tests := []struct {
		name    string
		content string
		params  map[string]interface{}
		wantPassed  bool
	}{
		{
			name:    "Not required",
			content: "Any response",
			params:  map[string]interface{}{"must_end_with_commit": false},
			wantPassed:  true,
		},
		{
			name:    "Required with commit structure",
			content: "My decision is to proceed with the next step",
			params: map[string]interface{}{
				"must_end_with_commit": true,
				"commit_fields":        []string{"decision", "next step"},
			},
			wantPassed: true,
		},
		{
			name:    "Missing commit structure",
			content: "Just a regular response without any commit",
			params: map[string]interface{}{
				"must_end_with_commit": true,
				"commit_fields":        []string{"decision"},
			},
			wantPassed: false,
		},
		{
			name:    "Has structure but missing required field",
			content: "Here is my decision to move forward",
			params: map[string]interface{}{
				"must_end_with_commit": true,
				"commit_fields":        []string{"decision", "rationale"},
			},
			wantPassed: false,
		},
		{
			name:    "No params",
			content: "Any content",
			params:  map[string]interface{}{},
			wantPassed:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.Validate(tt.content, tt.params)
			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() Passed = %v, want %v, details = %v", result.Passed, tt.wantPassed, result.Details)
			}
		})
	}
}

func TestLengthValidator(t *testing.T) {
	validator := NewLengthValidator()

	tests := []struct {
		name    string
		content string
		params  map[string]interface{}
		wantPassed  bool
	}{
		{
			name:    "Under character limit",
			content: "Short text",
			params:  map[string]interface{}{"max_characters": 100},
			wantPassed:  true,
		},
		{
			name:    "Over character limit",
			content: "This is a very long text that exceeds the character limit",
			params:  map[string]interface{}{"max_characters": 10},
			wantPassed:  false,
		},
		{
			name:    "Under token limit",
			content: "Short text",
			params:  map[string]interface{}{"max_tokens": 100},
			wantPassed:  true,
		},
		{
			name:    "Over token limit",
			content: "This is a text that should exceed the token limit when divided by four",
			params:  map[string]interface{}{"max_tokens": 5},
			wantPassed:  false,
		},
		{
			name:    "Both limits under",
			content: "Short",
			params: map[string]interface{}{
				"max_characters": 100,
				"max_tokens":     100,
			},
			wantPassed: true,
		},
		{
			name:    "No limits",
			content: "Any content of any length",
			params:  map[string]interface{}{},
			wantPassed:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.Validate(tt.content, tt.params)
			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() Passed = %v, want %v, details = %v", result.Passed, tt.wantPassed, result.Details)
			}
		})
	}
}

func TestCountSentences(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{
			name: "Empty string",
			text: "",
			want: 0,
		},
		{
			name: "Single sentence",
			text: "This is one sentence.",
			want: 1,
		},
		{
			name: "Multiple sentences with periods",
			text: "First sentence. Second sentence. Third sentence.",
			want: 3,
		},
		{
			name: "Multiple sentence types",
			text: "Question? Statement. Exclamation!",
			want: 3,
		},
		{
			name: "No punctuation",
			text: "This is text without sentence-ending punctuation",
			want: 1,
		},
		{
			name: "Multiple punctuation marks",
			text: "Really??? Yes!!! Wow...",
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countSentences(tt.text)
			if got != tt.want {
				t.Errorf("countSentences() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidationResult(t *testing.T) {
	result := ValidationResult{
		Passed:      true,
		Details: map[string]interface{}{"key": "value"},
	}

	if !result.Passed {
		t.Error("Expected OK to be true")
	}

	if result.Details == nil {
		t.Error("Expected Details to be non-nil")
	}
}
