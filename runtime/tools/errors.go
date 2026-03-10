package tools

import (
	"errors"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Sentinel errors for tool operations.
var (
	// ErrToolNotFound is returned when a requested tool is not found in the registry.
	ErrToolNotFound = errors.New("tool not found")

	// ErrToolNameRequired is returned when registering a tool without a name.
	ErrToolNameRequired = errors.New("tool name is required")

	// ErrToolDescriptionRequired is returned when registering a tool without a description.
	ErrToolDescriptionRequired = errors.New("tool description is required")

	// ErrInputSchemaRequired is returned when registering a tool without an input schema.
	ErrInputSchemaRequired = errors.New("input schema is required")

	// ErrOutputSchemaRequired is returned when registering a tool without an output schema.
	ErrOutputSchemaRequired = errors.New("output schema is required")

	// ErrInvalidToolMode is returned when a tool has an invalid mode.
	ErrInvalidToolMode = errors.New("mode must be 'mock', 'live', 'local', 'mcp', 'client', or a registered executor name")

	// ErrMockExecutorOnly is returned when a non-mock tool is passed to a mock executor.
	ErrMockExecutorOnly = errors.New("executor can only execute mock tools")

	// ErrMCPExecutorOnly is returned when a non-mcp tool is passed to an MCP executor.
	ErrMCPExecutorOnly = errors.New("MCP executor can only execute mcp tools")

	// ErrToolTimeout is returned when a tool execution exceeds its configured timeout.
	ErrToolTimeout = errors.New("tool execution timed out")
)

// PendingToolExecution captures a single tool call that returned ToolStatusPending.
type PendingToolExecution struct {
	CallID      string                  `json:"call_id"`
	ToolName    string                  `json:"tool_name"`
	Args        map[string]any          `json:"args"`
	PendingInfo *PendingToolInfo        `json:"pending_info,omitempty"`
	ToolResult  types.MessageToolResult `json:"tool_result"`
}

// ErrToolsPending is returned by executeToolCalls when one or more tool calls
// returned ToolStatusPending. The pipeline should suspend: completed tool
// results are still returned alongside this error so they can be appended to
// the message history.
type ErrToolsPending struct {
	Pending []PendingToolExecution
}

func (e *ErrToolsPending) Error() string {
	names := make([]string, len(e.Pending))
	for i, p := range e.Pending {
		names[i] = p.ToolName
	}
	return fmt.Sprintf("tools pending: %s", strings.Join(names, ", "))
}

// IsErrToolsPending checks whether err is or wraps an *ErrToolsPending.
func IsErrToolsPending(err error) (*ErrToolsPending, bool) {
	var ep *ErrToolsPending
	if errors.As(err, &ep) {
		return ep, true
	}
	return nil, false
}
