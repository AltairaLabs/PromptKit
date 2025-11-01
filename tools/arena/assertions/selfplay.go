package validators

import (
	"regexp"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/validators"
)

// ValidationResult is an alias for runtime validators.ValidationResult
type ValidationResult = validators.ValidationResult

// RoleIntegrityValidator checks that user LLM doesn't provide assistant-like responses
type RoleIntegrityValidator struct{}

// NewRoleIntegrityValidator creates a new role integrity validator
func NewRoleIntegrityValidator() *RoleIntegrityValidator {
	return &RoleIntegrityValidator{}
}

// Validate checks for role integrity violations
func (v *RoleIntegrityValidator) Validate(content string, params map[string]interface{}) ValidationResult {
	contentLower := strings.ToLower(content)

	problematicPhrases := []string{
		"here's your plan",
		"here's what you should do",
		"step 1:",
		"step 2:",
		"first, you should",
		"as an ai",
		"i recommend",
		"my suggestion is",
		"you should start by",
		"let me help you",
		"i'll create a plan",
		"the next steps are",
	}

	var violations []string
	for _, phrase := range problematicPhrases {
		if strings.Contains(contentLower, phrase) {
			violations = append(violations, phrase)
		}
	}

	return ValidationResult{
		OK: len(violations) == 0,
		Details: map[string]interface{}{
			"violations": violations,
		},
	}
}

// SupportsStreaming returns false as role integrity requires complete content
func (v *RoleIntegrityValidator) SupportsStreaming() bool {
	return false
}

// QuestionCapValidator ensures user messages have at most one question
type QuestionCapValidator struct{}

// NewQuestionCapValidator creates a new question cap validator
func NewQuestionCapValidator() *QuestionCapValidator {
	return &QuestionCapValidator{}
}

// Validate checks for question count violations
func (v *QuestionCapValidator) Validate(content string, params map[string]interface{}) ValidationResult {
	questionCount := strings.Count(content, "?")

	return ValidationResult{
		OK: questionCount <= 1,
		Details: map[string]interface{}{
			"question_count": questionCount,
			"max_allowed":    1,
		},
	}
}

// SupportsStreaming returns false as question counting requires complete content
func (v *QuestionCapValidator) SupportsStreaming() bool {
	return false
}

// LengthCapValidator ensures user messages are at most 2 sentences
type LengthCapValidator struct{}

// NewLengthCapValidator creates a new length cap validator
func NewLengthCapValidator() *LengthCapValidator {
	return &LengthCapValidator{}
}

// Validate checks for sentence count violations
func (v *LengthCapValidator) Validate(content string, params map[string]interface{}) ValidationResult {
	// Split by sentence endings and count non-empty sentences
	sentences := regexp.MustCompile(`[.!?]+`).Split(content, -1)

	var nonEmptySentences []string
	for _, s := range sentences {
		if strings.TrimSpace(s) != "" {
			nonEmptySentences = append(nonEmptySentences, s)
		}
	}

	sentenceCount := len(nonEmptySentences)

	return ValidationResult{
		OK: sentenceCount <= 2,
		Details: map[string]interface{}{
			"sentence_count": sentenceCount,
			"max_allowed":    2,
		},
	}
}

// SupportsStreaming returns false as sentence counting requires complete content
func (v *LengthCapValidator) SupportsStreaming() bool {
	return false
}
