package validators

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// TestBannedWordsValidator_ValidateChunk tests incremental banned word detection
func TestBannedWordsValidator_ValidateChunk(t *testing.T) {
	validator := NewBannedWordsValidator([]string{"badword", "offensive"})

	tests := []struct {
		name        string
		chunks      []providers.StreamChunk
		expectAbort bool
		abortAt     int // which chunk index should abort (-1 if no abort)
	}{
		{
			name: "clean content - no banned words",
			chunks: []providers.StreamChunk{
				{Content: "This is ", Delta: "This is "},
				{Content: "This is a clean ", Delta: "a clean "},
				{Content: "This is a clean message", Delta: "message"},
			},
			expectAbort: false,
			abortAt:     -1,
		},
		{
			name: "banned word in first chunk",
			chunks: []providers.StreamChunk{
				{Content: "This has badword ", Delta: "This has badword "},
			},
			expectAbort: true,
			abortAt:     0,
		},
		{
			name: "banned word in middle chunk",
			chunks: []providers.StreamChunk{
				{Content: "This is ", Delta: "This is "},
				{Content: "This is offensive ", Delta: "offensive "},
			},
			expectAbort: true,
			abortAt:     1,
		},
		{
			name: "banned word at chunk boundary",
			chunks: []providers.StreamChunk{
				{Content: "This is bad", Delta: "This is bad"},
				{Content: "This is badword here", Delta: "word here"},
			},
			expectAbort: true,
			abortAt:     1,
		},
		{
			name: "partial match not triggered",
			chunks: []providers.StreamChunk{
				{Content: "badge", Delta: "badge"},
				{Content: "badge and words", Delta: " and words"},
			},
			expectAbort: false,
			abortAt:     -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			for i, chunk := range tt.chunks {
				err = validator.ValidateChunk(chunk)
				if err != nil {
					if !tt.expectAbort {
						t.Errorf("unexpected abort at chunk %d: %v", i, err)
					} else if i != tt.abortAt {
						t.Errorf("abort at wrong chunk: got %d, want %d", i, tt.abortAt)
					}
					// Verify it's a ValidationAbortError
					if !providers.IsValidationAbort(err) {
						t.Errorf("expected ValidationAbortError, got %T", err)
					}
					return
				}
			}

			if tt.expectAbort {
				t.Errorf("expected abort but stream completed")
			}
		})
	}
}

// TestBannedWordsValidator_SupportsStreaming verifies streaming capability flag
func TestBannedWordsValidator_SupportsStreaming(t *testing.T) {
	validator := NewBannedWordsValidator([]string{"test"})

	if !validator.SupportsStreaming() {
		t.Error("BannedWordsValidator should support streaming")
	}
}

// TestLengthValidator_ValidateChunk tests early abort on length limits
func TestLengthValidator_ValidateChunk(t *testing.T) {
	validator := NewLengthValidator()

	tests := []struct {
		name        string
		params      map[string]interface{}
		chunks      []providers.StreamChunk
		expectAbort bool
		abortAt     int
	}{
		{
			name:   "within character limit",
			params: map[string]interface{}{"max_characters": 100},
			chunks: []providers.StreamChunk{
				{Content: "Short message", Delta: "Short message"},
			},
			expectAbort: false,
			abortAt:     -1,
		},
		{
			name:   "exceeds character limit",
			params: map[string]interface{}{"max_characters": 20},
			chunks: []providers.StreamChunk{
				{Content: "This is ", Delta: "This is "},
				{Content: "This is a message that exceeds limit", Delta: "a message that exceeds limit"},
			},
			expectAbort: true,
			abortAt:     1,
		},
		{
			name:   "within token limit",
			params: map[string]interface{}{"max_tokens": 50},
			chunks: []providers.StreamChunk{
				{Content: "Short message", Delta: "Short message", TokenCount: 3},
			},
			expectAbort: false,
			abortAt:     -1,
		},
		{
			name:   "exceeds token limit",
			params: map[string]interface{}{"max_tokens": 10},
			chunks: []providers.StreamChunk{
				{Content: "This is ", Delta: "This is ", TokenCount: 2},
				{Content: "This is a message that has too many tokens", Delta: "a message that has too many tokens", TokenCount: 15},
			},
			expectAbort: true,
			abortAt:     1,
		},
		{
			name:   "both character and token limits - character exceeded first",
			params: map[string]interface{}{"max_characters": 25, "max_tokens": 50},
			chunks: []providers.StreamChunk{
				{Content: "This is a message that exceeds character limit", Delta: "This is a message that exceeds character limit", TokenCount: 10},
			},
			expectAbort: true,
			abortAt:     0,
		},
		{
			name:   "no limits set - no abort",
			params: map[string]interface{}{},
			chunks: []providers.StreamChunk{
				{Content: "This is a very long message with no limits set and it should not abort at all", Delta: "This is a very long message with no limits set and it should not abort at all", TokenCount: 20},
			},
			expectAbort: false,
			abortAt:     -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			for i, chunk := range tt.chunks {
				err = validator.ValidateChunk(chunk, tt.params)
				if err != nil {
					if !tt.expectAbort {
						t.Errorf("unexpected abort at chunk %d: %v", i, err)
					} else if i != tt.abortAt {
						t.Errorf("abort at wrong chunk: got %d, want %d", i, tt.abortAt)
					}
					// Verify it's a ValidationAbortError
					if !providers.IsValidationAbort(err) {
						t.Errorf("expected ValidationAbortError, got %T", err)
					}
					return
				}
			}

			if tt.expectAbort {
				t.Errorf("expected abort but stream completed")
			}
		})
	}
}

// TestLengthValidator_SupportsStreaming verifies streaming capability flag
func TestLengthValidator_SupportsStreaming(t *testing.T) {
	validator := NewLengthValidator()

	if !validator.SupportsStreaming() {
		t.Error("LengthValidator should support streaming")
	}
}

// TestMaxSentencesValidator_SupportsStreaming verifies non-streaming validator
func TestMaxSentencesValidator_SupportsStreaming(t *testing.T) {
	validator := NewMaxSentencesValidator()

	if validator.SupportsStreaming() {
		t.Error("MaxSentencesValidator should not support streaming (requires complete content)")
	}
}

// TestRequiredFieldsValidator_SupportsStreaming verifies non-streaming validator
func TestRequiredFieldsValidator_SupportsStreaming(t *testing.T) {
	validator := NewRequiredFieldsValidator()

	if validator.SupportsStreaming() {
		t.Error("RequiredFieldsValidator should not support streaming (requires complete content)")
	}
}

// TestCommitValidator_SupportsStreaming verifies non-streaming validator
func TestCommitValidator_SupportsStreaming(t *testing.T) {
	validator := NewCommitValidator()

	if validator.SupportsStreaming() {
		t.Error("CommitValidator should not support streaming (requires complete content)")
	}
}

// TestBannedWordsValidator_PostValidation tests that post-stream validation still works
func TestBannedWordsValidator_PostValidation(t *testing.T) {
	validator := NewBannedWordsValidator([]string{"badword"})

	result := validator.Validate("This contains badword in it", nil)
	if result.OK {
		t.Error("expected validation to fail")
	}

	violations, ok := result.Details.([]string)
	if !ok {
		t.Fatalf("expected string slice, got %T", result.Details)
	}

	if len(violations) != 1 || violations[0] != "badword" {
		t.Errorf("expected [badword], got %v", violations)
	}
}

// TestLengthValidator_PostValidation tests that post-stream validation still works
func TestLengthValidator_PostValidation(t *testing.T) {
	validator := NewLengthValidator()

	params := map[string]interface{}{
		"max_characters": 10,
	}

	result := validator.Validate("This is too long", params)
	if result.OK {
		t.Error("expected validation to fail")
	}

	details, ok := result.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", result.Details)
	}

	if details["character_count"].(int) != 16 {
		t.Errorf("expected character_count=16, got %v", details["character_count"])
	}

	if details["max_characters"].(int) != 10 {
		t.Errorf("expected max_characters=10, got %v", details["max_characters"])
	}
}

// TestBannedWordsValidator_CaseInsensitive tests case-insensitive matching in streaming
func TestBannedWordsValidator_CaseInsensitive(t *testing.T) {
	validator := NewBannedWordsValidator([]string{"BadWord"})

	// Test various case combinations
	testCases := []string{
		"This has BADWORD",
		"This has badword",
		"This has BadWord",
		"This has BaDwOrD",
	}

	for _, content := range testCases {
		t.Run(content, func(t *testing.T) {
			chunk := providers.StreamChunk{Content: content, Delta: content}
			err := validator.ValidateChunk(chunk)
			if err == nil {
				t.Error("expected abort for case-insensitive match")
			}
		})
	}
}

// TestBannedWordsValidator_WordBoundaries tests word boundary matching
func TestBannedWordsValidator_WordBoundaries(t *testing.T) {
	validator := NewBannedWordsValidator([]string{"bad"})

	tests := []struct {
		name        string
		content     string
		expectAbort bool
	}{
		{
			name:        "whole word match",
			content:     "This is bad",
			expectAbort: true,
		},
		{
			name:        "not a word boundary - badge",
			content:     "This is a badge",
			expectAbort: false,
		},
		{
			name:        "not a word boundary - forbade",
			content:     "He forbade it",
			expectAbort: false,
		},
		{
			name:        "punctuation boundary",
			content:     "This is bad!",
			expectAbort: true,
		},
		{
			name:        "start of sentence",
			content:     "Bad things happened",
			expectAbort: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunk := providers.StreamChunk{Content: tt.content, Delta: tt.content}
			err := validator.ValidateChunk(chunk)
			if tt.expectAbort && err == nil {
				t.Error("expected abort but got none")
			}
			if !tt.expectAbort && err != nil {
				t.Errorf("unexpected abort: %v", err)
			}
		})
	}
}
