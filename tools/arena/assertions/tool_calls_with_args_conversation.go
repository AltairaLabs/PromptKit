package assertions

import (
	"context"
	"fmt"
)

// ToolCallsWithArgsConversationValidator ensures all calls to a specific tool
// include required arguments with expected values.
// Params:
// - tool_name: string
// - required_args: map[string]interface{} expected values; if value is nil, only presence is required
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

	if len(reqArgs) == 0 {
		return ConversationValidationResult{Passed: true, Message: "no required args configured"}
	}

	var violations []ConversationViolation
	for _, tc := range convCtx.ToolCalls {
		if toolName != "" && tc.ToolName != toolName {
			continue
		}
		// validate the call against required args
		violations = append(violations, validateRequiredArgs(tc, reqArgs)...)
	}

	if len(violations) > 0 {
		return ConversationValidationResult{
			Passed:     false,
			Message:    "required tool argument violations",
			Violations: violations,
		}
	}
	return ConversationValidationResult{Passed: true, Message: "all tool calls had required args"}
}

func asString(v interface{}) string { return fmt.Sprintf("%v", v) }

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
