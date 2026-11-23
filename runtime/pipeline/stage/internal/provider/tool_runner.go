package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// ToolRunnerConfig is the part of ProviderConfig the tool runner reads.
type ToolRunnerConfig struct {
	ApprovalGate    tools.ApprovalGate
	ApprovalChecker tools.ApprovalChecker
	// CallRate is the window ToolPolicy.MaxCallsPerMinute counts calls in:
	// the stage's resolved window (ProviderConfig.CallRate, or its own).
	CallRate     *CallRateWindow
	Caller       types.Caller
	ToolGrants   func() []string
	DescribeTool func(*tools.ToolDescriptor) *tools.ToolDescriptor
}

// ToolRunner executes a round's tool calls: policy and hook checks, the
// approval gate, the call-rate window, the registry call, result shaping and
// the tool events. It holds no turn state of its own.
type ToolRunner struct {
	Registry *tools.Registry
	// Policy returns the tool policy in force for the turn.
	Policy        func() *pipeline.ToolPolicy
	Emitter       *events.Emitter
	HookRegistry  *hooks.Registry
	TurnState     *core.TurnState
	Config        ToolRunnerConfig
	StateResolver WorkflowStateResolver
	// offers is the offer state of the stage that built the runner: it
	// outlives the runner, which is rebuilt on each use.
	offers *ToolOffer
	// Views, when set, is where DescribeTool's outcomes (ToolOffer.view) are
	// recorded and read instead of offers: a realtime stage keeps the view
	// of its session's last sync there, and no other offer state.
	Views *ToolOffer
	// Availability decides, for the registered tools a round calls, the
	// refusal of each the model may not call (see RoundRefusals), given the
	// turn's exclusions. Nil refuses nothing: the provider is never sent
	// tools.
	Availability func(called []*tools.ToolDescriptor, excluded map[string]bool) RoundRefusals
}

// NotAvailableRefusal is the refusal of a call to a registered tool the
// model may not call this turn.
func NotAvailableRefusal(name string) string {
	return fmt.Sprintf("Tool %s was not run: it is not available this turn", name)
}

// RoundRefusals holds, by tool name, the refusal of each of a round's calls
// to a registered tool the model may not call. The round decides it once,
// before any of its calls runs, so a grant or a revocation one call makes
// applies from the next round, and every use of the decision (the call
// budgets, execution, termination and rejection counting) sees the same one.
type RoundRefusals map[string]string

// refusal returns the refusal of a call to name, if the round refuses it.
func (rf RoundRefusals) refusal(name string) (string, bool) {
	errMsg, ok := rf[name]
	return errMsg, ok
}

// RoundDecision is a round's decision, taken once before any of its calls
// runs: the refusals (RoundRefusals) and the registered tools its calls name,
// each resolved from the registry once.
type RoundDecision struct {
	Refused RoundRefusals
	// resolved holds, by name, the registered tool each of the round's calls
	// names; a name it lacks names no registered tool (or the decision is
	// the zero value, which resolved nothing).
	resolved map[string]*tools.ToolDescriptor
}

// refusal returns the refusal of a call to name, if the round refuses it.
func (d RoundDecision) refusal(name string) (string, bool) {
	return d.Refused.refusal(name)
}

// allRefused reports whether the round refuses every one of calls, a
// round's calls (never none).
func (d RoundDecision) allRefused(calls []types.MessageToolCall) bool {
	return len(availableCalls(calls, d)) == 0
}

// tool returns the registered tool name names, as the round resolved it, or
// nil when the round resolved none under it.
func (d RoundDecision) tool(name string) *tools.ToolDescriptor {
	return d.resolved[name]
}

// ClassifyRound decides a round: it resolves the registered tools its calls
// name, once each, and which of them the model may not call (availability,
// given the turn's exclusions). A name the registry lacks is not refused
// here: it keeps its not-found error.
func (r *ToolRunner) ClassifyRound(calls []types.MessageToolCall, excluded map[string]bool) RoundDecision {
	if r.Registry == nil {
		return RoundDecision{}
	}
	resolved := make(map[string]*tools.ToolDescriptor, len(calls))
	seen := make(map[string]bool, len(calls))
	called := make([]*tools.ToolDescriptor, 0, len(calls))
	for _, tc := range calls {
		if seen[tc.Name] {
			continue
		}
		seen[tc.Name] = true
		if tool := r.Registry.Get(tc.Name); tool != nil {
			resolved[tc.Name] = tool
			called = append(called, tool)
		}
	}
	d := RoundDecision{resolved: resolved}
	if r.Availability != nil {
		d.Refused = r.Availability(called, excluded)
	}
	return d
}

// RefuseAmong is the round's refusals of called: refuse's answer for each
// tool's name, where it refuses one.
func RefuseAmong(called []*tools.ToolDescriptor, refuse func(name string) (string, bool)) RoundRefusals {
	refused := make(RoundRefusals)
	for _, tool := range called {
		if errMsg, no := refuse(tool.Name); no {
			refused[tool.Name] = errMsg
		}
	}
	return refused
}

// availableCalls returns calls less those refused refuses.
func availableCalls(calls []types.MessageToolCall, refused RoundDecision) []types.MessageToolCall {
	if len(refused.Refused) == 0 {
		return calls
	}
	out := make([]types.MessageToolCall, 0, len(calls))
	for _, tc := range calls {
		if _, no := refused.refusal(tc.Name); !no {
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
func (r *ToolRunner) toolLabels(name string) map[string]string {
	if r.Registry == nil {
		return nil
	}
	desc := r.Registry.Get(name)
	if desc == nil {
		return nil
	}
	return desc.Labels
}

// recordToolCalls reports a round's executed tool-call count to the resolver
// when it implements ToolCallRecorder, feeding the workflow's max_tool_calls
// budget. No-op without a resolver, or when the resolver does not enforce a
// budget — the ordinary non-workflow case.
func (r *ToolRunner) recordToolCalls(n int) {
	if n <= 0 || r.StateResolver == nil {
		return
	}
	if rec, ok := r.StateResolver.(ToolCallRecorder); ok {
		rec.RecordToolCalls(n)
	}
}

// getMaxParallelToolCalls returns the max concurrency for parallel tool execution.
func (r *ToolRunner) getMaxParallelToolCalls() int {
	if p := r.Policy(); p != nil && p.MaxParallelToolCalls > 0 {
		return p.MaxParallelToolCalls
	}
	return defaultMaxParallelToolCalls
}

// executeToolCalls runs a round's calls, refusing those refused names (the
// round's decision; nil refuses none), and returns their results in call
// order, less those held pending. It runs them as a round's calls run
// (runPlanned).
//
//nolint:unused // tests call it
func (r *ToolRunner) executeToolCalls(
	ctx context.Context, toolCalls []types.MessageToolCall, rr RoundRef, refused RoundDecision,
) ([]types.Message, error) {
	plan := roundPlan{calls: toolCalls, kinds: make([]callKind, len(toolCalls))} // every call runs
	slots := make([]*types.Message, len(toolCalls))
	held, err := r.runPlanned(ctx, plan, rr, refused, slots)
	if err != nil {
		return nil, err
	}
	results := make([]types.Message, 0, len(toolCalls))
	for _, m := range slots {
		if m != nil {
			results = append(results, *m)
		}
	}
	if len(held) > 0 {
		return results, &tools.ErrToolsPending{Pending: held}
	}
	return results, nil
}

// runToolCalls runs calls, refusing those refused names, and returns each
// call's outcome in its slot (toolCallResult.index is its position in
// toolCalls); a call held pending has pending set. The error is the
// context's, when it was canceled.
func (r *ToolRunner) runToolCalls(
	ctx context.Context, toolCalls []types.MessageToolCall, rr RoundRef, refused RoundDecision,
) ([]toolCallResult, error) {
	if r.Registry == nil {
		return nil, errors.New("tool registry not configured but tool calls present")
	}

	// Running tools is activity, so hold the idle timer open until they finish.
	// Each tool is still bounded by its own TimeoutMs.
	defer core.KeepIdleAlive(ctx)()

	resultSlots := make([]toolCallResult, len(toolCalls))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(r.getMaxParallelToolCalls())

	for i, tc := range toolCalls {
		idx := i
		toolCall := tc

		g.Go(func() error {
			result := r.executeSingleToolCall(gctx, toolCall, rr, refused)
			// However it was answered (the blocklist, a hook or the refusal), a
			// call the round refuses did not run, and no budget counts it.
			_, result.unavailable = refused.refusal(toolCall.Name)
			result.index = idx
			resultSlots[idx] = result // each goroutine writes its own slot
			return nil
		})
	}

	// errgroup goroutines always return nil, so this only errors on ctx cancel.
	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Report what actually ran for the workflow's max_tool_calls budget: not
	// pending calls, which have not executed and are counted when they run on
	// resume; errored and policy-blocked calls are, since both consumed a
	// round. A call to a tool the model may not call is not.
	ran := 0
	for i := range resultSlots {
		if resultSlots[i].pending == nil && !resultSlots[i].unavailable {
			ran++
		}
	}
	r.recordToolCalls(ran)
	return resultSlots, nil
}

// preExecCheck runs policy and hook checks before tool execution.
// Returns (hookDecision, blocked result, shouldSkip).
// When shouldSkip is true the caller should return the blocked result directly.
func (r *ToolRunner) preExecCheck(
	ctx context.Context, toolCall types.MessageToolCall, toolReq hooks.ToolRequest,
) (hooks.Decision, toolCallResult, bool) {
	if p := r.Policy(); p != nil && isToolBlocked(toolCall.Name, p.Blocklist) {
		errMsg := fmt.Sprintf("Tool %s is blocked by policy", toolCall.Name)
		return hooks.Decision{}, toolCallResult{
			message: ToolRefusal(toolCall, errMsg),
		}, true
	}

	var hookDecision hooks.Decision
	if r.HookRegistry != nil {
		hookDecision = r.HookRegistry.RunBeforeToolExecution(ctx, toolReq)
		if !hookDecision.Allow {
			errMsg := fmt.Sprintf(
				"Tool %s blocked by hook: %s", toolCall.Name, hookDecision.Reason,
			)
			msg := ToolRefusal(toolCall, errMsg)
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
func (r *ToolRunner) holdForApproval(
	ctx context.Context, toolCall types.MessageToolCall, rr RoundRef,
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
func (r *ToolRunner) approvalGate() tools.ApprovalGate {
	if r.Config.ApprovalGate != nil {
		return r.Config.ApprovalGate
	}
	if checker := r.Config.ApprovalChecker; checker != nil {
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
func (r *ToolRunner) approvalRefused(
	ctx context.Context, toolCall types.MessageToolCall, cause error, rr RoundRef,
) toolCallResult {
	errMsg := fmt.Sprintf("Tool %s was not run: the approval gate refused it: %v", toolCall.Name, cause)
	return r.refuseCall(ctx, toolCall, errMsg, cause, rr)
}

// refuseCall is the result of a call refused before it ran: an approval
// error, which the model is given in place of the call's result. A
// tool.call.started event and a tool.call.failed event naming cause report
// it.
func (r *ToolRunner) refuseCall(
	ctx context.Context, toolCall types.MessageToolCall, errMsg string, cause error, rr RoundRef,
) toolCallResult {
	labels := r.toolLabels(toolCall.Name)
	r.emitToolStarted(ctx, toolCall, labels, rr) // paired with the failure, as on every other failure path
	r.emitToolFailed(ctx, toolCall, cause, time.Now(), labels, rr)
	return toolCallResult{message: ToolRefusal(toolCall, errMsg)}
}

// ToolRefusal is the tool result answering a call that did not run: errMsg
// as both its text and its error, classified as an approval error (a policy,
// hook, gate, call-budget or offer refusal).
func ToolRefusal(tc types.MessageToolCall, errMsg string) types.Message {
	result := types.NewTextToolResult(tc.ID, tc.Name, errMsg)
	result.Error = errMsg
	result.ErrorType = types.ToolErrorApproval
	return types.NewToolResultMessage(result)
}

// checkCallable answers a call the round may not run before the approval
// gate is asked about it: a tool the model may not call (refused), or a
// name no tool is registered under.
func (r *ToolRunner) checkCallable(
	ctx context.Context, toolCall types.MessageToolCall, rr RoundRef, refused RoundDecision,
) (toolCallResult, bool) {
	if errMsg, no := refused.refusal(toolCall.Name); no {
		logger.Warn("refused a call to a tool the model may not call this turn", "tool", toolCall.Name)
		return r.refuseCall(ctx, toolCall, errMsg, errors.New(errMsg), rr), true
	}
	if refused.tool(toolCall.Name) != nil {
		return toolCallResult{}, false
	}
	// The registry's own error names the missing tool.
	if _, err := r.Registry.GetTool(toolCall.Name); err != nil {
		r.emitToolStarted(ctx, toolCall, nil, rr) // paired with the failure, as on every other failure path
		return r.toolCallFailed(ctx, toolCall, err, time.Now(), nil, rr), true
	}
	return toolCallResult{}, false
}

// executeSingleToolCall handles policy checks, hooks, execution, and event
// emission for a single tool call. It never returns an error — failures are
// captured as error results in the returned message, matching the previous
// sequential behavior where one tool failure does not cancel others.
func (r *ToolRunner) executeSingleToolCall(
	ctx context.Context,
	toolCall types.MessageToolCall,
	rr RoundRef,
	refused RoundDecision,
) toolCallResult {
	tool := refused.tool(toolCall.Name) // resolved once, for the whole round
	toolReq := r.toolRequest(toolCall, tool)
	hookDecision, blocked, skip := r.preExecCheck(ctx, toolCall, toolReq)
	if skip {
		return blocked
	}
	if answer, ok := r.checkCallable(ctx, toolCall, rr, refused); ok {
		return answer
	}

	if held, ok := r.holdForApproval(ctx, toolCall, rr); ok {
		return held // admitted under ToolPolicy.MaxCallsPerMinute when the host approves it
	}

	var labels map[string]string
	if tool != nil {
		labels = tool.Labels
	}
	r.emitToolStarted(ctx, toolCall, labels, rr)

	startTime := time.Now()
	ctx = tools.WithCallID(ctx, toolCall.ID)
	// The registry admits the call under ToolPolicy.MaxCallsPerMinute once its
	// own checks pass, immediately before the executor runs it.
	admission := r.callRateAdmission(toolCall)
	asyncResult, err := r.Registry.ExecuteCallAsyncAdmitted(ctx, r.registryCall(toolCall), admission.admit)
	core.ResetIdleFromContext(ctx)
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
	if r.Emitter != nil {
		r.Emitter.ToolCallEventCtx(ctx, events.EventToolCallCompleted, &events.ToolCallEventData{
			ToolName:       toolCall.Name,
			CallID:         toolCall.ID,
			Duration:       time.Since(startTime),
			Status:         string(asyncResult.Status),
			Parts:          result.Parts,
			Labels:         labels,
			Round:          rr.Round,
			ProviderCallID: rr.ProviderCallID,
		})
	}
	resultMsg := types.NewToolResultMessage(result)
	if hookDecision.Metadata != nil {
		resultMsg.Meta = hookDecision.Metadata
	}

	r.runAfterToolHooks(ctx, toolReq, result, startTime)

	return toolCallResult{message: resultMsg}
}

// toolRequest is the hook request for a tool call, built once per call:
// tool is the registered tool it names (its pack declaration), or nil.
func (r *ToolRunner) toolRequest(toolCall types.MessageToolCall, tool *tools.ToolDescriptor) hooks.ToolRequest {
	if r.HookRegistry == nil {
		return hooks.ToolRequest{}
	}
	var declaration *packspec.Tool
	if tool != nil {
		declaration = tool.Declaration
	}
	return hooks.ToolRequest{
		Name: toolCall.Name, Args: toolCall.Args, CallID: toolCall.ID,
		Declaration: declaration,
		Caller:      r.caller(),
	}
}

// registryCall is the registry request for a tool call: the call with the
// conversation's caller.
func (r *ToolRunner) registryCall(toolCall types.MessageToolCall) tools.ToolCall {
	return tools.ToolCall{Name: toolCall.Name, Args: toolCall.Args, ID: toolCall.ID, Caller: r.caller()}
}

// caller is who the conversation acts for (ProviderConfig.Caller).
func (r *ToolRunner) caller() types.Caller {
	return r.Config.Caller
}

// toolCallFailed emits tool.call.failed and returns the error result the
// model sees.
func (r *ToolRunner) toolCallFailed(
	ctx context.Context, toolCall types.MessageToolCall, err error,
	startTime time.Time, labels map[string]string, rr RoundRef,
) toolCallResult {
	r.emitToolFailed(ctx, toolCall, err, startTime, labels, rr)
	return toolCallResult{
		message: types.NewToolResultMessage(tools.ErrorResult(toolCall.ID, toolCall.Name, err)),
	}
}

// emitToolFailed emits tool.call.failed.
func (r *ToolRunner) emitToolFailed(
	ctx context.Context, toolCall types.MessageToolCall, err error,
	startTime time.Time, labels map[string]string, rr RoundRef,
) {
	if r.Emitter == nil {
		return
	}
	r.Emitter.ToolCallEventCtx(ctx, events.EventToolCallFailed, &events.ToolCallEventData{
		ToolName:       toolCall.Name,
		CallID:         toolCall.ID,
		Error:          err,
		Duration:       time.Since(startTime),
		Labels:         labels,
		Round:          rr.Round,
		ProviderCallID: rr.ProviderCallID,
	})
}

// resultRewriter returns the function that passes a result's text through
// the hook registry's ToolResultRewriters, or nil when there are none.
func (r *ToolRunner) resultRewriter(
	ctx context.Context, toolReq hooks.ToolRequest,
) func(string) (string, error) {
	if !r.HookRegistry.HasToolResultRewriters() {
		return nil
	}
	return func(content string) (string, error) {
		return r.HookRegistry.RunToolResultRewriters(ctx, toolReq, content)
	}
}

// emitToolStarted emits the tool call started event if an emitter is configured.
func (r *ToolRunner) emitToolStarted(
	ctx context.Context, toolCall types.MessageToolCall, labels map[string]string, rr RoundRef,
) {
	if r.Emitter == nil {
		return
	}
	var argsMap map[string]interface{}
	if toolCall.Args != nil {
		_ = json.Unmarshal(toolCall.Args, &argsMap)
	}
	r.Emitter.ToolCallEventCtx(ctx, events.EventToolCallStarted, &events.ToolCallEventData{
		ToolName:       toolCall.Name,
		CallID:         toolCall.ID,
		Args:           argsMap,
		Labels:         labels,
		Round:          rr.Round,
		ProviderCallID: rr.ProviderCallID,
	})
}

// buildPendingResult creates a toolCallResult for a pending tool execution.
// It emits a tool.client.request event so observers know a client tool is
// awaiting fulfillment, and a tool.call.completed with status "pending" so
// every tool.call.started has a matching completion.
func (r *ToolRunner) buildPendingResult(
	ctx context.Context, toolCall types.MessageToolCall, asyncResult *tools.ToolExecutionResult,
) toolCallResult {
	var argsMap map[string]any
	if toolCall.Args != nil {
		_ = json.Unmarshal(toolCall.Args, &argsMap)
	}

	// Emit client tool request event with consent/category metadata
	if r.Emitter != nil {
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
		r.Emitter.ClientToolRequest(reqData)
	}

	// Emit tool.call.completed with status "pending" so the started event is paired
	if r.Emitter != nil {
		labels := r.toolLabels(toolCall.Name)
		r.Emitter.ToolCallCompletedCtx(ctx, toolCall.Name, toolCall.ID, 0, "pending", nil, labels)
	}

	toolResult := r.handleToolResult(toolCall, asyncResult)
	return toolCallResult{
		pending: &tools.PendingToolExecution{
			CallID:      toolCall.ID,
			ToolName:    toolCall.Name,
			Args:        argsMap,
			RawArgs:     toolCall.Args,
			PendingInfo: asyncResult.PendingInfo,
			ToolResult:  toolResult,
		},
	}
}

// runAfterToolHooks runs AfterExecution hooks if a hook registry is configured.
func (r *ToolRunner) runAfterToolHooks(
	ctx context.Context,
	toolReq hooks.ToolRequest,
	result types.MessageToolResult,
	startTime time.Time,
) {
	if r.HookRegistry == nil {
		return
	}
	toolResp := hooks.ToolResponse{
		Name:      toolReq.Name,
		CallID:    toolReq.CallID,
		Content:   result.GetTextContent(),
		Error:     result.Error,
		LatencyMs: time.Since(startTime).Milliseconds(),
	}
	r.HookRegistry.RunAfterToolExecution(ctx, toolReq, toolResp)
}

// handleToolResult converts tool execution result to MessageToolResult
func (r *ToolRunner) handleToolResult(
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
func (r *ToolRunner) buildToolResult(
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
		// The model is given the result as BuildModelResult prepares it: formatted,
		// rewritten, capped, and its multimodal parts kept.
		result, err := tools.BuildModelResult(asyncResult.Content, asyncResult.Parts, tools.ModelResultOptions{
			ToolName: call.Name,
			MaxSize:  r.maxResultSize(),
			Rewrite:  rewrite,
		})
		if err != nil {
			return types.MessageToolResult{}, err
		}
		if len(result.Parts) > 0 {
			return types.MessageToolResult{ID: call.ID, Name: call.Name, Parts: result.Parts}, nil
		}
		return types.NewTextToolResult(call.ID, call.Name, result.Text), nil

	default:
		unknownMsg := fmt.Sprintf("Unknown tool status: %v", asyncResult.Status)
		unknownResult := types.NewTextToolResult(call.ID, call.Name, unknownMsg)
		unknownResult.Error = unknownMsg
		return unknownResult, nil
	}
}

// maxResultSize is the registry's tool result size limit in bytes, zero for
// none.
func (r *ToolRunner) maxResultSize() int {
	if r.Registry == nil {
		return 0
	}
	return r.Registry.MaxToolResultSize()
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

// toolExcludedReason is the tool.excluded reason for the rejection rule.
const toolExcludedReason = "rejected twice in one tool loop"

// updateExcludedTools increments rejection counts for tools whose results have
// errors and marks them for exclusion after the second rejection, publishing
// tool.excluded for each. Returns true if the excluded set changed (caller
// should rebuild provider tools).
func (r *ToolRunner) updateExcludedTools(
	ctx context.Context,
	results []types.Message,
	rejectionCounts map[string]int,
	excluded map[string]bool,
	round int,
	refused RoundDecision,
) bool {
	changed := false
	for i := range results {
		tr := results[i].ToolResult
		if tr == nil || tr.Error == "" {
			continue
		}
		if _, no := refused.refusal(tr.Name); no {
			continue // a tool the model may not call is not a rejected offer
		}
		rejectionCounts[tr.Name]++
		if rejectionCounts[tr.Name] > 1 && !excluded[tr.Name] {
			excluded[tr.Name] = true
			changed = true
			logger.Warn("Tool excluded after repeated rejection",
				"tool", tr.Name, "rejections", rejectionCounts[tr.Name])
			if r.Emitter != nil {
				r.Emitter.ToolExcludedCtx(ctx, tr.Name, toolExcludedReason, rejectionCounts[tr.Name], round)
			}
		}
	}
	return changed
}

// callRateAdmission returns the admission for toolCall: the zero value when
// no limit is set.
func (r *ToolRunner) callRateAdmission(toolCall types.MessageToolCall) *callRateAdmission {
	p := r.Policy()
	if p == nil || p.MaxCallsPerMinute <= 0 || r.Config.CallRate == nil {
		return &callRateAdmission{}
	}
	var conversationID string
	if r.TurnState != nil {
		conversationID = r.TurnState.ConversationID
	}
	return &callRateAdmission{
		window: r.Config.CallRate, conversationID: conversationID, tool: toolCall.Name, limit: p.MaxCallsPerMinute,
	}
}

// getMaxToolCallsPerTurn returns the cap on tool calls across all of a turn's
// rounds, or 0 for no cap.
func (r *ToolRunner) getMaxToolCallsPerTurn() int {
	if p := r.Policy(); p != nil {
		return p.MaxToolCallsPerTurn
	}
	return 0
}

// getMaxIdenticalToolCalls returns the threshold for aborting a loop where the
// same tool is called repeatedly with identical arguments.
func (r *ToolRunner) getMaxIdenticalToolCalls() int {
	if p := r.Policy(); p != nil && p.MaxIdenticalToolCalls > 0 {
		return p.MaxIdenticalToolCalls
	}
	return defaultMaxIdenticalCalls
}

// ToolTurn is one turn's tool-round state: what the turn's rounds have
// excluded, counted and spent. RunRound runs a round's calls against it;
// AddCost charges a provider call to it. A new turn starts a new ToolTurn.
type ToolTurn struct {
	r                   *ToolRunner
	MaxRounds           int
	Excluded            map[string]bool
	rejectionCounts     map[string]int
	identicalCallCounts map[string]int // keyed by "toolName\x00<canonical-args>"
	CumulativeCost      float64        // the run's spend: the history's plus this turn's
	nudgedLoop          bool           // already fed an identical-loop back to the model once
	toolCallsExecuted   int            // tool calls let through this turn, for max_tool_calls_per_turn
}

// RoundOutcome is what one tool round produced and what the turn does next.
type RoundOutcome struct {
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
	// Nudged reports that the round's repeated-call loop was answered with
	// feedback for one self-correction attempt. Its other calls ran as any
	// round's do: the call budget, exclusions, the offer, termination and the
	// round limit apply to it.
	Nudged bool
	// AllRefused reports that the round refused every one of its calls as
	// to a tool the model may not call: none of them ran.
	AllRefused bool
}

// NewTurn starts a turn whose run has already spent the cost on history, so
// MaxCostUSD bounds the whole run rather than each turn.
func (r *ToolRunner) NewTurn(history []types.Message, maxRounds int) *ToolTurn {
	return &ToolTurn{
		r:                   r,
		MaxRounds:           maxRounds,
		Excluded:            map[string]bool{},
		rejectionCounts:     map[string]int{},
		identicalCallCounts: map[string]int{},
		CumulativeCost:      SumHistoryCost(history),
	}
}

// SumHistoryCost totals the CostInfo across the conversation history fed into
// this turn — i.e. the run's spend before this turn's loop begins.
func SumHistoryCost(messages []types.Message) float64 {
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
func (t *ToolTurn) AddCost(c *types.CostInfo) error {
	if t == nil {
		return nil
	}
	if c != nil {
		t.CumulativeCost += c.TotalCost
	}
	if p := t.r.Policy(); p != nil && p.MaxCostUSD > 0 && t.CumulativeCost > p.MaxCostUSD {
		return fmt.Errorf(
			"provider stage: cost budget exceeded (%.4f > %.4f USD)", t.CumulativeCost, p.MaxCostUSD)
	}
	return nil
}

// RunRound runs one round's tool calls under the turn's policy: the
// repeated-identical-call breaker, the per-turn call cap, execution (policy,
// hooks, approval, the registry), rejection tracking, termination.tool_called
// and the round limit. A round with calls held pending returns as soon as
// they are held, without the checks after execution: a text turn ends there.
func (t *ToolTurn) RunRound(ctx context.Context, calls []types.MessageToolCall, rr RoundRef) RoundOutcome {
	return t.runRound(ctx, calls, rr, false)
}

// RunRoundPastPending is RunRound for a turn that goes on past calls held
// pending (a realtime session): the checks after execution apply to the
// calls that ran, and the held calls are returned in Pending alongside them.
func (t *ToolTurn) RunRoundPastPending(
	ctx context.Context, calls []types.MessageToolCall, rr RoundRef,
) RoundOutcome {
	return t.runRound(ctx, calls, rr, true)
}

// runRound is RunRound, going on past held calls when pastPending is set.
func (t *ToolTurn) runRound(
	ctx context.Context, calls []types.MessageToolCall, rr RoundRef, pastPending bool,
) RoundOutcome {
	// What the model may call is decided once, before any call runs.
	refused := t.r.ClassifyRound(calls, t.Excluded)
	plan := t.planRound(calls, refused)
	looping, loopingAt := plan.callsOf(callLoops)
	if err := t.nudgeOnce(looping); err != nil {
		return RoundOutcome{Stop: true, Err: err}
	}

	// The calls the breaker did not answer run as any round's do: under the
	// per-turn call budget, counted, and tracked for rejection and offer
	// changes. Each result takes its call's position in the round.
	grantsBefore := t.r.grantedTools()
	slots := make([]*types.Message, len(calls))
	held, err := t.r.runPlanned(ctx, plan, rr, refused, slots)
	if err != nil {
		return RoundOutcome{Err: fmt.Errorf("provider stage: tool execution failed: %w", err)}
	}
	nudges := t.loopNudges(looping)
	for i := range nudges {
		slots[loopingAt[i]] = &nudges[i]
	}
	over, overAt := plan.callsOf(callOverBudget)
	refusals := overBudgetResults(over)
	for i := range refusals {
		slots[overAt[i]] = &refusals[i]
	}
	results := make([]types.Message, 0, len(calls))
	for _, m := range slots {
		if m != nil {
			results = append(results, *m)
		}
	}
	if len(held) > 0 && !pastPending {
		return RoundOutcome{Results: results, Pending: held}
	}

	out := RoundOutcome{
		Results: results, Pending: held, AllRefused: refused.allRefused(calls), Nudged: len(looping) > 0,
	}
	// A skill__activate / skill__deactivate call changes the granted set.
	out.OfferChanged = t.r.updateExcludedTools(ctx, results, t.rejectionCounts, t.Excluded, rr.Round, refused) ||
		!stringSlicesEqual(grantsBefore, t.r.grantedTools())
	if limit := t.r.getMaxToolCallsPerTurn(); limit > 0 && t.toolCallsExecuted >= limit {
		out.OfferExhausted = true
	}
	out.Stop, out.Err = t.roundStop(plan.rest(), held, refused, rr)
	return out
}

// runPlanned runs the calls plan lets run, putting each result in slots at
// its call's position, and returns the calls held pending. A round the
// breaker answers entirely runs nothing.
func (r *ToolRunner) runPlanned(
	ctx context.Context, plan roundPlan, rr RoundRef, refused RoundDecision, slots []*types.Message,
) ([]tools.PendingToolExecution, error) {
	runs, runsAt := plan.callsOf(callRuns)
	if len(runs) == 0 {
		return nil, nil
	}
	ran, err := r.runToolCalls(ctx, runs, rr, refused)
	if err != nil {
		return nil, err
	}
	var held []tools.PendingToolExecution
	for i := range ran {
		if ran[i].pending != nil {
			held = append(held, *ran[i].pending)
			continue
		}
		slots[runsAt[ran[i].index]] = &ran[i].message
	}
	return held, nil
}

// roundStop decides whether the turn stops after a round whose calls the
// breaker did not answer were rest, held those of them held pending: when
// the termination tool ran, or at the round limit, with its error. A nudged
// round is no exception: in the last round there is none left for the
// model's retry.
func (t *ToolTurn) roundStop(
	rest []types.MessageToolCall, held []tools.PendingToolExecution, refused RoundDecision, rr RoundRef,
) (bool, error) {
	ran := callsRun(rest, held)
	switch {
	case len(held) > 0 && len(ran) == 0:
		// Every call is held: nothing ran, so no limit is reached.
		return false, nil
	case calledStopTool(t.r.Policy(), availableCalls(ran, refused)):
		// termination.tool_called: the named tool has run; stop cleanly. A
		// call refused as unavailable did not.
		return true, nil
	case rr.Round == t.MaxRounds:
		return true, &core.MaxRoundsError{Rounds: t.MaxRounds}
	}
	return false, nil
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

// callKind is how a round answers one of its calls.
type callKind int

const (
	// callRuns: the call runs (or gets its refusal): a refused call, or one
	// within the max_tool_calls_per_turn budget.
	callRuns callKind = iota
	// callLoops: the repeated-identical-call breaker answers the call.
	callLoops
	// callOverBudget: the call is over the budget, refused without running.
	callOverBudget
)

// roundPlan is how a round answers each of its calls, by position.
type roundPlan struct {
	calls []types.MessageToolCall
	kinds []callKind
}

// callsOf returns the calls the plan answers as kind, in order, with each
// one's position in the round.
func (p roundPlan) callsOf(kind callKind) (calls []types.MessageToolCall, at []int) {
	for i, k := range p.kinds {
		if k == kind {
			calls = append(calls, p.calls[i])
			at = append(at, i)
		}
	}
	return calls, at
}

// rest returns every call the breaker does not answer, in order.
func (p roundPlan) rest() []types.MessageToolCall {
	rest := make([]types.MessageToolCall, 0, len(p.calls))
	for i, k := range p.kinds {
		if k != callLoops {
			rest = append(rest, p.calls[i])
		}
	}
	return rest
}

// planRound decides, call by call, how the round answers each of its calls,
// counting each toward the repeated-identical-call breaker and the per-turn
// budget as it goes:
//
//   - A call the round refuses as to a tool the model may not call counts
//     toward the breaker, but it never runs, so it is answered with its
//     refusal: it neither gets the nudge nor uses it up, and no budget counts
//     it.
//   - A call whose (tool, arguments) pair has reached the breaker's threshold
//     is looping: the breaker answers it, and it does not run.
//   - Any other call runs within the budget and counts toward it, or is over
//     the budget and refused. A call over the budget did not run, so it does
//     not count toward the breaker either.
func (t *ToolTurn) planRound(calls []types.MessageToolCall, refused RoundDecision) roundPlan {
	threshold := t.r.getMaxIdenticalToolCalls()
	limit := t.r.getMaxToolCallsPerTurn()
	p := roundPlan{calls: calls, kinds: make([]callKind, len(calls))}
	for i, tc := range calls {
		key := tc.Name + "\x00" + canonicalArgs(tc.Args)
		t.identicalCallCounts[key]++
		_, no := refused.refusal(tc.Name)
		switch {
		case no:
			p.kinds[i] = callRuns
		case t.identicalCallCounts[key] >= threshold:
			p.kinds[i] = callLoops
		case limit > 0 && t.toolCallsExecuted >= limit:
			t.identicalCallCounts[key]--
			p.kinds[i] = callOverBudget
		default:
			t.toolCallsExecuted++
			p.kinds[i] = callRuns
		}
	}
	return p
}

// nudgeOnce applies the repeated-identical-call breaker to a round's looping
// calls. The first time the same tool is called with identical arguments too
// many times, the loop is fed back to the model once (loopNudges) and it may
// self-correct: loops are often transient confusion, such as a model retrying
// a failing call verbatim. If it loops again after the nudge, the error stops
// the turn.
func (t *ToolTurn) nudgeOnce(looping []types.MessageToolCall) error {
	if len(looping) == 0 {
		return nil
	}
	if t.nudgedLoop {
		return fmt.Errorf(
			"provider stage: tool loop persisted after a self-correction nudge: %q called with identical arguments",
			looping[0].Name)
	}
	t.nudgedLoop = true
	logger.Warn("identical tool-call loop detected; feeding it back to the model for one self-correction attempt",
		"tool", looping[0].Name)
	return nil
}

// loopNudges answers each looping call with the breaker's synthetic "you're
// repeating yourself — stop" result.
func (t *ToolTurn) loopNudges(looping []types.MessageToolCall) []types.Message {
	threshold := t.r.getMaxIdenticalToolCalls()
	nudges := make([]types.Message, 0, len(looping))
	for _, tc := range looping {
		msg := fmt.Sprintf(
			"Repeated call ignored: %q has been called %d times with identical arguments and was NOT "+
				"executed again — the result will not change. Stop repeating this exact call: change the "+
				"arguments, use a different tool, or finish the task.",
			tc.Name, threshold)
		nudges = append(nudges, types.NewToolResultMessage(types.NewTextToolResult(tc.ID, tc.Name, msg)))
	}
	return nudges
}

// overBudgetResults answers each call over the max_tool_calls_per_turn budget
// with a policy rejection. Every call the model made still gets a result, so
// the history stays valid to replay.
func overBudgetResults(calls []types.MessageToolCall) []types.Message {
	results := make([]types.Message, 0, len(calls))
	for _, tc := range calls {
		errMsg := fmt.Sprintf("Tool %s not executed: the tool call limit for this turn is reached", tc.Name)
		results = append(results, ToolRefusal(tc, errMsg))
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

// ToolOffer is the offer state that outlives a tools build: the names offered
// during the current turn and the fingerprint of the tools array last built.
// A ProviderStage owns one, and every runner it builds shares it; it is
// locked so a runner can be used from any goroutine. A realtime stage keeps
// its own per-turn offer (duplexToolTurn.offered) and builds runners without
// one: none of the methods that use it run there, and each treats a runner
// without one as an empty offer.
type ToolOffer struct {
	mu sync.Mutex
	// names is the union of the tools offered across the turn's rounds.
	names map[string]bool
	// describedKey fingerprints the tools array last built, as DescribeTool
	// shaped it (see describedToolsKey).
	describedKey string
	// built holds the names of the tools the provider was last sent
	// (markSent).
	built map[string]bool
	// allowed is the turn's allowed tools as the prompt (or the workflow
	// state the turn is in) declares them, before a ToolSelector narrows the
	// tools array: what the model may call is the offer built from them.
	allowed []string
	// view is what DescribeTool last made of each tool it described: true
	// offered under its own name, false withheld or renamed
	// (descriptorCollector.view).
	view map[string]bool
}

// setView records describe's outcomes as the view last built.
func (o *ToolOffer) setView(view map[string]bool) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.view = view
}

// lastView returns the view last built (setView), or nil.
func (o *ToolOffer) lastView() map[string]bool {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.view
}

// setAllowed records the turn's allowed tools.
func (o *ToolOffer) setAllowed(allowed []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.allowed = allowed
}

// allowedTools returns the turn's allowed tools (setAllowed).
func (o *ToolOffer) allowedTools() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.allowed
}

// Offer returns the tools the model may call: the allowed tools and the
// conversation's grants, plus the capability tools the registry holds, less
// excluded, each as DescribeTool describes it, sorted by name. Nil without a
// registry, or when tool calling is off for the turn (tool_choice: none).
func (r *ToolRunner) Offer(allowed []string, excluded map[string]bool) []*tools.ToolDescriptor {
	if r.Registry == nil || r.ToolsDisabled() {
		return nil
	}
	return r.collectOffer(allowed, r.grantedTools(), excluded)
}

// OfferedAmong reports which of the registered tools called offer(allowed,
// nil) would offer the model under their own names: those an allowed_tools
// entry or a grant selects (offerSelects) that DescribeTool keeps without
// renaming, without the turn's exclusions. A tool DescribeTool renames is
// offered under the new name only, so a call to its registry name is not to
// a tool the model was offered. Only called is described: a tool
// DescribeTool already described in this turn's offers is taken as it
// described it then, and only one it never did is described now. None while
// tool calling is off for the turn (tool_choice: none).
func (r *ToolRunner) OfferedAmong(allowed []string, called []*tools.ToolDescriptor) map[string]bool {
	offered := make(map[string]bool, len(called))
	if r.Registry == nil || r.ToolsDisabled() {
		return offered
	}
	grants := r.grantedTools()
	view := r.viewState().lastView()
	for _, tool := range called {
		if !offerSelects(allowed, tool.Name) && !slices.Contains(grants, tool.Name) {
			continue
		}
		kept, described := view[tool.Name]
		if !described {
			kept = r.Config.DescribeTool == nil || offeredAsItself(tool.Name, r.Config.DescribeTool(tool))
		}
		offered[tool.Name] = kept
	}
	return offered
}

// offeredAsItself reports whether described, DescribeTool's result for the
// registry tool name, offers the model that tool under name: it is kept,
// and not renamed.
func offeredAsItself(name string, described *tools.ToolDescriptor) bool {
	return described != nil && described.Name == name
}

// viewState is where DescribeTool's outcomes are recorded and read: views,
// or the offer state when none is set.
func (r *ToolRunner) viewState() *ToolOffer {
	if r.Views != nil {
		return r.Views
	}
	return r.offers
}

// offerSelects reports whether an offer built from entries includes name:
// an entry names it (a trailing "*" a wildcard, as collectDescriptors expands
// one), or it is a capability tool, which every offer includes.
func offerSelects(entries []string, name string) bool {
	return tools.IsImplicitTool(name) || selectsName(entries, name)
}

// selectsSameTools reports whether allowed lists a and b select the same
// registered tools: a wildcard and the names it matches, the same names in
// another order, and nil and empty all select the same. Grants, capability
// tools, exclusions and DescribeTool apply to both alike, so an offer built
// from either is the same.
func (r *ToolRunner) selectsSameTools(a, b []string) bool {
	if r.Registry == nil {
		return true
	}
	same := true
	r.Registry.IterateTools(func(name string, _ *tools.ToolDescriptor) {
		if offerSelects(a, name) != offerSelects(b, name) {
			same = false
		}
	})
	return same
}

// selectsName reports whether an allowed_tools entry names name (a trailing
// "*" a wildcard).
func selectsName(entries []string, name string) bool {
	for _, entry := range entries {
		if tools.MatchToolPattern(entry, name) {
			return true
		}
	}
	return false
}

// ToolsDisabled reports whether tool calling is off for the turn (tool_choice:
// none). The model cannot call a tool then, so declaring any only wastes input
// tokens and, on some models, primes spurious tool calls.
func (r *ToolRunner) ToolsDisabled() bool {
	p := r.Policy()
	return p != nil && p.ToolChoice == ToolChoiceNone
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
//
//nolint:unused // tests call it on the runner
func (r *ToolRunner) collectDescriptors(
	allowedTools []string, excluded map[string]bool,
) []*tools.ToolDescriptor {
	return r.collectOffer(allowedTools, nil, excluded)
}

// collectOffer is collectDescriptors for an offer that also holds grants:
// tool names (ProviderConfig.ToolGrants) offered exactly as named. A grant is
// never expanded as a pattern, so one ending in "*" names no tool and offers
// nothing: no grant can widen the offer beyond the tools it names.
func (r *ToolRunner) collectOffer(
	allowedTools, grants []string, excluded map[string]bool,
) []*tools.ToolDescriptor {
	c := &descriptorCollector{
		registry: r.Registry, describe: r.Config.DescribeTool, excluded: excluded, seen: make(map[string]bool),
	}
	if c.describe != nil {
		c.view = make(map[string]bool)
	}

	// 1. Pack-declared tools from the prompt's allowed list (slice order). An
	//    entry ending in "*" (e.g. mcp__server__*) is a wildcard expanded against
	//    the registry: MCP servers reveal their tool sets only at connection time,
	//    so authors can't enumerate exact names.
	for _, entry := range allowedTools {
		c.addAllowedEntry(entry)
	}
	for _, name := range grants {
		c.addExact(name)
	}

	// 2. Implicit capability tools (skill__, a2a__, workflow__, memory__, media),
	//    gathered in randomized map order — sorted below to make the whole set
	//    stable. MCP tools are NOT implicit: they surface only via an explicit
	//    allowed_tools entry in loop 1 above.
	r.Registry.IterateTools(func(name string, tool *tools.ToolDescriptor) {
		if c.skip(name) || !tools.IsImplicitTool(name) {
			return
		}
		c.add(tool)
	})

	sort.Slice(c.descriptors, func(i, j int) bool {
		return c.descriptors[i].Name < c.descriptors[j].Name
	})
	if c.view != nil {
		r.viewState().setView(c.view)
	}
	return c.descriptors
}

// descriptorCollector accumulates one offer's tool descriptors, skipping
// excluded and already-added names and applying describe to each.
type descriptorCollector struct {
	registry    *tools.Registry
	describe    func(*tools.ToolDescriptor) *tools.ToolDescriptor
	excluded    map[string]bool
	seen        map[string]bool
	descriptors []*tools.ToolDescriptor
	// view records describe's outcome per tool: true offered under its own
	// name, false withheld or renamed (offeredAsItself).
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

// addExact adds the tool registered under name, never expanding a pattern.
func (c *descriptorCollector) addExact(name string) {
	if c.skip(name) {
		return
	}
	if tool := c.registry.Get(name); tool != nil {
		c.add(tool)
	}
}

// add records what the model is offered of tool — its name, description and
// input schema — passing it through describe when one is configured; a nil
// result withholds the tool.
func (c *descriptorCollector) add(tool *tools.ToolDescriptor) {
	c.seen[tool.Name] = true
	if c.describe != nil {
		name := tool.Name
		tool = c.describe(tool)
		c.view[name] = offeredAsItself(name, tool)
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
func (r *ToolRunner) grantedTools() []string {
	if r.Config.ToolGrants == nil {
		return nil
	}
	grants := r.Config.ToolGrants()
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
//
//nolint:unused // tests call it on the stage
func (r *ToolRunner) withGrantedTools(allowedTools []string) []string {
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

// ResetOffered starts a fresh offered set for a turn.
//
// The union is across a turn's ROUNDS — skill grants widen it mid-turn, which
// is the whole point — but a stage outlives the turn: the SDK builds the
// pipeline once per conversation and every Send reuses it, and a streaming
// stage serves a whole session inside one Process call. Without this, a tool
// offered once is reported offered for the rest of the conversation and a
// tools_offered check with absent:true can never fail again.
func (r *ToolRunner) ResetOffered() {
	if r.offers == nil {
		return
	}
	r.offers.mu.Lock()
	defer r.offers.mu.Unlock()
	r.offers.names = nil
	r.offers.view = nil
}

// RecordOffered adds names to the tools handed to the model this turn.
//
// The set is a union across rounds, not a snapshot: skill tool grants widen it
// mid-turn, which is the whole point of them, so "what this turn offered" is
// everything the model saw at any round. Stamped onto each assistant message as
// types.MetaToolsOffered, which is what makes the tools_offered eval possible —
// nothing else records a tool the model was offered but never called.
func (r *ToolRunner) RecordOffered(names ...string) {
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

// OfferedToolNames returns the turn's offered set, sorted, or nil when the turn
// offered no tools at all.
func (r *ToolRunner) OfferedToolNames() []string {
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

// StampToolsOffered records on an assistant message what this turn offered the
// model, so an eval can assert on tool availability and not just on what the
// model chose to call.
//
// Every round implementation must call it. A round that skips it leaves the
// eval with no evidence of the tools offered.
func (r *ToolRunner) StampToolsOffered(msg *types.Message) {
	offered := r.OfferedToolNames()
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
func (r *ToolRunner) describedToolsKey(descriptors []*tools.ToolDescriptor) string {
	if r.Config.DescribeTool == nil {
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

// DescribedChanged reports whether descriptors differ from the tools array
// last built (MarkBuilt), as DescribeTool shapes them.
func (r *ToolRunner) DescribedChanged(descriptors []*tools.ToolDescriptor) bool {
	if r.offers == nil {
		return true
	}
	key := r.describedToolsKey(descriptors)
	r.offers.mu.Lock()
	defer r.offers.mu.Unlock()
	return key != r.offers.describedKey
}

// MarkBuilt records descriptors as the tools array last built, as
// DescribeTool shaped it (DescribedChanged compares against it).
func (r *ToolRunner) MarkBuilt(descriptors []*tools.ToolDescriptor) {
	if r.offers == nil {
		return
	}
	key := r.describedToolsKey(descriptors)
	r.offers.mu.Lock()
	defer r.offers.mu.Unlock()
	r.offers.describedKey = key
}

// markSent records descriptors as the tools the provider was last sent: none
// when it was sent no tools array.
func (r *ToolRunner) markSent(descriptors []*tools.ToolDescriptor) {
	if r.offers == nil {
		return
	}
	sent := make(map[string]bool, len(descriptors))
	for _, d := range descriptors {
		sent[d.Name] = true
	}
	r.offers.mu.Lock()
	defer r.offers.mu.Unlock()
	r.offers.built = sent
}

// sentTools returns the names of the tools the provider was last sent
// (markSent).
func (r *ToolRunner) sentTools() map[string]bool {
	if r.offers == nil {
		return nil
	}
	r.offers.mu.Lock()
	defer r.offers.mu.Unlock()
	return r.offers.built
}

// choiceFits reports whether forced tool choice choice can be given with the
// tools the provider was last sent (markSent): "required" or "any" needs a
// tool among them, and a tool's name needs that tool.
func (r *ToolRunner) choiceFits(choice string) bool {
	sent := r.sentTools()
	if choice == toolChoiceRequired || choice == toolChoiceAny {
		return len(sent) > 0
	}
	return sent[choice]
}
