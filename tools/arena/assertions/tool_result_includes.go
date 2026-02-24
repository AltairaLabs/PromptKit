package assertions

import (
	"fmt"
	"strings"

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

	matchCount := 0
	var missingPatterns []map[string]interface{}

	for _, tc := range trace {
		if v.tool != "" && tc.Name != v.tool {
			continue
		}

		resultLower := strings.ToLower(tc.Result)
		allFound := true
		var missing []string
		for _, p := range v.patterns {
			if !strings.Contains(resultLower, strings.ToLower(p)) {
				allFound = false
				missing = append(missing, p)
			}
		}

		if allFound {
			matchCount++
		} else {
			missingPatterns = append(missingPatterns, map[string]interface{}{
				"tool":             tc.Name,
				"missing_patterns": missing,
				"round_index":      tc.RoundIndex,
			})
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
			"missing_details": missingPatterns,
		},
	}
}
