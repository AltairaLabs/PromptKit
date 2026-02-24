package assertions

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
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

// ToEvalDef converts an AssertionConfig to an evals.EvalDef.
// This is the bridge for unifying arena assertions with runtime evals.
func (a AssertionConfig) ToEvalDef(index int) evals.EvalDef {
	def := evals.EvalDef{
		ID:        fmt.Sprintf("assertion_%d_%s", index, a.Type),
		Type:      a.Type,
		Trigger:   evals.TriggerEveryTurn,
		Params:    a.Params,
		Message:   a.Message,
		Threshold: &evals.Threshold{Passed: boolPtr(true)},
	}
	if a.When != nil {
		def.When = &evals.EvalWhen{
			ToolCalled:        a.When.ToolCalled,
			ToolCalledPattern: a.When.ToolCalledPattern,
			AnyToolCalled:     a.When.AnyToolCalled,
			MinToolCalls:      a.When.MinToolCalls,
		}
	}
	return def
}

// ToConversationEvalDef converts an AssertionConfig to an evals.EvalDef
// with TriggerOnConversationComplete. Used for conversation-level assertions.
func (a AssertionConfig) ToConversationEvalDef(index int) evals.EvalDef {
	def := a.ToEvalDef(index)
	def.Trigger = evals.TriggerOnConversationComplete
	return def
}

func boolPtr(b bool) *bool { return &b }
