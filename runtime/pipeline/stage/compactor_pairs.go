package stage

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// collapsePairsRule detects superseded assistant→tool pairs. When the same
// tool with the same arguments is called again later in the conversation,
// the earlier pair (assistant message requesting the call + tool result) is
// collapsed to a single-line summary.
type collapsePairsRule struct{}

// CollapsePairs returns a compaction rule that collapses superseded
// assistant→tool message pairs. A pair is superseded when the same tool
// name and arguments appear in a later tool result.
func CollapsePairs() CompactionRule { return &collapsePairsRule{} }

// Name implements CompactionRule.
func (r *collapsePairsRule) Name() string { return "collapse_pairs" }

// CanFold implements CompactionRule.
func (r *collapsePairsRule) CanFold(msg *types.Message, idx int, ctx *CompactionContext) bool {
	if msg.Role != roleTool || msg.ToolResult == nil {
		return false
	}
	if msg.ToolResult.Error != "" {
		return false
	}
	if strings.Contains(msg.GetContent(), compactedMarker) {
		return false
	}
	return isSuperseded(msg, idx, ctx)
}

// Fold implements CompactionRule.
func (r *collapsePairsRule) Fold(
	msg *types.Message, idx int, ctx *CompactionContext,
) (folded string, extraRemovals []int) {
	name := msg.ToolResult.Name
	if name == "" {
		name = "tool"
	}
	summary := fmt.Sprintf("[%s: superseded — %d bytes %s]",
		name, len(msg.GetContent()), compactedMarker)

	// Also collapse the preceding assistant message if it only contains
	// the tool call that produced this result.
	var extra []int
	if idx > 0 {
		prev := &ctx.Messages[idx-1]
		if prev.Role == roleAssistant && isMatchingAssistant(prev, msg) {
			extra = []int{idx - 1}
		}
	}
	return summary, extra
}

// isSuperseded returns true if a later message in the conversation has
// the same tool name and arguments.
func isSuperseded(msg *types.Message, idx int, ctx *CompactionContext) bool {
	key := toolResultKey(msg, ctx.Messages)
	if key == "" {
		return false
	}
	for j := idx + 1; j < len(ctx.Messages); j++ {
		other := &ctx.Messages[j]
		if other.Role != roleTool || other.ToolResult == nil {
			continue
		}
		if toolResultKey(other, ctx.Messages) == key {
			return true
		}
	}
	return false
}

// isMatchingAssistant returns true if the assistant message contains
// exactly one tool call that matches the tool result message.
func isMatchingAssistant(assistant, toolResult *types.Message) bool {
	if len(assistant.ToolCalls) != 1 {
		return false
	}
	tc := assistant.ToolCalls[0]
	return tc.Name == toolResult.ToolResult.Name &&
		tc.ID == toolResult.ToolResult.ID
}

// toolResultKey produces a canonical key "name:args" for a tool result
// by finding the assistant message that triggered it and extracting the args.
func toolResultKey(msg *types.Message, messages []types.Message) string {
	if msg.ToolResult == nil || msg.ToolResult.Name == "" {
		return ""
	}
	targetID := msg.ToolResult.ID
	name := msg.ToolResult.Name

	for i := range messages {
		m := &messages[i]
		if m.Role != roleAssistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID == targetID {
				return name + ":" + normalizeArgs(tc.Args)
			}
		}
	}
	// Fallback: key by name only (matches any call to the same tool)
	return name + ":"
}

// normalizeArgs produces a canonical string from tool call arguments
// for comparison. Re-marshals JSON objects for consistent key ordering.
func normalizeArgs(args json.RawMessage) string {
	if len(args) == 0 {
		return ""
	}
	var obj map[string]any
	if json.Unmarshal(args, &obj) == nil {
		canonical, err := json.Marshal(obj)
		if err == nil {
			return string(canonical)
		}
	}
	return string(args)
}
