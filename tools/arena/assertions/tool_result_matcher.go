package assertions

import "github.com/AltairaLabs/PromptKit/runtime/types"

// toolCallEntry abstracts a tool call record so the ID-first / name-fallback
// matching algorithm can be shared between context_builder.go (ToolCallRecord)
// and turn_tool_trace.go (TurnToolCall).
type toolCallEntry interface {
	callID() string
	callName() string
	isResolved() bool
	applyResult(result *types.MessageToolResult)
}

// matchResult pairs a MessageToolResult with the first matching unresolved
// toolCallEntry using ID-first, then name-fallback matching.
func matchResult(entries []toolCallEntry, result *types.MessageToolResult) {
	// Try matching by ID first (most reliable).
	if result.ID != "" {
		for i := range entries {
			if entries[i].callID() == result.ID && !entries[i].isResolved() {
				entries[i].applyResult(result)
				return
			}
		}
	}

	// Fall back to forward name matching (first unresolved record).
	for i := range entries {
		if entries[i].callName() == result.Name && !entries[i].isResolved() {
			entries[i].applyResult(result)
			return
		}
	}
}
