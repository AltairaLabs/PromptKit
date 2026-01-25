package assertions

import (
	"context"
	"fmt"
	"regexp"
)

// ToolCallsWithArgsConversationValidator ensures all calls to a specific tool
// include required arguments with expected values.
// Params:
// - tool_name: string
// - required_args: map[string]interface{} expected values; if value is nil, only presence is required
// - args_match: map[string]string regex patterns to match against argument values
// Type: "tool_calls_with_args"
type ToolCallsWithArgsConversationValidator struct{}

// Type returns the validator type name.
func (v *ToolCallsWithArgsConversationValidator) Type() string { return "tool_calls_with_args" }

// NewToolCallsWithArgsConversationValidator constructs validator instance.
func NewToolCallsWithArgsConversationValidator() ConversationValidator {
	return &ToolCallsWithArgsConversationValidator{}
}

// ValidateConversation checks all calls for required args and values.
func (v *ToolCallsWithArgsConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	toolName, _ := params["tool_name"].(string)
	reqArgs, _ := params["required_args"].(map[string]interface{})
	argsMatch := extractArgsMatch(params["args_match"])

	if len(reqArgs) == 0 && len(argsMatch) == 0 {
		return ConversationValidationResult{Passed: true, Message: "no required args or patterns configured"}
	}

	var violations []ConversationViolation
	matchedTool := false
	for _, tc := range convCtx.ToolCalls {
		if toolName != "" && tc.ToolName != toolName {
			continue
		}
		matchedTool = true
		// validate the call against required args (exact match)
		if len(reqArgs) > 0 {
			violations = append(violations, validateRequiredArgs(tc, reqArgs)...)
		}
		// validate the call against regex patterns
		if len(argsMatch) > 0 {
			violations = append(violations, validateArgsMatch(tc, argsMatch)...)
		}
	}

	if !matchedTool && toolName != "" {
		return ConversationValidationResult{
			Passed:  false,
			Message: fmt.Sprintf("tool '%s' was not called", toolName),
		}
	}

	if len(violations) > 0 {
		return ConversationValidationResult{
			Passed:     false,
			Message:    "tool argument violations",
			Violations: violations,
		}
	}
	return ConversationValidationResult{Passed: true, Message: "all tool calls had valid args"}
}

// extractArgsMatch converts the args_match parameter to a map of patterns
func extractArgsMatch(v interface{}) map[string]string {
	if v == nil {
		return nil
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, val := range m {
		if s, ok := val.(string); ok {
			result[k] = s
		}
	}
	return result
}

func asString(v interface{}) string { return fmt.Sprintf("%v", v) }

// validateArgsMatch returns violations for arguments that don't match regex patterns.
func validateArgsMatch(tc ToolCallRecord, patterns map[string]string) []ConversationViolation {
	var vios []ConversationViolation
	for arg, pattern := range patterns {
		actual, ok := tc.Arguments[arg]
		if !ok {
			vios = append(vios, ConversationViolation{
				TurnIndex:   tc.TurnIndex,
				Description: "missing argument for pattern match",
				Evidence: map[string]interface{}{
					"tool":     tc.ToolName,
					"argument": arg,
					"pattern":  pattern,
					"args":     tc.Arguments,
				},
			})
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			vios = append(vios, ConversationViolation{
				TurnIndex:   tc.TurnIndex,
				Description: "invalid regex pattern",
				Evidence: map[string]interface{}{
					"tool":     tc.ToolName,
					"argument": arg,
					"pattern":  pattern,
					"error":    err.Error(),
				},
			})
			continue
		}
		if !re.MatchString(asString(actual)) {
			vios = append(vios, ConversationViolation{
				TurnIndex:   tc.TurnIndex,
				Description: "argument value does not match pattern",
				Evidence: map[string]interface{}{
					"tool":     tc.ToolName,
					"argument": arg,
					"pattern":  pattern,
					"actual":   actual,
				},
			})
		}
	}
	return vios
}

// validateRequiredArgs returns violations for a single tool call given required args.
func validateRequiredArgs(tc ToolCallRecord, reqArgs map[string]interface{}) []ConversationViolation {
	var vios []ConversationViolation
	for arg, expected := range reqArgs {
		actual, ok := tc.Arguments[arg]
		if !ok {
			vios = append(vios, ConversationViolation{
				TurnIndex:   tc.TurnIndex,
				Description: "missing required argument",
				Evidence: map[string]interface{}{
					"tool":     tc.ToolName,
					"argument": arg,
					"args":     tc.Arguments,
				},
			})
			continue
		}
		if expected == nil { // only presence required
			continue
		}
		if asString(actual) != asString(expected) {
			vios = append(vios, ConversationViolation{
				TurnIndex:   tc.TurnIndex,
				Description: "argument value mismatch",
				Evidence: map[string]interface{}{
					"tool":     tc.ToolName,
					"argument": arg,
					"expected": expected,
					"actual":   actual,
				},
			})
		}
	}
	return vios
}
