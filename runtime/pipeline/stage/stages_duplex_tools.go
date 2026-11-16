package stage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// This file holds the duplex provider stage's tool execution: a realtime
// session's tool calls run through the same tool runner as ProviderStage's,
// under the same policy, admission, hooks, approval gate, result shaping,
// events and budgets.
//
// Three goroutines take part. The response goroutine, on a completed assistant
// turn with tool calls, stamps the message, emits provider.call.completed,
// sends the message downstream and only then queues the calls. One tool worker
// per session runs the queued rounds, one at a time. It hands each round's
// results to the input goroutine, the one that already sends to the session,
// which forwards the result messages downstream and sends the tool responses
// to the session (forwardInputMessages, sendToolResponses).
//
// A tool turn is the chain of assistant turns that tool results continue: it
// starts at the first assistant turn after a user turn and ends with an
// assistant turn that makes no tool calls, or when a user turn arrives (typed
// text, a user message, a materialized transcript). A round in flight when a
// new turn starts finishes on the turn that made it: its results answer its
// calls, and its counts stay with that turn.
//
// A limit that ends a text turn (max rounds, a repeated-call loop that survived
// the nudge, the cost budget, a termination tool) cannot end a realtime one:
// the provider waits for an answer to every call. So the calls the limit leaves
// unanswered get a refusal result, the limit is reported as an error element,
// and every later call of the same tool turn is refused without running. The
// session stays open, and the next tool turn starts fresh.

// duplexToolQueueSize bounds the rounds waiting for the tool worker.
const duplexToolQueueSize = 16

// errTerminationToolCalled stops a tool turn whose termination tool ran.
var errTerminationToolCalled = errors.New("the turn's termination tool was called")

// DuplexToolConfig is what a DuplexProviderStage's tool calls run with, as
// the matching ProviderConfig fields are for ProviderStage.
type DuplexToolConfig struct {
	// ApprovalGate holds or refuses a call before it runs (see
	// ProviderConfig.ApprovalGate).
	ApprovalGate tools.ApprovalGate
	// CallRate is the window ToolPolicy.MaxCallsPerMinute counts calls in;
	// nil gives the stage its own.
	CallRate *CallRateWindow
	// Caller is who the conversation acts for (ProviderConfig.Caller).
	Caller types.Caller
	// ToolGrants returns the tools granted beyond the prompt's
	// (ProviderConfig.ToolGrants).
	ToolGrants func() []string
	// DescribeTool shapes how a tool is described to the model
	// (ProviderConfig.DescribeTool).
	DescribeTool func(*tools.ToolDescriptor) *tools.ToolDescriptor
}

// SetToolExecution makes the stage run the session's tool calls through the
// registry, under policy merged with the prompt's tool_policy, recording
// executed calls and stamping workflow state through resolver (nil for none).
// A nil registry, the default, leaves tool calls to the host: they are
// forwarded downstream on the assistant message and nothing runs them here.
// Must be called before the stage runs.
func (s *DuplexProviderStage) SetToolExecution(
	registry *tools.Registry, policy *pipeline.ToolPolicy, cfg DuplexToolConfig, resolver WorkflowStateResolver,
) {
	if registry == nil {
		s.tools = nil
		return
	}
	if cfg.CallRate == nil {
		cfg.CallRate = NewCallRateWindow()
	}
	s.tools = &duplexTools{
		registry: registry,
		policy:   policy,
		cfg:      cfg,
		resolver: resolver,
		offers:   &toolOffer{},
		jobs:     make(chan *duplexToolJob, duplexToolQueueSize),
		results:  make(chan *duplexToolResult),
	}
}

// duplexTools is the stage's tool execution state.
type duplexTools struct {
	registry *tools.Registry
	policy   *pipeline.ToolPolicy
	cfg      DuplexToolConfig
	resolver WorkflowStateResolver
	// offers is shared by every runner the stage builds; it is locked.
	offers *toolOffer
	// jobs carries rounds from the response goroutine to the worker.
	jobs chan *duplexToolJob
	// results carries finished rounds from the worker to the input goroutine.
	results chan *duplexToolResult

	// mu guards the current tool turn, its round count and the session's
	// spend, which the response, input and worker goroutines all reach.
	mu    sync.Mutex
	turn  *duplexToolTurn
	round int
	spent float64

	// sendMu serializes sending tool responses with closing the session, so
	// none is sent after Close.
	sendMu sync.Mutex
	closed bool
}

// duplexToolTurn is a tool turn on a realtime session.
type duplexToolTurn struct {
	*toolTurn
	// stopErr is set once a limit stopped the turn: its later calls are
	// refused. Guarded by duplexTools.mu.
	stopErr error
}

// duplexToolJob is one round for the worker. Without calls it only reports
// err. With refuse set its calls are answered with refusals, not run.
type duplexToolJob struct {
	turn   *duplexToolTurn
	calls  []types.MessageToolCall
	rr     roundRef
	refuse error
	report error
}

// duplexToolResult is a finished round: its result messages, the tool
// responses for the session, the calls held pending, and an error to report.
type duplexToolResult struct {
	elem StreamElement
	err  error
}

// toolPolicy is the policy in force: the stage's merged with the prompt's
// tool_policy, as ProviderStage.policy.
func (s *DuplexProviderStage) toolPolicy() *pipeline.ToolPolicy {
	if s.turnState == nil || s.turnState.Template == nil {
		return s.tools.policy
	}
	return pipeline.MergeToolPolicy(s.tools.policy, s.turnState.Template.ToolPolicy)
}

// toolRunner builds the tool runner from the stage's current fields.
func (s *DuplexProviderStage) toolRunner() *toolRunner {
	dt := s.tools
	return &toolRunner{
		registry:     dt.registry,
		policy:       s.toolPolicy,
		emitter:      s.emitter,
		hookRegistry: s.hookRegistry,
		turnState:    s.turnState,
		cfg: toolRunnerConfig{
			ApprovalGate: dt.cfg.ApprovalGate,
			CallRate:     dt.cfg.CallRate,
			Caller:       dt.cfg.Caller,
			ToolGrants:   dt.cfg.ToolGrants,
			DescribeTool: dt.cfg.DescribeTool,
		},
		stateResolver: dt.resolver,
		offers:        dt.offers,
	}
}

// maxToolRounds is the policy's MaxRounds, or the default.
func (s *DuplexProviderStage) maxToolRounds() int {
	if p := s.toolPolicy(); p != nil && p.MaxRounds > 0 {
		return p.MaxRounds
	}
	return defaultMaxRounds
}

// sessionToolNames returns the names of the tools the session was offered.
func (s *DuplexProviderStage) sessionToolNames() []string {
	if s.baseConfig == nil {
		return nil
	}
	names := make([]string, 0, len(s.baseConfig.Tools))
	for i := range s.baseConfig.Tools {
		names = append(names, s.baseConfig.Tools[i].Name)
	}
	return names
}

// seedToolSpend counts a resumed conversation's spend toward MaxCostUSD, so
// the budget bounds the whole run. No-op without tool execution.
func (s *DuplexProviderStage) seedToolSpend(prior []types.Message) {
	if s.tools == nil {
		return
	}
	s.tools.mu.Lock()
	defer s.tools.mu.Unlock()
	s.tools.spent = sumHistoryCost(prior)
}

// beginUserTurn ends the current tool turn: the next assistant turn starts a
// new one. Nil-safe.
func (dt *duplexTools) beginUserTurn() {
	if dt == nil {
		return
	}
	dt.mu.Lock()
	defer dt.mu.Unlock()
	dt.turn = nil
}

// endTurn ends turn if it is still the current tool turn.
func (dt *duplexTools) endTurn(turn *duplexToolTurn) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	if dt.turn == turn {
		dt.turn = nil
	}
}

// stop marks turn stopped by err, unless it already is, and reports whether
// this call stopped it.
func (dt *duplexTools) stop(turn *duplexToolTurn, err error) bool {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	if turn.stopErr != nil {
		return false
	}
	turn.stopErr = err
	return true
}

// stopped returns what stopped turn, or nil.
func (dt *duplexTools) stopped(turn *duplexToolTurn) error {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return turn.stopErr
}

// markClosed records that the session is closing: no tool response is sent
// from here on. Nil-safe.
func (dt *duplexTools) markClosed() {
	if dt == nil {
		return
	}
	dt.sendMu.Lock()
	defer dt.sendMu.Unlock()
	dt.closed = true
}

// isClosed reports whether the session is closing.
func (dt *duplexTools) isClosed() bool {
	dt.sendMu.Lock()
	defer dt.sendMu.Unlock()
	return dt.closed
}

// toolRoundResults is the channel of finished rounds for the input goroutine,
// or nil (never ready) without tool execution.
func (s *DuplexProviderStage) toolRoundResults() <-chan *duplexToolResult {
	if s.tools == nil {
		return nil
	}
	return s.tools.results
}

// nextToolRound returns the current tool turn, starting one when there is
// none, and the next round of it.
func (s *DuplexProviderStage) nextToolRound() (*duplexToolTurn, roundRef) {
	dt := s.tools
	dt.mu.Lock()
	defer dt.mu.Unlock()
	if dt.turn == nil {
		r := s.toolRunner()
		r.resetOffered()
		r.recordOffered(s.sessionToolNames()...)
		t := r.newTurn(nil, s.maxToolRounds())
		t.cumulativeCost = dt.spent
		dt.turn = &duplexToolTurn{toolTurn: t}
		dt.round = 0
	}
	dt.round++
	return dt.turn, roundRef{round: dt.round, providerCallID: newProviderCallID()}
}

// recordSpend keeps the session's spend after turn charged a provider call.
func (dt *duplexTools) recordSpend(turn *duplexToolTurn) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	dt.spent = turn.cumulativeCost
}

// prepareToolRound is the response goroutine's part of a completed assistant
// turn: it stamps the message with the tools offered and the workflow state,
// emits provider.call.completed, charges the turn's cost, and returns the
// round for the worker once the message is sent, or nil. Without tool
// execution it does nothing.
func (s *DuplexProviderStage) prepareToolRound(ctx context.Context, elem *StreamElement) *duplexToolJob {
	msg := elem.Message
	if s.tools == nil || msg == nil || msg.Role != roleAssistant {
		return nil
	}
	dt := s.tools
	turn, rr := s.nextToolRound()
	turn.r.stampToolsOffered(msg)
	stampWorkflowStateMeta(dt.resolver, msg)
	s.emitToolRoundCompleted(ctx, msg, rr)
	costErr := turn.AddCost(msg.CostInfo)
	dt.recordSpend(turn)

	var report error
	if costErr != nil && dt.stop(turn, costErr) {
		report = costErr
	}
	if len(msg.ToolCalls) == 0 {
		dt.endTurn(turn)
		if report == nil {
			return nil
		}
		return &duplexToolJob{report: report}
	}
	return &duplexToolJob{
		turn: turn, calls: msg.ToolCalls, rr: rr, refuse: dt.stopped(turn), report: report,
	}
}

// emitToolRoundCompleted emits provider.call.completed for an assistant turn.
func (s *DuplexProviderStage) emitToolRoundCompleted(ctx context.Context, msg *types.Message, rr roundRef) {
	if s.emitter == nil {
		return
	}
	data := &events.ProviderCallCompletedData{
		Duration:      time.Duration(msg.LatencyMs) * time.Millisecond,
		ToolCallCount: len(msg.ToolCalls),
		FinishReason:  msg.FinishReason,
		Round:         rr.round,
		CallID:        rr.providerCallID,
	}
	if s.provider != nil {
		data.Provider = s.provider.ID()
		data.Model = s.provider.Model()
	}
	if c := msg.CostInfo; c != nil {
		data.InputTokens = c.InputTokens
		data.OutputTokens = c.OutputTokens
		data.CachedTokens = c.CachedTokens
		data.Cost = c.TotalCost
	}
	s.emitter.ProviderCallCompletedCtx(ctx, data)
}

// queueToolRound hands a prepared round to the worker. Nil-safe on job.
func (s *DuplexProviderStage) queueToolRound(ctx context.Context, job *duplexToolJob) error {
	if job == nil {
		return nil
	}
	select {
	case s.tools.jobs <- job:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runToolWorker runs queued rounds one at a time until ctx ends, handing each
// one's results to the input goroutine. Results it cannot hand over before ctx
// ends are dropped.
func (s *DuplexProviderStage) runToolWorker(ctx context.Context) {
	dt := s.tools
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-dt.jobs:
			res := s.runToolJob(ctx, job)
			select {
			case dt.results <- res:
			case <-ctx.Done():
				return
			}
		}
	}
}

// runToolJob runs one round, or refuses its calls, and returns its results.
func (s *DuplexProviderStage) runToolJob(ctx context.Context, job *duplexToolJob) *duplexToolResult {
	if len(job.calls) == 0 {
		return &duplexToolResult{err: job.report}
	}
	if job.refuse != nil {
		return toolRoundResult(refuseUnanswered(job.calls, nil, nil, job.refuse), nil, job.report)
	}
	out := job.turn.RunRound(ctx, job.calls, job.rr)
	if !out.Stop && out.Err == nil {
		return toolRoundResult(out.Results, out.Pending, nil)
	}
	// A limit, a termination tool or a failed execution ends the tool turn.
	cause := out.Err
	if cause == nil {
		cause = errTerminationToolCalled
	}
	s.tools.stop(job.turn, cause)
	return toolRoundResult(refuseUnanswered(job.calls, out.Results, out.Pending, cause), out.Pending, out.Err)
}

// toolRoundResult packs a round's results for the input goroutine.
func toolRoundResult(
	results []types.Message, pending []tools.PendingToolExecution, report error,
) *duplexToolResult {
	res := &duplexToolResult{err: report}
	res.elem.Meta.ToolResultMessages = results
	res.elem.Meta.ToolResponses = providerToolResponses(results)
	res.elem.Meta.PendingTools = pending
	return res
}

// refuseUnanswered returns results followed by a refusal naming cause for
// each call that has neither a result nor a pending hold, so the provider is
// given an answer to every call it made.
func refuseUnanswered(
	calls []types.MessageToolCall, results []types.Message, pending []tools.PendingToolExecution, cause error,
) []types.Message {
	answered := make(map[string]bool, len(results)+len(pending))
	for i := range results {
		if tr := results[i].ToolResult; tr != nil {
			answered[tr.ID] = true
		}
	}
	for i := range pending {
		answered[pending[i].CallID] = true
	}
	out := append([]types.Message(nil), results...)
	for _, tc := range calls {
		if answered[tc.ID] {
			continue
		}
		errMsg := fmt.Sprintf("Tool %s not executed: the turn's tool calls were stopped: %v", tc.Name, cause)
		result := types.NewTextToolResult(tc.ID, tc.Name, errMsg)
		result.Error = errMsg
		result.ErrorType = types.ToolErrorApproval
		out = append(out, types.NewToolResultMessage(result))
	}
	return out
}

// providerToolResponses converts tool result messages to the responses the
// session is sent.
func providerToolResponses(results []types.Message) []providers.ToolResponse {
	out := make([]providers.ToolResponse, 0, len(results))
	for i := range results {
		tr := results[i].ToolResult
		if tr == nil {
			continue
		}
		out = append(out, providers.ToolResponse{
			ToolCallID: tr.ID,
			Result:     tr.GetTextContent(),
			IsError:    tr.Error != "",
		})
	}
	return out
}

// deliverToolRound is the input goroutine's part of a finished round: the
// result messages go downstream (and into the hooks' history), the calls held
// pending go out on a control element, the responses go to the session, and
// a limit is reported as an error element. A round that finishes once the
// session is closing is dropped.
func (s *DuplexProviderStage) deliverToolRound(
	ctx context.Context, res *duplexToolResult, output chan<- StreamElement,
) error {
	dt := s.tools
	if dt.isClosed() {
		return nil
	}
	if err := s.forwardInputMessages(ctx, &res.elem, output); err != nil {
		return err
	}
	if pending := res.elem.Meta.PendingTools; len(pending) > 0 {
		var held StreamElement
		held.Meta.PendingTools = pending
		if err := sendStreamElement(ctx, held, output); err != nil {
			return err
		}
	}
	if responses := res.elem.Meta.ToolResponses; len(responses) > 0 {
		dt.sendMu.Lock()
		if !dt.closed {
			s.sendToolResponses(ctx, responses)
		}
		dt.sendMu.Unlock()
	}
	if res.err != nil {
		return sendStreamElement(ctx, NewErrorElement(res.err), output)
	}
	return nil
}

// serveToolRoundsAfterInput keeps delivering finished rounds once input has
// ended, until ctx ends: the session is still answering, and its tool calls
// still need their results. It reports a clean exit on done.
func (s *DuplexProviderStage) serveToolRoundsAfterInput(
	ctx context.Context, output chan<- StreamElement, done chan error,
) {
	s.markInputDone()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case res := <-s.tools.results:
			if err := s.deliverToolRound(ctx, res, output); err != nil {
				done <- nil
				return
			}
		}
	}
}
