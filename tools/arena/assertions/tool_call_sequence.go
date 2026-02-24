package assertions

import (
	"fmt"
	"strings"

	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// ToolCallSequenceValidator asserts that tool calls within a turn follow a specified order.
// Uses subsequence matching — non-matching tools are ignored.
type ToolCallSequenceValidator struct {
	sequence []string
}

// NewToolCallSequenceValidator creates a new tool_call_sequence validator from params.
func NewToolCallSequenceValidator(params map[string]interface{}) runtimeValidators.Validator {
	return &ToolCallSequenceValidator{
		sequence: extractStringSlice(params, "sequence"),
	}
}

// Validate checks that tool calls appear in the specified order (subsequence match).
func (v *ToolCallSequenceValidator) Validate(
	content string, params map[string]interface{},
) runtimeValidators.ValidationResult {
	if len(v.sequence) == 0 {
		return runtimeValidators.ValidationResult{
			Passed: true,
			Details: map[string]interface{}{
				"message": "empty sequence always passes",
			},
		}
	}

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

	cursor := 0
	for _, tc := range trace {
		if cursor < len(v.sequence) && tc.Name == v.sequence[cursor] {
			cursor++
		}
	}

	if cursor < len(v.sequence) {
		// Collect actual tool names for diagnostics
		actual := make([]string, len(trace))
		for i, tc := range trace {
			actual[i] = tc.Name
		}
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"message": fmt.Sprintf(
					"sequence not satisfied: matched %d/%d steps, stuck at %q",
					cursor, len(v.sequence), v.sequence[cursor],
				),
				"expected_sequence": v.sequence,
				"actual_tools":      strings.Join(actual, " → "),
				"matched_steps":     cursor,
			},
		}
	}

	return runtimeValidators.ValidationResult{
		Passed: true,
		Details: map[string]interface{}{
			"message":       "sequence satisfied",
			"matched_steps": len(v.sequence),
			"total_calls":   len(trace),
		},
	}
}
