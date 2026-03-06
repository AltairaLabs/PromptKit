package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// minCallsForComparison is the minimum number of tool calls needed to detect field loss.
const minCallsForComparison = 2

// InvariantFieldsPreservedHandler checks that field values in tool call arguments
// are not lost between calls to the same tool. If a field was present in an earlier
// call but disappears in a later call, that is a violation.
//
// Params:
//   - tool (string, required): The tool name to track across calls.
//   - fields ([]string, required): JSON field names to track in tool call arguments.
type InvariantFieldsPreservedHandler struct{}

// Type returns the eval type identifier.
func (h *InvariantFieldsPreservedHandler) Type() string { return "invariant_fields_preserved" }

// Eval checks that tracked fields are not lost between calls to the same tool.
func (h *InvariantFieldsPreservedHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	tool, _ := params["tool"].(string)
	fields := extractStringSlice(params, "fields")

	if tool == "" || len(fields) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "both 'tool' and 'fields' params are required",
		}, nil
	}

	matchingCalls := filterCallArgs(evalCtx.ToolCalls, tool)

	if len(matchingCalls) < minCallsForComparison {
		return &evals.EvalResult{
			Type:   h.Type(),
			Passed: true,
			Explanation: fmt.Sprintf(
				"fewer than %d calls to %q — nothing to compare",
				minCallsForComparison, tool),
		}, nil
	}

	violations := findFieldViolations(matchingCalls, fields)
	if len(violations) > 0 {
		return h.failResult(violations, tool), nil
	}

	return &evals.EvalResult{
		Type:   h.Type(),
		Passed: true,
		Explanation: fmt.Sprintf(
			"all %d tracked field(s) preserved across %d calls to %q",
			len(fields), len(matchingCalls), tool),
	}, nil
}

// filterCallArgs returns the Arguments maps for tool calls matching the given name.
func filterCallArgs(records []evals.ToolCallRecord, tool string) []map[string]any {
	var result []map[string]any
	for _, tc := range records {
		if tc.ToolName == tool {
			result = append(result, tc.Arguments)
		}
	}
	return result
}

// findFieldViolations checks each tracked field across calls. If a field was
// present in an earlier call but absent in a later one, it records a violation.
func findFieldViolations(calls []map[string]any, fields []string) []map[string]any {
	var violations []map[string]any
	for _, field := range fields {
		if v := checkFieldPreserved(calls, field); v != nil {
			violations = append(violations, v)
		}
	}
	return violations
}

// checkFieldPreserved returns a violation map if the field was present then lost,
// or nil if the field was always preserved (or never appeared).
func checkFieldPreserved(calls []map[string]any, field string) map[string]any {
	seen := false
	for i, args := range calls {
		_, present := args[field]
		if present {
			seen = true
		} else if seen {
			return map[string]any{
				"field":      field,
				"lost_at":    i,
				"call_index": i,
			}
		}
	}
	return nil
}

func (h *InvariantFieldsPreservedHandler) failResult(
	violations []map[string]any, tool string,
) *evals.EvalResult {
	msgs := make([]string, len(violations))
	for i, v := range violations {
		msgs[i] = fmt.Sprintf("field %q lost at call %d", v["field"], v["call_index"])
	}
	return &evals.EvalResult{
		Type:   h.Type(),
		Passed: false,
		Explanation: fmt.Sprintf(
			"%d field(s) lost: %s", len(violations), strings.Join(msgs, "; ")),
		Details: map[string]any{"violations": violations, "tool": tool},
	}
}
