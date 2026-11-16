package sdk

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// localExecutor is a tool executor for locally-handled tools (Mode: "local").
// It dispatches to the appropriate ToolHandler based on the tool name.
// Context-aware handlers (ctxHandlers) are preferred over plain handlers
// so that pipeline context (tracing, cancellation) propagates to tool calls.
//
// A pack tool's descriptor says "local" whatever the host means to do with it:
// execution mode is a hosting concern, not a pack field. So a call for a tool
// the conversation bound with OnClientTool or DeferClientTool arrives here too,
// and is handed to the conversation's client executor. The binding lives on
// the conversation, not on the descriptor, which may be shared with other
// conversations through a host registry.
type localExecutor struct {
	handlers    map[string]ToolHandler
	ctxHandlers map[string]ToolHandlerCtx
	live        *localHandlersAccessor
	client      *clientExecutor
}

// clientBound reports whether the conversation bound name as a client tool.
func (e *localExecutor) clientBound(name string) bool {
	return e.client != nil && e.client.isBound(name)
}

// toolCallHandler runs a whole tool call: its name, call ID, coerced arguments
// and caller. OnToolExecutor stores one for an executor that implements
// tools.CallExecutor.
type toolCallHandler func(ctx context.Context, call tools.ToolCall) (json.RawMessage, []types.ContentPart, error)

// ExecuteCallAsync is ExecuteAsync for the whole call: a client-bound tool goes
// to the client executor, which may defer it (ToolStatusPending), and every
// other tool runs through ExecuteCall.
func (e *localExecutor) ExecuteCallAsync(
	ctx context.Context, descriptor *tools.ToolDescriptor, call tools.ToolCall,
) (*tools.ToolExecutionResult, error) {
	if e.clientBound(descriptor.Name) {
		return e.client.ExecuteCallAsync(ctx, asClientTool(descriptor), call)
	}
	result, parts, err := e.ExecuteCall(ctx, descriptor, call)
	if err != nil {
		return nil, err
	}
	return &tools.ToolExecutionResult{Status: tools.ToolStatusComplete, Content: result, Parts: parts}, nil
}

// ExecuteCall runs a client-bound tool on the client executor, and any other
// on the conversation's call handler for the tool, which receives the whole
// call, or otherwise through Execute.
func (e *localExecutor) ExecuteCall(
	ctx context.Context, descriptor *tools.ToolDescriptor, call tools.ToolCall,
) (json.RawMessage, []types.ContentPart, error) {
	if e.clientBound(descriptor.Name) {
		return e.client.ExecuteCall(ctx, asClientTool(descriptor), call)
	}
	if handler, ok := e.liveCallHandler(descriptor.Name); ok {
		return handler(ctx, call)
	}
	result, err := e.Execute(ctx, descriptor, call.Args)
	return result, nil, err
}

// ExecuteAsync routes a client-bound tool to the client executor, which may
// defer it (ToolStatusPending), and runs every other tool through Execute.
func (e *localExecutor) ExecuteAsync(
	ctx context.Context, descriptor *tools.ToolDescriptor, args json.RawMessage,
) (*tools.ToolExecutionResult, error) {
	if e.clientBound(descriptor.Name) {
		return e.client.ExecuteAsync(ctx, asClientTool(descriptor), args)
	}
	result, err := e.Execute(ctx, descriptor, args)
	if err != nil {
		return nil, err
	}
	return &tools.ToolExecutionResult{Status: tools.ToolStatusComplete, Content: result}, nil
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

func (a *localHandlersAccessor) getCallHandler(name string) (toolCallHandler, bool) {
	a.conv.handlersMu.RLock()
	defer a.conv.handlersMu.RUnlock()
	h, ok := a.conv.callHandlers[name]
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

// liveCtxHandler reads the conversation's current context-aware handler, if the
// executor was built with an accessor.
func (e *localExecutor) liveCtxHandler(name string) (ToolHandlerCtx, bool) {
	if e.live == nil {
		return nil, false
	}
	return e.live.getCtxHandler(name)
}

// liveCallHandler reads the conversation's current call handler, if the
// executor was built with an accessor. There is no build-time snapshot: a call
// handler a later OnToolCtx replaced must not come back from one.
func (e *localExecutor) liveCallHandler(name string) (toolCallHandler, bool) {
	if e.live == nil {
		return nil, false
	}
	return e.live.getCallHandler(name)
}

// liveHandler reads the conversation's current plain handler, if the executor
// was built with an accessor.
func (e *localExecutor) liveHandler(name string) (ToolHandler, bool) {
	if e.live == nil {
		return nil, false
	}
	return e.live.getHandler(name)
}

// Execute dispatches to the appropriate handler based on tool name.
// Context-aware handlers are preferred so that tracing and cancellation propagate.
func (e *localExecutor) Execute(
	ctx context.Context, descriptor *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	if e.clientBound(descriptor.Name) {
		return e.client.Execute(ctx, asClientTool(descriptor), args)
	}

	// Parse args to map
	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	// Prefer context-aware handler; for each kind ask the conversation's live
	// handlers first and fall back to the build-time snapshot. The live map is
	// the same map the snapshot was copied from, only current: consulting the
	// copy first would pin a tool to whatever was registered at Open and
	// silently ignore a later re-registration for that name.
	ctxHandler, ok := e.liveCtxHandler(descriptor.Name)
	if !ok {
		ctxHandler, ok = e.ctxHandlers[descriptor.Name]
	}

	var result any
	var err error
	switch {
	case ok:
		result, err = ctxHandler(ctx, argsMap)
	default:
		handler, hok := e.liveHandler(descriptor.Name)
		if !hok {
			handler, hok = e.handlers[descriptor.Name]
		}
		if !hok {
			return nil, markSentinel(
				fmt.Errorf("no handler registered for tool: %s", descriptor.Name), ErrToolNotRegistered)
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
