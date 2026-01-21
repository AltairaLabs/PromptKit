package tools

import "errors"

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
	ErrInvalidToolMode = errors.New("mode must be 'mock', 'live', 'mcp', or a registered executor name")

	// ErrMockExecutorOnly is returned when a non-mock tool is passed to a mock executor.
	ErrMockExecutorOnly = errors.New("executor can only execute mock tools")

	// ErrMCPExecutorOnly is returned when a non-mcp tool is passed to an MCP executor.
	ErrMCPExecutorOnly = errors.New("MCP executor can only execute mcp tools")
)
