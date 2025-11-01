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
		wantOK  bool
	}{
		{
			name:    "No banned words",
			content: "This is a clean and appropriate message",
			wantOK:  true,
		},
		{
			name:    "Contains banned word",
			content: "This message contains a badword in it",
			wantOK:  false,
		},
		{
			name:    "Case insensitive match",
			content: "This message contains OFFENSIVE content",
			wantOK:  false,
		},
		{
			name:    "Multiple banned words",
			content: "This is badword and also offensive",
			wantOK:  false,
		},
		{
			name:    "Partial word match should not trigger",
			content: "This is good behavior",
			wantOK:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.Validate(tt.content, nil)
			if result.OK != tt.wantOK {
				t.Errorf("Validate() OK = %v, want %v", result.OK, tt.wantOK)
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
		wantOK  bool
	}{
		{
			name:    "Under limit",
			content: "This is sentence one. This is sentence two.",
			params:  map[string]interface{}{"max_sentences": 3},
			wantOK:  true,
		},
		{
			name:    "At limit",
			content: "Sentence one. Sentence two. Sentence three.",
			params:  map[string]interface{}{"max_sentences": 3},
			wantOK:  true,
		},
		{
			name:    "Over limit",
			content: "One. Two. Three. Four.",
			params:  map[string]interface{}{"max_sentences": 3},
			wantOK:  false,
		},
		{
			name:    "No params",
			content: "Any content here.",
			params:  map[string]interface{}{},
			wantOK:  true,
		},
		{
			name:    "Invalid param type",
			content: "Any content.",
			params:  map[string]interface{}{"max_sentences": "not an int"},
			wantOK:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.Validate(tt.content, tt.params)
			if result.OK != tt.wantOK {
				t.Errorf("Validate() OK = %v, want %v, details = %v", result.OK, tt.wantOK, result.Details)
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
		wantOK  bool
	}{
		{
			name:    "All fields present",
			content: "Response includes name and email and phone",
			params:  map[string]interface{}{"required_fields": []string{"name", "email"}},
			wantOK:  true,
		},
		{
			name:    "Missing one field",
			content: "Response includes name only",
			params:  map[string]interface{}{"required_fields": []string{"name", "email"}},
			wantOK:  false,
		},
		{
			name:    "No required fields param",
			content: "Any content",
			params:  map[string]interface{}{},
			wantOK:  true,
		},
		{
			name:    "Invalid param type",
			content: "Any content",
			params:  map[string]interface{}{"required_fields": "not a slice"},
			wantOK:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.Validate(tt.content, tt.params)
			if result.OK != tt.wantOK {
				t.Errorf("Validate() OK = %v, want %v, details = %v", result.OK, tt.wantOK, result.Details)
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
		wantOK  bool
	}{
		{
			name:    "Not required",
			content: "Any response",
			params:  map[string]interface{}{"must_end_with_commit": false},
			wantOK:  true,
		},
		{
			name:    "Required with commit structure",
			content: "My decision is to proceed with the next step",
			params: map[string]interface{}{
				"must_end_with_commit": true,
				"commit_fields":        []string{"decision", "next step"},
			},
			wantOK: true,
		},
		{
			name:    "Missing commit structure",
			content: "Just a regular response without any commit",
			params: map[string]interface{}{
				"must_end_with_commit": true,
				"commit_fields":        []string{"decision"},
			},
			wantOK: false,
		},
		{
			name:    "Has structure but missing required field",
			content: "Here is my decision to move forward",
			params: map[string]interface{}{
				"must_end_with_commit": true,
				"commit_fields":        []string{"decision", "rationale"},
			},
			wantOK: false,
		},
		{
			name:    "No params",
			content: "Any content",
			params:  map[string]interface{}{},
			wantOK:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.Validate(tt.content, tt.params)
			if result.OK != tt.wantOK {
				t.Errorf("Validate() OK = %v, want %v, details = %v", result.OK, tt.wantOK, result.Details)
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
		wantOK  bool
	}{
		{
			name:    "Under character limit",
			content: "Short text",
			params:  map[string]interface{}{"max_characters": 100},
			wantOK:  true,
		},
		{
			name:    "Over character limit",
			content: "This is a very long text that exceeds the character limit",
			params:  map[string]interface{}{"max_characters": 10},
			wantOK:  false,
		},
		{
			name:    "Under token limit",
			content: "Short text",
			params:  map[string]interface{}{"max_tokens": 100},
			wantOK:  true,
		},
		{
			name:    "Over token limit",
			content: "This is a text that should exceed the token limit when divided by four",
			params:  map[string]interface{}{"max_tokens": 5},
			wantOK:  false,
		},
		{
			name:    "Both limits under",
			content: "Short",
			params: map[string]interface{}{
				"max_characters": 100,
				"max_tokens":     100,
			},
			wantOK: true,
		},
		{
			name:    "No limits",
			content: "Any content of any length",
			params:  map[string]interface{}{},
			wantOK:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.Validate(tt.content, tt.params)
			if result.OK != tt.wantOK {
				t.Errorf("Validate() OK = %v, want %v, details = %v", result.OK, tt.wantOK, result.Details)
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
		OK:      true,
		Details: map[string]interface{}{"key": "value"},
	}

	if !result.OK {
		t.Error("Expected OK to be true")
	}

	if result.Details == nil {
		t.Error("Expected Details to be non-nil")
	}
}
