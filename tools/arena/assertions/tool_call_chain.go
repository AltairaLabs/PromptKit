package assertions

import (
	"fmt"

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

	views := toolCallViewsFromTrace(trace)
	completed, failure := coreToolCallChain(views, v.steps)

	if failure != nil {
		return runtimeValidators.ValidationResult{
			Passed:  false,
			Details: failure,
		}
	}

	if completed < len(v.steps) {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"message": fmt.Sprintf(
					"chain incomplete: satisfied %d/%d steps, missing %q",
					completed, len(v.steps), v.steps[completed].tool,
				),
				"completed_steps": completed,
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

// extractBoolParam extracts a boolean param from a map.
func extractBoolParam(params map[string]interface{}, key string) bool {
	val, ok := params[key].(bool)
	return ok && val
}
