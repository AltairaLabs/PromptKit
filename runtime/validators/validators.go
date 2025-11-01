// Package validators provides content validation for LLM responses and user inputs.
//
// This package implements various validators to ensure conversation quality:
//   - Length and sentence count limits
//   - Banned word detection
//   - Role integrity (preventing role confusion)
//   - Required field presence
//   - Question and commit block validation
//
// Validators are used during test execution to catch policy violations and
// ensure LLM responses meet quality standards.
package validators

import (
	"regexp"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// ValidationResult holds the result of a validation check
type ValidationResult struct {
	OK      bool        `json:"ok"`
	Details interface{} `json:"details,omitempty"`
}

// Validator interface for all validation checks
type Validator interface {
	Validate(content string, params map[string]interface{}) ValidationResult
}

// StreamingValidator interface for validators that can check content incrementally
// and abort streaming early if validation fails
type StreamingValidator interface {
	Validator

	// ValidateChunk validates a stream chunk and returns error to abort stream
	// Returns nil to continue, ValidationAbortError to abort stream
	ValidateChunk(chunk providers.StreamChunk, params ...map[string]interface{}) error

	// SupportsStreaming returns true if this validator can validate incrementally
	SupportsStreaming() bool
}

// BannedWordsValidator checks for banned words
type BannedWordsValidator struct {
	bannedWords []string
	patterns    []*regexp.Regexp
}

// NewBannedWordsValidator creates a new banned words validator
func NewBannedWordsValidator(bannedWords []string) *BannedWordsValidator {
	patterns := make([]*regexp.Regexp, len(bannedWords))
	for i, word := range bannedWords {
		// Create case-insensitive word boundary regex
		pattern := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(word) + `\b`)
		patterns[i] = pattern
	}

	return &BannedWordsValidator{
		bannedWords: bannedWords,
		patterns:    patterns,
	}
}

// Validate checks for banned words in content
func (v *BannedWordsValidator) Validate(content string, params map[string]interface{}) ValidationResult {
	var violations []string

	for i, pattern := range v.patterns {
		if pattern.MatchString(content) {
			violations = append(violations, v.bannedWords[i])
		}
	}

	return ValidationResult{
		OK:      len(violations) == 0,
		Details: violations,
	}
}

// ValidateChunk validates a stream chunk for banned words and aborts if found
func (v *BannedWordsValidator) ValidateChunk(chunk providers.StreamChunk, params ...map[string]interface{}) error {
	// Check accumulated content for banned words
	for i, pattern := range v.patterns {
		if pattern.MatchString(chunk.Content) {
			return &providers.ValidationAbortError{
				Reason: "banned word detected: " + v.bannedWords[i],
				Chunk:  chunk,
			}
		}
	}
	return nil
}

// SupportsStreaming returns true as banned words can be detected incrementally
func (v *BannedWordsValidator) SupportsStreaming() bool {
	return true
}

// MaxSentencesValidator checks sentence count limits
type MaxSentencesValidator struct{}

// NewMaxSentencesValidator creates a new sentence count validator
func NewMaxSentencesValidator() *MaxSentencesValidator {
	return &MaxSentencesValidator{}
}

// Validate checks sentence count against max limit
func (v *MaxSentencesValidator) Validate(content string, params map[string]interface{}) ValidationResult {
	maxSentences, exists := params["max_sentences"]
	if !exists {
		return ValidationResult{OK: true}
	}

	max, ok := maxSentences.(int)
	if !ok {
		return ValidationResult{OK: true}
	}

	count := countSentences(content)

	return ValidationResult{
		OK: count <= max,
		Details: map[string]interface{}{
			"count": count,
			"max":   max,
		},
	}
}

// SupportsStreaming returns false as sentence counting requires complete content
func (v *MaxSentencesValidator) SupportsStreaming() bool {
	return false
}

// RequiredFieldsValidator checks for required fields in content
type RequiredFieldsValidator struct{}

// NewRequiredFieldsValidator creates a new required fields validator
func NewRequiredFieldsValidator() *RequiredFieldsValidator {
	return &RequiredFieldsValidator{}
}

// Validate checks for required fields in content
func (v *RequiredFieldsValidator) Validate(content string, params map[string]interface{}) ValidationResult {
	requiredFields, exists := params["required_fields"]
	if !exists {
		return ValidationResult{OK: true}
	}

	fields, ok := requiredFields.([]string)
	if !ok {
		return ValidationResult{OK: true}
	}

	var missing []string
	for _, field := range fields {
		if !strings.Contains(content, field) {
			missing = append(missing, field)
		}
	}

	return ValidationResult{
		OK: len(missing) == 0,
		Details: map[string]interface{}{
			"missing": missing,
		},
	}
}

// SupportsStreaming returns false as required fields must be in complete content
func (v *RequiredFieldsValidator) SupportsStreaming() bool {
	return false
}

// CommitValidator checks for commit/decision blocks in conversation responses
type CommitValidator struct{}

// NewCommitValidator creates a new commit validator
func NewCommitValidator() *CommitValidator {
	return &CommitValidator{}
}

// Validate checks for commit block with required fields
func (v *CommitValidator) Validate(content string, params map[string]interface{}) ValidationResult {
	mustEndWithCommit, exists := params["must_end_with_commit"]
	if !exists || !mustEndWithCommit.(bool) {
		return ValidationResult{OK: true}
	}

	commitFields, exists := params["commit_fields"]
	if !exists {
		return ValidationResult{OK: true}
	}

	fields, ok := commitFields.([]string)
	if !ok {
		return ValidationResult{OK: true}
	}

	// Check if content contains commit-like structure
	hasCommitStructure := strings.Contains(strings.ToLower(content), "decision") ||
		strings.Contains(strings.ToLower(content), "next step") ||
		strings.Contains(strings.ToLower(content), "commit")

	if !hasCommitStructure {
		return ValidationResult{
			OK: false,
			Details: map[string]interface{}{
				"error": "missing commit structure",
			},
		}
	}

	// Check for required commit fields
	var missing []string
	for _, field := range fields {
		fieldLower := strings.ToLower(field)
		contentLower := strings.ToLower(content)
		if !strings.Contains(contentLower, fieldLower) {
			missing = append(missing, field)
		}
	}

	return ValidationResult{
		OK: len(missing) == 0,
		Details: map[string]interface{}{
			"missing_fields": missing,
		},
	}
}

// SupportsStreaming returns false as commit validation requires complete content
func (v *CommitValidator) SupportsStreaming() bool {
	return false
}

// LengthValidator checks content length limits
type LengthValidator struct{}

// NewLengthValidator creates a new length validator
func NewLengthValidator() *LengthValidator {
	return &LengthValidator{}
}

// Validate checks content length against limits
func (v *LengthValidator) Validate(content string, params map[string]interface{}) ValidationResult {
	maxChars, hasMaxChars := params["max_characters"]
	maxTokens, hasMaxTokens := params["max_tokens"]

	result := ValidationResult{OK: true, Details: map[string]interface{}{}}

	if hasMaxChars {
		if max, ok := maxChars.(int); ok {
			charCount := len(content)
			if charCount > max {
				result.OK = false
			}
			result.Details.(map[string]interface{})["character_count"] = charCount
			result.Details.(map[string]interface{})["max_characters"] = max
		}
	}

	if hasMaxTokens {
		if max, ok := maxTokens.(int); ok {
			// Rough token estimation (1 token ≈ 4 characters)
			tokenCount := len(content) / 4
			if tokenCount > max {
				result.OK = false
			}
			result.Details.(map[string]interface{})["token_count"] = tokenCount
			result.Details.(map[string]interface{})["max_tokens"] = max
		}
	}

	return result
}

// ValidateChunk validates stream chunk against length limits and aborts if exceeded
func (v *LengthValidator) ValidateChunk(chunk providers.StreamChunk, params ...map[string]interface{}) error {
	// Extract params if provided
	var p map[string]interface{}
	if len(params) > 0 {
		p = params[0]
	}

	if p == nil {
		return nil
	}

	// Check character limit
	if maxChars, hasMaxChars := p["max_characters"]; hasMaxChars {
		if max, ok := maxChars.(int); ok {
			charCount := len(chunk.Content)
			if charCount > max {
				return &providers.ValidationAbortError{
					Reason: "exceeded max_characters limit",
					Chunk:  chunk,
				}
			}
		}
	}

	// Check token limit
	if maxTokens, hasMaxTokens := p["max_tokens"]; hasMaxTokens {
		if max, ok := maxTokens.(int); ok {
			// Use actual token count if available, otherwise estimate
			tokenCount := chunk.TokenCount
			if tokenCount == 0 {
				tokenCount = len(chunk.Content) / 4
			}
			if tokenCount > max {
				return &providers.ValidationAbortError{
					Reason: "exceeded max_tokens limit",
					Chunk:  chunk,
				}
			}
		}
	}

	return nil
}

// SupportsStreaming returns true as length can be checked incrementally
func (v *LengthValidator) SupportsStreaming() bool {
	return true
}

// Helper function to count sentences
func countSentences(text string) int {
	// Simple sentence counting by splitting on sentence-ending punctuation
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}

	// Split by sentence-ending punctuation
	sentences := regexp.MustCompile(`[.!?]+`).Split(text, -1)

	// Count non-empty sentences
	count := 0
	for _, sentence := range sentences {
		if strings.TrimSpace(sentence) != "" {
			count++
		}
	}

	// If no sentence-ending punctuation found, count as 1 sentence
	if count == 0 && strings.TrimSpace(text) != "" {
		count = 1
	}

	return count
}
