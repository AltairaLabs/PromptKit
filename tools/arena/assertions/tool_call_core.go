package assertions

import (
	"fmt"
	"regexp"
	"strings"
)

// ToolCallView is a normalized view of a tool call for shared validation logic.
// Both TurnToolCall and ToolCallRecord can convert to this type, enabling
// core validation functions to be reused across turn-level and conversation-level adapters.
type ToolCallView struct {
	Name   string
	Args   map[string]interface{}
	Result string
	Error  string
	Index  int // RoundIndex (turn) or TurnIndex (conversation)
}

// toolCallViewsFromTrace converts turn-level TurnToolCall slice to normalized views.
func toolCallViewsFromTrace(trace []TurnToolCall) []ToolCallView {
	views := make([]ToolCallView, len(trace))
	for i, tc := range trace {
		views[i] = ToolCallView{
			Name:   tc.Name,
			Args:   tc.Args,
			Result: tc.Result,
			Error:  tc.Error,
			Index:  tc.RoundIndex,
		}
	}
	return views
}

// toolCallViewsFromRecords converts conversation-level ToolCallRecord slice to normalized views.
func toolCallViewsFromRecords(records []ToolCallRecord) []ToolCallView {
	views := make([]ToolCallView, len(records))
	for i, r := range records {
		views[i] = ToolCallView{
			Name:   r.ToolName,
			Args:   r.Arguments,
			Result: fmt.Sprintf("%v", r.Result),
			Error:  r.Error,
			Index:  r.TurnIndex,
		}
		// Handle nil Result — fmt.Sprintf("%v", nil) produces "<nil>"
		if r.Result == nil {
			views[i].Result = ""
		}
	}
	return views
}

// coreNoToolErrors checks for tool errors in the given calls.
// If tools is non-empty, only checks calls matching those tool names.
// Returns a slice of error detail maps (empty if no errors found).
func coreNoToolErrors(calls []ToolCallView, tools []string) []map[string]interface{} {
	scopeSet := make(map[string]bool, len(tools))
	for _, t := range tools {
		scopeSet[t] = true
	}

	var errors []map[string]interface{}
	for _, tc := range calls {
		if len(scopeSet) > 0 && !scopeSet[tc.Name] {
			continue
		}
		if tc.Error != "" {
			errors = append(errors, map[string]interface{}{
				"tool":  tc.Name,
				"error": tc.Error,
				"index": tc.Index,
			})
		}
	}
	return errors
}

// coreToolCallCount counts matching calls and checks bounds.
// Returns the count and a violation message (empty string if within bounds).
func coreToolCallCount(calls []ToolCallView, tool string, minCount, maxCount int) (count int, violation string) {
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
// Returns the number of matching calls and details about misses.
func coreToolResultIncludes(
	calls []ToolCallView, tool string, patterns []string,
) (matchCount int, missingDetails []map[string]interface{}) {
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
			missingDetails = append(missingDetails, map[string]interface{}{
				"tool":             tc.Name,
				"missing_patterns": missing,
				"index":            tc.Index,
			})
		}
	}
	return matchCount, missingDetails
}

// coreToolResultMatches checks a regex pattern on tool results.
// Returns the number of matching calls and any regex compilation error.
func coreToolResultMatches(
	calls []ToolCallView, tool, pattern string,
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
// Returns the number of matched steps and the actual tool names list.
func coreToolCallSequence(calls []ToolCallView, sequence []string) (matched int, actualTools []string) {
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

// coreToolCallChain checks a dependency chain with per-step constraints.
// Returns the number of completed steps and a failure detail map (nil if all steps pass).
func coreToolCallChain(
	calls []ToolCallView, steps []chainStep,
) (completedSteps int, failure map[string]interface{}) {
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

// validateChainStepView checks a single step's constraints against a ToolCallView.
func validateChainStepView(tc *ToolCallView, step chainStep, stepIndex int) map[string]interface{} {
	if step.noError && tc.Error != "" {
		return chainStepFailure(stepIndex, step.tool, "unexpected error", map[string]interface{}{
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

func checkChainResultIncludesView(tc *ToolCallView, step chainStep, stepIndex int) map[string]interface{} {
	if len(step.resultIncludes) == 0 {
		return nil
	}
	resultLower := strings.ToLower(tc.Result)
	for _, pattern := range step.resultIncludes {
		if !strings.Contains(resultLower, strings.ToLower(pattern)) {
			return chainStepFailure(stepIndex, step.tool,
				fmt.Sprintf("result missing pattern %q", pattern),
				map[string]interface{}{"missing_pattern": pattern})
		}
	}
	return nil
}

func checkChainResultMatchesView(tc *ToolCallView, step chainStep, stepIndex int) map[string]interface{} {
	if step.resultMatches == "" {
		return nil
	}
	re, err := regexp.Compile(step.resultMatches)
	if err != nil {
		return chainStepFailure(stepIndex, step.tool,
			fmt.Sprintf("invalid regex %q", step.resultMatches),
			map[string]interface{}{"error": err.Error()})
	}
	if !re.MatchString(tc.Result) {
		return chainStepFailure(stepIndex, step.tool,
			"result does not match pattern",
			map[string]interface{}{"pattern": step.resultMatches})
	}
	return nil
}

func checkChainArgsMatchView(tc *ToolCallView, step chainStep, stepIndex int) map[string]interface{} {
	for argName, pattern := range step.argsMatch {
		argVal, exists := tc.Args[argName]
		if !exists {
			return chainStepFailure(stepIndex, step.tool,
				fmt.Sprintf("missing argument %q", argName),
				map[string]interface{}{"argument": argName})
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return chainStepFailure(stepIndex, step.tool,
				fmt.Sprintf("invalid arg regex %q", pattern),
				map[string]interface{}{"error": err.Error()})
		}
		if !re.MatchString(asString(argVal)) {
			return chainStepFailure(stepIndex, step.tool,
				fmt.Sprintf("argument %q does not match pattern", argName),
				map[string]interface{}{"argument": argName, "pattern": pattern, "actual": argVal})
		}
	}
	return nil
}
