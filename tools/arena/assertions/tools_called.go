package assertions

import (
	"github.com/AltairaLabs/PromptKit/runtime/types"
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

const roleAssistant = "assistant"

// ToolsCalledValidator checks that expected tools were called in the response
type ToolsCalledValidator struct {
	expectedTools []string
}

// NewToolsCalledValidator creates a new tools_called validator from params
func NewToolsCalledValidator(params map[string]interface{}) runtimeValidators.Validator {
	tools := extractStringSlice(params, "tools")
	return &ToolsCalledValidator{expectedTools: tools}
}

// Validate checks if expected tools were called
func (v *ToolsCalledValidator) Validate(content string, params map[string]interface{}) runtimeValidators.ValidationResult {
	// Extract tool calls from turn messages or legacy params
	var toolCalls []types.MessageToolCall

	// New approach: extract from _turn_messages
	if messages, ok := params["_turn_messages"].([]types.Message); ok {
		toolCalls = extractToolCallsFromTurnMessages(messages)
	} else {
		// Legacy approach: use pre-extracted tool calls (for backward compatibility)
		toolCalls = extractToolCalls(params)
	}

	// Build set of called tools
	calledTools := make(map[string]bool)
	for _, call := range toolCalls {
		calledTools[call.Name] = true
	}

	// Check which expected tools are missing
	var missing []string
	for _, expected := range v.expectedTools {
		if !calledTools[expected] {
			missing = append(missing, expected)
		}
	}

	return runtimeValidators.ValidationResult{
		Passed: len(missing) == 0,
		Details: map[string]interface{}{
			"missing_tools": missing,
		},
	}
}

// extractStringSlice safely extracts a string slice from params
func extractStringSlice(params map[string]interface{}, key string) []string {
	value, exists := params[key]
	if !exists {
		return []string{}
	}

	// Handle []string
	if strSlice, ok := value.([]string); ok {
		return strSlice
	}

	// Handle []interface{} with string elements
	if ifaceSlice, ok := value.([]interface{}); ok {
		result := make([]string, 0, len(ifaceSlice))
		for _, item := range ifaceSlice {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	}

	return []string{}
}

// extractToolCalls extracts tool calls from enhanced params
func extractToolCalls(params map[string]interface{}) []types.MessageToolCall {
	value, exists := params["_message_tool_calls"]
	if !exists {
		return []types.MessageToolCall{}
	}

	// Handle []types.MessageToolCall
	if toolCalls, ok := value.([]types.MessageToolCall); ok {
		return toolCalls
	}

	return []types.MessageToolCall{}
}

// extractToolCallsFromTurnMessages extracts tool calls from messages in the current turn.
// Uses the Source field to identify current turn messages (not loaded from StateStore).
func extractToolCallsFromTurnMessages(messages []types.Message) []types.MessageToolCall {
	if len(messages) == 0 {
		return []types.MessageToolCall{}
	}

	// Collect tool calls from assistant messages in current turn
	// Current turn messages have Source != "statestore"
	var allToolCalls []types.MessageToolCall
	for _, msg := range messages {
		// Only process messages from current turn (not loaded from history)
		if msg.Source == "statestore" {
			continue
		}

		// Collect tool calls from assistant messages
		if msg.Role == roleAssistant {
			allToolCalls = append(allToolCalls, msg.ToolCalls...)
		}
	}

	return allToolCalls
}
