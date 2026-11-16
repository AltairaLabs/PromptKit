package stage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
}

// toolCallResult holds the outcome of a single tool call execution,
// including its original index for preserving result ordering.
type toolCallResult struct {
	index   int
	message types.Message
	pending *tools.PendingToolExecution // non-nil if tool returned pending status
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

func (r *toolRunner) executeToolCalls(
	ctx context.Context, toolCalls []types.MessageToolCall, rr roundRef,
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
			result := r.executeSingleToolCall(gctx, toolCall, rr)
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
	for i := range resultSlots {
		if resultSlots[i].pending != nil {
			continue
		}
		results = append(results, resultSlots[i].message)
	}

	// Report what actually ran for the workflow's max_tool_calls budget. results
	// excludes pending calls, which have not executed and are counted when they
	// run on resume; errored and policy-blocked calls are included, since both
	// consumed a round.
	r.recordToolCalls(len(results))

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
		result := types.NewTextToolResult(toolCall.ID, toolCall.Name, errMsg)
		result.Error = errMsg
		result.ErrorType = types.ToolErrorApproval
		return hooks.Decision{}, toolCallResult{
			message: types.NewToolResultMessage(result),
		}, true
	}

	var hookDecision hooks.Decision
	if r.hookRegistry != nil {
		hookDecision = r.hookRegistry.RunBeforeToolExecution(ctx, toolReq)
		if !hookDecision.Allow {
			errMsg := fmt.Sprintf(
				"Tool %s blocked by hook: %s", toolCall.Name, hookDecision.Reason,
			)
			hookResult := types.NewTextToolResult(toolCall.ID, toolCall.Name, errMsg)
			hookResult.Error = errMsg
			hookResult.ErrorType = types.ToolErrorApproval
			msg := types.NewToolResultMessage(hookResult)
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
	result := types.NewTextToolResult(toolCall.ID, toolCall.Name, errMsg)
	result.Error = errMsg
	result.ErrorType = types.ToolErrorApproval
	labels := r.toolLabels(toolCall.Name)
	r.emitToolStarted(ctx, toolCall, labels, rr) // paired with the failure, as on every other failure path
	r.emitToolFailed(ctx, toolCall, cause, time.Now(), labels, rr)
	return toolCallResult{message: types.NewToolResultMessage(result)}
}

// executeSingleToolCall handles policy checks, hooks, execution, and event
// emission for a single tool call. It never returns an error — failures are
// captured as error results in the returned message, matching the previous
// sequential behavior where one tool failure does not cancel others.
func (r *toolRunner) executeSingleToolCall(
	ctx context.Context,
	toolCall types.MessageToolCall,
	rr roundRef,
) toolCallResult {
	toolReq := r.toolRequest(toolCall)
	hookDecision, blocked, skip := r.preExecCheck(ctx, toolCall, toolReq)
	if skip {
		return blocked
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
