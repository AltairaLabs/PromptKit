package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
)

const errSerializeClientToolResult = "failed to serialize client tool result: %w"

// ClientToolRequest contains information about a client-side tool invocation.
// It is passed to handlers registered via [Conversation.OnClientTool].
type ClientToolRequest struct {
	// ToolName is the tool's name as defined in the pack.
	ToolName string

	// CallID is the provider-assigned ID for this particular invocation.
	CallID string

	// Args contains the parsed arguments from the LLM.
	Args map[string]any

	// ConsentMsg is the human-readable consent message from the pack's
	// client.consent.message field. Empty when no consent is configured.
	ConsentMsg string

	// Categories are the semantic consent categories (e.g., ["location"]).
	Categories []string

	// Descriptor provides the full tool descriptor for advanced use cases.
	Descriptor *tools.ToolDescriptor
}

// ClientToolHandler is a function that fulfillls a client-side tool call.
// It receives a context (carrying the tool timeout from ClientConfig.TimeoutMs)
// and a [ClientToolRequest] with the invocation details.
//
// The return value should be JSON-serializable and will be sent back to the
// LLM as the tool result.
type ClientToolHandler func(ctx context.Context, req ClientToolRequest) (any, error)

// OnClientTool registers a handler for a client-side tool.
//
// Client tools (mode: "client") are tools that must be fulfillled on the
// caller's device — for example GPS, camera, or biometric sensors. The
// handler is invoked synchronously when the LLM calls the tool.
//
// Example:
//
//	conv.OnClientTool("get_location", func(ctx context.Context, req sdk.ClientToolRequest) (any, error) {
//	    coords, err := deviceGPS(ctx, req.Args["accuracy"].(string))
//	    if err != nil {
//	        return nil, err
//	    }
//	    return map[string]any{"lat": coords.Lat, "lng": coords.Lng}, nil
//	})
func (c *Conversation) OnClientTool(name string, handler ClientToolHandler) {
	c.clientHandlersMu.Lock()
	defer c.clientHandlersMu.Unlock()
	if c.clientHandlers == nil {
		c.clientHandlers = make(map[string]ClientToolHandler)
	}
	c.clientHandlers[name] = handler
}

// OnClientTools registers multiple client tool handlers at once.
//
//	conv.OnClientTools(map[string]sdk.ClientToolHandler{
//	    "get_location": locationHandler,
//	    "read_contacts": contactsHandler,
//	})
func (c *Conversation) OnClientTools(handlers map[string]ClientToolHandler) {
	c.clientHandlersMu.Lock()
	defer c.clientHandlersMu.Unlock()
	if c.clientHandlers == nil {
		c.clientHandlers = make(map[string]ClientToolHandler)
	}
	for name, handler := range handlers {
		c.clientHandlers[name] = handler
	}
}

// clientExecutor dispatches client-mode tool calls to registered
// ClientToolHandler functions. It implements tools.Executor.
type clientExecutor struct {
	handlers   map[string]ClientToolHandler
	handlersMu *clientHandlersMuAccessor
}

// clientHandlersMuAccessor provides read access to the conversation's
// client handlers under its mutex. This avoids exposing the full
// Conversation to the executor.
type clientHandlersMuAccessor struct {
	conv *Conversation
}

func (a *clientHandlersMuAccessor) getHandler(name string) (ClientToolHandler, bool) {
	a.conv.clientHandlersMu.RLock()
	defer a.conv.clientHandlersMu.RUnlock()
	h, ok := a.conv.clientHandlers[name]
	return h, ok
}

// Name returns "client" to match mode: "client" tools.
func (e *clientExecutor) Name() string {
	return "client"
}

// Execute dispatches to the registered ClientToolHandler for the tool.
// Returns an error if no handler is registered (sync fallback path).
func (e *clientExecutor) Execute(
	ctx context.Context, descriptor *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	// Parse args
	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return nil, fmt.Errorf("failed to parse client tool arguments: %w", err)
	}

	// Look up handler — first from snapshot, then live via mutex accessor
	handler, ok := e.handlers[descriptor.Name]
	if !ok && e.handlersMu != nil {
		handler, ok = e.handlersMu.getHandler(descriptor.Name)
	}
	if !ok {
		return nil, fmt.Errorf("no client handler registered for tool: %s", descriptor.Name)
	}

	return e.executeHandler(ctx, handler, descriptor, argsMap)
}

// ExecuteAsync checks for a registered handler. If one exists it executes
// synchronously and returns ToolStatusComplete. If no handler is registered
// the tool is deferred: a ToolStatusPending result is returned so the
// pipeline suspends and the caller can fulfill the tool via SendToolResult.
func (e *clientExecutor) ExecuteAsync(
	ctx context.Context, descriptor *tools.ToolDescriptor, args json.RawMessage,
) (*tools.ToolExecutionResult, error) {
	// Parse args
	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return nil, fmt.Errorf("failed to parse client tool arguments: %w", err)
	}

	// Look up handler — first from snapshot, then live via mutex accessor
	handler, ok := e.handlers[descriptor.Name]
	if !ok && e.handlersMu != nil {
		handler, ok = e.handlersMu.getHandler(descriptor.Name)
	}

	// No handler → deferred mode: return Pending
	if !ok {
		info := &tools.PendingToolInfo{
			Reason:   "client_tool_deferred",
			Message:  fmt.Sprintf("Client tool %q awaiting caller fulfillment", descriptor.Name),
			ToolName: descriptor.Name,
			Args:     args,
		}
		if descriptor.ClientConfig != nil {
			if descriptor.ClientConfig.Consent != nil {
				info.Message = descriptor.ClientConfig.Consent.Message
			}
			info.Metadata = map[string]any{
				"categories": descriptor.ClientConfig.Categories,
			}
		}
		return &tools.ToolExecutionResult{
			Status:      tools.ToolStatusPending,
			PendingInfo: info,
		}, nil
	}

	// Handler present → execute synchronously (with multimodal support)
	return e.executeHandlerAsync(ctx, handler, descriptor, argsMap)
}

// SendToolResult provides the result for a deferred client tool.
//
// callID must match one of the [PendingClientTool.CallID] values returned in
// the [Response]. result should be JSON-serializable.
//
// After all pending tools have been resolved (via SendToolResult or
// RejectClientTool), call [Conversation.Resume] to continue the pipeline.
func (c *Conversation) SendToolResult(_ context.Context, callID string, result any) error {
	c.mu.RLock()
	closed := c.closed
	c.mu.RUnlock()
	if closed {
		return ErrConversationClosed
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf(errSerializeClientToolResult, err)
	}
	c.resolvedStore.Add(&sdktools.ToolResolution{
		ID:         callID,
		ResultJSON: resultJSON,
	})
	return nil
}

// SendToolResultMultimodal provides a multimodal result for a deferred client tool.
//
// callID must match one of the [PendingClientTool.CallID] values returned in
// the [Response]. parts should contain one or more [types.ContentPart] values
// (text, images, audio, etc.) that will be sent directly to the LLM.
//
// After all pending tools have been resolved (via SendToolResult,
// SendToolResultMultimodal, or RejectClientTool), call [Conversation.Resume]
// to continue the pipeline.
func (c *Conversation) SendToolResultMultimodal(_ context.Context, callID string, parts []types.ContentPart) error {
	c.mu.RLock()
	closed := c.closed
	c.mu.RUnlock()
	if closed {
		return ErrConversationClosed
	}
	if len(parts) == 0 {
		return fmt.Errorf("parts must not be empty")
	}
	c.resolvedStore.Add(&sdktools.ToolResolution{
		ID:    callID,
		Parts: parts,
	})
	return nil
}

// RejectClientTool rejects a deferred client tool with a human-readable reason.
//
// callID must match one of the [PendingClientTool.CallID] values returned in
// the [Response]. The rejection reason is sent to the LLM as the tool result.
func (c *Conversation) RejectClientTool(_ context.Context, callID, reason string) {
	c.mu.RLock()
	closed := c.closed
	c.mu.RUnlock()
	if closed {
		return
	}
	c.resolvedStore.Add(&sdktools.ToolResolution{
		ID:              callID,
		Rejected:        true,
		RejectionReason: reason,
	})
}

// Resume continues pipeline execution after all deferred client tools have
// been resolved via [Conversation.SendToolResult] or [Conversation.RejectClientTool].
//
// The resolved tool results are injected as tool-result messages and a new
// LLM round is triggered. The returned Response contains the assistant's reply.
func (c *Conversation) Resume(ctx context.Context) (*Response, error) {
	startTime := time.Now()

	if err := c.validateSendState(); err != nil {
		return nil, err
	}

	toolMsgs, err := c.buildToolResultMessages()
	if err != nil {
		return nil, err
	}

	// Inject tool results into session history and re-execute
	result, err := c.unarySession.ResumeWithToolResults(ctx, toolMsgs)
	if err != nil {
		return nil, fmt.Errorf("resume failed: %w", err)
	}

	resp := c.buildResponse(result, startTime)

	// Skip lifecycle hooks when the resumed pipeline returns more pending tools.
	if !resp.HasPendingClientTools() {
		c.sessionHooks.IncrementTurn()
		c.sessionHooks.SessionUpdate(ctx)
		c.evalMW.dispatchTurnEvals(ctx)
	}
	return resp, nil
}

// ResumeStream is the streaming equivalent of [Conversation.Resume].
//
// It continues pipeline execution after deferred client tools have been resolved,
// returning a channel of [StreamChunk] values. The final chunk (Type == ChunkDone)
// contains the complete Response.
//
// Example:
//
//	conv.SendToolResult(ctx, "call-1", locationData)
//	for chunk := range conv.ResumeStream(ctx) {
//	    if chunk.Error != nil { break }
//	    fmt.Print(chunk.Text)
//	}
func (c *Conversation) ResumeStream(ctx context.Context) <-chan StreamChunk {
	ch := make(chan StreamChunk, streamChannelBufferSize)

	c.startOTelSession(ctx)

	go func() {
		defer close(ch)
		startTime := time.Now()

		if err := c.validateSendState(); err != nil {
			ch <- StreamChunk{Error: err}
			return
		}

		toolMsgs, err := c.buildToolResultMessages()
		if err != nil {
			ch <- StreamChunk{Error: err}
			return
		}

		streamCh, err := c.unarySession.ResumeStreamWithToolResults(ctx, toolMsgs)
		if err != nil {
			ch <- StreamChunk{Error: fmt.Errorf("resume stream failed: %w", err)}
			return
		}

		state := &streamState{}
		if err := c.processAndFinalizeStreamWithState(ctx, streamCh, ch, startTime, state); err != nil {
			ch <- StreamChunk{Error: err}
			return
		}

		// Skip lifecycle hooks when pipeline is suspended for pending tools
		if len(state.pendingTools) == 0 {
			c.sessionHooks.IncrementTurn()
			c.sessionHooks.SessionUpdate(ctx)
			c.evalMW.dispatchTurnEvals(ctx)
		}
	}()

	return ch
}

// buildToolResultMessages pops all resolved tool results and builds
// tool-result messages. Shared by Resume() and ResumeStream().
// It also emits tool.client.resolved events for each resolution.
func (c *Conversation) buildToolResultMessages() ([]types.Message, error) {
	resolutions := c.resolvedStore.PopAll()
	if len(resolutions) == 0 {
		return nil, fmt.Errorf("no resolved tool results to resume with")
	}

	toolMsgs := make([]types.Message, 0, len(resolutions))
	for _, res := range resolutions {
		var toolResult types.MessageToolResult

		switch {
		case res.Rejected:
			toolResult = types.NewTextToolResult(res.ID, "",
				fmt.Sprintf("Tool rejected: %s", res.RejectionReason))
		case res.Error != nil:
			toolResult = types.NewTextToolResult(res.ID, "",
				fmt.Sprintf("Tool error: %v", res.Error))
		case len(res.Parts) > 0:
			toolResult = types.MessageToolResult{
				ID:    res.ID,
				Parts: res.Parts,
			}
		default:
			toolResult = types.NewTextToolResult(res.ID, "", string(res.ResultJSON))
		}

		toolMsgs = append(toolMsgs, types.NewToolResultMessage(toolResult))
	}

	c.emitClientToolResolvedEvents(resolutions)

	return toolMsgs, nil
}

// emitClientToolResolvedEvents emits a tool.client.resolved event for each
// resolution so that observers see the full request → resolved lifecycle.
func (c *Conversation) emitClientToolResolvedEvents(resolutions []*sdktools.ToolResolution) {
	if c.config.eventBus == nil {
		return
	}
	emitter := events.NewEmitter(c.config.eventBus, "", "", "")
	for _, res := range resolutions {
		status := "fulfilled"
		reason := ""
		if res.Rejected {
			status = "rejected"
			reason = res.RejectionReason
		} else if res.Error != nil {
			status = "error"
		}
		emitter.ClientToolResolved(&events.ClientToolResolvedData{
			CallID:          res.ID,
			Status:          status,
			RejectionReason: reason,
		})
	}
}

// executeHandler runs a ClientToolHandler and returns serialized JSON.
func (e *clientExecutor) executeHandler(
	ctx context.Context, handler ClientToolHandler,
	descriptor *tools.ToolDescriptor, argsMap map[string]any,
) (json.RawMessage, error) {
	req := ClientToolRequest{
		ToolName:   descriptor.Name,
		CallID:     tools.CallIDFromContext(ctx),
		Args:       argsMap,
		Descriptor: descriptor,
	}

	// Populate consent fields from ClientConfig
	if descriptor.ClientConfig != nil {
		if descriptor.ClientConfig.Consent != nil {
			req.ConsentMsg = descriptor.ClientConfig.Consent.Message
		}
		req.Categories = descriptor.ClientConfig.Categories
	}

	// Execute handler
	result, err := handler(ctx, req)
	if err != nil {
		return nil, err
	}

	// Serialize result
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf(errSerializeClientToolResult, err)
	}

	return resultJSON, nil
}

// executeHandlerAsync runs a ClientToolHandler and returns a ToolExecutionResult.
// If the handler returns []types.ContentPart, the parts are stored directly
// instead of being JSON-serialized. Other return types are JSON-serialized as usual.
func (e *clientExecutor) executeHandlerAsync(
	ctx context.Context, handler ClientToolHandler,
	descriptor *tools.ToolDescriptor, argsMap map[string]any,
) (*tools.ToolExecutionResult, error) {
	req := ClientToolRequest{
		ToolName:   descriptor.Name,
		CallID:     tools.CallIDFromContext(ctx),
		Args:       argsMap,
		Descriptor: descriptor,
	}

	if descriptor.ClientConfig != nil {
		if descriptor.ClientConfig.Consent != nil {
			req.ConsentMsg = descriptor.ClientConfig.Consent.Message
		}
		req.Categories = descriptor.ClientConfig.Categories
	}

	result, err := handler(ctx, req)
	if err != nil {
		return nil, err
	}

	// Check for multimodal content parts
	if parts, ok := result.([]types.ContentPart); ok {
		return &tools.ToolExecutionResult{
			Status: tools.ToolStatusComplete,
			Parts:  parts,
		}, nil
	}

	// Default: JSON-serialize result
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf(errSerializeClientToolResult, err)
	}

	return &tools.ToolExecutionResult{
		Status:  tools.ToolStatusComplete,
		Content: resultJSON,
	}, nil
}
