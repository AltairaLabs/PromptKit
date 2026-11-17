package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
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

	// ConsentMsg is the human-readable consent message from the tool
	// descriptor's ClientConfig.Consent.Message. Empty when no consent is configured.
	ConsentMsg string

	// Categories are the semantic consent categories (e.g., ["location"]).
	Categories []string

	// Descriptor provides the full tool descriptor for advanced use cases.
	Descriptor *tools.ToolDescriptor

	// Caller is who the conversation acts for ([WithCaller]); the zero Caller
	// when none was set.
	Caller types.Caller
}

// ClientToolHandler is a function that fulfills a client-side tool call.
// It receives a context (carrying the tool timeout from ClientConfig.TimeoutMs)
// and a [ClientToolRequest] with the invocation details.
//
// The return value should be JSON-serializable and will be sent back to the
// LLM as the tool result.
type ClientToolHandler func(ctx context.Context, req ClientToolRequest) (any, error)

// OnClientTool binds name as a client tool served by handler.
//
// Client tools are tools that must be fulfilled on the caller's device — for
// example GPS, camera, or biometric sensors. A pack does not say how a tool
// executes; this binding is what makes name a client tool in this
// conversation, in Send, Stream and their Resume calls alike. Other
// conversations over the same pack, or the same host tool registry, are
// unaffected. A client binding takes precedence over an [Conversation.OnTool]
// handler for the same name.
//
// The handler is invoked synchronously when the LLM calls the tool. A nil
// handler defers the call to the caller instead; see
// [Conversation.DeferClientTool].
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

// OnClientTools binds several client tools at once, as [Conversation.OnClientTool]
// does for one.
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

// DeferClientTool makes name a client tool that the caller fulfills after the
// turn suspends, rather than through a handler.
//
// When the LLM calls the tool, Send returns a [Response] whose
// [Response.ClientTools] lists the call (Stream emits a [ChunkClientTool]
// chunk). Answer it with [Conversation.SendToolResult],
// [Conversation.RejectClientTool] or [Conversation.FailClientTool], then call
// [Conversation.Resume] (or [Conversation.ResumeStream]).
//
//	conv.DeferClientTool("get_location")
//	resp, _ := conv.Send(ctx, "Where am I?")
//	for _, call := range resp.ClientTools() {
//	    conv.SendToolResult(ctx, call.CallID, coordsFromDevice())
//	}
//	resp, _ = conv.Resume(ctx)
//
// It is the same binding as OnClientTool(name, nil).
func (c *Conversation) DeferClientTool(name string) {
	c.OnClientTool(name, nil)
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

// getHandler returns the binding for name: ok reports whether name is bound
// as a client tool at all, and a nil handler is a deferred binding.
func (a *clientHandlersMuAccessor) getHandler(name string) (ClientToolHandler, bool) {
	a.conv.clientHandlersMu.RLock()
	defer a.conv.clientHandlersMu.RUnlock()
	h, ok := a.conv.clientHandlers[name]
	return h, ok
}

// binding resolves the client binding for name: the conversation's live
// bindings first, then the build-time snapshot. Live comes first so a binding
// made after the pipeline was built (OnClientTool after Open) is seen, and a
// later re-binding of the same name wins over the snapshot. bound reports
// whether name is a client tool; a nil handler means it is deferred.
func (e *clientExecutor) binding(name string) (handler ClientToolHandler, bound bool) {
	if e.handlersMu != nil {
		if h, ok := e.handlersMu.getHandler(name); ok {
			return h, true
		}
	}
	h, ok := e.handlers[name]
	return h, ok
}

// isBound reports whether the host bound name as a client tool, with or
// without a handler.
func (e *clientExecutor) isBound(name string) bool {
	_, bound := e.binding(name)
	return bound
}

// lookupClientHandler resolves the handler that fulfills name synchronously.
// It reports false for an unbound name and for a deferred binding.
func (e *clientExecutor) lookupClientHandler(name string) (ClientToolHandler, bool) {
	h, _ := e.binding(name)
	return h, h != nil
}

// asClientTool returns a copy of d routed as a client tool. A pack tool's
// descriptor says "local" (its mode is a hosting concern, not a pack field);
// the conversation's client binding is what makes it a client tool, so the
// handler sees it as one. The registry's descriptor, which other
// conversations may share, is left untouched.
func asClientTool(d *tools.ToolDescriptor) *tools.ToolDescriptor {
	if d == nil || d.Mode == toolModeClient {
		return d
	}
	cp := *d
	cp.Mode = toolModeClient
	return &cp
}

// toolModeClient is the execution mode of a client tool.
const toolModeClient = "client"

// Name returns "client" to match mode: "client" tools.
func (e *clientExecutor) Name() string {
	return "client"
}

// Execute dispatches to the registered ClientToolHandler for the tool.
// Returns an error if no handler is registered (sync fallback path).
func (e *clientExecutor) Execute(
	ctx context.Context, descriptor *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	result, _, err := e.ExecuteCall(ctx, descriptor, tools.ToolCall{Name: descriptor.Name, Args: args})
	return result, err
}

// ExecuteCall is Execute for the whole call: the handler's request carries the
// call's ID and caller.
func (e *clientExecutor) ExecuteCall(
	ctx context.Context, descriptor *tools.ToolDescriptor, call tools.ToolCall,
) (json.RawMessage, []types.ContentPart, error) {
	argsMap, err := parseClientToolArgs(call.Args)
	if err != nil {
		return nil, nil, err
	}

	// Snapshot first, then live via the mutex accessor.
	handler, ok := e.lookupClientHandler(descriptor.Name)
	if !ok {
		return nil, nil, markSentinel(
			fmt.Errorf("no client handler registered for tool: %s", descriptor.Name), ErrToolNotRegistered)
	}

	result, err := e.executeHandler(ctx, handler, newClientToolRequest(ctx, descriptor, call, argsMap))
	return result, nil, err
}

// ExecuteAsync checks for a registered handler. If one exists it executes
// synchronously and returns ToolStatusComplete. If no handler is registered
// the tool is deferred: a ToolStatusPending result is returned so the
// pipeline suspends and the caller can fulfill the tool via SendToolResult.
func (e *clientExecutor) ExecuteAsync(
	ctx context.Context, descriptor *tools.ToolDescriptor, args json.RawMessage,
) (*tools.ToolExecutionResult, error) {
	return e.ExecuteCallAsync(ctx, descriptor, tools.ToolCall{Name: descriptor.Name, Args: args})
}

// Defer defers a call to tool when the executor has no handler for it
// (tools.DeferringExecutor): it returns the pending result, decided by the
// same lookup ExecuteCallAsync makes, so a handler bound after Defer passed
// is the registry's to run and count normally.
func (e *clientExecutor) Defer(tool *tools.ToolDescriptor, call tools.ToolCall) (*tools.ToolExecutionResult, bool) {
	if _, ok := e.lookupClientHandler(tool.Name); ok {
		return nil, false
	}
	return deferredClientTool(tool, call.Args), true
}

// ExecuteCallAsync is ExecuteAsync for the whole call: a handler's request
// carries the call's ID and caller.
func (e *clientExecutor) ExecuteCallAsync(
	ctx context.Context, descriptor *tools.ToolDescriptor, call tools.ToolCall,
) (*tools.ToolExecutionResult, error) {
	argsMap, err := parseClientToolArgs(call.Args)
	if err != nil {
		return nil, err
	}

	handler, ok := e.lookupClientHandler(descriptor.Name)

	// No handler → deferred mode: return Pending
	if !ok {
		return deferredClientTool(descriptor, call.Args), nil
	}

	// Handler present → execute synchronously (with multimodal support)
	return e.executeHandlerAsync(ctx, handler, newClientToolRequest(ctx, descriptor, call, argsMap))
}

// parseClientToolArgs parses a client tool call's JSON arguments.
func parseClientToolArgs(args json.RawMessage) (map[string]any, error) {
	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return nil, fmt.Errorf("failed to parse client tool arguments: %w", err)
	}
	return argsMap, nil
}

// deferredClientTool is the pending result of a client tool with no handler:
// the caller fulfills it after the turn suspends.
func deferredClientTool(descriptor *tools.ToolDescriptor, args json.RawMessage) *tools.ToolExecutionResult {
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
	}
}

// SendToolResult provides the result for a deferred client tool.
//
// callID must match one of the [PendingClientTool.CallID] values returned in
// the [Response]. result should be JSON-serializable.
//
// After all pending tools have been resolved (via SendToolResult or
// RejectClientTool), call [Conversation.Resume] to continue the pipeline.
func (c *Conversation) SendToolResult(ctx context.Context, callID string, result any) error {
	c.mu.RLock()
	closed := c.closed
	c.mu.RUnlock()
	if closed {
		return ErrConversationClosed
	}
	if awaitErr := c.checkAwaited(ctx, callID); awaitErr != nil {
		return awaitErr
	}
	res, err := clientResolution(callID, result, nil, nil)
	if err != nil {
		return err
	}
	c.addResolution(res)
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
func (c *Conversation) SendToolResultMultimodal(ctx context.Context, callID string, parts []types.ContentPart) error {
	c.mu.RLock()
	closed := c.closed
	c.mu.RUnlock()
	if closed {
		return ErrConversationClosed
	}
	if awaitErr := c.checkAwaited(ctx, callID); awaitErr != nil {
		return awaitErr
	}
	if len(parts) == 0 {
		return fmt.Errorf("parts must not be empty")
	}
	res, err := clientResolution(callID, nil, parts, nil)
	if err != nil {
		return err
	}
	c.addResolution(res)
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
	c.addResolution(&sdktools.ToolResolution{
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
// A result for a call the history already answers, or a second result for the
// same call, is dropped: each call is answered once. So is a result for a
// call the suspended turn did not make. Results stored for a suspended turn
// are discarded when Send or Stream starts a new turn instead of resuming it.
//
// A client's result reaches the model as the JSON the client sent, unformatted
// and not truncated: the stored result is also what an AG-UI consumer is shown
// as TOOL_CALL_RESULT when the history is replayed. It is passed through the
// conversation's hooks.ToolResultRewriter hooks (a failed tool's partial
// output too), so what is stored, and replayed, is the rewritten text. This differs from [Conversation.Continue] and
// [Conversation.ContinueDuplex], which give the model an approved or answered
// result as a tool the pipeline ran would, formatted, rewritten and capped.
func (c *Conversation) Resume(ctx context.Context) (*Response, error) {
	startTime := time.Now()

	release, gateErr := c.turn.acquire(ctx, c)
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	ctx = c.turn.mark(ctx, c)

	if err := c.validateSendState(); err != nil {
		return nil, err
	}

	toolMsgs, err := c.buildToolResultMessages(ctx)
	if err != nil {
		return nil, err
	}

	// Inject tool results into session history and re-execute
	result, err := c.unarySession.ResumeWithToolResults(c.withConversationState(ctx), toolMsgs)
	if err != nil {
		return nil, fmt.Errorf("resume failed: %w", c.wrapTurnError(err))
	}

	resp := c.buildResponse(ctx, result, startTime)

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
//
// As with [Conversation.Stream], drain the channel or cancel ctx: the stream
// holds the conversation's turn until it ends.
func (c *Conversation) ResumeStream(ctx context.Context) <-chan StreamChunk {
	ch := make(chan StreamChunk, streamChannelBufferSize)

	c.startOTelSession(ctx)

	go func() {
		defer close(ch)
		startTime := time.Now()

		release, gateErr := c.turn.acquire(ctx, c)
		if gateErr != nil {
			ch <- StreamChunk{Error: gateErr}
			return
		}
		defer release()
		ctx = c.turn.mark(ctx, c)

		if err := c.validateSendState(); err != nil {
			ch <- StreamChunk{Error: err}
			return
		}

		toolMsgs, err := c.buildToolResultMessages(ctx)
		if err != nil {
			ch <- StreamChunk{Error: err}
			return
		}

		streamCh, err := c.unarySession.ResumeStreamWithToolResults(c.withConversationState(ctx), toolMsgs)
		if err != nil {
			ch <- StreamChunk{Error: fmt.Errorf("resume stream failed: %w", c.wrapTurnError(err))}
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
func (c *Conversation) buildToolResultMessages(ctx context.Context) ([]types.Message, error) {
	history, err := c.settlementHistory(ctx)
	if err != nil {
		return nil, err
	}
	resolutions := c.takeSettlement(history)
	if len(resolutions) == 0 {
		return nil, fmt.Errorf("no resolved tool results to resume with")
	}
	calls := callsIn(history)
	callNames := callNamesOf(calls)
	c.validateDeferredResults(resolutions, callNames)
	c.admitDeferredResults(resolutions, callNames)

	toolMsgs := make([]types.Message, 0, len(resolutions))
	for _, res := range resolutions {
		toolMsgs = append(toolMsgs, types.NewToolResultMessage(c.resumeResult(ctx, calls, res)))
	}
	c.settled(resolutions)

	c.emitClientToolResolvedEvents(resolutions)

	return toolMsgs, nil
}

// callNamesOf maps each call calls returns to the name of the tool it called,
// on first use only.
func callNamesOf(calls func() map[string]types.MessageToolCall) func() map[string]string {
	var names map[string]string
	return func() map[string]string {
		if names == nil {
			byID := calls()
			names = make(map[string]string, len(byID))
			for id := range byID {
				names[id] = byID[id].Name
			}
		}
		return names
	}
}

// rejectionText is what the model is told about a rejected client tool call.
// When the tool's consent config declares a decline strategy, it is the
// strategy's text (tools.ConsentDeclinedMessage, as PromptArena words it)
// followed by the caller's reason; otherwise it is "Tool rejected: <reason>".
func (c *Conversation) rejectionText(toolName, reason string) string {
	strategy := c.declineStrategy(toolName)
	if strategy == "" {
		return fmt.Sprintf("Tool rejected: %s", reason)
	}
	msg := tools.ConsentDeclinedMessage(toolName, strategy)
	if reason == "" {
		return msg
	}
	return strings.TrimSuffix(msg, ".") + ". Reason: " + reason
}

// declineStrategy is the decline strategy toolName's descriptor declares, or
// "" when it declares none or the tool is unknown.
func (c *Conversation) declineStrategy(toolName string) string {
	if c.toolRegistry == nil || toolName == "" {
		return ""
	}
	desc := c.toolRegistry.Get(toolName)
	if desc == nil || desc.ClientConfig == nil || desc.ClientConfig.Consent == nil {
		return ""
	}
	return desc.ClientConfig.Consent.DeclineStrategy
}

// clientResultValidator checks caller-supplied client tool results. It is
// safe for concurrent use and caches compiled schemas.
var clientResultValidator = tools.NewSchemaValidator()

// validateDeferredResults checks each caller-supplied JSON result against its
// tool's output schema when the tool's client config sets validate_output. A
// result that does not match becomes a failure, so the model is told the call
// failed and why, as it is when a handler registered with OnClientTool
// returns an invalid result.
func (c *Conversation) validateDeferredResults(
	resolutions []*sdktools.ToolResolution, callNames func() map[string]string,
) {
	if c.toolRegistry == nil || c.getBaseSession() == nil {
		return
	}
	for _, res := range resolutions {
		// An approval's result was checked when it ran (checkApprovedResult):
		// only sdktools.ResolveApproved sets Arguments. One SettleClientTool
		// prepared was checked then.
		if res.Rejected || res.Error != nil || len(res.Parts) > 0 || res.Arguments != nil || c.isPrepared(res) {
			continue
		}
		desc, err := c.toolRegistry.GetTool(callNames()[res.ID])
		if err != nil || desc.ClientConfig == nil || !desc.ClientConfig.ValidateOutput {
			continue
		}
		validated, _, err := clientResultValidator.CoerceResult(desc, res.ResultJSON)
		if err != nil {
			res.ResultJSON = nil
			res.Error = fmt.Errorf("result validation failed: %w", err)
			continue
		}
		res.ResultJSON = validated
	}
}

// admitDeferredResults admits, under ToolPolicy.MaxCallsPerMinute and the
// tool registry's own rate limit, each deferred call (a client tool, or any
// call its executor left pending) whose result or failure is being used. A result over the limit is refused: the
// limit error replaces it, and the model is told the call was not executed.
// A rejection is never counted. An approval is skipped, because
// ResolveToolWithArgs admitted it (only sdktools.ResolveApproved sets
// Arguments), and so is an answer SettleClientTool admitted.
func (c *Conversation) admitDeferredResults(
	resolutions []*sdktools.ToolResolution, callNames func() map[string]string,
) {
	limit := c.callRateLimit()
	for _, res := range resolutions {
		if res.Rejected || res.Arguments != nil || c.isPrepared(res) {
			continue
		}
		name := callNames()[res.ID]
		if name == "" {
			continue
		}
		if err := c.admitSettled(name, limit); err != nil {
			res.Result, res.ResultJSON, res.Parts, res.Error = nil, nil, nil, err
		}
	}
}

// admitSettled admits the deferred call to tool name whose result is being
// used: under the tool registry's own rate limit (tools.Registry.AdmitSettled,
// which counts a deferred call only now) and under ToolPolicy.MaxCallsPerMinute
// (limit, when set). Neither counts a call the other refuses.
func (c *Conversation) admitSettled(name string, limit int) error {
	_, err := c.admitSettledRelease(name, limit)
	return err
}

// admitSettledRelease is admitSettled, also returning release, which takes
// back what both limits counted, for an answer then discarded unused.
func (c *Conversation) admitSettledRelease(name string, limit int) (release func(), err error) {
	var windowRelease func()
	var admit func() error
	if c.callRate != nil && limit > 0 {
		admit = func() error {
			r, admitErr := c.callRate.AdmitRelease(c.ID(), name, limit)
			windowRelease = r
			return admitErr
		}
	}
	var registryRelease func()
	switch {
	case c.toolRegistry != nil:
		registryRelease, err = c.toolRegistry.AdmitSettledRelease(name, admit)
	case admit != nil:
		err = admit()
	}
	if err != nil {
		return nil, err
	}
	return func() {
		if registryRelease != nil {
			registryRelease()
		}
		if windowRelease != nil {
			windowRelease()
		}
	}, nil
}

// callsMadeByLastAssistant maps each call the last assistant message in
// history made, by call ID, to the call.
func callsMadeByLastAssistant(history []types.Message) map[string]types.MessageToolCall {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != roleAssistant {
			continue
		}
		calls := make(map[string]types.MessageToolCall, len(history[i].ToolCalls))
		for _, tc := range history[i].ToolCalls {
			calls[tc.ID] = tc
		}
		return calls
	}
	return map[string]types.MessageToolCall{}
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
			status = clientToolStatusError
		}
		emitter.ClientToolResolved(&events.ClientToolResolvedData{
			CallID:          res.ID,
			Status:          status,
			RejectionReason: reason,
		})
	}
}

// newClientToolRequest is the request a ClientToolHandler receives for call.
// A call without an ID takes the one on ctx (tools.WithCallID).
func newClientToolRequest(
	ctx context.Context, descriptor *tools.ToolDescriptor, call tools.ToolCall, argsMap map[string]any,
) ClientToolRequest {
	callID := call.ID
	if callID == "" {
		callID = tools.CallIDFromContext(ctx)
	}
	req := ClientToolRequest{
		ToolName:   descriptor.Name,
		CallID:     callID,
		Args:       argsMap,
		Descriptor: descriptor,
		Caller:     call.Caller,
	}
	if descriptor.ClientConfig != nil {
		if descriptor.ClientConfig.Consent != nil {
			req.ConsentMsg = descriptor.ClientConfig.Consent.Message
		}
		req.Categories = descriptor.ClientConfig.Categories
	}
	return req
}

// executeHandler runs a ClientToolHandler and returns serialized JSON.
func (e *clientExecutor) executeHandler(
	ctx context.Context, handler ClientToolHandler, req ClientToolRequest,
) (json.RawMessage, error) {
	result, err := handler(ctx, req)
	if err != nil {
		return nil, err
	}

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
	ctx context.Context, handler ClientToolHandler, req ClientToolRequest,
) (*tools.ToolExecutionResult, error) {
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

// unansweredResolutions drops the resolutions that would answer a call twice:
// one for a call the suspended round already holds a result for (a server-side
// tool that ran in the same round as the client call), and any second
// resolution for the same call, keeping the first. A provider rejects a
// history in which one call is answered twice.
//
// Only results after the last assistant message count: that message made the
// calls the turn is suspended on. Call ids are unique only within a response
// (Gemini numbers them call_0, call_1, ...), so an earlier turn's result for
// the same id answers a different call.
func unansweredResolutions(
	history []types.Message, resolutions []*sdktools.ToolResolution,
) []*sdktools.ToolResolution {
	answered := map[string]bool{}
	for _, id := range answeredSinceLastAssistant(history) {
		answered[id] = true
	}
	made := callsOfLastAssistant(history)
	out := make([]*sdktools.ToolResolution, 0, len(resolutions))
	for _, res := range resolutions {
		if answered[res.ID] {
			logger.Debug("dropping a second answer for a tool call", "call_id", res.ID)
			continue
		}
		if len(made) > 0 && !made[res.ID] {
			logger.Warn("dropping an answer for a call the suspended turn did not make", "call_id", res.ID)
			continue
		}
		answered[res.ID] = true
		out = append(out, res)
	}
	return out
}

// checkAwaited returns ErrToolCallNotAwaited, naming callID, when the
// suspended turn did not make that call (it belongs to a turn that was
// continued or abandoned), and nil when it did or the conversation cannot
// tell. An answer for a call the turn made but already answered is not
// refused here: it is a duplicate, and Continue and Resume drop it.
func (c *Conversation) checkAwaited(ctx context.Context, callID string) error {
	if c.mode != UnaryMode || c.unarySession == nil {
		return nil
	}
	return c.checkAwaitedIn(c.Messages(ctx), callID)
}

// checkAwaitedIn is checkAwaited against history, read by the caller.
func (c *Conversation) checkAwaitedIn(history []types.Message, callID string) error {
	if c.mode != UnaryMode || c.unarySession == nil {
		return nil
	}
	made := callsOfLastAssistant(history)
	if made == nil || made[callID] {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrToolCallNotAwaited, callID)
}

// takeSettlement pops the stored answers for the suspended turn, dropping
// the ones that would answer a call twice or answer a call the turn did not
// make.
func (c *Conversation) takeSettlement(history []types.Message) []*sdktools.ToolResolution {
	popped := c.resolvedStore.PopAll()
	kept := unansweredResolutions(history, popped)
	if len(kept) < len(popped) {
		c.dropPrepared(popped, kept)
	}
	return kept
}

// dropPrepared forgets the results SettleClientTool prepared for the
// resolutions in popped that kept does not hold, answers not used, and takes
// back their admissions.
func (c *Conversation) dropPrepared(popped, kept []*sdktools.ToolResolution) {
	used := make(map[*sdktools.ToolResolution]bool, len(kept))
	for _, res := range kept {
		used[res] = true
	}
	var releases []func()
	c.settledMu.Lock()
	for _, res := range popped {
		if p, ok := c.prepared[res]; ok && !used[res] {
			releases = append(releases, p.release)
			delete(c.prepared, res)
		}
	}
	c.settledMu.Unlock()
	runReleases(releases)
}

// settlementHistory reads the conversation's history once for a batch of
// settled results: what the batch is checked against and the calls it
// answers. A store that cannot be read fails the batch, rather than reading
// as an empty history.
func (c *Conversation) settlementHistory(ctx context.Context) ([]types.Message, error) {
	s := c.getBaseSession()
	if s == nil {
		return nil, nil
	}
	history, err := s.Messages(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read the conversation history: %w", err)
	}
	return history, nil
}

// callsIn returns a function that maps each call the last assistant message in
// history made, by call ID, to the call itself.
func callsIn(history []types.Message) func() map[string]types.MessageToolCall {
	calls := callsMadeByLastAssistant(history)
	return func() map[string]types.MessageToolCall { return calls }
}

// callsOfLastAssistant returns the ids of the calls the last assistant message
// in history made: the calls a suspended turn is waiting on.
func callsOfLastAssistant(history []types.Message) map[string]bool {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != roleAssistant {
			continue
		}
		made := make(map[string]bool, len(history[i].ToolCalls))
		for _, tc := range history[i].ToolCalls {
			made[tc.ID] = true
		}
		return made
	}
	return nil
}

// discardAbandonedResolutions drops answers stored for a suspended turn when
// a new turn starts instead of resuming it. They answer calls of the turn the
// user moved on from; resumed later, they would reach the model as results
// for calls the current turn never made, or, where a provider reuses call ids
// (call_0, call_1, ...), in place of the real answer.
func (c *Conversation) discardAbandonedResolutions() {
	if c.resolvedStore == nil {
		return
	}
	if dropped := c.resolvedStore.PopAll(); len(dropped) > 0 {
		logger.Debug("discarding tool answers for an abandoned suspended turn", "count", len(dropped))
	}
	c.forgetSettled()
}

// answeredSinceLastAssistant lists the calls answered by tool results after the
// last assistant message in history.
func answeredSinceLastAssistant(history []types.Message) []string {
	var ids []string
	for i := len(history) - 1; i >= 0 && history[i].Role != roleAssistant; i-- {
		if history[i].ToolResult != nil {
			ids = append(ids, history[i].ToolResult.ID)
		}
	}
	return ids
}

// FailClientTool reports that a deferred client tool failed.
//
// callID must match one of the [PendingClientTool.CallID] values returned in
// the [Response]. partial, when not nil, is the output the tool produced
// before failing and must be JSON-serializable. The model is told about the
// failure, the stored tool result carries err as its Error, and the
// tool.client.resolved event reports the call as an error. Call
// [Conversation.Resume] once every pending tool is resolved.
func (c *Conversation) FailClientTool(ctx context.Context, callID string, partial any, err error) error {
	c.mu.RLock()
	closed := c.closed
	c.mu.RUnlock()
	if closed {
		return ErrConversationClosed
	}
	if awaitErr := c.checkAwaited(ctx, callID); awaitErr != nil {
		return awaitErr
	}
	if err == nil {
		return fmt.Errorf("FailClientTool needs an error")
	}
	res, resErr := clientResolution(callID, partial, nil, err)
	if resErr != nil {
		return resErr
	}
	c.addResolution(res)
	return nil
}

// addResolution records res, an answer to a held or deferred call, for
// Continue or Resume to use, under settleMu: SettleClientTool's check for an
// earlier answer and its record of its own are one step no other answer can
// come between.
func (c *Conversation) addResolution(res *sdktools.ToolResolution) {
	c.settleMu.Lock()
	defer c.settleMu.Unlock()
	c.resolvedStore.Add(res)
}

// clientToolStatusError is the resolved-event status for a client tool that
// failed.
const clientToolStatusError = "error"
