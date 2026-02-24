package handlers

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// countNotSet is the sentinel value indicating a min/max count was not specified.
const countNotSet = -1

// toolCallView is a normalized view of a tool call for shared validation logic.
type toolCallView struct {
	Name   string
	Args   map[string]any
	Result string
	Error  string
	Index  int
}

// viewsFromRecords converts ToolCallRecords to normalized views.
func viewsFromRecords(records []evals.ToolCallRecord) []toolCallView {
	views := make([]toolCallView, len(records))
	for i, r := range records {
		views[i] = toolCallView{
			Name:  r.ToolName,
			Args:  r.Arguments,
			Error: r.Error,
			Index: r.TurnIndex,
		}
		if r.Result != nil {
			views[i].Result = fmt.Sprintf("%v", r.Result)
		}
	}
	return views
}

// coreNoToolErrors checks for tool errors in the given calls.
// If tools is non-empty, only checks calls matching those tool names.
func coreNoToolErrors(calls []toolCallView, tools []string) []map[string]any {
	scopeSet := make(map[string]bool, len(tools))
	for _, t := range tools {
		scopeSet[t] = true
	}

	var errors []map[string]any
	for _, tc := range calls {
		if len(scopeSet) > 0 && !scopeSet[tc.Name] {
			continue
		}
		if tc.Error != "" {
			errors = append(errors, map[string]any{
				"tool":  tc.Name,
				"error": tc.Error,
				"index": tc.Index,
			})
		}
	}
	return errors
}

// coreToolCallCount counts matching calls and checks bounds.
func coreToolCallCount(calls []toolCallView, tool string, minCount, maxCount int) (count int, violation string) {
	for _, tc := range calls {
		if tool == "" || tc.Name == tool {
			count++
		}
	}

	if minCount != countNotSet && count < minCount {
		return count, fmt.Sprintf("expected at least %d call(s), got %d", minCount, count)
	}
	if maxCount != countNotSet && count > maxCount {
		return count, fmt.Sprintf("expected at most %d call(s), got %d", maxCount, count)
	}
	return count, ""
}

// coreToolResultIncludes checks substring patterns in tool results.
func coreToolResultIncludes(
	calls []toolCallView, tool string, patterns []string,
) (matchCount int, missingDetails []map[string]any) {
	for _, tc := range calls {
		if tool != "" && tc.Name != tool {
			continue
		}

		resultLower := strings.ToLower(tc.Result)
		allFound := true
		var missing []string
		for _, p := range patterns {
			if !strings.Contains(resultLower, strings.ToLower(p)) {
				allFound = false
				missing = append(missing, p)
			}
		}

		if allFound {
			matchCount++
		} else {
			missingDetails = append(missingDetails, map[string]any{
				"tool":             tc.Name,
				"missing_patterns": missing,
				"index":            tc.Index,
			})
		}
	}
	return matchCount, missingDetails
}

// coreToolResultMatches checks a regex pattern on tool results.
func coreToolResultMatches(
	calls []toolCallView, tool, pattern string,
) (int, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return 0, err
	}

	matchCount := 0
	for _, tc := range calls {
		if tool != "" && tc.Name != tool {
			continue
		}
		if re.MatchString(tc.Result) {
			matchCount++
		}
	}
	return matchCount, nil
}

// coreToolCallSequence checks subsequence ordering of tool calls.
func coreToolCallSequence(calls []toolCallView, sequence []string) (matched int, actualTools []string) {
	for _, tc := range calls {
		if matched < len(sequence) && tc.Name == sequence[matched] {
			matched++
		}
	}

	actualTools = make([]string, len(calls))
	for i, tc := range calls {
		actualTools[i] = tc.Name
	}
	return matched, actualTools
}

// chainStep defines a single step in a tool call chain.
type chainStep struct {
	tool           string
	resultIncludes []string
	resultMatches  string
	argsMatch      map[string]string
	noError        bool
}

// parseChainSteps extracts chain steps from params.
func parseChainSteps(params map[string]any) []chainStep {
	stepsRaw, _ := params["steps"].([]any)
	steps := make([]chainStep, 0, len(stepsRaw))

	for _, raw := range stepsRaw {
		stepMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		step := chainStep{
			noError: extractBool(stepMap, "no_error"),
		}
		step.tool, _ = stepMap["tool"].(string)
		step.resultIncludes = extractStringSlice(stepMap, "result_includes")
		step.resultMatches, _ = stepMap["result_matches"].(string)
		step.argsMatch = extractMapStringString(stepMap, "args_match")
		steps = append(steps, step)
	}

	return steps
}

// coreToolCallChain checks a dependency chain with per-step constraints.
func coreToolCallChain(
	calls []toolCallView, steps []chainStep,
) (completedSteps int, failure map[string]any) {
	for _, tc := range calls {
		if completedSteps >= len(steps) {
			break
		}
		step := steps[completedSteps]
		if tc.Name != step.tool {
			continue
		}

		if violation := validateChainStepView(&tc, step, completedSteps); violation != nil {
			return completedSteps, violation
		}
		completedSteps++
	}
	return completedSteps, nil
}

func validateChainStepView(tc *toolCallView, step chainStep, stepIndex int) map[string]any {
	if step.noError && tc.Error != "" {
		return chainStepFailure(stepIndex, step.tool, "unexpected error", map[string]any{
			"error": tc.Error,
		})
	}

	if f := checkChainResultIncludesView(tc, step, stepIndex); f != nil {
		return f
	}

	if f := checkChainResultMatchesView(tc, step, stepIndex); f != nil {
		return f
	}

	return checkChainArgsMatchView(tc, step, stepIndex)
}

func checkChainResultIncludesView(tc *toolCallView, step chainStep, stepIndex int) map[string]any {
	if len(step.resultIncludes) == 0 {
		return nil
	}
	resultLower := strings.ToLower(tc.Result)
	for _, pattern := range step.resultIncludes {
		if !strings.Contains(resultLower, strings.ToLower(pattern)) {
			return chainStepFailure(stepIndex, step.tool,
				fmt.Sprintf("result missing pattern %q", pattern),
				map[string]any{"missing_pattern": pattern})
		}
	}
	return nil
}

func checkChainResultMatchesView(tc *toolCallView, step chainStep, stepIndex int) map[string]any {
	if step.resultMatches == "" {
		return nil
	}
	re, err := regexp.Compile(step.resultMatches)
	if err != nil {
		return chainStepFailure(stepIndex, step.tool,
			fmt.Sprintf("invalid regex %q", step.resultMatches),
			map[string]any{"error": err.Error()})
	}
	if !re.MatchString(tc.Result) {
		return chainStepFailure(stepIndex, step.tool,
			"result does not match pattern",
			map[string]any{"pattern": step.resultMatches})
	}
	return nil
}

func checkChainArgsMatchView(tc *toolCallView, step chainStep, stepIndex int) map[string]any {
	for argName, pattern := range step.argsMatch {
		argVal, exists := tc.Args[argName]
		if !exists {
			return chainStepFailure(stepIndex, step.tool,
				fmt.Sprintf("missing argument %q", argName),
				map[string]any{"argument": argName})
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return chainStepFailure(stepIndex, step.tool,
				fmt.Sprintf("invalid arg regex %q", pattern),
				map[string]any{"error": err.Error()})
		}
		if !re.MatchString(asString(argVal)) {
			return chainStepFailure(stepIndex, step.tool,
				fmt.Sprintf("argument %q does not match pattern", argName),
				map[string]any{"argument": argName, "pattern": pattern, "actual": argVal})
		}
	}
	return nil
}

func chainStepFailure(
	stepIndex int, tool, reason string, extra map[string]any,
) map[string]any {
	result := map[string]any{
		"message":    fmt.Sprintf("step %d (%s): %s", stepIndex, tool, reason),
		"step_index": stepIndex,
		"tool":       tool,
	}
	for k, v := range extra {
		result[k] = v
	}
	return result
}
