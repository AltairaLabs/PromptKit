package assertions

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// GuardrailTriggeredValidator is an assertion validator that checks if a specific
// guardrail validator triggered (or didn't trigger) as expected.
// This is useful for testing that guardrails work correctly in PromptArena scenarios.
type GuardrailTriggeredValidator struct{}

// NewGuardrailTriggeredValidator creates a new GuardrailTriggeredValidator instance
func NewGuardrailTriggeredValidator(params map[string]interface{}) runtimeValidators.Validator {
	return &GuardrailTriggeredValidator{}
}

// Validate checks if the expected validator triggered as expected
func (v *GuardrailTriggeredValidator) Validate(
	content string,
	params map[string]interface{},
) runtimeValidators.ValidationResult {
	// Extract and validate parameters
	execMessages, expectedType, shouldTrigger, result := v.extractParams(params)
	if !result.Passed {
		return result
	}

	// Find the last assistant message with validations
	validations, result := v.findAssistantValidations(execMessages)
	if !result.Passed {
		return result
	}

	// Find the specific validator result
	found := v.findValidatorResult(validations, expectedType)

	// Check if validator behaved as expected
	return v.checkValidatorBehavior(found, expectedType, shouldTrigger)
}

// extractParams extracts and validates required parameters
func (v *GuardrailTriggeredValidator) extractParams(
	params map[string]interface{},
) (execMessages []types.Message, expectedType string, shouldTrigger bool, result runtimeValidators.ValidationResult) {
	// Get the execution context messages from params
	execMessages, ok := params["_execution_context_messages"].([]types.Message)
	if !ok {
		result = runtimeValidators.ValidationResult{
			Passed:      false,
			Details: "missing _execution_context_messages parameter",
		}
		return execMessages, expectedType, shouldTrigger, result
	}

	// Get expected validator type from params - support both 'validator' and 'validator_type'
	expectedType, ok = params["validator_type"].(string)
	if !ok {
		// Try 'validator' as alternative parameter name
		expectedType, ok = params["validator"].(string)
		if !ok {
			result = runtimeValidators.ValidationResult{
				Passed:      false,
				Details: "validator or validator_type parameter required",
			}
			return execMessages, expectedType, shouldTrigger, result
		}
	}

	// Get whether we expect the validator to trigger (fail)
	shouldTrigger, ok = params["should_trigger"].(bool)
	if !ok {
		// Default: expect validator to trigger
		shouldTrigger = true
	}

	result = runtimeValidators.ValidationResult{Passed: true}
	return execMessages, expectedType, shouldTrigger, result
}

// findAssistantValidations finds validation results from the last assistant message
func (v *GuardrailTriggeredValidator) findAssistantValidations(
	messages []types.Message,
) (validations []types.ValidationResult, result runtimeValidators.ValidationResult) {
	// Find the last assistant message
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			validations = messages[i].Validations
			result = runtimeValidators.ValidationResult{Passed: true}
			return validations, result
		}
	}

	result = runtimeValidators.ValidationResult{
		Passed:      false,
		Details: "no assistant message found to check validations",
	}
	return validations, result
}

// findValidatorResult searches for a specific validator's result
func (v *GuardrailTriggeredValidator) findValidatorResult(
	validations []types.ValidationResult,
	expectedType string,
) *types.ValidationResult {
	for i := range validations {
		if v.validatorTypeMatches(validations[i].ValidatorType, expectedType) {
			return &validations[i]
		}
	}
	return nil
}

// validatorTypeMatches checks if a validator type matches the expected name
// Supports both exact matches and friendly name matching (e.g., "banned_words" matches "*validators.BannedWordsValidator")
func (v *GuardrailTriggeredValidator) validatorTypeMatches(validatorType, expectedName string) bool {
	// Exact match
	if validatorType == expectedName {
		return true
	}

	// Try friendly name matching - convert snake_case to PascalCase
	// e.g., "banned_words" -> "BannedWords"
	friendlyName := v.snakeToPascal(expectedName)

	// Check if validatorType contains the friendly name
	// e.g., "*validators.BannedWordsValidator" contains "BannedWords"
	return len(friendlyName) > 0 &&
		(validatorType == friendlyName+"Validator" ||
			validatorType == "*validators."+friendlyName+"Validator")
}

// snakeToPascal converts snake_case to PascalCase
// e.g., "banned_words" -> "BannedWords"
func (v *GuardrailTriggeredValidator) snakeToPascal(s string) string {
	if s == "" {
		return ""
	}

	var result []rune
	capitalizeNext := true

	for _, r := range s {
		if r == '_' {
			capitalizeNext = true
			continue
		}

		if capitalizeNext {
			result = append(result, r-('a'-'A'))
			capitalizeNext = false
		} else {
			result = append(result, r)
		}
	}

	return string(result)
}

// checkValidatorBehavior verifies if the validator behaved as expected
func (v *GuardrailTriggeredValidator) checkValidatorBehavior(
	found *types.ValidationResult,
	expectedType string,
	shouldTrigger bool,
) runtimeValidators.ValidationResult {
	// If validator wasn't found
	if found == nil {
		return v.handleMissingValidator(expectedType, shouldTrigger)
	}

	// Validator was found - check if it passed/failed as expected
	return v.validateResult(found, expectedType, shouldTrigger)
}

// handleMissingValidator handles the case when validator didn't run
func (v *GuardrailTriggeredValidator) handleMissingValidator(
	expectedType string,
	shouldTrigger bool,
) runtimeValidators.ValidationResult {
	if shouldTrigger {
		// We expected it to run and fail, but it didn't even run
		return runtimeValidators.ValidationResult{
			Passed:      false,
			Details: fmt.Sprintf("expected validator %q to run but it did not", expectedType),
		}
	}
	// We expected it not to trigger, and it didn't run - that's OK
	return runtimeValidators.ValidationResult{
		Passed: true,
		Details: map[string]interface{}{
			"validator": expectedType,
			"triggered": false,
			"message":   "validator did not run (as expected)",
		},
	}
}

// validateResult checks if the validator result matches expectations
func (v *GuardrailTriggeredValidator) validateResult(
	found *types.ValidationResult,
	expectedType string,
	shouldTrigger bool,
) runtimeValidators.ValidationResult {
	// shouldTrigger=true means we expect the validator to FAIL (trigger)
	// shouldTrigger=false means we expect the validator to PASS (not trigger)

	if shouldTrigger && found.Passed {
		// We expected it to fail, but it passed
		return runtimeValidators.ValidationResult{
			Passed:      false,
			Details: fmt.Sprintf("expected validator %q to fail but it passed", expectedType),
		}
	}

	if !shouldTrigger && !found.Passed {
		// We expected it to pass, but it failed
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: fmt.Sprintf("expected validator %q to pass but it failed: %v",
				expectedType, found.Details),
		}
	}

	// Validation behaved as expected
	return runtimeValidators.ValidationResult{
		Passed: true,
		Details: map[string]interface{}{
			"validator": expectedType,
			"triggered": !found.Passed,
			"details":   found.Details,
		},
	}
}
