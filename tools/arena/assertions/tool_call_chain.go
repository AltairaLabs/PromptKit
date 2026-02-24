package assertions

import (
	"fmt"
	"regexp"
	"strings"

	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// ToolCallChainValidator asserts a dependency chain of tool calls with per-step constraints.
type ToolCallChainValidator struct {
	steps []chainStep
}

type chainStep struct {
	tool           string
	resultIncludes []string
	resultMatches  string
	argsMatch      map[string]string
	noError        bool
}

// NewToolCallChainValidator creates a new tool_call_chain validator from params.
func NewToolCallChainValidator(params map[string]interface{}) runtimeValidators.Validator {
	stepsRaw, _ := params["steps"].([]interface{})
	steps := make([]chainStep, 0, len(stepsRaw))

	for _, raw := range stepsRaw {
		stepMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		step := chainStep{
			noError: extractBoolParam(stepMap, "no_error"),
		}
		step.tool, _ = stepMap["tool"].(string)
		step.resultIncludes = extractStringSlice(stepMap, "result_includes")
		step.resultMatches, _ = stepMap["result_matches"].(string)
		step.argsMatch = extractMapStringString(stepMap, "args_match")
		steps = append(steps, step)
	}

	return &ToolCallChainValidator{steps: steps}
}

// Validate checks that the chain of tool calls satisfies all step constraints in order.
func (v *ToolCallChainValidator) Validate(
	content string, params map[string]interface{},
) runtimeValidators.ValidationResult {
	if len(v.steps) == 0 {
		return runtimeValidators.ValidationResult{
			Passed: true,
			Details: map[string]interface{}{
				"message": "empty chain always passes",
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

	stepCursor := 0
	for _, tc := range trace {
		if stepCursor >= len(v.steps) {
			break
		}
		step := v.steps[stepCursor]
		if tc.Name != step.tool {
			continue
		}

		// Validate step constraints
		if violation := validateChainStep(&tc, step, stepCursor); violation != nil {
			return runtimeValidators.ValidationResult{
				Passed:  false,
				Details: violation,
			}
		}
		stepCursor++
	}

	if stepCursor < len(v.steps) {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"message": fmt.Sprintf(
					"chain incomplete: satisfied %d/%d steps, missing %q",
					stepCursor, len(v.steps), v.steps[stepCursor].tool,
				),
				"completed_steps": stepCursor,
				"total_steps":     len(v.steps),
			},
		}
	}

	return runtimeValidators.ValidationResult{
		Passed: true,
		Details: map[string]interface{}{
			"message":         "chain fully satisfied",
			"completed_steps": len(v.steps),
		},
	}
}

// validateChainStep checks a single step's constraints against a tool call.
// Returns nil if all constraints pass, or a details map describing the failure.
func validateChainStep(tc *TurnToolCall, step chainStep, stepIndex int) map[string]interface{} {
	if step.noError && tc.Error != "" {
		return chainStepFailure(stepIndex, step.tool, "unexpected error", map[string]interface{}{
			"error": tc.Error,
		})
	}

	if f := checkChainResultIncludes(tc, step, stepIndex); f != nil {
		return f
	}

	if f := checkChainResultMatches(tc, step, stepIndex); f != nil {
		return f
	}

	return checkChainArgsMatch(tc, step, stepIndex)
}

// chainStepFailure builds a standardized failure map for a chain step.
func chainStepFailure(
	stepIndex int, tool, reason string, extra map[string]interface{},
) map[string]interface{} {
	result := map[string]interface{}{
		"message":    fmt.Sprintf("step %d (%s): %s", stepIndex, tool, reason),
		"step_index": stepIndex,
		"tool":       tool,
	}
	for k, v := range extra {
		result[k] = v
	}
	return result
}

func checkChainResultIncludes(tc *TurnToolCall, step chainStep, stepIndex int) map[string]interface{} {
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

func checkChainResultMatches(tc *TurnToolCall, step chainStep, stepIndex int) map[string]interface{} {
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

func checkChainArgsMatch(tc *TurnToolCall, step chainStep, stepIndex int) map[string]interface{} {
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

// extractBoolParam extracts a boolean param from a map.
func extractBoolParam(params map[string]interface{}, key string) bool {
	val, ok := params[key].(bool)
	return ok && val
}
