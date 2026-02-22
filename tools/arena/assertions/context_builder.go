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

	idIndex := extractToolCallRecords(ctx, messages)
	matchToolResults(ctx, messages, idIndex)
	extractWorkflowMetadata(ctx, messages)
	aggregateCosts(ctx, messages)

	return ctx
}

// extractToolCallRecords populates ctx.ToolCalls from assistant-message tool
// invocations and returns a map from tool call ID to ToolCalls slice index.
func extractToolCallRecords(ctx *ConversationContext, messages []types.Message) map[string]int {
	idIndex := make(map[string]int)
	for idx := range messages {
		for _, tc := range messages[idx].ToolCalls {
			var args map[string]interface{}
			if len(tc.Args) > 0 {
				_ = json.Unmarshal(tc.Args, &args)
			}
			recIdx := len(ctx.ToolCalls)
			ctx.ToolCalls = append(ctx.ToolCalls, ToolCallRecord{
				TurnIndex: idx,
				ToolName:  tc.Name,
				Arguments: args,
			})
			if tc.ID != "" {
				idIndex[tc.ID] = recIdx
			}
		}
	}
	return idIndex
}

// matchToolResults matches tool-role messages back to their ToolCallRecords,
// populating Result, Error, and Duration.
func matchToolResults(ctx *ConversationContext, messages []types.Message, idIndex map[string]int) {
	for idx := range messages {
		msg := messages[idx]
		if msg.Role != "tool" || msg.ToolResult == nil {
			continue
		}

		// Try matching by tool call ID first (most reliable).
		if msg.ToolResult.ID != "" {
			if i, ok := idIndex[msg.ToolResult.ID]; ok {
				populateToolResult(&ctx.ToolCalls[i], msg.ToolResult)
				continue
			}
		}

		// Fall back to forward name matching (first unresolved record).
		for i := range ctx.ToolCalls {
			rec := &ctx.ToolCalls[i]
			if rec.ToolName == msg.ToolResult.Name && rec.Result == nil && rec.Error == "" {
				populateToolResult(rec, msg.ToolResult)
				break
			}
		}
	}
}

// populateToolResult fills a ToolCallRecord with data from a MessageToolResult.
func populateToolResult(rec *ToolCallRecord, result *types.MessageToolResult) {
	rec.Result = result.Content
	rec.Error = result.Error
	rec.Duration = time.Duration(result.LatencyMs) * time.Millisecond
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
