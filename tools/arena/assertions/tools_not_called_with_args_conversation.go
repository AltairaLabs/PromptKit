package assertions

import (
	"context"
	"fmt"
)

// ToolsNotCalledWithArgsConversationValidator ensures a given tool was never called
// with any of the forbidden argument values.
// Params:
// - tool_name: string
// - forbidden_args: map[string][]interface{} where key is arg name and value is list of forbidden values
// Type: "tools_not_called_with_args"
type ToolsNotCalledWithArgsConversationValidator struct{}

// Type returns the validator type name.
func (v *ToolsNotCalledWithArgsConversationValidator) Type() string {
	return "tools_not_called_with_args"
}

// NewToolsNotCalledWithArgsConversationValidator constructs validator instance.
func NewToolsNotCalledWithArgsConversationValidator() ConversationValidator {
	return &ToolsNotCalledWithArgsConversationValidator{}
}

// ValidateConversation checks all tool calls for forbidden argument values.
func (v *ToolsNotCalledWithArgsConversationValidator) ValidateConversation(
	ctx context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	toolName, _ := params["tool_name"].(string)
	fa, _ := params["forbidden_args"].(map[string]interface{})

	var violations []ConversationViolation

	for _, tc := range convCtx.ToolCalls {
		if toolName != "" && tc.ToolName != toolName {
			continue
		}
		for argName, forbiddenVals := range fa {
			actual, ok := tc.Arguments[argName]
			if !ok {
				continue
			}
			// normalize slice
			values := asInterfaceSlice(forbiddenVals)
			for _, fv := range values {
				if fmt.Sprintf("%v", actual) == fmt.Sprintf("%v", fv) {
					violations = append(violations, ConversationViolation{
						TurnIndex:   tc.TurnIndex,
						Description: fmt.Sprintf("%s called with %s=%v", tc.ToolName, argName, fv),
						Evidence: map[string]interface{}{
							"tool":     tc.ToolName,
							"argument": argName,
							"value":    actual,
							"args":     tc.Arguments,
						},
					})
				}
			}
		}
	}

	if len(violations) > 0 {
		return ConversationValidationResult{Passed: false, Message: "forbidden tool args detected", Violations: violations}
	}
	return ConversationValidationResult{Passed: true, Message: "no forbidden tool args"}
}

func asInterfaceSlice(v interface{}) []interface{} {
	switch x := v.(type) {
	case []interface{}:
		return x
	case []string:
		res := make([]interface{}, len(x))
		for i, s := range x {
			res[i] = s
		}
		return res
	default:
		return []interface{}{x}
	}
}
