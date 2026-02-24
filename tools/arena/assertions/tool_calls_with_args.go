package assertions

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// ToolCallsWithArgsValidator checks that a tool was called with expected arguments.
// Supports both exact value matching (expected_args) and regex pattern matching (args_match).
// Optionally validates tool results when _turn_messages are available.
type ToolCallsWithArgsValidator struct {
	toolName       string
	expectedArgs   map[string]interface{}
	argsMatch      map[string]string
	resultIncludes []string // substrings that must appear in the tool result
	resultMatches  string   // regex pattern that the tool result must match
	noError        bool     // if true, the tool must not have returned an error
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
	resultMatches, _ := params["result_matches"].(string)

	return &ToolCallsWithArgsValidator{
		toolName:       toolName,
		expectedArgs:   expectedArgs,
		argsMatch:      argsMatch,
		resultIncludes: extractStringSlice(params, "result_includes"),
		resultMatches:  resultMatches,
		noError:        extractBoolParam(params, "no_error"),
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

	// If no requirements at all, just check tool was called
	if len(v.expectedArgs) == 0 && len(v.argsMatch) == 0 && !v.hasResultConstraints() {
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

	// Validate result constraints if configured and turn trace is available
	if v.hasResultConstraints() {
		resultViolations, skipped := v.validateResults(params)
		if skipped {
			return runtimeValidators.ValidationResult{
				Passed: len(violations) == 0,
				Details: map[string]interface{}{
					"violations":           violations,
					"matching_calls":       len(matchingCalls),
					"result_check_skipped": true,
				},
			}
		}
		violations = append(violations, resultViolations...)
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

// hasResultConstraints returns true if any result-level constraints are configured.
func (v *ToolCallsWithArgsValidator) hasResultConstraints() bool {
	return len(v.resultIncludes) > 0 || v.resultMatches != "" || v.noError
}

// validateResults checks result-level constraints using the turn tool trace.
// Returns violations and a bool indicating whether the check was skipped (duplex path).
func (v *ToolCallsWithArgsValidator) validateResults(
	params map[string]interface{},
) ([]map[string]interface{}, bool) {
	trace, ok := resolveTurnToolTrace(params)
	if !ok {
		return nil, true
	}

	var violations []map[string]interface{}
	for i := range trace {
		if v.toolName != "" && trace[i].Name != v.toolName {
			continue
		}
		violations = append(violations, v.checkResultConstraints(&trace[i])...)
	}

	return violations, false
}

// checkResultConstraints checks all result-level constraints on a single tool call.
func (v *ToolCallsWithArgsValidator) checkResultConstraints(
	tc *TurnToolCall,
) []map[string]interface{} {
	var violations []map[string]interface{}

	if v.noError && tc.Error != "" {
		violations = append(violations, map[string]interface{}{
			"type": "tool_error", "tool": tc.Name, "error": tc.Error,
		})
	}

	violations = append(violations, checkResultIncludes(tc.Name, tc.Result, v.resultIncludes)...)
	violations = append(violations, checkResultMatchesPattern(tc.Name, tc.Result, v.resultMatches)...)

	return violations
}

// checkResultIncludes checks that a result contains all expected substrings.
func checkResultIncludes(toolName, result string, patterns []string) []map[string]interface{} {
	if len(patterns) == 0 {
		return nil
	}
	var violations []map[string]interface{}
	resultLower := strings.ToLower(result)
	for _, pattern := range patterns {
		if !strings.Contains(resultLower, strings.ToLower(pattern)) {
			violations = append(violations, map[string]interface{}{
				"type": "result_missing_pattern", "tool": toolName, "pattern": pattern,
			})
		}
	}
	return violations
}

// checkResultMatchesPattern checks that a result matches a regex pattern.
func checkResultMatchesPattern(toolName, result, pattern string) []map[string]interface{} {
	if pattern == "" {
		return nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return []map[string]interface{}{{
			"type": "invalid_result_pattern", "tool": toolName,
			"pattern": pattern, "error": err.Error(),
		}}
	}
	if !re.MatchString(result) {
		return []map[string]interface{}{{
			"type": "result_pattern_mismatch", "tool": toolName, "pattern": pattern,
		}}
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
