package assertions

import (
	"github.com/AltairaLabs/PromptKit/runtime/types"
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

const (
	roleAssistant    = "assistant"
	sourceStatestore = "statestore"
)

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
	toolCalls := resolveToolCalls(params)
	missing := findMissing(toolCalls, v.expectedTools)

	return runtimeValidators.ValidationResult{
		Passed: len(missing) == 0,
		Details: map[string]interface{}{
			"missing_tools": missing,
		},
	}
}

// resolveToolCalls extracts tool calls from params, preferring _turn_messages
// over the legacy _message_tool_calls approach.
func resolveToolCalls(params map[string]interface{}) []types.MessageToolCall {
	if messages, ok := params["_turn_messages"].([]types.Message); ok {
		return extractToolCallsFromTurnMessages(messages)
	}
	return extractToolCalls(params)
}

// findMissing returns which of the expected names are not present in toolCalls.
func findMissing(toolCalls []types.MessageToolCall, expected []string) []string {
	calledTools := make(map[string]bool, len(toolCalls))
	for _, call := range toolCalls {
		calledTools[call.Name] = true
	}

	var missing []string
	for _, name := range expected {
		if !calledTools[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

// findForbiddenCalled returns which of the forbidden names appear in toolCalls (deduplicated).
func findForbiddenCalled(toolCalls []types.MessageToolCall, forbidden []string) []string {
	forbiddenSet := make(map[string]bool, len(forbidden))
	for _, name := range forbidden {
		forbiddenSet[name] = true
	}

	var called []string
	seen := make(map[string]bool)
	for _, call := range toolCalls {
		if forbiddenSet[call.Name] && !seen[call.Name] {
			called = append(called, call.Name)
			seen[call.Name] = true
		}
	}
	return called
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
		if msg.Source == sourceStatestore {
			continue
		}

		// Collect tool calls from assistant messages
		if msg.Role == roleAssistant {
			allToolCalls = append(allToolCalls, msg.ToolCalls...)
		}
	}

	return allToolCalls
}
