package assertions

import (
	"fmt"

	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// ToolResultMatchesValidator asserts that a tool's result matches a regex pattern.
type ToolResultMatchesValidator struct {
	tool       string
	pattern    string
	occurrence int
}

// NewToolResultMatchesValidator creates a new tool_result_matches validator from params.
func NewToolResultMatchesValidator(params map[string]interface{}) runtimeValidators.Validator {
	tool, _ := params["tool"].(string)
	pattern, _ := params["pattern"].(string)
	occurrence := extractIntParam(params, "occurrence", 1)

	return &ToolResultMatchesValidator{
		tool:       tool,
		pattern:    pattern,
		occurrence: occurrence,
	}
}

// Validate checks that at least `occurrence` matching tool calls have results matching the regex.
func (v *ToolResultMatchesValidator) Validate(
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

	if v.pattern == "" {
		return runtimeValidators.ValidationResult{
			Passed: true,
			Details: map[string]interface{}{
				"message": "no pattern to check",
			},
		}
	}

	views := toolCallViewsFromTrace(trace)
	matchCount, err := coreToolResultMatches(views, v.tool, v.pattern)

	if err != nil {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error":   "invalid_regex",
				"pattern": v.pattern,
				"message": err.Error(),
			},
		}
	}

	if matchCount >= v.occurrence {
		return runtimeValidators.ValidationResult{
			Passed: true,
			Details: map[string]interface{}{
				"message":     "pattern matched in tool results",
				"match_count": matchCount,
			},
		}
	}

	return runtimeValidators.ValidationResult{
		Passed: false,
		Details: map[string]interface{}{
			"message": fmt.Sprintf(
				"expected %d call(s) matching pattern, found %d",
				v.occurrence, matchCount,
			),
			"pattern": v.pattern,
			"tool":    v.tool,
		},
	}
}
