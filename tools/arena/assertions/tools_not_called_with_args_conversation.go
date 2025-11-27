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
	forbiddenMap := buildForbiddenMap(params["forbidden_args"]) // arg -> set of forbidden values (as strings)

	if len(forbiddenMap) == 0 {
		return ConversationValidationResult{Passed: true, Message: "no forbidden tool args (none configured)"}
	}

	var violations []ConversationViolation

	for _, tc := range convCtx.ToolCalls {
		if toolName != "" && tc.ToolName != toolName {
			continue
		}

		// Check only arguments that are forbidden for faster exit
		for argName, actual := range tc.Arguments {
			if forbiddenSet, found := forbiddenMap[argName]; found {
				if isForbiddenValue(actual, forbiddenSet) {
					violations = append(violations, ConversationViolation{
						TurnIndex:   tc.TurnIndex,
						Description: fmt.Sprintf("%s called with %s=%v", tc.ToolName, argName, actual),
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

// buildForbiddenMap converts the input parameter into a map of argument name -> set of forbidden values (stringified).
func buildForbiddenMap(v interface{}) map[string]map[string]struct{} {
	res := make(map[string]map[string]struct{})
	m, ok := v.(map[string]interface{})
	if !ok {
		return res
	}
	for arg, rawVals := range m {
		set := make(map[string]struct{})
		for _, iv := range asInterfaceSlice(rawVals) {
			set[fmt.Sprintf("%v", iv)] = struct{}{}
		}
		res[arg] = set
	}
	return res
}

// isForbiddenValue checks if actual matches any value in the provided set.
func isForbiddenValue(actual interface{}, set map[string]struct{}) bool {
	_, found := set[fmt.Sprintf("%v", actual)]
	return found
}
