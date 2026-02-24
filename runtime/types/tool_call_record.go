package types //nolint:revive // package name matches existing convention

import "time"

// ToolCallRecord captures a single tool invocation for eval/assertion context.
// Shared between runtime/evals and tools/arena/assertions.
type ToolCallRecord struct {
	TurnIndex int            `json:"turn_index"`
	ToolName  string         `json:"tool_name"`
	Arguments map[string]any `json:"arguments"`
	Result    any            `json:"result,omitempty"`
	Error     string         `json:"error,omitempty"`
	Duration  time.Duration  `json:"duration,omitempty"`
}
