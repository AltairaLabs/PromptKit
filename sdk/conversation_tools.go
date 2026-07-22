package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
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
	if c.ctxHandlers == nil {
		c.ctxHandlers = make(map[string]ToolHandlerCtx)
	}
	c.ctxHandlers[name] = handler
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
	if c.ctxHandlers == nil {
		c.ctxHandlers = make(map[string]ToolHandlerCtx)
	}
	c.ctxHandlers[name] = config.HandlerCtx()
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
	if c.ctxHandlers == nil {
		c.ctxHandlers = make(map[string]ToolHandlerCtx)
	}
	c.ctxHandlers[name] = func(ctx context.Context, args map[string]any) (any, error) {
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

		// Execute with pipeline context for tracing and cancellation
		result, err := executor.Execute(ctx, desc, argsJSON)
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
//
// Lock ordering contract: asyncHandlersMu is acquired first, then handlersMu.
// All code paths that acquire both locks must follow this order to avoid deadlock.
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
		c.pendingStore, c.ownsPendingStore = newPendingStore(c.config)
	}
	if c.resolvedStore == nil {
		c.resolvedStore = sdktools.NewResolvedStore()
	}

	c.asyncHandlers[name] = checkFunc

	// Register the execution handler (handlersMu acquired after asyncHandlersMu per lock ordering)
	c.handlersMu.Lock()
	c.handlers[name] = execFunc
	c.handlersMu.Unlock()
}

// newPendingStore returns the durable pending store configured via
// WithPendingStore (owned=false — the caller manages its lifecycle), or a
// default in-memory store (owned=true — the SDK closes it on Conversation.Close).
func newPendingStore(cfg *config) (store sdktools.PendingStore, owned bool) {
	if cfg != nil && cfg.pendingStore != nil {
		return cfg.pendingStore, false
	}
	return sdktools.NewMemoryPendingStore(), true
}

// lookupExecHandler recovers the registered execution handler for a tool by
// name. A persisted pending call carries no closure, so the handler is
// re-attached here at resolve time — which is why the resolving process must
// have registered the same OnToolAsync handler.
func (c *Conversation) lookupExecHandler(name string) sdktools.ExecFunc {
	c.handlersMu.RLock()
	handler := c.handlers[name]
	c.handlersMu.RUnlock()
	if handler == nil {
		return nil
	}
	return sdktools.ExecFunc(handler)
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
//	    result, _ := conv.ResolveTool(ctx, pending.ID)
//	    // Continue the conversation with the result
//	    resp, _ = conv.Continue(ctx)
//	}
func (c *Conversation) ResolveTool(ctx context.Context, id string) (*sdktools.ToolResolution, error) {
	return c.ResolveToolWithArgs(ctx, id, nil)
}

// ResolveToolWithArgs approves a pending tool call with reviewer-supplied
// argument overrides (approve-with-edits), then executes it.
//
// The call is claimed atomically from the store, so concurrent instances of the
// same agent cannot double-resolve it — a losing caller gets
// [sdktools.ErrPendingAlreadyResolved]. The execution handler is recovered by
// tool name, so the resolving process must have registered the matching
// OnToolAsync handler (see WithPendingStore).
//
// Overrides are shallow-merged over the arguments the model proposed: keys in
// overrides replace the originals, absent keys are preserved. A nil or empty
// map is identical to ResolveTool (approve as-proposed). The resulting
// ToolResolution reports Edited=true and carries the effective Arguments.
//
// Works on both paths: after Send() follow with Continue(), and after a duplex
// pending surfaces follow with ContinueDuplex() — both consume the resolution
// this produces.
//
//	pending := resp.PendingTools()[0]
//	// reviewer tweaks the draft before it sends:
//	conv.ResolveToolWithArgs(ctx, pending.ID, map[string]any{"body": editedText})
//	resp, _ = conv.Continue(ctx)
func (c *Conversation) ResolveToolWithArgs(
	ctx context.Context, id string, overrides map[string]any,
) (*sdktools.ToolResolution, error) {
	if c.pendingStore == nil {
		return nil, fmt.Errorf("no pending tools")
	}
	// Recover the handler BEFORE claiming. Claim deletes the record, so if we
	// claimed first and then found no handler, the held call would be lost with
	// no way to retry — the exact failure mode when a resolving process forgot
	// to re-register its OnToolAsync handlers. Peeking first leaves the record
	// intact so the caller can register the handler and retry.
	peek, ok, err := c.pendingStore.Get(ctx, c.ID(), id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, sdktools.ErrPendingAlreadyResolved
	}
	handler := c.lookupExecHandler(peek.Name)
	if handler == nil {
		return nil, fmt.Errorf("no handler registered for tool %q", peek.Name)
	}
	// Claim atomically (single-winner across instances) now that we can execute.
	call, ok, err := c.pendingStore.Claim(ctx, c.ID(), id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, sdktools.ErrPendingAlreadyResolved
	}
	resolution := sdktools.ResolveApproved(call, handler, overrides)
	// Store for Continue()/ContinueDuplex() to use
	if c.resolvedStore != nil {
		c.resolvedStore.Add(resolution)
	}
	return resolution, nil
}

// RejectTool rejects a pending tool call.
//
// The call is claimed atomically, so a reject races safely against a concurrent
// approve — the loser gets [sdktools.ErrPendingAlreadyResolved].
//
//	resp, _ := conv.RejectTool(ctx, pending.ID, "Not authorized for this amount")
func (c *Conversation) RejectTool(ctx context.Context, id, reason string) (*sdktools.ToolResolution, error) {
	if c.pendingStore == nil {
		return nil, fmt.Errorf("no pending tools")
	}
	_, ok, err := c.pendingStore.Claim(ctx, c.ID(), id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, sdktools.ErrPendingAlreadyResolved
	}
	resolution := sdktools.ResolveRejected(id, reason)
	// Store for Continue() to use
	if c.resolvedStore != nil {
		c.resolvedStore.Add(resolution)
	}
	return resolution, nil
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

	// Get all resolved tool results
	var resolutions []*sdktools.ToolResolution
	if c.resolvedStore != nil {
		resolutions = c.resolvedStore.PopAll()
	}

	if len(resolutions) == 0 {
		return nil, fmt.Errorf("no resolved tools to continue with")
	}

	// Build tool result messages from resolutions
	toolMsgs := make([]types.Message, 0, len(resolutions))
	for _, res := range resolutions {
		var content string
		var errStr string
		if res.Rejected {
			content = fmt.Sprintf("Tool call rejected: %s", res.RejectionReason)
			errStr = "rejected by user"
		} else if res.Error != nil {
			content = fmt.Sprintf("Tool error: %s", res.Error.Error())
			errStr = res.Error.Error()
		} else if res.ResultJSON != nil {
			content = string(res.ResultJSON)
		} else {
			content = fmt.Sprintf("%v", res.Result)
		}

		toolResult := types.NewTextToolResult(res.ID, "", content)
		toolResult.Error = errStr
		toolMsgs = append(toolMsgs, types.NewToolResultMessage(toolResult))
	}

	// Execute the pipeline with each tool result message sequentially.
	// Each call appends the tool result to conversation state; the final call
	// returns the LLM's response incorporating all accumulated tool results.
	var result *rtpipeline.ExecutionResult
	var err error

	for i := range toolMsgs {
		result, err = c.executePipeline(ctx, &toolMsgs[i])
		if err != nil {
			return nil, fmt.Errorf("failed to process tool result %d: %w", i, err)
		}
	}

	return c.buildResponse(ctx, result, startTime), nil
}

// ContinueDuplex sends resolved/rejected HITL tool results back into the
// duplex stream. Unlike Continue() (which re-executes the unary pipeline),
// this pushes tool results into the live duplex pipeline via SubmitToolResults.
//
// Usage:
//
//	for chunk := range conv.Response() {
//	    if len(chunk.PendingTools) > 0 {
//	        for _, pt := range chunk.PendingTools {
//	            conv.ResolveTool(pt.CallID) // or RejectTool
//	        }
//	        conv.ContinueDuplex(ctx)
//	    }
//	}
func (c *Conversation) ContinueDuplex(ctx context.Context) error {
	c.mu.RLock()
	if err := c.requireDuplex("ContinueDuplex()"); err != nil {
		c.mu.RUnlock()
		return err
	}
	c.mu.RUnlock()

	resolutions := c.resolvedStore.PopAll()
	if len(resolutions) == 0 {
		return fmt.Errorf("no resolved tools to continue with")
	}

	responses := make([]providers.ToolResponse, 0, len(resolutions))
	for _, res := range resolutions {
		var result string
		var isError bool
		switch {
		case res.Rejected:
			result = fmt.Sprintf("Tool call rejected: %s", res.RejectionReason)
			isError = true
		case res.Error != nil:
			result = fmt.Sprintf("Tool error: %s", res.Error.Error())
			isError = true
		case res.ResultJSON != nil:
			result = string(res.ResultJSON)
		default:
			result = fmt.Sprintf("%v", res.Result)
		}
		responses = append(responses, providers.ToolResponse{
			ToolCallID: res.ID,
			Result:     result,
			IsError:    isError,
		})
	}

	return c.duplexSession.SubmitToolResults(ctx, responses)
}

// PendingTools returns all pending tool calls awaiting approval for this
// conversation. With a durable store this reflects calls held by any instance,
// including ones that survived a restart. Returns nil (no error) when no store
// is configured.
func (c *Conversation) PendingTools(ctx context.Context) ([]*sdktools.PendingToolCall, error) {
	if c.pendingStore == nil {
		return nil, nil
	}
	return c.pendingStore.List(ctx, c.ID())
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
// Guarded by mcpExecutorsRegistered to avoid redundant ListAllTools I/O on every pipeline build (e.g. Fork).
func (c *Conversation) registerMCPExecutors() {
	if c.mcpRegistry == nil || c.mcpExecutorsRegistered {
		return
	}
	c.mcpExecutorsRegistered = true

	ctx := context.Background()
	mcpTools, err := c.mcpRegistry.ListAllTools(ctx)
	if err != nil {
		return
	}

	// Register a single runtime MCP executor that dispatches every
	// Mode="mcp" tool to the underlying MCP client. This is the canonical
	// wiring used by PromptArena's engine —
	// the runtime's tools.Registry.getExecutorForTool resolves Mode="mcp"
	// to executor name "mcp", which only the runtime executor satisfies.
	c.toolRegistry.RegisterExecutor(tools.NewMCPExecutor(c.mcpRegistry))

	for serverName, serverTools := range mcpTools {
		// Look up the server config to check for tool filters.
		var toolFilter *mcp.ToolFilter
		for _, srv := range c.mcpRegistry.ListServers() {
			if srv == serverName {
				if cfg, ok := c.mcpRegistry.GetServerConfig(serverName); ok && cfg.ToolFilter != nil {
					toolFilter = cfg.ToolFilter
				}
				break
			}
		}

		for _, tool := range serverTools {
			// Apply tool filter if configured.
			if toolFilter != nil && !toolFilter.Includes(tool.Name) {
				continue
			}

			qualifiedName := fmt.Sprintf("mcp__%s__%s", serverName, tool.Name)

			// Register the MCP tool in the registry with qualified name.
			// The runtime MCPExecutor strips the namespace and looks up
			// the owning server via mcp.Registry.toolIndex.
			desc := &tools.ToolDescriptor{
				Name:        qualifiedName,
				Description: tool.Description,
				InputSchema: tool.InputSchema,
				Mode:        "mcp",
			}
			_ = c.toolRegistry.Register(desc)
		}
	}
}
