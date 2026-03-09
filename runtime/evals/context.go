package evals

import (
	"encoding/json"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const roleAssistant = "assistant"

// BuildEvalContext constructs an EvalContext from a message history snapshot.
// It extracts the last assistant message as CurrentOutput, builds ToolCallRecords
// from assistant tool calls matched with their tool-role results, and pulls
// workflow metadata from message Meta fields.
//
// This is the canonical way to build an EvalContext outside of a live conversation.
// Both Arena (PackEvalHook) and the SDK (Evaluate) use this function.
func BuildEvalContext(
	messages []types.Message,
	turnIndex int,
	sessionID string,
	promptID string,
	metadata map[string]any,
) *EvalContext {
	var currentOutput string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == roleAssistant {
			currentOutput = messages[i].Content
			break
		}
	}

	return &EvalContext{
		Messages:      messages,
		TurnIndex:     turnIndex,
		CurrentOutput: currentOutput,
		ToolCalls:     ExtractToolCalls(messages),
		SessionID:     sessionID,
		PromptID:      promptID,
		Extras:        ExtractWorkflowExtras(messages),
		Metadata:      metadata,
	}
}

// ExtractToolCalls builds ToolCallRecords from a message history by matching
// assistant tool calls with their corresponding tool-role result messages.
func ExtractToolCalls(messages []types.Message) []ToolCallRecord {
	toolResults := buildToolResultMap(messages)

	var toolCalls []ToolCallRecord
	for i := range messages {
		if messages[i].Role != "assistant" {
			continue
		}
		for _, tc := range messages[i].ToolCalls {
			toolCalls = append(toolCalls, buildToolCallRecord(tc, i, toolResults))
		}
	}
	return toolCalls
}

// ExtractWorkflowExtras pulls workflow metadata from message Meta fields.
// Returns nil if no workflow metadata is found.
func ExtractWorkflowExtras(messages []types.Message) map[string]any {
	extras := make(map[string]any)
	for i := range messages {
		if messages[i].Meta == nil {
			continue
		}
		if state, ok := messages[i].Meta["_workflow_state"]; ok {
			extras["workflow_state"] = state
		}
		if transitions, ok := messages[i].Meta["_workflow_transitions"]; ok {
			extras["workflow_transitions"] = transitions
		}
		if complete, ok := messages[i].Meta["_workflow_complete"]; ok {
			extras["workflow_complete"] = complete
		}
	}
	if len(extras) == 0 {
		return nil
	}
	return extras
}

// buildToolResultMap creates a map of tool call ID → result message.
func buildToolResultMap(messages []types.Message) map[string]types.Message {
	toolResults := make(map[string]types.Message)
	for i := range messages {
		if messages[i].Role == "tool" && messages[i].ToolResult != nil {
			toolResults[messages[i].ToolResult.ID] = messages[i]
		}
	}
	return toolResults
}

// buildToolCallRecord creates a ToolCallRecord from a tool call and its result.
func buildToolCallRecord(
	tc types.MessageToolCall, turnIndex int, toolResults map[string]types.Message,
) ToolCallRecord {
	record := ToolCallRecord{
		TurnIndex: turnIndex,
		ToolName:  tc.Name,
	}
	if len(tc.Args) > 0 {
		record.Arguments = parseJSONArgs(tc.Args)
	}
	if resultMsg, ok := toolResults[tc.ID]; ok {
		if resultMsg.ToolResult != nil && len(resultMsg.ToolResult.Parts) > 0 {
			record.Result = resultMsg.ToolResult.Parts
		} else {
			record.Result = resultMsg.Content
		}
		if resultMsg.ToolResult != nil && resultMsg.ToolResult.Error != "" {
			record.Error = resultMsg.ToolResult.Error
		}
	}
	return record
}

// parseJSONArgs parses JSON bytes into a map, returning nil on failure.
func parseJSONArgs(data []byte) map[string]any {
	var args map[string]any
	if err := json.Unmarshal(data, &args); err != nil {
		return nil
	}
	return args
}
