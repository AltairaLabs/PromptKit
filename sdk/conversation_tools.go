package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// OnTool registers a handler for a tool defined in the pack.
//
// The tool name should match a tool defined in the pack's tools section.
// OnTool does not check it: a handler for a name the pack does not define is
// stored and never called. When the LLM calls the tool, your handler receives the parsed arguments
// and returns a result.
//
//	conv.OnTool("get_weather", func(args map[string]any) (any, error) {
//	    city := args["city"].(string)
//	    return weatherAPI.GetCurrent(city)
//	})
//
// The handler's return value is automatically serialized to JSON and sent
// back to the LLM as the tool result.
//
// The handler runs inside the conversation's turn, so it must not start a turn
// on the same conversation: with no context to recognize it by, that call
// waits for the turn running it. A handler that needs to should use
// [Conversation.OnToolCtx] and pass its context on, which gets
// [ErrReentrantTurn] instead.
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
	delete(c.callHandlers, name)
	delete(c.asyncCallHandlers, name)
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
	delete(c.callHandlers, name)
	delete(c.asyncCallHandlers, name)
}

// OnToolExecutor registers a custom executor for tools.
//
// Use this when you need full control over tool execution or want to use
// a runtime executor directly:
//
//	executor := &MyCustomExecutor{}
//	conv.OnToolExecutor("custom_tool", executor)
//
// The executor must implement the runtime/tools.Executor interface. An
// executor that also implements [tools.CallExecutor] is given each call
// through ExecuteCall, so it sees the provider's call ID and the caller the
// conversation acts for ([WithCaller]). Otherwise one that implements
// [tools.MultimodalExecutor] is given each call through ExecuteMultimodal.
// Either way, the content parts it returns (images, audio) go to the model
// with its result.
//
// Where the conversation can suspend a call, an executor that implements
// [tools.AsyncCallExecutor] is given it through ExecuteCallAsync, and one
// that implements [tools.AsyncToolExecutor] through ExecuteAsync, as the
// tool registry gives a call to an executor registered on it. The content
// parts of a completed result go to the model, and a [tools.ToolStatusPending]
// result suspends the call: the Response reports it in ClientTools, and the
// host answers it with [Conversation.SendToolResult] and [Conversation.Resume].
func (c *Conversation) OnToolExecutor(name string, executor tools.Executor) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	if c.ctxHandlers == nil {
		c.ctxHandlers = make(map[string]ToolHandlerCtx)
	}
	delete(c.callHandlers, name)
	c.registerExecutorAsyncLocked(name, executor)
	if run := executorCallFunc(executor); run != nil {
		if c.callHandlers == nil {
			c.callHandlers = make(map[string]toolCallHandler)
		}
		c.callHandlers[name] = func(
			ctx context.Context, call tools.ToolCall,
		) (json.RawMessage, []types.ContentPart, error) {
			desc, err := c.packToolDescriptor(name)
			if err != nil {
				return nil, nil, err
			}
			return run(ctx, desc, call)
		}
	}
	c.ctxHandlers[name] = func(ctx context.Context, args map[string]any) (any, error) {
		// Convert args to JSON for the executor
		argsJSON, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal args: %w", err)
		}

		desc, err := c.packToolDescriptor(name)
		if err != nil {
			return nil, err
		}

		// Execute with pipeline context for tracing and cancellation
		result, err := executor.Execute(ctx, desc, argsJSON)
		if err != nil {
			return nil, err
		}

		return parseToolResult(result), nil
	}
}

// executorCallFunc returns the entry point that runs a whole call on
// executor and returns its content parts with its result, as the tool
// registry calls it: ExecuteCall for a tools.CallExecutor, otherwise
// ExecuteMultimodal for a tools.MultimodalExecutor. It returns nil for an
// executor with neither, which runs through Execute.
func executorCallFunc(executor tools.Executor) func(
	context.Context, *tools.ToolDescriptor, tools.ToolCall,
) (json.RawMessage, []types.ContentPart, error) {
	if callExec, ok := executor.(tools.CallExecutor); ok {
		return callExec.ExecuteCall
	}
	if mmExec, ok := executor.(tools.MultimodalExecutor); ok {
		return func(
			ctx context.Context, desc *tools.ToolDescriptor, call tools.ToolCall,
		) (json.RawMessage, []types.ContentPart, error) {
			return mmExec.ExecuteMultimodal(ctx, desc, call.Args)
		}
	}
	return nil
}

// registerExecutorAsyncLocked stores executor's async entry point as name's
// async call handler, or removes name's when executor has none. The caller
// holds handlersMu.
func (c *Conversation) registerExecutorAsyncLocked(name string, executor tools.Executor) {
	run := executorAsyncFunc(executor)
	if run == nil {
		delete(c.asyncCallHandlers, name)
		return
	}
	if c.asyncCallHandlers == nil {
		c.asyncCallHandlers = make(map[string]toolAsyncCallHandler)
	}
	c.asyncCallHandlers[name] = func(ctx context.Context, call tools.ToolCall) (*tools.ToolExecutionResult, error) {
		desc, err := c.packToolDescriptor(name)
		if err != nil {
			return nil, err
		}
		result, err := run(ctx, desc, call)
		if err == nil && result == nil {
			return nil, fmt.Errorf("tool %s: executor returned no result", name)
		}
		return result, err
	}
}

// executorAsyncFunc returns the executor's entry point that may suspend a
// call, as the tool registry calls it: ExecuteCallAsync for a
// tools.AsyncCallExecutor, otherwise ExecuteAsync for a
// tools.AsyncToolExecutor. It returns nil for a synchronous executor.
//
// ExecuteAsync takes no call, so an executor reached through it, even one
// that is also a tools.CallExecutor, does not see the caller; it can read the
// call's ID from its context with tools.CallIDFromContext.
func executorAsyncFunc(executor tools.Executor) func(
	context.Context, *tools.ToolDescriptor, tools.ToolCall,
) (*tools.ToolExecutionResult, error) {
	if callExec, ok := executor.(tools.AsyncCallExecutor); ok {
		return callExec.ExecuteCallAsync
	}
	if asyncExec, ok := executor.(tools.AsyncToolExecutor); ok {
		return func(
			ctx context.Context, desc *tools.ToolDescriptor, call tools.ToolCall,
		) (*tools.ToolExecutionResult, error) {
			return asyncExec.ExecuteAsync(ctx, desc, call.Args)
		}
	}
	return nil
}

// ToolCall is one call of a tool, as a handler registered with
// [Conversation.OnToolCall] or [Conversation.OnToolAsyncCall] receives it.
type ToolCall struct {
	// Name is the tool's name as defined in the pack.
	Name string
	// CallID is the provider-assigned ID of this call.
	CallID string
	// Args are the call's arguments, parsed from the model's JSON.
	Args map[string]any
	// Caller is who the conversation acts for ([WithCaller]); the zero Caller
	// when none was set.
	Caller types.Caller
}

// OnToolCall registers a handler for a tool defined in the pack that receives
// the whole call: its name, the provider's call ID, the parsed arguments and
// the caller the conversation acts for ([WithCaller]).
//
//	conv.OnToolCall("get_balance", func(ctx context.Context, call sdk.ToolCall) (any, error) {
//	    return accounts.Balance(ctx, call.Caller.ID)
//	})
//
// The return value is serialized to JSON and sent back to the model as the
// tool result. A later OnToolCtx, OnToolHTTP or OnToolExecutor for the same
// name replaces it; a later OnTool does not. A later OnToolAsync or
// OnToolAsyncWithContext replaces it only for held calls once approved: a
// call its check lets through still runs this handler.
func (c *Conversation) OnToolCall(name string, handler func(ctx context.Context, call ToolCall) (any, error)) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.registerCallHandlerLocked(name, handler)
}

// registerCallHandlerLocked stores handler for name in the call handlers,
// which receive the whole call. The caller holds handlersMu.
func (c *Conversation) registerCallHandlerLocked(
	name string, handler func(ctx context.Context, call ToolCall) (any, error),
) {
	if c.callHandlers == nil {
		c.callHandlers = make(map[string]toolCallHandler)
	}
	delete(c.asyncCallHandlers, name)
	c.callHandlers[name] = func(
		ctx context.Context, call tools.ToolCall,
	) (json.RawMessage, []types.ContentPart, error) {
		var args map[string]any
		if len(call.Args) > 0 {
			if err := json.Unmarshal(call.Args, &args); err != nil {
				return nil, nil, fmt.Errorf("failed to parse tool arguments: %w", err)
			}
		}
		result, err := handler(ctx, ToolCall{Name: call.Name, CallID: call.ID, Args: args, Caller: call.Caller})
		if err != nil {
			return nil, nil, err
		}
		resultJSON, err := json.Marshal(result)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to serialize tool result: %w", err)
		}
		return resultJSON, nil, nil
	}
}

// callerIdentity is the caller the conversation acts for ([WithCaller]).
func (c *Conversation) callerIdentity() types.Caller {
	if c.config == nil {
		return types.Caller{}
	}
	return c.config.caller
}

// packToolDescriptor is the descriptor OnToolExecutor hands its executor for
// the pack tool name: the pack's name, description, parameters and
// declaration.
func (c *Conversation) packToolDescriptor(name string) (*tools.ToolDescriptor, error) {
	packTool := c.pack.GetTool(name)
	if packTool == nil {
		return nil, markSentinel(fmt.Errorf("tool %q not found in pack", name), ErrToolNotInPack)
	}
	paramsJSON, err := json.Marshal(packTool.Parameters)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tool schema: %w", err)
	}
	return &tools.ToolDescriptor{
		Name:        packTool.Name,
		Description: packTool.Description,
		InputSchema: paramsJSON,
		Declaration: c.toolDeclaration(name, packTool),
	}, nil
}

// OnToolAsync registers a handler that may require approval before execution.
//
// Use this for Human-in-the-Loop (HITL) workflows where certain actions
// require human approval before proceeding:
//
//	conv.OnToolAsync("process_refund", func(args map[string]any) sdktools.PendingResult {
//	    amount := args["amount"].(float64)
//	    if amount > 1000 {
//	        return sdktools.PendingResult{
//	            Reason:  "high_value_refund",
//	            Message: fmt.Sprintf("Refund of $%.2f requires approval", amount),
//	        }
//	    }
//	    return sdktools.PendingResult{} // Proceed immediately
//	}, func(args map[string]any) (any, error) {
//	    // Execute the actual refund
//	    return refundAPI.Process(args)
//	})
//
// The first function checks if approval is needed, the second executes the action.
// A held call, once approved ([Conversation.ResolveTool]), runs the second
// function, whatever other handler is registered for the tool.
//
// A call the check lets through runs on the tool's handlers as any call does,
// by the same rule in unary, VAD and realtime duplex conversations: a handler
// registered with [Conversation.OnToolCall], [Conversation.OnToolExecutor],
// [Conversation.OnToolCtx] or [Conversation.OnToolHTTP] runs it, even one
// registered before this call; otherwise the second function does, since it
// replaces the tool's [Conversation.OnTool] handler.
//
// Lock ordering contract: asyncHandlersMu is acquired first, then handlersMu.
// All code paths that acquire both locks must follow this order to avoid deadlock.
func (c *Conversation) OnToolAsync(
	name string,
	checkFunc func(args map[string]any) sdktools.PendingResult,
	execFunc ToolHandler,
) {
	c.OnToolAsyncWithContext(name, func(_ context.Context, _ string, args map[string]any) sdktools.PendingResult {
		return checkFunc(args)
	}, execFunc)
}

// OnToolAsyncWithContext is OnToolAsync with a check that receives the
// tool-execution context and the provider's call id. Use it when the decision
// consults a remote service that must honor cancellation. When it depends on
// who is calling, use [Conversation.OnToolAsyncCall], whose check receives
// the caller the conversation acts for.
//
//	conv.OnToolAsyncWithContext("process_refund",
//	    func(ctx context.Context, callID string, args map[string]any) sdktools.PendingResult {
//	        if policy.NeedsApproval(ctx, callID, args) {
//	            return sdktools.PendingResult{Reason: "policy"}
//	        }
//	        return sdktools.PendingResult{}
//	    },
//	    refund)
//
// As with OnToolAsync, a held call, once approved, runs execFunc, and a call
// the check lets through runs on the tool's handlers by OnToolAsync's rule.
func (c *Conversation) OnToolAsyncWithContext(name string, checkFunc sdktools.AsyncToolCheck, execFunc ToolHandler) {
	c.asyncHandlersMu.Lock()
	defer c.asyncHandlersMu.Unlock()
	c.initAsyncStoresLocked()
	c.asyncHandlers[name] = checkFunc
	c.approvalExecs[name] = func(_ context.Context, _ string, _ types.Caller, args map[string]any) (any, error) {
		return execFunc(args)
	}

	// Register the execution handler (handlersMu acquired after asyncHandlersMu per lock ordering)
	c.handlersMu.Lock()
	c.handlers[name] = execFunc
	c.handlersMu.Unlock()
}

// OnToolAsyncCall is [Conversation.OnToolAsyncWithContext] with a check and
// an execution handler that each receive the whole call, as an
// [Conversation.OnToolCall] handler does: the tool's name, the provider's call
// ID, the parsed arguments and the caller the conversation acts for
// ([WithCaller]). Use it when the approval decision depends on who is calling.
//
// A call the check holds runs exec when it is approved
// ([Conversation.ResolveTool]), whatever other handler is registered for the
// tool. A call the check lets through runs exec too, in every mode: exec
// replaces the tool's handlers as [Conversation.OnToolCall] does, until a
// later OnToolCtx, OnToolHTTP or OnToolExecutor replaces it.
//
// The approved execution acts for the caller of the conversation that
// resolves it, so a host that resumes a conversation to approve a call (with
// a durable [WithPendingStore], say) must open it with the original
// requester's [WithCaller].
//
//	conv.OnToolAsyncCall("process_refund",
//	    func(ctx context.Context, call sdk.ToolCall) sdktools.PendingResult {
//	        if policy.NeedsApproval(ctx, call.Caller, call.Args) {
//	            return sdktools.PendingResult{Reason: "policy"}
//	        }
//	        return sdktools.PendingResult{}
//	    },
//	    func(ctx context.Context, call sdk.ToolCall) (any, error) {
//	        return refunds.Process(ctx, call.Caller.ID, call.Args)
//	    })
func (c *Conversation) OnToolAsyncCall(
	name string,
	check func(ctx context.Context, call ToolCall) sdktools.PendingResult,
	exec func(ctx context.Context, call ToolCall) (any, error),
) {
	c.asyncHandlersMu.Lock()
	defer c.asyncHandlersMu.Unlock()
	c.initAsyncStoresLocked()
	c.asyncHandlers[name] = func(ctx context.Context, callID string, args map[string]any) sdktools.PendingResult {
		return check(ctx, ToolCall{Name: name, CallID: callID, Args: args, Caller: c.callerIdentity()})
	}
	c.approvalExecs[name] = func(
		ctx context.Context, callID string, caller types.Caller, args map[string]any,
	) (any, error) {
		return exec(ctx, ToolCall{Name: name, CallID: callID, Args: args, Caller: caller})
	}

	// handlersMu acquired after asyncHandlersMu per lock ordering.
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.registerCallHandlerLocked(name, exec)
	// An earlier OnToolAsync's handler is replaced, not kept as a fallback.
	delete(c.handlers, name)
}

// initAsyncStoresLocked creates the async check map and the pending and
// resolved stores, and the approval executions, on first use. The caller
// holds asyncHandlersMu.
func (c *Conversation) initAsyncStoresLocked() {
	if c.asyncHandlers == nil {
		c.asyncHandlers = make(map[string]sdktools.AsyncToolCheck)
	}
	if c.approvalExecs == nil {
		c.approvalExecs = make(map[string]approvalExec)
	}
	if c.pendingStore == nil {
		c.pendingStore, c.ownsPendingStore = newPendingStore(c.config)
	}
	if c.resolvedStore == nil {
		c.resolvedStore = sdktools.NewResolvedStore()
	}
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

// approvalExec runs a held call once it is approved, with the held call's ID,
// the caller the conversation acts for and the approved arguments.
type approvalExec func(ctx context.Context, callID string, caller types.Caller, args map[string]any) (any, error)

// lookupExecHandler recovers the registered execution handler for a tool by
// name. A persisted pending call carries no closure, so the handler is
// re-attached here at resolve time — which is why the resolving process must
// have registered the same OnToolAsync handler.
//
// One rule decides what an approved call runs: the execution handler
// OnToolAsync, OnToolAsyncWithContext or OnToolAsyncCall registered for the
// tool, whatever other handler is registered for it. It is given the held
// call's ID and the caller the conversation acts for: the call was held in
// this conversation, which has one caller. A tool with no async registration
// in this process falls back to its plain handler.
func (c *Conversation) lookupExecHandler(ctx context.Context, name, callID string) sdktools.ExecFunc {
	c.asyncHandlersMu.RLock()
	approved := c.approvalExecs[name]
	c.asyncHandlersMu.RUnlock()
	if approved != nil {
		caller := c.callerIdentity()
		return func(args map[string]any) (any, error) {
			return approved(tools.WithCallID(ctx, callID), callID, caller, args)
		}
	}
	c.handlersMu.RLock()
	handler := c.handlers[name]
	c.handlersMu.RUnlock()
	if handler == nil {
		return nil
	}
	return sdktools.ExecFunc(handler)
}

// parseToolResult decodes a tool's JSON result, or returns it as a string
// when it is not JSON.
func parseToolResult(result json.RawMessage) any {
	var parsed any
	if json.Unmarshal(result, &parsed) != nil {
		return string(result)
	}
	return parsed
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
// The approval admits the call under ToolPolicy.MaxCallsPerMinute (see
// WithToolPolicy). If the tool's calls admitted in the last minute already
// reach the limit, the handler is not called: the resolution's Error wraps a
// *stage.CallRateLimitError, which Continue passes to the model as the call's
// result.
//
// The handler's result is held to the tool's output schema as the result of
// a call the turn runs is (tools.Registry.CheckResult): coerced, or, when it
// does not match, replaced by the validation error, which the resolution's
// Error wraps in a *ToolError and Continue passes to the model as the call's
// result.
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
	if awaitErr := c.discardIfNotAwaited(ctx, id); awaitErr != nil {
		return nil, awaitErr
	}
	handler := c.lookupExecHandler(ctx, peek.Name, peek.ID)
	if handler == nil {
		return nil, markSentinel(fmt.Errorf("no handler registered for tool %q", peek.Name), ErrToolNotRegistered)
	}
	// Claim atomically (single-winner across instances) now that we can execute.
	call, ok, err := c.pendingStore.Claim(ctx, c.ID(), id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, sdktools.ErrPendingAlreadyResolved
	}
	// The approval admits the call under ToolPolicy.MaxCallsPerMinute. Over the
	// limit, the call does not run: the limit error takes the place of its
	// result.
	if limitErr := c.callRate.Admit(call.ConversationID, call.Name, c.callRateLimit()); limitErr != nil {
		handler = func(map[string]any) (any, error) { return nil, limitErr }
	}
	c.admitCall(call.ID, call.Name, call.Arguments)
	resolution := sdktools.ResolveApproved(call, handler, overrides)
	c.checkApprovedResult(call.Name, resolution)
	if resolution.Error != nil {
		resolution.Error = &ToolError{ToolName: call.Name, Cause: resolution.Error}
	}
	// Store for Continue()/ContinueDuplex() to use
	if c.resolvedStore != nil {
		c.addResolution(resolution)
	}
	return resolution, nil
}

// checkApprovedResult holds an approved call's result to the checks the
// registry applies to the result of a call it runs (Registry.CheckResult), so
// the model is given, and the ToolResultRewriter hooks see, what they would
// had the turn run the call: the result as coerced (Result and ResultJSON
// both), or, when it does not match the tool's output schema, the validation
// error in its place. A handler failure, or a tool the registry does not
// know, is left as it is.
func (c *Conversation) checkApprovedResult(name string, res *sdktools.ToolResolution) {
	if res.Error != nil || c.toolRegistry == nil {
		return
	}
	checked, err := c.toolRegistry.CheckResult(name, res.ResultJSON, res.Parts)
	if err != nil {
		return
	}
	if checked.Status == tools.ToolStatusFailed {
		res.Result, res.ResultJSON = nil, nil
		res.Error = errors.New(checked.Error)
		return
	}
	if !bytes.Equal(checked.Content, res.ResultJSON) {
		res.Result = parseToolResult(checked.Content) // the coerced value
	}
	res.ResultJSON = checked.Content
}

// discardIfNotAwaited removes held call id from the pending store and returns
// ErrToolCallNotAwaited when the conversation is no longer waiting on it, so
// an answer that would never reach the model is refused before the tool runs.
func (c *Conversation) discardIfNotAwaited(ctx context.Context, id string) error {
	awaitErr := c.checkAwaited(ctx, id)
	if awaitErr == nil {
		return nil
	}
	if _, _, err := c.pendingStore.Claim(ctx, c.ID(), id); err != nil {
		return errors.Join(awaitErr, err)
	}
	return awaitErr
}

// RejectTool rejects a pending tool call.
//
// The call is claimed atomically, so a reject races safely against a concurrent
// approve — the loser gets [sdktools.ErrPendingAlreadyResolved].
//
//	resolution, _ := conv.RejectTool(ctx, pending.ID, "Not authorized for this amount")
func (c *Conversation) RejectTool(ctx context.Context, id, reason string) (*sdktools.ToolResolution, error) {
	if c.pendingStore == nil {
		return nil, fmt.Errorf("no pending tools")
	}
	if err := c.discardIfNotAwaited(ctx, id); err != nil {
		return nil, err
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
		c.addResolution(resolution)
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
//	    conv.ResolveTool(ctx, pending.ID)
//	}
//	resp, _ = conv.Continue(ctx) // LLM receives tool results
func (c *Conversation) Continue(ctx context.Context) (*Response, error) {
	startTime := time.Now()

	release, gateErr := c.turn.acquire(ctx, c)
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	ctx = c.turn.mark(ctx, c)

	if err := c.validateContinueState(); err != nil {
		return nil, err
	}

	// Get all resolved tool results
	history, err := c.settlementHistory(ctx)
	if err != nil {
		return nil, err
	}
	var resolutions []*sdktools.ToolResolution
	if c.resolvedStore != nil {
		resolutions = c.takeSettlement(history)
	}

	if len(resolutions) == 0 {
		return nil, fmt.Errorf("no resolved tools to continue with")
	}
	// A client tool answered here rather than through Resume is admitted the
	// same way.
	calls := callsIn(history)
	c.admitDeferredResults(resolutions, callNamesOf(calls))

	// Build tool result messages from resolutions
	toolMsgs := make([]types.Message, 0, len(resolutions))
	for _, res := range resolutions {
		toolMsgs = append(toolMsgs, types.NewToolResultMessage(c.continuedResult(ctx, calls, res)))
	}
	c.settled(resolutions)

	// Give the model every result in one call: a provider that requires each
	// call of a round to be answered rejects a request missing any of them.
	result, err := c.unarySession.ResumeWithToolResults(c.withConversationState(ctx), toolMsgs)
	if err != nil {
		return nil, fmt.Errorf("failed to process tool results: %w", c.wrapTurnError(err))
	}

	return c.buildResponse(ctx, result, startTime), nil
}

// validateContinueState returns an error when Continue cannot run: the
// conversation is closed, or it is duplex (use ContinueDuplex).
func (c *Conversation) validateContinueState() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.requireUnary("Continue()")
}

// ContinueDuplex sends resolved/rejected HITL tool results back into the
// duplex stream. Unlike Continue() (which re-executes the unary pipeline),
// this pushes tool results into the live duplex pipeline via SubmitToolResults.
//
// Usage:
//
//	respCh, _ := conv.Response()
//	for chunk := range respCh {
//	    if len(chunk.PendingTools) > 0 {
//	        for _, pt := range chunk.PendingTools {
//	            conv.ResolveTool(ctx, pt.CallID) // or RejectTool
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

	history, err := c.settlementHistory(ctx)
	if err != nil {
		return err
	}
	resolutions := c.resolvedStore.PopAll()
	if len(resolutions) == 0 {
		return fmt.Errorf("no resolved tools to continue with")
	}
	calls := callsIn(history)
	c.admitDeferredResults(resolutions, callNamesOf(calls))

	results := make([]types.MessageToolResult, 0, len(resolutions))
	responses := make([]providers.ToolResponse, 0, len(resolutions))
	for _, res := range resolutions {
		tr := c.continuedResult(ctx, calls, res)
		results = append(results, tr)
		responses = append(responses, toolResponseOf(tr, len(res.Parts) > 0 && tr.Error == ""))
	}
	c.settled(resolutions)

	// The history records each result as Continue's history would, with its
	// error and error type, where the session can take them.
	if s, ok := c.duplexSession.(toolResultMessageSubmitter); ok {
		return s.SubmitToolResultMessages(ctx, responses, results)
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
//	for name, desc := range conv.ToolRegistry().GetTools() {
//	    fmt.Printf("Tool: %s (%s)\n", name, desc.Description)
//	}
func (c *Conversation) ToolRegistry() *tools.Registry {
	return c.toolRegistry
}

// registerMCPExecutors registers executors for MCP tools.
// Guarded by mcpExecutorsRegistered to avoid redundant ListAllTools I/O on every pipeline build (e.g. Fork).
//
// The guard is set only after a successful enumeration. A server that is still
// starting, or briefly unreachable, would otherwise leave the conversation with
// no MCP tools for its whole life even though the next build would succeed.
func (c *Conversation) registerMCPExecutors() {
	if c.mcpRegistry == nil || c.mcpExecutorsRegistered {
		return
	}

	// Register a single runtime MCP executor that dispatches every
	// Mode="mcp" tool to the underlying MCP client. This is the canonical
	// wiring used by PromptArena's engine —
	// the runtime's tools.Registry.getExecutorForTool resolves Mode="mcp"
	// to executor name "mcp", which only the runtime executor satisfies.
	// It and the tracking come before the listing: tools that a retry
	// lists later land in this registry and need the executor behind them.
	c.toolRegistry.RegisterExecutor(tools.NewMCPExecutor(c.mcpRegistry))
	if c.mcpTools != nil {
		c.mcpTools.track(c.toolRegistry)
	}

	ctx := context.Background()
	mcpTools, err := c.mcpRegistry.ListAllTools(ctx)
	if err != nil {
		// Every mcp__ tool is absent until a retry lists it. Say so: the
		// symptom otherwise surfaces layers away as "tool not registered",
		// with nothing connecting it to the server that could not be reached.
		logger.Warn("mcp tools not registered: listing tools failed; retrying in the background",
			"servers", c.mcpRegistry.ListServers(),
			"error", err)
		c.retryUnlistedMCPServers(nil)
		return
	}
	c.mcpExecutorsRegistered = true

	for serverName, serverTools := range mcpTools {
		registerMCPServerTools(c.toolRegistry, c.mcpRegistry, serverName, serverTools)
	}
	if unlisted := c.retryUnlistedMCPServers(mcpTools); len(unlisted) > 0 {
		logger.Warn("mcp tools not registered for some servers: listing their tools failed; retrying in the background",
			"servers", unlisted)
	}
}

// retryUnlistedMCPServers has every server missing from listed retried in
// the background, and returns their names.
func (c *Conversation) retryUnlistedMCPServers(listed map[string][]mcp.Tool) []string {
	var unlisted []string
	for _, name := range c.mcpRegistry.ListServers() {
		if _, ok := listed[name]; !ok {
			unlisted = append(unlisted, name)
		}
	}
	sort.Strings(unlisted)
	if c.mcpTools != nil {
		for _, name := range unlisted {
			c.mcpTools.retry(name)
		}
	}
	return unlisted
}

// toolDeclaration returns the declaration an executor registered with
// OnToolExecutor receives: the copy the conversation's registry holds, which
// is the same value tool hooks see on hooks.ToolRequest.Declaration (RFC 0016),
// so it is not re-copied on every call. A fresh copy of the pack entry covers a
// conversation whose registry does not hold the tool.
func (c *Conversation) toolDeclaration(name string, packTool *packspec.Tool) *packspec.Tool {
	if c.toolRegistry != nil {
		if desc := c.toolRegistry.Get(name); desc != nil && desc.Declaration != nil {
			return desc.Declaration
		}
	}
	return packspec.Clone(packTool)
}
