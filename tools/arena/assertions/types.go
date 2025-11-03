package assertions

import (
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// AssertionConfig extends ValidatorConfig with arena-specific fields
type AssertionConfig struct {
	Type    string                 `json:"type" yaml:"type"`
	Params  map[string]interface{} `json:"params" yaml:"params"`
	Message string                 `json:"message,omitempty" yaml:"message,omitempty"` // Human-readable description of what this assertion checks
}

// ToValidatorConfig converts AssertionConfig to runtime ValidatorConfig
func (a AssertionConfig) ToValidatorConfig() runtimeValidators.ValidatorConfig {
	return runtimeValidators.ValidatorConfig{
		Type:   a.Type,
		Params: a.Params,
	}
}

// AssertionResult extends ValidationResult with the assertion message
type AssertionResult struct {
	Passed  bool        `json:"passed"`
	Details interface{} `json:"details,omitempty"`
	Message string      `json:"message,omitempty"` // Human-readable description from config
}

// FromValidationResult creates an AssertionResult from a ValidationResult
func FromValidationResult(vr runtimeValidators.ValidationResult, message string) AssertionResult {
	return AssertionResult{
		Passed:  vr.Passed,
		Details: vr.Details,
		Message: message,
	}
}
