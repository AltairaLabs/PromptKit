package assertions

import (
	"encoding/json"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// BuildConversationContextFromMessages constructs a ConversationContext from a
// sequence of messages and caller-supplied metadata. It extracts tool-call
// records from assistant messages, matches them with tool-role result messages,
// and aggregates cost/token information.
//
// Callers are responsible for populating Metadata.Extras with any
// engine-specific data (e.g. judge targets, prompt registries) before or after
// calling this function.
func BuildConversationContextFromMessages(
	messages []types.Message,
	metadata *ConversationMetadata,
) *ConversationContext {
	if metadata.Extras == nil {
		metadata.Extras = make(map[string]interface{})
	}

	ctx := &ConversationContext{
		AllTurns: messages,
		Metadata: *metadata,
	}

	entries := extractToolCallRecords(ctx, messages)
	matchToolResults(entries, messages)
	extractWorkflowMetadata(ctx, messages)
	aggregateCosts(ctx, messages)

	return ctx
}

// extractToolCallRecords populates ctx.ToolCalls from assistant-message tool
// invocations and returns toolCallEntry adapters for result matching.
func extractToolCallRecords(
	ctx *ConversationContext, messages []types.Message,
) []toolCallEntry {
	for idx := range messages {
		for _, tc := range messages[idx].ToolCalls {
			var args map[string]interface{}
			if len(tc.Args) > 0 {
				_ = json.Unmarshal(tc.Args, &args)
			}
			ctx.ToolCalls = append(ctx.ToolCalls, ToolCallRecord{
				TurnIndex: idx,
				ToolName:  tc.Name,
				Arguments: args,
			})
		}
	}

	// Build adapter slice that pairs each record with its original call ID.
	entries := make([]toolCallEntry, 0, len(ctx.ToolCalls))
	i := 0
	for idx := range messages {
		for _, tc := range messages[idx].ToolCalls {
			entries = append(entries, &toolCallRecordEntry{
				id:  tc.ID,
				rec: &ctx.ToolCalls[i],
			})
			i++
		}
	}
	return entries
}

// matchToolResults matches tool-role messages back to their ToolCallRecords,
// populating Result, Error, and Duration via the shared matchResult algorithm.
func matchToolResults(entries []toolCallEntry, messages []types.Message) {
	for idx := range messages {
		msg := messages[idx]
		if msg.Role != "tool" || msg.ToolResult == nil {
			continue
		}
		matchResult(entries, msg.ToolResult)
	}
}

// toolCallRecordEntry adapts a ToolCallRecord to the toolCallEntry interface
// so it can participate in the shared ID-first / name-fallback matching.
type toolCallRecordEntry struct {
	id  string          // original MessageToolCall.ID (not stored in ToolCallRecord)
	rec *ToolCallRecord // pointer into ctx.ToolCalls slice
}

func (e *toolCallRecordEntry) callID() string {
	return e.id
}

func (e *toolCallRecordEntry) callName() string {
	return e.rec.ToolName
}

func (e *toolCallRecordEntry) isResolved() bool {
	return e.rec.Result != nil || e.rec.Error != ""
}

func (e *toolCallRecordEntry) applyResult(result *types.MessageToolResult) {
	e.rec.Result = result.Content
	e.rec.Error = result.Error
	e.rec.Duration = time.Duration(result.LatencyMs) * time.Millisecond
}

// extractWorkflowMetadata copies workflow-related Meta fields into Extras.
func extractWorkflowMetadata(ctx *ConversationContext, messages []types.Message) {
	for idx := range messages {
		meta := messages[idx].Meta
		if meta == nil {
			continue
		}
		for _, key := range []string{"_workflow_state", "_workflow_transitions", "_workflow_complete"} {
			if v, ok := meta[key]; ok {
				ctx.Metadata.Extras[key] = v
			}
		}
	}
}

// aggregateCosts sums cost and token information across all messages.
func aggregateCosts(ctx *ConversationContext, messages []types.Message) {
	for idx := range messages {
		ci := messages[idx].CostInfo
		if ci == nil {
			continue
		}
		ctx.Metadata.TotalCost += ci.TotalCost
		ctx.Metadata.TotalTokens += ci.InputTokens + ci.OutputTokens
	}
}
