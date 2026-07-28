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
// Both Arena (EvalOrchestrator) and the SDK (Evaluate) use this function.
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
		Extras:        workflowExtras(messages, metadata),
		Metadata:      metadata,
		PriorResults:  validationsToPriorResults(messages),
	}
}

// BuildGuardrailEvalContext constructs an EvalContext for a guardrail, which
// judges one specific message rather than scanning a transcript.
//
// Two things make this different from BuildEvalContext, and both are
// deliberate:
//
// It does NOT infer CurrentOutput from the last assistant turn. An input
// guardrail judges the *user's* message, so deriving the content under test
// from assistant messages makes a content check silently pass everything —
// that was #1679. The caller states the content, and ContentScope is pinned to
// ContentScopeCurrent to match.
//
// Everything else is derived from the same message history BuildEvalContext
// uses. factory.go promises that any registered eval handler can serve as a
// guardrail, so a handler must see the same ToolCalls, Extras, Metadata and
// PriorResults either way. Building this context inline with only three fields
// set left ~30 ToolCalls-reading handlers blind, made cost_budget compute zero
// spend and never fire, and made state_is report "no workflow state" — which
// scores 0, and a score below 1.0 means enforce, so it blocked every turn
// (#1704).
//
// TurnIndex, SessionID, PromptID and Variables are still absent: unlike the
// rest they cannot be derived from the message history, and the hook boundary
// does not carry them. Handlers depending on those remain unsuitable as
// guardrails.
func BuildGuardrailEvalContext(
	messages []types.Message, currentOutput string, metadata map[string]any,
) *EvalContext {
	return &EvalContext{
		Messages:      messages,
		CurrentOutput: currentOutput,
		ContentScope:  ContentScopeCurrent,
		ToolCalls:     ExtractToolCalls(messages),
		Extras:        workflowExtras(messages, metadata),
		Metadata:      metadata,
		PriorResults:  validationsToPriorResults(messages),
	}
}

// workflowMetadataKeys are the workflow fields an engine sets on the metadata
// map for workflow scenarios, rather than on message Meta.
var workflowMetadataKeys = []string{
	"workflow_current_state",
	"workflow_transitions",
	"workflow_complete",
}

// workflowExtras merges workflow state found in the message history with the
// workflow keys an engine may have placed on the metadata map. Shared by
// BuildEvalContext and BuildGuardrailEvalContext so the eval and guardrail
// paths cannot drift on what a workflow handler gets to see.
//
// Indexing a nil metadata map is safe, so no nil check is needed.
func workflowExtras(messages []types.Message, metadata map[string]any) map[string]any {
	extras := ExtractWorkflowExtras(messages)
	for _, key := range workflowMetadataKeys {
		v, ok := metadata[key]
		if !ok {
			continue
		}
		if extras == nil {
			extras = make(map[string]any)
		}
		extras[key] = v
	}
	return extras
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
		if state, ok := messages[i].Meta["_workflow_current_state"]; ok {
			extras["workflow_current_state"] = state
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
			record.ErrorType = resultMsg.ToolResult.ErrorType
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

// validationsToPriorResults converts ValidationResult entries from the last
// assistant message into EvalResult entries. This seeds PriorResults so that
// evals like guardrail_triggered can inspect pipeline-level guardrail outcomes
// without coupling to message.Validations directly.
func validationsToPriorResults(messages []types.Message) []EvalResult {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != roleAssistant {
			continue
		}
		if len(messages[i].Validations) == 0 {
			return nil
		}
		results := make([]EvalResult, len(messages[i].Validations))
		for j, v := range messages[i].Validations {
			score := 1.0
			if !v.Passed {
				score = 0.0
			}
			results[j] = EvalResult{
				Type:  v.ValidatorType,
				Score: &score,
			}
		}
		return results
	}
	return nil
}
