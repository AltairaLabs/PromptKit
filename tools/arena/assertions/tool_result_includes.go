package assertions

import (
	"fmt"

	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// ToolResultIncludesValidator asserts that a tool's result contains expected substrings.
type ToolResultIncludesValidator struct {
	tool       string
	patterns   []string
	occurrence int
}

// NewToolResultIncludesValidator creates a new tool_result_includes validator from params.
func NewToolResultIncludesValidator(params map[string]interface{}) runtimeValidators.Validator {
	tool, _ := params["tool"].(string)
	occurrence := extractIntParam(params, "occurrence", 1)

	return &ToolResultIncludesValidator{
		tool:       tool,
		patterns:   extractStringSlice(params, "patterns"),
		occurrence: occurrence,
	}
}

// Validate checks that at least `occurrence` matching tool calls have all patterns in their result.
func (v *ToolResultIncludesValidator) Validate(
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

	if len(v.patterns) == 0 {
		return runtimeValidators.ValidationResult{
			Passed: true,
			Details: map[string]interface{}{
				"message": "no patterns to check",
			},
		}
	}

	views := toolCallViewsFromTrace(trace)
	matchCount, missingDetails := coreToolResultIncludes(views, v.tool, v.patterns)

	// Remap "index" to "round_index" for turn-level compatibility
	turnMissing := make([]map[string]interface{}, len(missingDetails))
	for i, m := range missingDetails {
		turnMissing[i] = map[string]interface{}{
			"tool":             m["tool"],
			"missing_patterns": m["missing_patterns"],
			"round_index":      m["index"],
		}
	}

	if matchCount >= v.occurrence {
		return runtimeValidators.ValidationResult{
			Passed: true,
			Details: map[string]interface{}{
				"message":     "patterns found in tool results",
				"match_count": matchCount,
			},
		}
	}

	return runtimeValidators.ValidationResult{
		Passed: false,
		Details: map[string]interface{}{
			"message": fmt.Sprintf(
				"expected %d call(s) with all patterns, found %d",
				v.occurrence, matchCount,
			),
			"missing_details": turnMissing,
		},
	}
}
