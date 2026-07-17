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
// Context-aware handlers (ctxHandlers) are preferred over plain handlers
// so that pipeline context (tracing, cancellation) propagates to tool calls.
type localExecutor struct {
	handlers    map[string]ToolHandler
	ctxHandlers map[string]ToolHandlerCtx
	live        *localHandlersAccessor
}

// localHandlersAccessor provides live read access to the conversation's local
// tool handlers under its mutex. It lets the executor dispatch handlers
// registered AFTER the pipeline was built — e.g. OnTool/OnToolCtx called after
// OpenDuplex, whose duplex pipeline is built once (mirrors the client-tool
// accessor). Without it, a duplex session's local tools use only the empty
// build-time snapshot and fail with "no handler registered".
type localHandlersAccessor struct {
	conv *Conversation
}

func (a *localHandlersAccessor) getCtxHandler(name string) (ToolHandlerCtx, bool) {
	a.conv.handlersMu.RLock()
	defer a.conv.handlersMu.RUnlock()
	h, ok := a.conv.ctxHandlers[name]
	return h, ok
}

func (a *localHandlersAccessor) getHandler(name string) (ToolHandler, bool) {
	a.conv.handlersMu.RLock()
	defer a.conv.handlersMu.RUnlock()
	h, ok := a.conv.handlers[name]
	return h, ok
}

// Name returns "local" to match the Mode on tools in the pack.
func (e *localExecutor) Name() string {
	return "local"
}

// Execute dispatches to the appropriate handler based on tool name.
// Context-aware handlers are preferred so that tracing and cancellation propagate.
func (e *localExecutor) Execute(
	ctx context.Context, descriptor *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	// Parse args to map
	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	// Prefer context-aware handler; for each kind look at the build-time
	// snapshot first, then live handlers via the accessor (handlers registered
	// after the pipeline was built, e.g. after OpenDuplex).
	ctxHandler, ok := e.ctxHandlers[descriptor.Name]
	if !ok && e.live != nil {
		ctxHandler, ok = e.live.getCtxHandler(descriptor.Name)
	}

	var result any
	var err error
	switch {
	case ok:
		result, err = ctxHandler(ctx, argsMap)
	default:
		handler, hok := e.handlers[descriptor.Name]
		if !hok && e.live != nil {
			handler, hok = e.live.getHandler(descriptor.Name)
		}
		if !hok {
			return nil, fmt.Errorf("no handler registered for tool: %s", descriptor.Name)
		}
		result, err = handler(argsMap)
	}
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
	qualifiedName string // Namespaced name used as registry key (e.g. "mcp__fs__read_file")
	rawName       string // Original MCP tool name sent to the server (e.g. "read_file")
	registry      mcp.Registry
}

// Name returns the qualified tool name.
func (a *mcpHandlerAdapter) Name() string {
	return a.qualifiedName
}

// Execute runs the MCP tool with the given arguments.
func (a *mcpHandlerAdapter) Execute(
	ctx context.Context, _ *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	// Use the raw MCP name for server communication
	client, err := a.registry.GetClientForTool(ctx, a.rawName)
	if err != nil {
		return nil, fmt.Errorf("failed to get MCP client for tool %q: %w", a.qualifiedName, err)
	}

	// Call the tool using the raw name the MCP server knows
	resp, err := client.CallTool(ctx, a.rawName, args)
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
