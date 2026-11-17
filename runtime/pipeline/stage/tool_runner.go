package stage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// toolRunnerConfig is the part of ProviderConfig the tool runner reads.
type toolRunnerConfig struct {
	ApprovalGate    tools.ApprovalGate
	ApprovalChecker tools.ApprovalChecker
	// CallRate is the window ToolPolicy.MaxCallsPerMinute counts calls in:
	// the stage's resolved window (ProviderConfig.CallRate, or its own).
	CallRate     *CallRateWindow
	Caller       types.Caller
	ToolGrants   func() []string
	DescribeTool func(*tools.ToolDescriptor) *tools.ToolDescriptor
}

// toolRunner executes a round's tool calls: policy and hook checks, the
// approval gate, the call-rate window, the registry call, result shaping and
// the tool events. It holds no turn state of its own.
type toolRunner struct {
	registry *tools.Registry
	// policy returns the tool policy in force for the turn.
	policy        func() *pipeline.ToolPolicy
	emitter       *events.Emitter
	hookRegistry  *hooks.Registry
	turnState     *TurnState
	cfg           toolRunnerConfig
	stateResolver WorkflowStateResolver
	// offers is the offer state of the stage that built the runner: it
	// outlives the runner, which is rebuilt on each use.
	offers *toolOffer
	// availability takes a round's snapshot of what the model may call (see
	// availability). Nil, or a nil snapshot, refuses nothing: the provider is
	// never sent tools.
	availability func() availability
}

// availability is one round's answer to whether a call to a registered tool
// may run: refused with the refusal's text when not. A round takes it once,
// before any of its calls runs, so a grant or a revocation one call makes
// applies from the next round.
type availability func(name string) (errMsg string, refused bool)

// notAvailableRefusal is the refusal of a call to a registered tool the
// model may not call this turn.
func notAvailableRefusal(name string) string {
	return fmt.Sprintf("Tool %s was not run: it is not available this turn", name)
}

// roundAvailability takes the round's availability snapshot, or nil.
func (r *toolRunner) roundAvailability() availability {
	if r.availability == nil {
		return nil
	}
	return r.availability()
}

// unavailable reports whether avail refuses a call to name, and the refusal.
// A name the registry lacks is not refused here: it keeps its not-found
// error. A non-nil snapshot is only taken with a registry.
func (r *toolRunner) unavailable(avail availability, name string) (string, bool) {
	if avail == nil || r.registry.Get(name) == nil {
		return "", false
	}
	return avail(name)
}

// availableCalls returns calls less those avail refuses.
func (r *toolRunner) availableCalls(calls []types.MessageToolCall, avail availability) []types.MessageToolCall {
	if avail == nil {
		return calls
	}
	out := make([]types.MessageToolCall, 0, len(calls))
	for _, tc := range calls {
		if _, refused := r.unavailable(avail, tc.Name); !refused {
			out = append(out, tc)
		}
	}
	return out
}

// toolCallResult holds the outcome of a single tool call execution,
// including its original index for preserving result ordering.
type toolCallResult struct {
	index   int
	message types.Message
	pending *tools.PendingToolExecution // non-nil if tool returned pending status
	// unavailable marks a call refused because the model may not call the
	// tool: it did not run, and no call budget counts it.
	unavailable bool
}

// toolLabels returns the Labels from the ToolDescriptor for the given tool name,
// or nil if the tool is not found or has no labels.
func (r *toolRunner) toolLabels(name string) map[string]string {
	if r.registry == nil {
		return nil
	}
	desc := r.registry.Get(name)
	if desc == nil {
		return nil
	}
	return desc.Labels
}

// toolDeclaration returns the pack declaration the registry holds for a tool,
// or nil when the tool is undeclared or unknown.
func (r *toolRunner) toolDeclaration(name string) *packspec.Tool {
	if r.registry == nil {
		return nil
	}
	if desc := r.registry.Get(name); desc != nil {
		return desc.Declaration
	}
	return nil
}

// recordToolCalls reports a round's executed tool-call count to the resolver
// when it implements ToolCallRecorder, feeding the workflow's max_tool_calls
// budget. No-op without a resolver, or when the resolver does not enforce a
// budget — the ordinary non-workflow case.
func (r *toolRunner) recordToolCalls(n int) {
	if n <= 0 || r.stateResolver == nil {
		return
	}
	if rec, ok := r.stateResolver.(ToolCallRecorder); ok {
		rec.RecordToolCalls(n)
	}
}

// getMaxParallelToolCalls returns the max concurrency for parallel tool execution.
func (r *toolRunner) getMaxParallelToolCalls() int {
	if p := r.policy(); p != nil && p.MaxParallelToolCalls > 0 {
		return p.MaxParallelToolCalls
	}
	return defaultMaxParallelToolCalls
}

// executeToolCalls runs a round's calls, checking each against avail (the
// round's snapshot; nil checks nothing), and returns their results in call
// order, less those held pending.
func (r *toolRunner) executeToolCalls(
	ctx context.Context, toolCalls []types.MessageToolCall, rr roundRef, avail availability,
) ([]types.Message, error) {
	if r.registry == nil {
		return nil, errors.New("tool registry not configured but tool calls present")
	}

	// Running tools is activity, so hold the idle timer open until they finish.
	// Each tool is still bounded by its own TimeoutMs.
	defer keepIdleAlive(ctx)()

	resultSlots := make([]toolCallResult, len(toolCalls))
	var mu sync.Mutex
	var pendingTools []tools.PendingToolExecution

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(r.getMaxParallelToolCalls())

	for i, tc := range toolCalls {
		idx := i
		toolCall := tc

		g.Go(func() error {
			result := r.executeSingleToolCall(gctx, toolCall, rr, avail)
			mu.Lock()
			defer mu.Unlock()
			result.index = idx
			resultSlots[idx] = result
			if result.pending != nil {
				pendingTools = append(pendingTools, *result.pending)
			}
			return nil
		})
	}

	// errgroup goroutines always return nil, so this only errors on ctx cancel.
	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Collect results in original order, excluding pending entries.
	results := make([]types.Message, 0, len(toolCalls))
	unavailable := 0
	for i := range resultSlots {
		if resultSlots[i].pending != nil {
			continue
		}
		if resultSlots[i].unavailable {
			unavailable++
		}
		results = append(results, resultSlots[i].message)
	}

	// Report what actually ran for the workflow's max_tool_calls budget. results
	// excludes pending calls, which have not executed and are counted when they
	// run on resume; errored and policy-blocked calls are included, since both
	// consumed a round. A call to a tool the model may not call is not.
	r.recordToolCalls(len(results) - unavailable)

	if len(pendingTools) > 0 {
		return results, &tools.ErrToolsPending{Pending: pendingTools}
	}

	return results, nil
}

// preExecCheck runs policy and hook checks before tool execution.
// Returns (hookDecision, blocked result, shouldSkip).
// When shouldSkip is true the caller should return the blocked result directly.
func (r *toolRunner) preExecCheck(
	ctx context.Context, toolCall types.MessageToolCall, toolReq hooks.ToolRequest,
) (hooks.Decision, toolCallResult, bool) {
	if p := r.policy(); p != nil && isToolBlocked(toolCall.Name, p.Blocklist) {
		errMsg := fmt.Sprintf("Tool %s is blocked by policy", toolCall.Name)
		return hooks.Decision{}, toolCallResult{
			message: toolRefusal(toolCall, errMsg),
		}, true
	}

	var hookDecision hooks.Decision
	if r.hookRegistry != nil {
		hookDecision = r.hookRegistry.RunBeforeToolExecution(ctx, toolReq)
		if !hookDecision.Allow {
			errMsg := fmt.Sprintf(
				"Tool %s blocked by hook: %s", toolCall.Name, hookDecision.Reason,
			)
			msg := toolRefusal(toolCall, errMsg)
			if hookDecision.Metadata != nil {
				msg.Meta = hookDecision.Metadata
			}
			return hookDecision, toolCallResult{message: msg}, true
		}
	}
	return hookDecision, toolCallResult{}, false
}

// holdForApproval is the human-in-the-loop approval gate: if a checker holds
// this call, it is surfaced as pending (via ErrToolsPending / PendingTools
// metadata) instead of executing, and ok is true. The caller resolves it
// (approve / approve-with-edits / reject) and resumes.
//
// A gate that refuses the call (ApprovalGate returned an error) also sets ok:
// the call does not run, and its result is the refusal.
func (r *toolRunner) holdForApproval(
	ctx context.Context, toolCall types.MessageToolCall, rr roundRef,
) (toolCallResult, bool) {
	gate := r.approvalGate()
	if gate == nil {
		return toolCallResult{}, false
	}
	var argsMap map[string]any
	if len(toolCall.Args) > 0 {
		_ = json.Unmarshal(toolCall.Args, &argsMap)
	}
	info, err := gate(ctx, toolCall.ID, toolCall.Name, argsMap)
	if err != nil {
		return r.approvalRefused(ctx, toolCall, err, rr), true
	}
	if info == nil {
		return toolCallResult{}, false
	}
	if info.ToolName == "" {
		info.ToolName = toolCall.Name
	}
	return r.buildPendingResult(ctx, toolCall, &tools.ToolExecutionResult{
		Status:      tools.ToolStatusPending,
		PendingInfo: info,
	}), true
}

// approvalGate is the configured approval gate: ApprovalGate when set, else
// ApprovalChecker as a gate that never refuses, else nil.
func (r *toolRunner) approvalGate() tools.ApprovalGate {
	if r.cfg.ApprovalGate != nil {
		return r.cfg.ApprovalGate
	}
	if checker := r.cfg.ApprovalChecker; checker != nil {
		return func(ctx context.Context, callID, name string, args map[string]any) (*tools.PendingToolInfo, error) {
			return checker(ctx, callID, name, args), nil
		}
	}
	return nil
}

// approvalRefused is the result of a call the approval gate refused: an
// approval error naming the cause, which the model is given in place of the
// call's result. A tool.call.started event and a tool.call.failed event
// report it.
func (r *toolRunner) approvalRefused(
	ctx context.Context, toolCall types.MessageToolCall, cause error, rr roundRef,
) toolCallResult {
	errMsg := fmt.Sprintf("Tool %s was not run: the approval gate refused it: %v", toolCall.Name, cause)
	return r.refuseCall(ctx, toolCall, errMsg, cause, rr)
}

// refuseCall is the result of a call refused before it ran: an approval
// error, which the model is given in place of the call's result. A
// tool.call.started event and a tool.call.failed event naming cause report
// it.
func (r *toolRunner) refuseCall(
	ctx context.Context, toolCall types.MessageToolCall, errMsg string, cause error, rr roundRef,
) toolCallResult {
	labels := r.toolLabels(toolCall.Name)
	r.emitToolStarted(ctx, toolCall, labels, rr) // paired with the failure, as on every other failure path
	r.emitToolFailed(ctx, toolCall, cause, time.Now(), labels, rr)
	return toolCallResult{message: toolRefusal(toolCall, errMsg)}
}

// toolRefusal is the tool result answering a call that did not run: errMsg
// as both its text and its error, classified as an approval error (a policy,
// hook, gate, call-budget or offer refusal).
func toolRefusal(tc types.MessageToolCall, errMsg string) types.Message {
	result := types.NewTextToolResult(tc.ID, tc.Name, errMsg)
	result.Error = errMsg
	result.ErrorType = types.ToolErrorApproval
	return types.NewToolResultMessage(result)
}

// checkCallable answers a call the round may not run before the approval
// gate is asked about it: a tool the model may not call (avail), or a name
// no tool is registered under.
func (r *toolRunner) checkCallable(
	ctx context.Context, toolCall types.MessageToolCall, rr roundRef, avail availability,
) (toolCallResult, bool) {
	if errMsg, refused := r.unavailable(avail, toolCall.Name); refused {
		logger.Warn("refused a call to a tool the model may not call this turn", "tool", toolCall.Name)
		res := r.refuseCall(ctx, toolCall, errMsg, errors.New(errMsg), rr)
		res.unavailable = true
		return res, true
	}
	if _, err := r.registry.GetTool(toolCall.Name); err != nil {
		r.emitToolStarted(ctx, toolCall, nil, rr) // paired with the failure, as on every other failure path
		return r.toolCallFailed(ctx, toolCall, err, time.Now(), nil, rr), true
	}
	return toolCallResult{}, false
}

// executeSingleToolCall handles policy checks, hooks, execution, and event
// emission for a single tool call. It never returns an error — failures are
// captured as error results in the returned message, matching the previous
// sequential behavior where one tool failure does not cancel others.
func (r *toolRunner) executeSingleToolCall(
	ctx context.Context,
	toolCall types.MessageToolCall,
	rr roundRef,
	avail availability,
) toolCallResult {
	toolReq := r.toolRequest(toolCall)
	hookDecision, blocked, skip := r.preExecCheck(ctx, toolCall, toolReq)
	if skip {
		return blocked
	}
	if refused, ok := r.checkCallable(ctx, toolCall, rr, avail); ok {
		return refused
	}

	if held, ok := r.holdForApproval(ctx, toolCall, rr); ok {
		return held // admitted under ToolPolicy.MaxCallsPerMinute when the host approves it
	}

	labels := r.toolLabels(toolCall.Name)
	r.emitToolStarted(ctx, toolCall, labels, rr)

	startTime := time.Now()
	ctx = tools.WithCallID(ctx, toolCall.ID)
	// The registry admits the call under ToolPolicy.MaxCallsPerMinute once its
	// own checks pass, immediately before the executor runs it.
	admission := r.callRateAdmission(toolCall)
	asyncResult, err := r.registry.ExecuteCallAsyncAdmitted(ctx, r.registryCall(toolCall), admission.admit)
	ResetIdleFromContext(ctx)
	if refusal, limited := callRateRefusal(toolCall, err); limited {
		r.emitToolFailed(ctx, toolCall, err, startTime, labels, rr)
		return toolCallResult{message: refusal}
	}
	if err != nil {
		return r.toolCallFailed(ctx, toolCall, err, startTime, labels, rr)
	}

	if asyncResult.Status == tools.ToolStatusPending {
		admission.withdraw() // a deferred call is admitted when its result is used
		return r.buildPendingResult(ctx, toolCall, asyncResult)
	}

	result, err := r.buildToolResult(toolCall, asyncResult, r.resultRewriter(ctx, toolReq))
	if err != nil {
		return r.toolCallFailed(ctx, toolCall, err, startTime, labels, rr)
	}
	if r.emitter != nil {
		r.emitter.ToolCallEventCtx(ctx, events.EventToolCallCompleted, &events.ToolCallEventData{
			ToolName:       toolCall.Name,
			CallID:         toolCall.ID,
			Duration:       time.Since(startTime),
			Status:         string(asyncResult.Status),
			Parts:          result.Parts,
			Labels:         labels,
			Round:          rr.round,
			ProviderCallID: rr.providerCallID,
		})
	}
	resultMsg := types.NewToolResultMessage(result)
	if hookDecision.Metadata != nil {
		resultMsg.Meta = hookDecision.Metadata
	}

	r.runAfterToolHooks(ctx, toolReq, result, startTime)

	return toolCallResult{message: resultMsg}
}

// toolRequest is the hook request for a tool call, built once per call.
func (r *toolRunner) toolRequest(toolCall types.MessageToolCall) hooks.ToolRequest {
	if r.hookRegistry == nil {
		return hooks.ToolRequest{}
	}
	return hooks.ToolRequest{
		Name: toolCall.Name, Args: toolCall.Args, CallID: toolCall.ID,
		Declaration: r.toolDeclaration(toolCall.Name),
		Caller:      r.caller(),
	}
}

// registryCall is the registry request for a tool call: the call with the
// conversation's caller.
func (r *toolRunner) registryCall(toolCall types.MessageToolCall) tools.ToolCall {
	return tools.ToolCall{Name: toolCall.Name, Args: toolCall.Args, ID: toolCall.ID, Caller: r.caller()}
}

// caller is who the conversation acts for (ProviderConfig.Caller).
func (r *toolRunner) caller() types.Caller {
	return r.cfg.Caller
}

// toolCallFailed emits tool.call.failed and returns the error result the
// model sees.
func (r *toolRunner) toolCallFailed(
	ctx context.Context, toolCall types.MessageToolCall, err error,
	startTime time.Time, labels map[string]string, rr roundRef,
) toolCallResult {
	r.emitToolFailed(ctx, toolCall, err, startTime, labels, rr)
	errResult := types.NewTextToolResult(toolCall.ID, toolCall.Name, fmt.Sprintf("Error: %v", err))
	errResult.Error = err.Error()
	errResult.ErrorType = classifyToolError(err)
	return toolCallResult{
		message: types.NewToolResultMessage(errResult),
	}
}

// emitToolFailed emits tool.call.failed.
func (r *toolRunner) emitToolFailed(
	ctx context.Context, toolCall types.MessageToolCall, err error,
	startTime time.Time, labels map[string]string, rr roundRef,
) {
	if r.emitter == nil {
		return
	}
	r.emitter.ToolCallEventCtx(ctx, events.EventToolCallFailed, &events.ToolCallEventData{
		ToolName:       toolCall.Name,
		CallID:         toolCall.ID,
		Error:          err,
		Duration:       time.Since(startTime),
		Labels:         labels,
		Round:          rr.round,
		ProviderCallID: rr.providerCallID,
	})
}

// resultRewriter returns the function that passes a result's text through
// the hook registry's ToolResultRewriters, or nil when there are none.
func (r *toolRunner) resultRewriter(
	ctx context.Context, toolReq hooks.ToolRequest,
) func(string) (string, error) {
	if !r.hookRegistry.HasToolResultRewriters() {
		return nil
	}
	return func(content string) (string, error) {
		return r.hookRegistry.RunToolResultRewriters(ctx, toolReq, content)
	}
}

// emitToolStarted emits the tool call started event if an emitter is configured.
func (r *toolRunner) emitToolStarted(
	ctx context.Context, toolCall types.MessageToolCall, labels map[string]string, rr roundRef,
) {
	if r.emitter == nil {
		return
	}
	var argsMap map[string]interface{}
	if toolCall.Args != nil {
		_ = json.Unmarshal(toolCall.Args, &argsMap)
	}
	r.emitter.ToolCallEventCtx(ctx, events.EventToolCallStarted, &events.ToolCallEventData{
		ToolName:       toolCall.Name,
		CallID:         toolCall.ID,
		Args:           argsMap,
		Labels:         labels,
		Round:          rr.round,
		ProviderCallID: rr.providerCallID,
	})
}

// buildPendingResult creates a toolCallResult for a pending tool execution.
// It emits a tool.client.request event so observers know a client tool is
// awaiting fulfillment, and a tool.call.completed with status "pending" so
// every tool.call.started has a matching completion.
func (r *toolRunner) buildPendingResult(
	ctx context.Context, toolCall types.MessageToolCall, asyncResult *tools.ToolExecutionResult,
) toolCallResult {
	var argsMap map[string]any
	if toolCall.Args != nil {
		_ = json.Unmarshal(toolCall.Args, &argsMap)
	}

	// Emit client tool request event with consent/category metadata
	if r.emitter != nil {
		reqData := &events.ClientToolRequestData{
			CallID:   toolCall.ID,
			ToolName: toolCall.Name,
			Args:     argsMap,
		}
		if asyncResult.PendingInfo != nil {
			reqData.ConsentMsg = asyncResult.PendingInfo.Message
			if cats, ok := asyncResult.PendingInfo.Metadata["categories"].([]string); ok {
				reqData.Categories = cats
			}
		}
		r.emitter.ClientToolRequest(reqData)
	}

	// Emit tool.call.completed with status "pending" so the started event is paired
	if r.emitter != nil {
		labels := r.toolLabels(toolCall.Name)
		r.emitter.ToolCallCompletedCtx(ctx, toolCall.Name, toolCall.ID, 0, "pending", nil, labels)
	}

	toolResult := r.handleToolResult(toolCall, asyncResult)
	return toolCallResult{
		pending: &tools.PendingToolExecution{
			CallID:      toolCall.ID,
			ToolName:    toolCall.Name,
			Args:        argsMap,
			PendingInfo: asyncResult.PendingInfo,
			ToolResult:  toolResult,
		},
	}
}

// runAfterToolHooks runs AfterExecution hooks if a hook registry is configured.
func (r *toolRunner) runAfterToolHooks(
	ctx context.Context,
	toolReq hooks.ToolRequest,
	result types.MessageToolResult,
	startTime time.Time,
) {
	if r.hookRegistry == nil {
		return
	}
	toolResp := hooks.ToolResponse{
		Name:      toolReq.Name,
		CallID:    toolReq.CallID,
		Content:   result.GetTextContent(),
		Error:     result.Error,
		LatencyMs: time.Since(startTime).Milliseconds(),
	}
	r.hookRegistry.RunAfterToolExecution(ctx, toolReq, toolResp)
}

// handleToolResult converts tool execution result to MessageToolResult
func (r *toolRunner) handleToolResult(
	call types.MessageToolCall,
	asyncResult *tools.ToolExecutionResult,
) types.MessageToolResult {
	result, _ := r.buildToolResult(call, asyncResult, nil)
	return result
}

// buildToolResult converts a tool execution result to a MessageToolResult.
// rewrite, when set, rewrites a completed result's text before the size limit
// applies: the whole text of a plain result, or each text part of a
// multimodal one, leaving media parts where they are. Its error is returned.
func (r *toolRunner) buildToolResult(
	call types.MessageToolCall,
	asyncResult *tools.ToolExecutionResult,
	rewrite func(string) (string, error),
) (types.MessageToolResult, error) {
	switch asyncResult.Status {
	case tools.ToolStatusPending:
		// Tool is held pending external resolution (HITL approval or a client-mode
		// tool awaiting fulfillment). The call is surfaced via ErrToolsPending /
		// PendingTools metadata and the pipeline suspends; this placeholder result
		// is only used if the pending execution is inspected before it resolves.
		pendingMsg := asyncResult.PendingInfo.Message
		if pendingMsg == "" {
			pendingMsg = fmt.Sprintf("Tool %s is awaiting approval", call.Name)
		}
		return types.NewTextToolResult(call.ID, call.Name, pendingMsg), nil

	case tools.ToolStatusFailed:
		failResult := types.NewTextToolResult(
			call.ID, call.Name,
			fmt.Sprintf("Tool execution failed: %s", asyncResult.Error),
		)
		failResult.Error = asyncResult.Error
		return failResult, nil

	case tools.ToolStatusComplete:
		// Tool completed successfully
		content := string(asyncResult.Content)

		// Try to format nicely if it's JSON
		var resultValue interface{}
		if json.Unmarshal(asyncResult.Content, &resultValue) == nil {
			content = formatToolResult(resultValue)
		}

		// If the executor returned multimodal parts, propagate them directly;
		// otherwise wrap the text content as a single text ContentPart (legacy path).
		if len(asyncResult.Parts) > 0 {
			parts, err := r.rewriteTextParts(call.Name, asyncResult.Parts, rewrite)
			if err != nil {
				return types.MessageToolResult{}, err
			}
			return types.MessageToolResult{
				ID:    call.ID,
				Name:  call.Name,
				Parts: parts,
			}, nil
		}

		if rewrite != nil {
			var err error
			if content, err = rewrite(content); err != nil {
				return types.MessageToolResult{}, err
			}
		}
		// Enforce tool result size limit
		content = r.enforceResultSizeLimit(call.Name, content)
		return types.NewTextToolResult(call.ID, call.Name, content), nil

	default:
		unknownMsg := fmt.Sprintf("Unknown tool status: %v", asyncResult.Status)
		unknownResult := types.NewTextToolResult(call.ID, call.Name, unknownMsg)
		unknownResult.Error = unknownMsg
		return unknownResult, nil
	}
}

// rewriteTextParts rewrites each text part of a multimodal result, capped
// at the size limit, and keeps the media parts in place. Without rewrite the
// parts are returned as they are.
func (r *toolRunner) rewriteTextParts(
	name string, parts []types.ContentPart, rewrite func(string) (string, error),
) ([]types.ContentPart, error) {
	if rewrite == nil {
		return parts, nil
	}
	out := make([]types.ContentPart, len(parts))
	for i, p := range parts {
		if p.Type != types.ContentTypeText || p.Text == nil {
			out[i] = p
			continue
		}
		text, err := rewrite(*p.Text)
		if err != nil {
			return nil, err
		}
		out[i] = types.NewTextPart(r.enforceResultSizeLimit(name, text))
	}
	return out, nil
}

// enforceResultSizeLimit truncates the tool result content if it exceeds the
// configured maximum size from the tool registry.
func (r *toolRunner) enforceResultSizeLimit(toolName, content string) string {
	if r.registry == nil {
		return content
	}
	maxSize := r.registry.MaxToolResultSize()
	if maxSize <= 0 {
		return content
	}
	size := len(content)
	if size <= maxSize {
		return content
	}
	logger.Warn("Tool result truncated",
		"tool", toolName,
		"size", size,
		"limit", maxSize,
	)
	truncated := content[:maxSize]
	return fmt.Sprintf(
		"%s\n... [truncated, %d bytes exceeded limit of %d bytes]",
		truncated, size, maxSize,
	)
}

// isToolBlocked checks if a tool is in the blocklist
func isToolBlocked(toolName string, blocklist []string) bool {
	for _, blocked := range blocklist {
		if blocked == toolName {
			return true
		}
	}
	return false
}

// formatToolResult formats tool result for display
func formatToolResult(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case map[string]interface{}:
		// Pretty print JSON objects
		bytes, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(bytes)
	case []interface{}:
		// Pretty print JSON arrays
		bytes, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(bytes)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// toolExcludedReason is the tool.excluded reason for the rejection rule.
const toolExcludedReason = "rejected twice in one tool loop"

// updateExcludedTools increments rejection counts for tools whose results have
// errors and marks them for exclusion after the second rejection, publishing
// tool.excluded for each. Returns true if the excluded set changed (caller
// should rebuild provider tools).
func (r *toolRunner) updateExcludedTools(
	ctx context.Context,
	results []types.Message,
	rejectionCounts map[string]int,
	excluded map[string]bool,
	round int,
) bool {
	changed := false
	for i := range results {
		tr := results[i].ToolResult
		if tr == nil || tr.Error == "" {
			continue
		}
		rejectionCounts[tr.Name]++
		if rejectionCounts[tr.Name] > 1 && !excluded[tr.Name] {
			excluded[tr.Name] = true
			changed = true
			logger.Warn("Tool excluded after repeated rejection",
				"tool", tr.Name, "rejections", rejectionCounts[tr.Name])
			if r.emitter != nil {
				r.emitter.ToolExcludedCtx(ctx, tr.Name, toolExcludedReason, rejectionCounts[tr.Name], round)
			}
		}
	}
	return changed
}

// callRateAdmission returns the admission for toolCall: the zero value when
// no limit is set.
func (r *toolRunner) callRateAdmission(toolCall types.MessageToolCall) *callRateAdmission {
	p := r.policy()
	if p == nil || p.MaxCallsPerMinute <= 0 || r.cfg.CallRate == nil {
		return &callRateAdmission{}
	}
	var conversationID string
	if r.turnState != nil {
		conversationID = r.turnState.ConversationID
	}
	return &callRateAdmission{
		window: r.cfg.CallRate, conversationID: conversationID, tool: toolCall.Name, limit: p.MaxCallsPerMinute,
	}
}

// getMaxToolCallsPerTurn returns the cap on tool calls across all of a turn's
// rounds, or 0 for no cap.
func (r *toolRunner) getMaxToolCallsPerTurn() int {
	if p := r.policy(); p != nil {
		return p.MaxToolCallsPerTurn
	}
	return 0
}

// getMaxIdenticalToolCalls returns the threshold for aborting a loop where the
// same tool is called repeatedly with identical arguments.
func (r *toolRunner) getMaxIdenticalToolCalls() int {
	if p := r.policy(); p != nil && p.MaxIdenticalToolCalls > 0 {
		return p.MaxIdenticalToolCalls
	}
	return defaultMaxIdenticalCalls
}

// toolTurn is one turn's tool-round state: what the turn's rounds have
// excluded, counted and spent. RunRound runs a round's calls against it;
// AddCost charges a provider call to it. A new turn starts a new toolTurn.
type toolTurn struct {
	r                   *toolRunner
	maxRounds           int
	excluded            map[string]bool
	rejectionCounts     map[string]int
	identicalCallCounts map[string]int // keyed by "toolName\x00<canonical-args>"
	cumulativeCost      float64        // the run's spend: the history's plus this turn's
	nudgedLoop          bool           // already fed an identical-loop back to the model once
	toolCallsExecuted   int            // tool calls let through this turn, for max_tool_calls_per_turn
}

// roundOutcome is what one tool round produced and what the turn does next.
type roundOutcome struct {
	// Results answers every call that was not held: executed, refused by
	// policy, over the per-turn cap, or fed back as a repeated call.
	Results []types.Message
	// Pending are the calls held for approval or a deferred result.
	Pending []tools.PendingToolExecution
	// Stop ends the turn after this round, with Err when the stop is a
	// limit (max rounds, a repeated-call loop that survived the nudge) and
	// without one when termination.tool_called fired.
	Stop bool
	// Err is the turn's error. Without Stop it is a tool execution failure,
	// and Results is empty.
	Err error
	// OfferChanged reports that what the model may call changed this round:
	// a tool was excluded or the granted set moved.
	OfferChanged bool
	// OfferExhausted reports that max_tool_calls_per_turn is spent, so no
	// tools should be offered from here on.
	OfferExhausted bool
	// Nudged reports that the round was a repeated-call loop answered with
	// feedback for one self-correction attempt. The round counts for nothing
	// else: the offer, termination and the round limit are not evaluated.
	Nudged bool
}

// newTurn starts a turn whose run has already spent the cost on history, so
// MaxCostUSD bounds the whole run rather than each turn.
func (r *toolRunner) newTurn(history []types.Message, maxRounds int) *toolTurn {
	return &toolTurn{
		r:                   r,
		maxRounds:           maxRounds,
		excluded:            map[string]bool{},
		rejectionCounts:     map[string]int{},
		identicalCallCounts: map[string]int{},
		cumulativeCost:      sumHistoryCost(history),
	}
}

// sumHistoryCost totals the CostInfo across the conversation history fed into
// this turn — i.e. the run's spend before this turn's loop begins.
func sumHistoryCost(messages []types.Message) float64 {
	var total float64
	for i := range messages {
		if messages[i].CostInfo != nil {
			total += messages[i].CostInfo.TotalCost
		}
	}
	return total
}

// AddCost charges a provider call's cost to the run and returns the budget
// error once the run's spend exceeds ToolPolicy.MaxCostUSD. A nil cost adds
// nothing but is still checked; a nil turn tracks nothing.
func (t *toolTurn) AddCost(c *types.CostInfo) error {
	if t == nil {
		return nil
	}
	if c != nil {
		t.cumulativeCost += c.TotalCost
	}
	if p := t.r.policy(); p != nil && p.MaxCostUSD > 0 && t.cumulativeCost > p.MaxCostUSD {
		return fmt.Errorf(
			"provider stage: cost budget exceeded (%.4f > %.4f USD)", t.cumulativeCost, p.MaxCostUSD)
	}
	return nil
}

// RunRound runs one round's tool calls under the turn's policy: the
// repeated-identical-call breaker, the per-turn call cap, execution (policy,
// hooks, approval, the registry), rejection tracking, termination.tool_called
// and the round limit. A round with calls held pending returns as soon as
// they are held, without the checks after execution: a text turn ends there.
func (t *toolTurn) RunRound(ctx context.Context, calls []types.MessageToolCall, rr roundRef) roundOutcome {
	return t.runRound(ctx, calls, rr, false)
}

// RunRoundPastPending is RunRound for a turn that goes on past calls held
// pending (a realtime session): the checks after execution apply to the
// calls that ran, and the held calls are returned in Pending alongside them.
func (t *toolTurn) RunRoundPastPending(
	ctx context.Context, calls []types.MessageToolCall, rr roundRef,
) roundOutcome {
	return t.runRound(ctx, calls, rr, true)
}

// runRound is RunRound, going on past held calls when pastPending is set.
func (t *toolTurn) runRound(
	ctx context.Context, calls []types.MessageToolCall, rr roundRef, pastPending bool,
) roundOutcome {
	// Repeated-identical-call breaker. The first time the same tool is called
	// with identical arguments too many times, feed that back to the model once
	// (a synthetic "you're repeating yourself — stop" result) and let it
	// self-correct — loops are often transient confusion (e.g. a model retrying
	// a failing call verbatim). If it loops AGAIN after the nudge, abort.
	// What the model may call is taken once, before any call runs.
	avail := t.r.roundAvailability()
	if looping := t.identicalLoopCalls(calls); len(looping) > 0 {
		return t.identicalLoopOutcome(ctx, calls, looping, rr, avail)
	}

	grantsBefore := t.r.grantedTools()
	within, over := t.splitCallBudget(calls, avail)
	results, err := t.r.executeToolCalls(ctx, within, rr, avail)
	results = append(results, overBudgetResults(over)...)
	var held []tools.PendingToolExecution
	if err != nil {
		pending, ok := tools.IsErrToolsPending(err)
		if !ok {
			return roundOutcome{Err: fmt.Errorf("provider stage: tool execution failed: %w", err)}
		}
		if !pastPending {
			return roundOutcome{Results: results, Pending: pending.Pending}
		}
		held = pending.Pending
	}

	out := roundOutcome{Results: results, Pending: held}
	// A skill__activate / skill__deactivate call changes the granted set.
	out.OfferChanged = t.r.updateExcludedTools(ctx, results, t.rejectionCounts, t.excluded, rr.round) ||
		!stringSlicesEqual(grantsBefore, t.r.grantedTools())
	if limit := t.r.getMaxToolCallsPerTurn(); limit > 0 && t.toolCallsExecuted >= limit {
		out.OfferExhausted = true
	}
	ran := callsRun(calls, held)
	switch {
	case len(held) > 0 && len(ran) == 0:
		// Every call is held: nothing ran, so no limit is reached.
	case calledStopTool(t.r.policy(), t.r.availableCalls(ran, avail)):
		// termination.tool_called: the named tool has run; stop cleanly. A
		// call refused as unavailable did not.
		out.Stop = true
	case rr.round == t.maxRounds:
		out.Stop, out.Err = true, &MaxRoundsError{Rounds: t.maxRounds}
	}
	return out
}

// callsRun returns calls less those held pending.
func callsRun(calls []types.MessageToolCall, held []tools.PendingToolExecution) []types.MessageToolCall {
	if len(held) == 0 {
		return calls
	}
	isHeld := make(map[string]bool, len(held))
	for i := range held {
		isHeld[held[i].CallID] = true
	}
	run := make([]types.MessageToolCall, 0, len(calls))
	for _, tc := range calls {
		if !isHeld[tc.ID] {
			run = append(run, tc)
		}
	}
	return run
}

// identicalLoopOutcome applies the repeated-identical-call breaker for a round
// whose calls include looping ones: the first time it feeds the loop back to
// the model as synthetic results for one self-correction attempt; after that
// nudge it stops the turn.
func (t *toolTurn) identicalLoopOutcome(
	ctx context.Context, calls, looping []types.MessageToolCall, rr roundRef, avail availability,
) roundOutcome {
	if t.nudgedLoop {
		return roundOutcome{Stop: true, Err: fmt.Errorf(
			"provider stage: tool loop persisted after a self-correction nudge: %q called with identical arguments",
			looping[0].Name)}
	}
	t.nudgedLoop = true
	logger.Warn("identical tool-call loop detected; feeding it back to the model for one self-correction attempt",
		"tool", looping[0].Name)
	return roundOutcome{Results: t.loopFeedbackResults(ctx, calls, looping, rr, avail), Nudged: true}
}

// identicalLoopCalls increments per-call counters and returns the calls whose
// (tool, args) pair reached the configured threshold.
func (t *toolTurn) identicalLoopCalls(toolCalls []types.MessageToolCall) []types.MessageToolCall {
	threshold := t.r.getMaxIdenticalToolCalls()
	var looping []types.MessageToolCall
	for i := range toolCalls {
		tc := toolCalls[i]
		key := tc.Name + "\x00" + canonicalArgs(tc.Args)
		t.identicalCallCounts[key]++
		if t.identicalCallCounts[key] >= threshold {
			looping = append(looping, tc)
		}
	}
	return looping
}

// loopFeedbackResults builds tool-result messages for a round where an identical
// loop was detected: the looping calls get a synthetic "you repeated this — stop"
// result (NOT executed, since the result wouldn't change), while any non-looping
// calls in the same round execute normally so every tool_use still has a result.
func (t *toolTurn) loopFeedbackResults(
	ctx context.Context, all, looping []types.MessageToolCall, rr roundRef, avail availability,
) []types.Message {
	loopSet := make(map[string]bool, len(looping))
	for _, c := range looping {
		loopSet[c.ID] = true
	}
	threshold := t.r.getMaxIdenticalToolCalls()
	results := make([]types.Message, 0, len(all))
	var toExecute []types.MessageToolCall
	for i := range all {
		tc := all[i]
		if !loopSet[tc.ID] {
			toExecute = append(toExecute, tc)
			continue
		}
		msg := fmt.Sprintf(
			"Repeated call ignored: %q has been called %d times with identical arguments and was NOT "+
				"executed again — the result will not change. Stop repeating this exact call: change the "+
				"arguments, use a different tool, or finish the task.",
			tc.Name, threshold)
		results = append(results, types.NewToolResultMessage(types.NewTextToolResult(tc.ID, tc.Name, msg)))
	}
	if len(toExecute) > 0 {
		executed, _ := t.r.executeToolCalls(ctx, toExecute, rr, avail)
		results = append(results, executed...)
	}
	return results
}

// splitCallBudget divides a round's tool calls into those within the turn's
// max_tool_calls_per_turn budget and those over it, in call order, counting
// the former as executed. A call avail refuses is within, and counted for
// nothing: it will not run. Without a cap every call is within budget.
func (t *toolTurn) splitCallBudget(
	calls []types.MessageToolCall, avail availability,
) (within, over []types.MessageToolCall) {
	limit := t.r.getMaxToolCallsPerTurn()
	within = make([]types.MessageToolCall, 0, len(calls))
	for _, tc := range calls {
		if _, refused := t.r.unavailable(avail, tc.Name); refused {
			within = append(within, tc)
			continue
		}
		if limit > 0 && t.toolCallsExecuted >= limit {
			over = append(over, tc)
			continue
		}
		t.toolCallsExecuted++
		within = append(within, tc)
	}
	return within, over
}

// overBudgetResults answers each call over the max_tool_calls_per_turn budget
// with a policy rejection. Every call the model made still gets a result, so
// the history stays valid to replay.
func overBudgetResults(calls []types.MessageToolCall) []types.Message {
	results := make([]types.Message, 0, len(calls))
	for _, tc := range calls {
		errMsg := fmt.Sprintf("Tool %s not executed: the tool call limit for this turn is reached", tc.Name)
		results = append(results, toolRefusal(tc, errMsg))
	}
	return results
}

// calledStopTool reports whether the policy names a terminal tool
// (termination.tool_called) and one of the round's calls invoked it.
func calledStopTool(policy *pipeline.ToolPolicy, toolCalls []types.MessageToolCall) bool {
	if policy == nil || policy.StopOnTool == "" {
		return false
	}
	for _, tc := range toolCalls {
		if tc.Name == policy.StopOnTool {
			return true
		}
	}
	return false
}

// toolOffer is the offer state that outlives a tools build: the names offered
// during the current turn and the fingerprint of the tools array last built.
// A ProviderStage owns one, and every runner it builds shares it; it is
// locked so a runner can be used from any goroutine. A realtime stage keeps
// its own per-turn offer (duplexToolTurn.offered) and builds runners without
// one: none of the methods that use it run there, and each treats a runner
// without one as an empty offer.
type toolOffer struct {
	mu sync.Mutex
	// names is the union of the tools offered across the turn's rounds.
	names map[string]bool
	// describedKey fingerprints the tools array last built, as DescribeTool
	// shaped it (see describedToolsKey).
	describedKey string
	// allowed is the turn's allowed tools as the prompt (or the workflow
	// state the turn is in) declares them, before a ToolSelector narrows the
	// tools array: what the model may call is the offer built from them.
	allowed []string
	// view is what DescribeTool last made of each tool it described: true
	// kept, false withheld (descriptorCollector.view).
	view map[string]bool
}

// setView records describe's outcomes as the view last built.
func (o *toolOffer) setView(view map[string]bool) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.view = view
}

// lastView returns the view last built (setView), or nil.
func (o *toolOffer) lastView() map[string]bool {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.view
}

// setAllowed records the turn's allowed tools.
func (o *toolOffer) setAllowed(allowed []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.allowed = allowed
}

// allowedTools returns the turn's allowed tools (setAllowed).
func (o *toolOffer) allowedTools() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.allowed
}

// offeredNames returns the names in offer.
func offeredNames(offer []*tools.ToolDescriptor) map[string]bool {
	names := make(map[string]bool, len(offer))
	for _, d := range offer {
		names[d.Name] = true
	}
	return names
}

// offer returns the tools the model may call: the allowed tools and the
// conversation's grants, plus the capability tools the registry holds, less
// excluded, each as DescribeTool describes it, sorted by name. Nil without a
// registry, or when tool calling is off for the turn (tool_choice: none).
func (r *toolRunner) offer(allowed []string, excluded map[string]bool) []*tools.ToolDescriptor {
	if r.registry == nil || r.toolsDisabled() {
		return nil
	}
	return r.collectDescriptors(r.withGrantedTools(allowed), excluded)
}

// eligibleNames is the names offer(allowed, nil) would give, without the
// turn's exclusions, each tool taken as DescribeTool last described it in
// this turn's offers (so a round's check does not describe every tool again)
// and described now only when it was not. Nil when offer would be.
func (r *toolRunner) eligibleNames(allowed []string) map[string]bool {
	if r.registry == nil || r.toolsDisabled() {
		return nil
	}
	describe := r.cfg.DescribeTool
	if describe != nil {
		view := r.offers.lastView()
		describe = func(d *tools.ToolDescriptor) *tools.ToolDescriptor {
			kept, described := view[d.Name]
			switch {
			case !described:
				return r.cfg.DescribeTool(d)
			case kept:
				return d
			default:
				return nil
			}
		}
	}
	return offeredNames(r.collectWith(r.withGrantedTools(allowed), nil, describe).descriptors)
}

// toolsDisabled reports whether tool calling is off for the turn (tool_choice:
// none). The model cannot call a tool then, so declaring any only wastes input
// tokens and, on some models, primes spurious tool calls.
func (r *toolRunner) toolsDisabled() bool {
	p := r.policy()
	return p != nil && p.ToolChoice == toolChoiceNone
}

// collectDescriptors builds the deterministic tool list for an offer:
// pack-declared tools (allowedTools) plus capability tools (system-namespaced).
// Order MUST be stable across calls within a run — the tools array is part of
// the provider's cached prefix (Anthropic caches tools+system+first-message),
// and capability tools are gathered via IterateTools in Go map-iteration order,
// which is randomized. A varying order changes the cached-prefix bytes and
// busts prompt caching entirely, re-billing the full context at full price
// every round. Sorting by name keeps the prefix byte-stable so caching
// actually engages.
func (r *toolRunner) collectDescriptors(
	allowedTools []string, excluded map[string]bool,
) []*tools.ToolDescriptor {
	c := r.collectWith(allowedTools, excluded, r.cfg.DescribeTool)
	if c.view != nil {
		r.offers.setView(c.view)
	}
	return c.descriptors
}

// collectWith is collectDescriptors with describe in place of DescribeTool,
// returning the collector.
func (r *toolRunner) collectWith(
	allowedTools []string, excluded map[string]bool, describe func(*tools.ToolDescriptor) *tools.ToolDescriptor,
) *descriptorCollector {
	c := &descriptorCollector{
		registry: r.registry, describe: describe, excluded: excluded, seen: make(map[string]bool),
	}
	if describe != nil {
		c.view = make(map[string]bool)
	}

	// 1. Pack-declared tools from the prompt's allowed list (slice order). An
	//    entry ending in "*" (e.g. mcp__server__*) is a wildcard expanded against
	//    the registry: MCP servers reveal their tool sets only at connection time,
	//    so authors can't enumerate exact names.
	for _, entry := range allowedTools {
		c.addAllowedEntry(entry)
	}

	// 2. Implicit capability tools (skill__, a2a__, workflow__, memory__, media),
	//    gathered in randomized map order — sorted below to make the whole set
	//    stable. MCP tools are NOT implicit: they surface only via an explicit
	//    allowed_tools entry in loop 1 above.
	r.registry.IterateTools(func(name string, tool *tools.ToolDescriptor) {
		if c.skip(name) || !tools.IsImplicitTool(name) {
			return
		}
		c.add(tool)
	})

	sort.Slice(c.descriptors, func(i, j int) bool {
		return c.descriptors[i].Name < c.descriptors[j].Name
	})
	return c
}

// descriptorCollector accumulates one offer's tool descriptors, skipping
// excluded and already-added names and applying describe to each.
type descriptorCollector struct {
	registry    *tools.Registry
	describe    func(*tools.ToolDescriptor) *tools.ToolDescriptor
	excluded    map[string]bool
	seen        map[string]bool
	descriptors []*tools.ToolDescriptor
	// view records describe's outcome per tool: true kept, false withheld.
	view map[string]bool
}

// skip reports whether name is excluded or already collected.
func (c *descriptorCollector) skip(name string) bool {
	return c.seen[name] || c.excluded[name]
}

// addAllowedEntry adds the tool an allowed_tools entry names, expanding a
// trailing "*" wildcard against the registry.
func (c *descriptorCollector) addAllowedEntry(entry string) {
	if strings.HasSuffix(entry, "*") {
		c.registry.IterateTools(func(name string, tool *tools.ToolDescriptor) {
			if c.skip(name) || !tools.MatchToolPattern(entry, name) {
				return
			}
			c.add(tool)
		})
		return
	}
	if c.excluded[entry] {
		return
	}
	tool, err := c.registry.GetTool(entry)
	if err != nil {
		logger.Warn("Tool not found in registry", "tool", entry, "error", err)
		return
	}
	c.add(tool)
}

// add records what the model is offered of tool — its name, description and
// input schema — passing it through describe when one is configured; a nil
// result withholds the tool.
func (c *descriptorCollector) add(tool *tools.ToolDescriptor) {
	c.seen[tool.Name] = true
	if c.describe != nil {
		name := tool.Name
		tool = c.describe(tool)
		c.view[name] = tool != nil
		if tool == nil {
			return
		}
	}
	c.descriptors = append(c.descriptors, &tools.ToolDescriptor{
		Name:        tool.Name,
		Description: tool.Description,
		InputSchema: tool.InputSchema,
	})
}

// grantedTools returns the currently granted tool names (ToolGrants), sorted,
// or nil when nothing is granted.
func (r *toolRunner) grantedTools() []string {
	if r.cfg.ToolGrants == nil {
		return nil
	}
	grants := r.cfg.ToolGrants()
	if len(grants) == 0 {
		return nil
	}
	out := make([]string, len(grants))
	copy(out, grants)
	sort.Strings(out)
	return out
}

// withGrantedTools returns allowedTools extended with the currently granted
// tools that it does not already name. The input slice is not mutated.
func (r *toolRunner) withGrantedTools(allowedTools []string) []string {
	grants := r.grantedTools()
	if len(grants) == 0 {
		return allowedTools
	}
	seen := make(map[string]bool, len(allowedTools))
	for _, name := range allowedTools {
		seen[name] = true
	}
	out := make([]string, 0, len(allowedTools)+len(grants))
	out = append(out, allowedTools...)
	for _, name := range grants {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// resetOffered starts a fresh offered set for a turn.
//
// The union is across a turn's ROUNDS — skill grants widen it mid-turn, which
// is the whole point — but a stage outlives the turn: the SDK builds the
// pipeline once per conversation and every Send reuses it, and a streaming
// stage serves a whole session inside one Process call. Without this, a tool
// offered once is reported offered for the rest of the conversation and a
// tools_offered check with absent:true can never fail again.
func (r *toolRunner) resetOffered() {
	if r.offers == nil {
		return
	}
	r.offers.mu.Lock()
	defer r.offers.mu.Unlock()
	r.offers.names = nil
	r.offers.view = nil
}

// recordOffered adds names to the tools handed to the model this turn.
//
// The set is a union across rounds, not a snapshot: skill tool grants widen it
// mid-turn, which is the whole point of them, so "what this turn offered" is
// everything the model saw at any round. Stamped onto each assistant message as
// types.MetaToolsOffered, which is what makes the tools_offered eval possible —
// nothing else records a tool the model was offered but never called.
func (r *toolRunner) recordOffered(names ...string) {
	if len(names) == 0 || r.offers == nil {
		return
	}
	r.offers.mu.Lock()
	defer r.offers.mu.Unlock()
	if r.offers.names == nil {
		r.offers.names = make(map[string]bool, len(names))
	}
	for _, name := range names {
		r.offers.names[name] = true
	}
}

// offeredToolNames returns the turn's offered set, sorted, or nil when the turn
// offered no tools at all.
func (r *toolRunner) offeredToolNames() []string {
	if r.offers == nil {
		return nil
	}
	r.offers.mu.Lock()
	defer r.offers.mu.Unlock()
	if len(r.offers.names) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.offers.names))
	for name := range r.offers.names {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// stampToolsOffered records on an assistant message what this turn offered the
// model, so an eval can assert on tool availability and not just on what the
// model chose to call.
//
// Every round implementation must call it. A round that skips it leaves the
// eval with no evidence of the tools offered.
func (r *toolRunner) stampToolsOffered(msg *types.Message) {
	offered := r.offeredToolNames()
	if len(offered) == 0 {
		return
	}
	if msg.Meta == nil {
		msg.Meta = map[string]interface{}{}
	}
	msg.Meta[types.MetaToolsOffered] = offered
}

// describedToolsKey fingerprints what DescribeTool made of a tools build — the
// name, description and schema of each descriptor, in order — so a later
// round can tell whether the per-conversation view changed. Empty when no
// DescribeTool is configured: without one, grants and exclusions are the only
// things that change the array mid-turn, and they are tracked directly.
func (r *toolRunner) describedToolsKey(descriptors []*tools.ToolDescriptor) string {
	if r.cfg.DescribeTool == nil {
		return ""
	}
	var sb strings.Builder
	for _, d := range descriptors {
		sb.WriteString(d.Name)
		sb.WriteByte(0)
		sb.WriteString(d.Description)
		sb.WriteByte(0)
		sb.Write(d.InputSchema)
		sb.WriteByte(0)
	}
	return sb.String()
}

// describedChanged reports whether descriptors differ from the tools array
// last built (markBuilt), as DescribeTool shapes them.
func (r *toolRunner) describedChanged(descriptors []*tools.ToolDescriptor) bool {
	if r.offers == nil {
		return true
	}
	key := r.describedToolsKey(descriptors)
	r.offers.mu.Lock()
	defer r.offers.mu.Unlock()
	return key != r.offers.describedKey
}

// markBuilt records descriptors as the tools array last built.
func (r *toolRunner) markBuilt(descriptors []*tools.ToolDescriptor) {
	if r.offers == nil {
		return
	}
	key := r.describedToolsKey(descriptors)
	r.offers.mu.Lock()
	defer r.offers.mu.Unlock()
	r.offers.describedKey = key
}
