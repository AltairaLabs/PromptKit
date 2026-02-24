package assertions

import (
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

const (
	countNotSet      = -1
	countWithinBound = "count within bounds"
)

// ToolCallCountValidator asserts count constraints on tool calls in a turn.
type ToolCallCountValidator struct {
	tool string // optional — if empty, count all tools
	min  int
	max  int
}

// NewToolCallCountValidator creates a new tool_call_count validator from params.
func NewToolCallCountValidator(params map[string]interface{}) runtimeValidators.Validator {
	tool, _ := params["tool"].(string)

	return &ToolCallCountValidator{
		tool: tool,
		min:  extractIntParam(params, "min", countNotSet),
		max:  extractIntParam(params, "max", countNotSet),
	}
}

// Validate checks that the count of tool calls matches the specified constraints.
func (v *ToolCallCountValidator) Validate(
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

	views := toolCallViewsFromTrace(trace)
	count, violation := coreToolCallCount(views, v.tool, v.min, v.max)

	details := map[string]interface{}{
		"count": count,
	}
	if v.tool != "" {
		details["tool"] = v.tool
	}

	if violation != "" {
		details["message"] = violation
		return runtimeValidators.ValidationResult{Passed: false, Details: details}
	}

	details["message"] = countWithinBound
	return runtimeValidators.ValidationResult{Passed: true, Details: details}
}

// extractIntParam extracts an integer param, handling YAML float64->int coercion.
func extractIntParam(params map[string]interface{}, key string, defaultVal int) int {
	val, ok := params[key]
	if !ok {
		return defaultVal
	}

	switch v := val.(type) {
	case int:
		return v
	case float64:
		return int(v)
	case int64:
		return int(v)
	default:
		return defaultVal
	}
}
