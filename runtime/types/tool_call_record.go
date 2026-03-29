package types //nolint:revive // package name matches existing convention

import "time"

// ToolErrorType classifies why a tool call failed.
type ToolErrorType string

const (
	// ToolErrorNone indicates a successful tool call (zero value).
	ToolErrorNone ToolErrorType = ""
	// ToolErrorValidation indicates the LLM sent invalid arguments (schema mismatch, null args).
	ToolErrorValidation ToolErrorType = "validation"
	// ToolErrorExecution indicates the tool ran but returned an error.
	ToolErrorExecution ToolErrorType = "execution"
	// ToolErrorApproval indicates a user or policy rejected the tool call.
	ToolErrorApproval ToolErrorType = "approval"
)

// ToolCallRecord captures a single tool invocation for eval/assertion context.
// Shared between runtime/evals and tools/arena/assertions.
type ToolCallRecord struct {
	TurnIndex int            `json:"turn_index"`
	ToolName  string         `json:"tool_name"`
	Arguments map[string]any `json:"arguments"`
	Result    any            `json:"result,omitempty"`
	Error     string         `json:"error,omitempty"`
	ErrorType ToolErrorType  `json:"error_type,omitempty"`
	Duration  time.Duration  `json:"duration,omitempty"`
}
