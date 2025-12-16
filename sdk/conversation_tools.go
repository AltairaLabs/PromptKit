package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
)

// OnTool registers a handler for a tool defined in the pack.
//
// The tool name must match a tool defined in the pack's tools section.
// When the LLM calls the tool, your handler receives the parsed arguments
// and returns a result.
//
//	conv.OnTool("get_weather", func(args map[string]any) (any, error) {
//	    city := args["city"].(string)
//	    return weatherAPI.GetCurrent(city)
//	})
//
// The handler's return value is automatically serialized to JSON and sent
// back to the LLM as the tool result.
func (c *Conversation) OnTool(name string, handler ToolHandler) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.handlers[name] = handler
}

// OnToolCtx registers a context-aware handler for a tool.
//
// Use this when your tool implementation needs the request context
// for cancellation, deadlines, or tracing:
//
//	conv.OnToolCtx("search_db", func(ctx context.Context, args map[string]any) (any, error) {
//	    return db.SearchWithContext(ctx, args["query"].(string))
//	})
func (c *Conversation) OnToolCtx(name string, handler ToolHandlerCtx) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	// Wrap ToolHandlerCtx as ToolHandler (context will be injected at call time)
	c.handlers[name] = func(args map[string]any) (any, error) {
		// Note: This wrapper will be replaced with proper context injection
		// when the pipeline is built
		return handler(context.Background(), args)
	}
}

// OnTools registers multiple tool handlers at once.
//
//	conv.OnTools(map[string]sdk.ToolHandler{
//	    "get_weather":   getWeatherHandler,
//	    "search_docs":   searchDocsHandler,
//	    "send_email":    sendEmailHandler,
//	})
func (c *Conversation) OnTools(handlers map[string]ToolHandler) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	for name, handler := range handlers {
		c.handlers[name] = handler
	}
}

// OnToolHTTP registers a tool that makes HTTP requests.
//
// This is a convenience method for tools that call external APIs:
//
//	conv.OnToolHTTP("create_ticket", sdktools.NewHTTPToolConfig(
//	    "https://api.tickets.example.com/tickets",
//	    sdktools.WithMethod("POST"),
//	    sdktools.WithHeader("Authorization", "Bearer "+apiKey),
//	    sdktools.WithTimeout(5000),
//	))
//
// The tool arguments from the LLM are serialized to JSON and sent as the
// request body. The response is parsed and returned to the LLM.
func (c *Conversation) OnToolHTTP(name string, config *sdktools.HTTPToolConfig) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.handlers[name] = config.Handler()
}

// OnToolExecutor registers a custom executor for tools.
//
// Use this when you need full control over tool execution or want to use
// a runtime executor directly:
//
//	executor := &MyCustomExecutor{}
//	conv.OnToolExecutor("custom_tool", executor)
//
// The executor must implement the runtime/tools.Executor interface.
func (c *Conversation) OnToolExecutor(name string, executor tools.Executor) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.handlers[name] = func(args map[string]any) (any, error) {
		// Convert args to JSON for the executor
		argsJSON, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal args: %w", err)
		}

		// Get tool descriptor from pack
		packTool := c.pack.GetTool(name)
		if packTool == nil {
			return nil, fmt.Errorf("tool %q not found in pack", name)
		}

		paramsJSON, err := json.Marshal(packTool.Parameters)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal tool schema: %w", err)
		}

		desc := &tools.ToolDescriptor{
			Name:        packTool.Name,
			Description: packTool.Description,
			InputSchema: paramsJSON,
		}

		// Execute
		result, err := executor.Execute(desc, argsJSON)
		if err != nil {
			return nil, err
		}

		// Parse result
		var parsed any
		if json.Unmarshal(result, &parsed) != nil {
			return string(result), nil
		}
		return parsed, nil
	}
}

// OnToolAsync registers a handler that may require approval before execution.
//
// Use this for Human-in-the-Loop (HITL) workflows where certain actions
// require human approval before proceeding:
//
//	conv.OnToolAsync("process_refund", func(args map[string]any) sdk.PendingResult {
//	    amount := args["amount"].(float64)
//	    if amount > 1000 {
//	        return sdk.PendingResult{
//	            Reason:  "high_value_refund",
//	            Message: fmt.Sprintf("Refund of $%.2f requires approval", amount),
//	        }
//	    }
//	    return sdk.PendingResult{} // Proceed immediately
//	}, func(args map[string]any) (any, error) {
//	    // Execute the actual refund
//	    return refundAPI.Process(args)
//	})
//
// The first function checks if approval is needed, the second executes the action.
func (c *Conversation) OnToolAsync(
	name string,
	checkFunc func(args map[string]any) sdktools.PendingResult,
	execFunc ToolHandler,
) {
	c.asyncHandlersMu.Lock()
	defer c.asyncHandlersMu.Unlock()

	// Initialize maps if needed
	if c.asyncHandlers == nil {
		c.asyncHandlers = make(map[string]sdktools.AsyncToolHandler)
	}
	if c.pendingStore == nil {
		c.pendingStore = sdktools.NewPendingStore()
	}

	c.asyncHandlers[name] = checkFunc

	// Register the execution handler
	c.handlersMu.Lock()
	c.handlers[name] = execFunc
	c.handlersMu.Unlock()
}

// ResolveTool approves and executes a pending tool call.
//
// After calling Send() and receiving pending tools in the response,
// use this to approve and execute them:
//
//	resp, _ := conv.Send(ctx, "Process refund for order #12345")
//	if len(resp.PendingTools()) > 0 {
//	    pending := resp.PendingTools()[0]
//	    // ... get approval ...
//	    result, _ := conv.ResolveTool(pending.ID)
//	    // Continue the conversation with the result
//	    resp, _ = conv.Continue(ctx)
//	}
func (c *Conversation) ResolveTool(id string) (*sdktools.ToolResolution, error) {
	if c.pendingStore == nil {
		return nil, fmt.Errorf("no pending tools")
	}
	return c.pendingStore.Resolve(id)
}

// RejectTool rejects a pending tool call.
//
// Use this when the human reviewer decides not to approve the tool:
//
//	resp, _ := conv.RejectTool(pending.ID, "Not authorized for this amount")
func (c *Conversation) RejectTool(id, reason string) (*sdktools.ToolResolution, error) {
	if c.pendingStore == nil {
		return nil, fmt.Errorf("no pending tools")
	}
	return c.pendingStore.Reject(id, reason)
}

// Continue resumes conversation after resolving pending tools.
//
// Call this after approving/rejecting all pending tools to continue
// the conversation with the tool results:
//
//	resp, _ := conv.Send(ctx, "Process refund")
//	for _, pending := range resp.PendingTools() {
//	    conv.ResolveTool(pending.ID)
//	}
//	resp, _ = conv.Continue(ctx) // LLM receives tool results
func (c *Conversation) Continue(ctx context.Context) (*Response, error) {
	startTime := time.Now()

	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, ErrConversationClosed
	}
	c.mu.RUnlock()

	// Build the tool results from resolved pending tools
	// and continue the conversation
	// For now, this is a simplified implementation that re-sends
	// with the last context - full implementation would inject tool results

	// Get the last message from state store
	msgs := c.Messages(ctx)
	if len(msgs) == 0 {
		return nil, fmt.Errorf("no messages to continue from")
	}

	// Execute pipeline with empty message (continuation)
	userMsg := &types.Message{Role: "user"}
	userMsg.AddTextPart("continue")

	result, err := c.executePipeline(ctx, userMsg)
	if err != nil {
		return nil, err
	}

	return c.buildResponse(result, startTime), nil
}

// PendingTools returns all pending tool calls awaiting approval.
func (c *Conversation) PendingTools() []*sdktools.PendingToolCall {
	if c.pendingStore == nil {
		return nil
	}
	return c.pendingStore.List()
}

// CheckPending checks if a tool call should be pending and creates it if so.
// Returns (pending call, should wait) - if should wait is true, the tool shouldn't execute yet.
//
// This method is used internally when processing tool calls from the LLM.
// It can also be useful for testing HITL workflows:
//
//	pending, shouldWait := conv.CheckPending("risky_tool", args)
//	if shouldWait {
//	    // Tool requires approval
//	}
func (c *Conversation) CheckPending(
	name string,
	args map[string]any,
) (*sdktools.PendingToolCall, bool) {
	c.asyncHandlersMu.RLock()
	checkFunc, isAsync := c.asyncHandlers[name]
	c.asyncHandlersMu.RUnlock()

	if !isAsync {
		return nil, false
	}

	result := checkFunc(args)
	if !result.IsPending() {
		return nil, false
	}

	// Create pending call
	pending := &sdktools.PendingToolCall{
		ID:        uuid.New().String(),
		Name:      name,
		Arguments: args,
		Reason:    result.Reason,
		Message:   result.Message,
	}

	// Store it
	c.pendingStore.Add(pending)

	return pending, true
}

// ToolRegistry returns the underlying tool registry.
//
// This is a power-user method for direct registry access. Tool descriptors
// are loaded from the pack; this allows inspecting them or registering
// custom executors.
//
//	registry := conv.ToolRegistry().(*tools.Registry)
//	for _, desc := range registry.Descriptors() {
//	    fmt.Printf("Tool: %s\n", desc.Name)
//	}
func (c *Conversation) ToolRegistry() *tools.Registry {
	return c.toolRegistry
}

// registerMCPExecutors registers executors for MCP tools.
func (c *Conversation) registerMCPExecutors() {
	if c.mcpRegistry == nil {
		return
	}

	ctx := context.Background()
	mcpTools, err := c.mcpRegistry.ListAllTools(ctx)
	if err != nil {
		return
	}

	for _, serverTools := range mcpTools {
		for _, tool := range serverTools {
			// Register the MCP tool in the registry if not already present
			desc := &tools.ToolDescriptor{
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: tool.InputSchema,
				Mode:        "mcp",
			}
			_ = c.toolRegistry.Register(desc)

			adapter := &mcpHandlerAdapter{name: tool.Name, registry: c.mcpRegistry}
			c.toolRegistry.RegisterExecutor(adapter)
		}
	}
}
