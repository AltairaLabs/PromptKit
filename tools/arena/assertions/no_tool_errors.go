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

	views := toolCallViewsFromTrace(trace)
	errors := coreNoToolErrors(views, v.tools)

	if len(errors) > 0 {
		// Remap "index" to "round_index" for turn-level compatibility
		turnErrors := make([]map[string]interface{}, len(errors))
		for i, e := range errors {
			turnErrors[i] = map[string]interface{}{
				"tool":        e["tool"],
				"error":       e["error"],
				"round_index": e["index"],
			}
		}
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"message":     fmt.Sprintf("%d tool call(s) returned errors", len(errors)),
				"tool_errors": turnErrors,
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
