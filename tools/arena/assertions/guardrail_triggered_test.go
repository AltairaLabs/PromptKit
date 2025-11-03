package assertions

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
)

func TestGuardrailTriggeredValidator_ValidatorTriggeredAsExpected(t *testing.T) {
	validator := NewGuardrailTriggeredValidator(nil)

	messages := []types.Message{
		{
			Role:    "assistant",
			Content: "test response",
			Validations: []types.ValidationResult{
				{
					ValidatorType: "pii_detector",
					Passed:        false,
					Details: map[string]interface{}{
						"message": "PII detected",
					},
				},
			},
		},
	}

	params := map[string]interface{}{
		"_execution_context_messages": messages,
		"validator_type":              "pii_detector",
		"should_trigger":              true,
	}

	result := validator.Validate("", params)
	assert.True(t, result.Passed)
}

func TestGuardrailTriggeredValidator_ValidatorDidNotTriggerAsExpected(t *testing.T) {
	validator := NewGuardrailTriggeredValidator(nil)

	messages := []types.Message{
		{
			Role:    "assistant",
			Content: "test response",
			Validations: []types.ValidationResult{
				{
					ValidatorType: "pii_detector",
					Passed:        true,
					Details: map[string]interface{}{
						"message": "No PII detected",
					},
				},
			},
		},
	}

	params := map[string]interface{}{
		"_execution_context_messages": messages,
		"validator_type":              "pii_detector",
		"should_trigger":              false,
	}

	result := validator.Validate("", params)
	assert.True(t, result.Passed)
}

func TestGuardrailTriggeredValidator_UnexpectedTrigger(t *testing.T) {
	validator := NewGuardrailTriggeredValidator(nil)

	messages := []types.Message{
		{
			Role:    "assistant",
			Content: "test response",
			Validations: []types.ValidationResult{
				{
					ValidatorType: "pii_detector",
					Passed:        false,
					Details: map[string]interface{}{
						"message": "PII detected",
					},
				},
			},
		},
	}

	params := map[string]interface{}{
		"_execution_context_messages": messages,
		"validator_type":              "pii_detector",
		"should_trigger":              false,
	}

	result := validator.Validate("", params)
	assert.False(t, result.Passed)
	assert.Contains(t, result.Details, "expected validator \"pii_detector\" to pass but it failed")
}

func TestGuardrailTriggeredValidator_UnexpectedPass(t *testing.T) {
	validator := NewGuardrailTriggeredValidator(nil)

	messages := []types.Message{
		{
			Role:    "assistant",
			Content: "test response",
			Validations: []types.ValidationResult{
				{
					ValidatorType: "pii_detector",
					Passed:        true,
					Details: map[string]interface{}{
						"message": "No PII detected",
					},
				},
			},
		},
	}

	params := map[string]interface{}{
		"_execution_context_messages": messages,
		"validator_type":              "pii_detector",
		"should_trigger":              true,
	}

	result := validator.Validate("", params)
	assert.False(t, result.Passed)
	assert.Contains(t, result.Details, "expected validator \"pii_detector\" to fail but it passed")
}

func TestGuardrailTriggeredValidator_MissingValidatorType(t *testing.T) {
	validator := NewGuardrailTriggeredValidator(nil)

	messages := []types.Message{
		{
			Role:    "assistant",
			Content: "test response",
			Validations: []types.ValidationResult{
				{
					ValidatorType: "other_validator",
					Passed:        false,
					Details: map[string]interface{}{
						"message": "Other validation failed",
					},
				},
			},
		},
	}

	params := map[string]interface{}{
		"_execution_context_messages": messages,
		"validator_type":              "pii_detector",
		"should_trigger":              true,
	}

	result := validator.Validate("", params)
	assert.False(t, result.Passed)
	assert.Contains(t, result.Details, "expected validator \"pii_detector\" to run but it did not")
}

func TestGuardrailTriggeredValidator_MissingValidatorOK(t *testing.T) {
	validator := NewGuardrailTriggeredValidator(nil)

	messages := []types.Message{
		{
			Role:    "assistant",
			Content: "test response",
			Validations: []types.ValidationResult{
				{
					ValidatorType: "other_validator",
					Passed:        true,
					Details: map[string]interface{}{
						"message": "Other validation passed",
					},
				},
			},
		},
	}

	params := map[string]interface{}{
		"_execution_context_messages": messages,
		"validator_type":              "pii_detector",
		"should_trigger":              false,
	}

	result := validator.Validate("", params)
	assert.True(t, result.Passed)
}

func TestGuardrailTriggeredValidator_NoAssistantMessage(t *testing.T) {
	validator := NewGuardrailTriggeredValidator(nil)

	messages := []types.Message{
		{
			Role:    "user",
			Content: "test request",
		},
	}

	params := map[string]interface{}{
		"_execution_context_messages": messages,
		"validator_type":              "pii_detector",
		"should_trigger":              true,
	}

	result := validator.Validate("", params)
	assert.False(t, result.Passed)
	assert.Contains(t, result.Details, "no assistant message found")
}

func TestGuardrailTriggeredValidator_MissingParameters(t *testing.T) {
	validator := NewGuardrailTriggeredValidator(nil)

	t.Run("missing execution context", func(t *testing.T) {
		params := map[string]interface{}{
			"validator_type": "pii_detector",
		}

		result := validator.Validate("", params)
		assert.False(t, result.Passed)
		assert.Contains(t, result.Details, "missing _execution_context_messages")
	})

	t.Run("missing validator type", func(t *testing.T) {
		params := map[string]interface{}{
			"_execution_context_messages": []types.Message{},
		}

		result := validator.Validate("", params)
		assert.False(t, result.Passed)
		assert.Contains(t, result.Details, "validator or validator_type parameter required")
	})
}

func TestGuardrailTriggeredValidator_DefaultShouldTrigger(t *testing.T) {
	validator := NewGuardrailTriggeredValidator(nil)

	messages := []types.Message{
		{
			Role:    "assistant",
			Content: "test response",
			Validations: []types.ValidationResult{
				{
					ValidatorType: "pii_detector",
					Passed:        false,
					Details: map[string]interface{}{
						"message": "PII detected",
					},
				},
			},
		},
	}

	// Don't specify should_trigger - should default to true
	params := map[string]interface{}{
		"_execution_context_messages": messages,
		"validator_type":              "pii_detector",
	}

	result := validator.Validate("", params)
	assert.True(t, result.Passed)
}

func TestGuardrailTriggeredValidator_ValidatorParamWithNameMatch(t *testing.T) {
	validator := NewGuardrailTriggeredValidator(nil)

	messages := []types.Message{
		{
			Role:    "assistant",
			Content: "test response",
			Validations: []types.ValidationResult{
				{
					ValidatorType: "*validators.BannedWordsValidator",
					Passed:        false,
					Details: map[string]interface{}{
						"message": "Banned words detected",
					},
				},
			},
		},
	}

	// Use 'validator' param with friendly name instead of full type
	params := map[string]interface{}{
		"_execution_context_messages": messages,
		"validator":                   "banned_words",
		"should_trigger":              true,
	}

	result := validator.Validate("", params)
	assert.True(t, result.Passed)
}

func TestGuardrailTriggeredValidator_ValidatorParamNoMatch(t *testing.T) {
	validator := NewGuardrailTriggeredValidator(nil)

	messages := []types.Message{
		{
			Role:    "assistant",
			Content: "test response",
			Validations: []types.ValidationResult{
				{
					ValidatorType: "*validators.BannedWordsValidator",
					Passed:        true,
					Details: map[string]interface{}{
						"message": "No banned words",
					},
				},
			},
		},
	}

	// Use 'validator' param - should not match because it passed but we expect trigger
	params := map[string]interface{}{
		"_execution_context_messages": messages,
		"validator":                   "banned_words",
		"should_trigger":              true,
	}

	result := validator.Validate("", params)
	assert.False(t, result.Passed)
	assert.Contains(t, result.Details, "expected validator")
	assert.Contains(t, result.Details, "to fail but it passed")
}
