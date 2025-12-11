package sdk

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// localExecutor is a tool executor for locally-handled tools (Mode: "local").
// It dispatches to the appropriate ToolHandler based on the tool name.
type localExecutor struct {
	handlers map[string]ToolHandler
}

// Name returns "local" to match the Mode on tools in the pack.
func (e *localExecutor) Name() string {
	return "local"
}

// Execute dispatches to the appropriate handler based on tool name.
func (e *localExecutor) Execute(descriptor *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	handler, ok := e.handlers[descriptor.Name]
	if !ok {
		return nil, fmt.Errorf("no handler registered for tool: %s", descriptor.Name)
	}

	// Parse args to map
	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	// Call handler
	result, err := handler(argsMap)
	if err != nil {
		return nil, err
	}

	// Serialize result
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize tool result: %w", err)
	}

	return resultJSON, nil
}

// handlerAdapter adapts an SDK ToolHandler to the runtime's tools.Executor interface.
type handlerAdapter struct {
	name    string
	handler ToolHandler
}

// Name returns the tool name.
func (a *handlerAdapter) Name() string {
	return a.name
}

// Execute runs the handler with the given arguments.
func (a *handlerAdapter) Execute(descriptor *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	// Parse args to map
	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	// Call handler
	result, err := a.handler(argsMap)
	if err != nil {
		return nil, err
	}

	// Serialize result
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize tool result: %w", err)
	}

	return resultJSON, nil
}

// mcpHandlerAdapter adapts MCP tool calls to the runtime's tools.Executor interface.
type mcpHandlerAdapter struct {
	name     string
	registry mcp.Registry
}

// Name returns the tool name.
func (a *mcpHandlerAdapter) Name() string {
	return a.name
}

// Execute runs the MCP tool with the given arguments.
func (a *mcpHandlerAdapter) Execute(descriptor *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	ctx := context.Background()

	// Get client for this tool
	client, err := a.registry.GetClientForTool(ctx, a.name)
	if err != nil {
		return nil, fmt.Errorf("failed to get MCP client for tool %q: %w", a.name, err)
	}

	// Call the tool
	resp, err := client.CallTool(ctx, a.name, args)
	if err != nil {
		return nil, fmt.Errorf("MCP tool call failed: %w", err)
	}

	// Check for tool error
	if resp.IsError {
		errMsg := "MCP tool returned error"
		if len(resp.Content) > 0 && resp.Content[0].Text != "" {
			errMsg = resp.Content[0].Text
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	// Extract text content from response
	var result any
	if len(resp.Content) == 1 && resp.Content[0].Type == "text" {
		// Single text response - return as-is
		result = resp.Content[0].Text
	} else {
		// Multiple content items - return as array
		result = resp.Content
	}

	// Serialize result
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize MCP tool result: %w", err)
	}

	return resultJSON, nil
}
