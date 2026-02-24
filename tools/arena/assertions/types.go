package assertions

import (
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// AssertionConfig extends ValidatorConfig with arena-specific fields
type AssertionConfig struct {
	Type    string                 `json:"type" yaml:"type"`
	Params  map[string]interface{} `json:"params" yaml:"params"`
	Message string                 `json:"message,omitempty" yaml:"message,omitempty"`
	When    *AssertionWhen         `json:"when,omitempty" yaml:"when,omitempty"`
}

// AssertionWhen specifies preconditions that must be met for an assertion to run.
// If any condition is not met, the assertion is skipped (not failed).
type AssertionWhen struct {
	// ToolCalled requires an exact tool name to have been called.
	ToolCalled string `json:"tool_called,omitempty" yaml:"tool_called,omitempty"`
	// ToolCalledPattern is a regex that must match at least one tool name.
	ToolCalledPattern string `json:"tool_called_pattern,omitempty" yaml:"tool_called_pattern,omitempty"`
	// AnyToolCalled requires at least one tool to have been called.
	AnyToolCalled bool `json:"any_tool_called,omitempty" yaml:"any_tool_called,omitempty"`
	// MinToolCalls is the minimum number of tool calls required.
	MinToolCalls int `json:"min_tool_calls,omitempty" yaml:"min_tool_calls,omitempty"`
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
