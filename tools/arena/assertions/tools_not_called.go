package validators

import (
	"github.com/AltairaLabs/PromptKit/runtime/types"
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
	// Extract tool calls from turn messages or legacy params
	var toolCalls []types.MessageToolCall

	// New approach: extract from _turn_messages
	if messages, ok := params["_turn_messages"].([]types.Message); ok {
		toolCalls = extractToolCallsFromTurnMessages(messages)
	} else {
		// Legacy approach: use pre-extracted tool calls (for backward compatibility)
		toolCalls = extractToolCalls(params)
	}

	// Build set of forbidden tools for quick lookup
	forbidden := make(map[string]bool)
	for _, tool := range v.forbiddenTools {
		forbidden[tool] = true
	}

	// Check which forbidden tools were called
	var called []string
	calledSet := make(map[string]bool) // To avoid duplicates
	for _, call := range toolCalls {
		if forbidden[call.Name] && !calledSet[call.Name] {
			called = append(called, call.Name)
			calledSet[call.Name] = true
		}
	}

	return runtimeValidators.ValidationResult{
		OK: len(called) == 0,
		Details: map[string]interface{}{
			"forbidden_tools_called": called,
		},
	}
}
