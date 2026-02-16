package assertions

import (
	"encoding/json"
	"regexp"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// ToolCallsWithArgsValidator checks that a tool was called with expected arguments.
// Supports both exact value matching (expected_args) and regex pattern matching (args_match).
type ToolCallsWithArgsValidator struct {
	toolName     string
	expectedArgs map[string]interface{}
	argsMatch    map[string]string
}

// NewToolCallsWithArgsValidator creates a new tool_calls_with_args validator from params.
// Params:
// - tool_name: string - name of the tool to check
// - expected_args: map[string]interface{} - exact values to match (nil means presence-only)
// - args_match: map[string]string - regex patterns to match against argument values
func NewToolCallsWithArgsValidator(params map[string]interface{}) runtimeValidators.Validator {
	toolName, _ := params["tool_name"].(string)
	expectedArgs := extractMapStringInterface(params, "expected_args")
	argsMatch := extractMapStringString(params, "args_match")

	return &ToolCallsWithArgsValidator{
		toolName:     toolName,
		expectedArgs: expectedArgs,
		argsMatch:    argsMatch,
	}
}

// Validate checks if the tool was called with expected arguments.
func (v *ToolCallsWithArgsValidator) Validate(
	content string, params map[string]interface{},
) runtimeValidators.ValidationResult {
	// Extract tool calls from turn messages
	toolCalls := resolveToolCalls(params)

	// Find matching tool calls
	var matchingCalls []types.MessageToolCall
	for _, call := range toolCalls {
		if v.toolName == "" || call.Name == v.toolName {
			matchingCalls = append(matchingCalls, call)
		}
	}

	// If tool_name specified but not found
	if v.toolName != "" && len(matchingCalls) == 0 {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error":     "tool_not_called",
				"tool_name": v.toolName,
			},
		}
	}

	// If no requirements, just check tool was called
	if len(v.expectedArgs) == 0 && len(v.argsMatch) == 0 {
		return runtimeValidators.ValidationResult{
			Passed: true,
			Details: map[string]interface{}{
				"message": "no argument requirements configured",
			},
		}
	}

	// Validate arguments for all matching calls
	var violations []map[string]interface{}
	for _, call := range matchingCalls {
		callViolations := v.validateCall(call)
		violations = append(violations, callViolations...)
	}

	return runtimeValidators.ValidationResult{
		Passed: len(violations) == 0,
		Details: map[string]interface{}{
			"violations":     violations,
			"matching_calls": len(matchingCalls),
		},
	}
}

// validateCall validates a single tool call against requirements.
func (v *ToolCallsWithArgsValidator) validateCall(call types.MessageToolCall) []map[string]interface{} {
	args, parseErr := parseToolArgs(call)
	if parseErr != nil {
		return []map[string]interface{}{parseErr}
	}

	var violations []map[string]interface{}
	violations = append(violations, v.validateExactArgs(call.Name, args)...)
	violations = append(violations, v.validatePatternArgs(call.Name, args)...)
	return violations
}

// parseToolArgs parses JSON args from a tool call, returns violation map on error.
func parseToolArgs(call types.MessageToolCall) (args, violation map[string]interface{}) {
	if len(call.Args) == 0 {
		return make(map[string]interface{}), nil
	}
	if err := json.Unmarshal(call.Args, &args); err != nil {
		return nil, map[string]interface{}{
			"type":  "invalid_args_json",
			"tool":  call.Name,
			"error": err.Error(),
		}
	}
	if args == nil {
		args = make(map[string]interface{})
	}
	return args, nil
}

// validateExactArgs validates exact argument matches.
func (v *ToolCallsWithArgsValidator) validateExactArgs(
	toolName string, args map[string]interface{},
) []map[string]interface{} {
	var violations []map[string]interface{}
	for argName, expectedValue := range v.expectedArgs {
		actualValue, exists := args[argName]
		if !exists {
			violations = append(violations, map[string]interface{}{
				"type": "missing_argument", "tool": toolName, "argument": argName,
			})
			continue
		}
		if expectedValue != nil && asString(actualValue) != asString(expectedValue) {
			violations = append(violations, map[string]interface{}{
				"type": "value_mismatch", "tool": toolName, "argument": argName,
				"expected": expectedValue, "actual": actualValue,
			})
		}
	}
	return violations
}

// validatePatternArgs validates regex pattern matches on arguments.
func (v *ToolCallsWithArgsValidator) validatePatternArgs(
	toolName string, args map[string]interface{},
) []map[string]interface{} {
	var violations []map[string]interface{}
	for argName, pattern := range v.argsMatch {
		actualValue, exists := args[argName]
		if !exists {
			violations = append(violations, map[string]interface{}{
				"type": "missing_argument_for_pattern", "tool": toolName,
				"argument": argName, "pattern": pattern,
			})
			continue
		}
		if vio := matchPattern(toolName, argName, pattern, actualValue); vio != nil {
			violations = append(violations, vio)
		}
	}
	return violations
}

// matchPattern checks if a value matches a regex pattern, returns violation on failure.
func matchPattern(toolName, argName, pattern string, value interface{}) map[string]interface{} {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return map[string]interface{}{
			"type": "invalid_pattern", "tool": toolName, "argument": argName,
			"pattern": pattern, "error": err.Error(),
		}
	}
	if !re.MatchString(asString(value)) {
		return map[string]interface{}{
			"type": "pattern_mismatch", "tool": toolName, "argument": argName,
			"pattern": pattern, "actual": value,
		}
	}
	return nil
}

// extractMapStringInterface extracts a map[string]interface{} from params.
func extractMapStringInterface(params map[string]interface{}, key string) map[string]interface{} {
	value, exists := params[key]
	if !exists {
		return nil
	}

	if m, ok := value.(map[string]interface{}); ok {
		return m
	}

	return nil
}

// extractMapStringString extracts a map[string]string from params.
func extractMapStringString(params map[string]interface{}, key string) map[string]string {
	value, exists := params[key]
	if !exists {
		return nil
	}

	if m, ok := value.(map[string]interface{}); ok {
		result := make(map[string]string, len(m))
		for k, v := range m {
			if s, ok := v.(string); ok {
				result[k] = s
			}
		}
		return result
	}

	if m, ok := value.(map[string]string); ok {
		return m
	}

	return nil
}
