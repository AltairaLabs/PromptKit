package assertions

import (
	"fmt"

	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// NoToolErrorsValidator asserts that all tool calls in a turn succeeded (no errors).
type NoToolErrorsValidator struct {
	tools []string // optional scope — if empty, check all tools
}

// NewNoToolErrorsValidator creates a new no_tool_errors validator from params.
func NewNoToolErrorsValidator(params map[string]interface{}) runtimeValidators.Validator {
	return &NoToolErrorsValidator{
		tools: extractStringSlice(params, "tools"),
	}
}

// Validate checks that no tool calls in the turn returned errors.
func (v *NoToolErrorsValidator) Validate(
	content string, params map[string]interface{},
) runtimeValidators.ValidationResult {
	trace, ok := resolveTurnToolTrace(params)
	if !ok {
		return runtimeValidators.ValidationResult{
			Passed: true,
			Details: map[string]interface{}{
				"skipped": true,
				"reason":  "turn tool trace not available (duplex path)",
			},
		}
	}

	if len(trace) == 0 {
		return runtimeValidators.ValidationResult{
			Passed: true,
			Details: map[string]interface{}{
				"message": "no tool calls in turn",
			},
		}
	}

	scopeSet := make(map[string]bool, len(v.tools))
	for _, t := range v.tools {
		scopeSet[t] = true
	}

	var errors []map[string]interface{}
	for _, tc := range trace {
		if len(scopeSet) > 0 && !scopeSet[tc.Name] {
			continue
		}
		if tc.Error != "" {
			errors = append(errors, map[string]interface{}{
				"tool":        tc.Name,
				"error":       tc.Error,
				"round_index": tc.RoundIndex,
			})
		}
	}

	if len(errors) > 0 {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"message":     fmt.Sprintf("%d tool call(s) returned errors", len(errors)),
				"tool_errors": errors,
			},
		}
	}

	return runtimeValidators.ValidationResult{
		Passed: true,
		Details: map[string]interface{}{
			"message": "all tool calls succeeded",
		},
	}
}
