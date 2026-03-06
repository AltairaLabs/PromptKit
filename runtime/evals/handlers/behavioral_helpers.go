package handlers

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// compareToolSets checks if the actual tool calls match the expected set and returns an EvalResult.
func compareToolSets(
	evalType string,
	evalCtx *evals.EvalContext,
	expectedTools []string,
	expectedLabel string,
) *evals.EvalResult {
	actual := uniqueToolNames(evalCtx.ToolCalls)
	expected := toSortedSet(expectedTools)

	if setsEqual(actual, expected) {
		return &evals.EvalResult{
			Type:        evalType,
			Passed:      true,
			Explanation: fmt.Sprintf("tool calls match %s: [%s]", expectedLabel, strings.Join(expected, ", ")),
			Details:     map[string]any{"actual": actual, expectedLabel: expected},
		}
	}

	return &evals.EvalResult{
		Type:   evalType,
		Passed: false,
		Explanation: fmt.Sprintf(
			"tool calls differ from %s: got [%s], %s [%s]",
			expectedLabel, strings.Join(actual, ", "), expectedLabel, strings.Join(expected, ", "),
		),
		Details: map[string]any{"actual": actual, expectedLabel: expected},
	}
}

// compareWorkflowState checks if the actual workflow state matches the expected value.
func compareWorkflowState(
	evalType string,
	evalCtx *evals.EvalContext,
	expectedState string,
	expectedLabel string,
) *evals.EvalResult {
	raw, ok := evalCtx.Extras["workflow_state"]
	if !ok {
		return &evals.EvalResult{
			Type:        evalType,
			Passed:      false,
			Explanation: "workflow_state not found in eval context extras",
		}
	}

	actualState := asString(raw)
	if actualState == expectedState {
		return &evals.EvalResult{
			Type:        evalType,
			Passed:      true,
			Explanation: fmt.Sprintf("state matches %s: %q", expectedLabel, expectedState),
			Details:     map[string]any{"actual": actualState, expectedLabel: expectedState},
		}
	}

	return &evals.EvalResult{
		Type:        evalType,
		Passed:      false,
		Explanation: fmt.Sprintf("state %q does not match %s %q", actualState, expectedLabel, expectedState),
		Details:     map[string]any{"actual": actualState, expectedLabel: expectedState},
	}
}

// uniqueToolNames extracts a sorted, deduplicated list of tool names from tool calls.
func uniqueToolNames(calls []evals.ToolCallRecord) []string {
	seen := make(map[string]struct{}, len(calls))
	for _, tc := range calls {
		seen[tc.ToolName] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// toSortedSet returns a sorted, deduplicated copy of the input slice.
func toSortedSet(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		seen[item] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for item := range seen {
		result = append(result, item)
	}
	sort.Strings(result)
	return result
}

// setsEqual checks if two sorted string slices are identical.
func setsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
