package assertions

import (
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// ToolsNotCalledValidator checks that forbidden tools were NOT called in the response
type ToolsNotCalledValidator struct {
	forbiddenTools []string
}

// NewToolsNotCalledValidator creates a new tools_not_called validator from params
func NewToolsNotCalledValidator(params map[string]interface{}) runtimeValidators.Validator {
	tools := extractStringSlice(params, "tools")
	return &ToolsNotCalledValidator{forbiddenTools: tools}
}

// Validate checks if any forbidden tools were called
func (v *ToolsNotCalledValidator) Validate(content string, params map[string]interface{}) runtimeValidators.ValidationResult {
	toolCalls := resolveToolCalls(params)
	called := findForbiddenCalled(toolCalls, v.forbiddenTools)

	return runtimeValidators.ValidationResult{
		Passed: len(called) == 0,
		Details: map[string]interface{}{
			"forbidden_tools_called": called,
		},
	}
}
