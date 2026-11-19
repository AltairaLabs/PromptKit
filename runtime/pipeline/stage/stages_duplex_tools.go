package stage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
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
// session stays open and the next tool turn starts fresh, unless the model
// keeps calling tools: past maxStoppedTurnRefusals refused batches the session
// is closed. The cost budget is different: MaxCostUSD bounds the whole
// session, so once the spend passes it the calls are refused (the refusals are
// recorded downstream but not sent: a tool response starts another paid
// reply) and the session is closed.

// duplexToolQueueSize bounds the rounds waiting for the tool worker.
const duplexToolQueueSize = 16

// maxStoppedTurnRefusals is how many batches of calls a stopped tool turn
// answers with refusals. The model calling tools again after that closes the
// session: every refusal is another paid provider turn.
const maxStoppedTurnRefusals = 2

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
	// Labels are propagated to the provider call events
	// (ProviderConfig.Labels).
	Labels map[string]string
	// Source is the origin of the provider calls: "agent" (default), "judge",
	// "selfplay" (ProviderConfig.Source).
	Source string
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
	// offer is what the session holds of the tool offer
	// (stages_duplex_tool_offer.go).
	offer sessionOffer
	// view holds what DescribeTool made of each tool the session's last
	// sync offered (toolRunner.views).
	view toolOffer
	// jobs carries rounds from the response goroutine to the worker.
	jobs chan *duplexToolJob
	// results carries finished rounds from the worker to the input goroutine.
	results chan *duplexToolResult

	// mu guards the current tool turn, its round count, the provider call in
	// progress, the session's spend and the terminal flag, which the
	// response, input and worker goroutines all reach.
	mu sync.Mutex
	// completionMu serializes sending provider.call.completed and
	// provider.call.usage events, so held completions go out in order and a
	// call's usage never precedes its completion. Taken before mu.
	completionMu sync.Mutex
	turn         *duplexToolTurn
	round        int
	open         *duplexRound
	// gens maps the provider's generations to the calls opened for them, so
	// cost reported for a generation lands on its own call; last is the call
	// started last (stages_duplex_cost.go). genOrder bounds gens.
	gens     map[int64]*duplexRound
	genOrder []int64
	// newTurnDue is set by a user turn that arrived while a call was under
	// way: the next generation's call starts the user's tool turn.
	newTurnDue bool
	// forgottenUpTo is the newest generation evicted from gens.
	forgottenUpTo int64
	last          *duplexRound
	spent         float64
	// held are the completions waiting for their call's usage, in the order
	// the calls completed (stages_duplex_cost_hold.go).
	held []*heldCompletion
	// terminal is set once a limit closes the session: nothing more is run
	// or reported.
	terminal bool

	// sendMu serializes sending tool responses with closing the session, so
	// none is sent after Close.
	sendMu sync.Mutex
	closed bool

	// inflight counts the rounds queued for the worker, running, or waiting
	// to be delivered. replyDue is set once a round's responses are sent and
	// cleared when the next assistant turn completes: the reply they start.
	inflight atomic.Int64
	replyDue atomic.Bool

	// stopWorker cancels the worker's context and workerDone closes once the
	// worker has returned (startToolWorker); undelivered are the rounds it
	// finished and could not hand over before it was stopped
	// (finishToolRounds).
	stopWorker context.CancelFunc
	workerDone chan struct{}
	// workerMu guards undelivered and abandoned, set once finishToolRounds
	// has gone on without the worker.
	workerMu    sync.Mutex
	undelivered []*duplexToolResult
	abandoned   bool
}

// duplexRound is a realtime provider call in progress: the round it is and
// when it started.
type duplexRound struct {
	rr    roundRef
	start time.Time
	// turn is the tool turn the call belongs to; gen is the provider
	// generation it is for (0 until a chunk names it); pending is cost the
	// provider reported for it while it was open, which its
	// provider.call.completed carries. Guarded by duplexTools.mu.
	turn    *duplexToolTurn
	gen     int64
	pending *types.CostInfo
	// startedCh is closed once its provider.call.started is out: its
	// completion and usage events wait for it, so they never precede it.
	startedCh chan struct{}
}

// duplexToolTurn is a tool turn on a realtime session.
type duplexToolTurn struct {
	*toolTurn
	// stopErr is set once a limit stopped the turn: its later calls are
	// refused. refusals counts the batches refused for it. Both are guarded
	// by duplexTools.mu.
	stopErr  error
	refusals int
	// offered names every tool the session held during the turn, for
	// tools_offered. Guarded by duplexTools.mu.
	offered map[string]bool
}

// duplexToolJob is one round for the worker. Without calls it only reports
// err. With refuse set its calls are answered with refusals, not run. A
// terminal job closes the session once its results are delivered.
type duplexToolJob struct {
	turn     *duplexToolTurn
	calls    []types.MessageToolCall
	rr       roundRef
	refuse   error
	report   error
	terminal bool
}

// duplexToolResult is a finished round: its result messages, the tool
// responses for the session, the calls held pending, an error to report, and
// whether the session closes after it.
type duplexToolResult struct {
	elem         StreamElement
	err          error
	turn         *duplexToolTurn
	closeSession bool
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
	r := &toolRunner{
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
		// The session's syncs record what DescribeTool made of each tool they
		// offered (descriptorCollector.view), so a round's check reuses it.
		views: &dt.view,
	}
	r.availability = func(called []*tools.ToolDescriptor, excluded map[string]bool) roundRefusals {
		return s.roundRefusals(r, called, excluded)
	}
	return r
}

// roundRefusals refuses each called tool the model may not call this round
// on the session. What the conversation offers the session (wanted: the
// tools a text turn would offer, without the turn's exclusions, and the
// host's StreamingInputConfig.Tools; none under a tool_choice of none) and
// what the session holds are both taken now, so a grant or a revocation
// since the session's tools were last brought in line is judged as it stands
// when the round begins.
//
// A wanted tool runs when the session holds it, or when the turn excluded
// it and the session dropped it for that (an exclusion is not a
// withdrawal; a tool the session never held is refused, and a refusal never
// excludes a tool, so an excluded tool is one the session held). On a
// session that cannot change its tools, or whose update failed, a wanted
// tool it lacks is refused as having become available and a held tool not
// wanted as withdrawn (sessionSyncRefusal). Any other is not available. The
// stage runs tools for the session, so this applies even when the offer is
// empty.
//
// No lock is taken: the session's tools are read from their snapshot, so a
// sync holding the offer's lock across the provider's UpdateTools does not
// hold up the round, and the host's ToolGrants and DescribeTool run under no
// lock of the stage's.
func (s *DuplexProviderStage) roundRefusals(
	r *toolRunner, called []*tools.ToolDescriptor, excluded map[string]bool,
) roundRefusals {
	wanted := s.sessionOffers(r, called)
	o := &s.tools.offer
	held := o.heldSet()
	fixed := !s.sessionCanUpdateTools() || o.updateFailed.Load()
	return refuseAmong(called, func(name string) (string, bool) {
		switch {
		case wanted[name] && (held[name] || excluded[name]):
			return "", false
		case fixed && wanted[name] != held[name]:
			return o.sessionSyncRefusal(name, wanted[name]), true
		default:
			return notAvailableRefusal(name), true
		}
	})
}

// sessionCanUpdateTools reports whether the session can change its tools
// (providers.StreamToolUpdater).
func (s *DuplexProviderStage) sessionCanUpdateTools() bool {
	_, ok := s.session.(providers.StreamToolUpdater)
	return ok
}

// sessionOffers reports which of called the session is offered, by the
// rules its sync applies (sessionTools): the called tools the conversation
// offers (offeredAmong, without the turn's exclusions), merged with the
// host's tools. Only called is resolved, and DescribeTool's outcome for a
// tool is taken from the session's last sync where it described it.
func (s *DuplexProviderStage) sessionOffers(r *toolRunner, called []*tools.ToolDescriptor) map[string]bool {
	offered := r.offeredAmong(s.allowedTools(), called)
	offer := make([]*tools.ToolDescriptor, 0, len(offered))
	for _, tool := range called {
		if offered[tool.Name] {
			offer = append(offer, &tools.ToolDescriptor{Name: tool.Name})
		}
	}
	wanted := make(map[string]bool, len(offer))
	for _, name := range toolDefNames(s.sessionTools(r, offer, nil)) {
		wanted[name] = true
	}
	return wanted
}

// maxToolRounds is the policy's MaxRounds, or the default.
func (s *DuplexProviderStage) maxToolRounds() int {
	if p := s.toolPolicy(); p != nil && p.MaxRounds > 0 {
		return p.MaxRounds
	}
	return defaultMaxRounds
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

// startToolTurnLocked starts a new tool turn, which has offered the tools
// named by offered so far, and returns it. Called with dt.mu held.
func (s *DuplexProviderStage) startToolTurnLocked(offered []string) *duplexToolTurn {
	dt := s.tools
	dt.turn = &duplexToolTurn{toolTurn: s.toolRunner().newTurn(nil, s.maxToolRounds())}
	recordOfferedLocked(dt.turn, offered)
	dt.round = 0
	return dt.turn
}

// openRoundLocked starts the next provider call of the current tool turn.
// Called with dt.mu held.
func (dt *duplexTools) openRoundLocked() *duplexRound {
	dt.round++
	dt.open = &duplexRound{
		rr:        roundRef{round: dt.round, providerCallID: newProviderCallID()},
		start:     time.Now(),
		turn:      dt.turn,
		startedCh: make(chan struct{}),
	}
	dt.last = dt.open
	return dt.open
}

// beginToolUserTurn starts a new tool turn for a user turn, and its first
// provider call, and brings the session's tools in line with the new turn (a
// tool excluded or withdrawn on the last one). A user turn whose first call
// has not completed yet (more input before the reply) keeps it. Nil-safe.
func (s *DuplexProviderStage) beginToolUserTurn(ctx context.Context) {
	dt := s.tools
	if dt == nil {
		return
	}
	dt.mu.Lock()
	if dt.terminal || (dt.open != nil && dt.round == 1 && dt.open.gen == 0) {
		dt.mu.Unlock()
		return
	}
	if dt.open != nil {
		// User input never ends a call: the one under way goes on to its own
		// end. The user's turn starts with the next generation's call.
		dt.newTurnDue = true
		dt.mu.Unlock()
		return
	}
	// The turn's offer is what the session holds once it is brought in line.
	turn := s.startToolTurnLocked(nil)
	opened := dt.openRoundLocked()
	dt.newTurnDue = false
	dt.mu.Unlock()
	s.syncSessionTools(ctx, turn, nil, true)
	s.emitRoundStarted(ctx, opened)
}

// beginTranscribedToolTurn is beginToolUserTurn for a user turn known from
// its transcript, which can arrive once the reply to it has begun: a provider
// call already in progress is then that reply's, and is kept. Nil-safe.
func (s *DuplexProviderStage) beginTranscribedToolTurn(ctx context.Context) {
	dt := s.tools
	if dt == nil {
		return
	}
	dt.mu.Lock()
	inProgress := dt.open != nil
	dt.mu.Unlock()
	if !inProgress {
		s.beginToolUserTurn(ctx)
	}
}

// openToolRoundIfIdle starts a provider call when the session answers with
// none in progress: input the stage cannot see the end of (audio the provider
// does not transcribe). Nil-safe.
func (s *DuplexProviderStage) openToolRoundIfIdle(ctx context.Context) {
	dt := s.tools
	if dt == nil {
		return
	}
	dt.mu.Lock()
	if dt.terminal || dt.open != nil {
		dt.mu.Unlock()
		return
	}
	var started *duplexToolTurn
	if dt.turn == nil {
		started = s.startToolTurnLocked(dt.offer.heldNames())
	}
	opened := dt.openRoundLocked()
	dt.mu.Unlock()
	s.syncStartedTurn(ctx, started)
	s.emitRoundStarted(ctx, opened)
}

// syncStartedTurn brings the session's tools in line with a tool turn that
// started once the session was already answering (no user turn was seen): an
// exclusion of the last turn is lifted for the replies that follow. The reply
// under way began with the tools the session held, which the turn was
// started with, so nothing more is recorded as offered. Nil-safe on turn.
func (s *DuplexProviderStage) syncStartedTurn(ctx context.Context, turn *duplexToolTurn) {
	if turn != nil {
		s.syncSessionTools(ctx, turn, nil, false)
	}
}

// openNextToolRound starts the provider call that tool responses for turn
// start, while turn is still current and no call is in progress.
func (s *DuplexProviderStage) openNextToolRound(ctx context.Context, turn *duplexToolTurn) {
	dt := s.tools
	dt.mu.Lock()
	if dt.terminal || dt.turn != turn || dt.open != nil {
		dt.mu.Unlock()
		return
	}
	opened := dt.openRoundLocked()
	dt.mu.Unlock()
	s.emitRoundStarted(ctx, opened)
}

// completion is a provider call closed and not yet reported: its
// provider.call.completed is emitted once whether it produced a message is
// known (after the output guardrails).
type completion struct {
	round    *duplexRound
	turn     *duplexToolTurn
	reported types.Message
	dur      time.Duration
}

// closeRound closes the provider call the chunk being handled belongs to:
// the call its generation is bound to, or the open call (starting one when
// none is) for a generation not bound yet. It reports false, closing
// nothing, when the generation's call has already completed: the chunk is
// then that call's late cost. Response goroutine only.
func (s *DuplexProviderStage) closeRound(ctx context.Context, msg *types.Message) (*completion, bool) {
	dt := s.tools
	dt.mu.Lock()
	if _, st := dt.lookupLocked(s.chunkGen); st == genCompleted || st == genForgotten {
		dt.mu.Unlock()
		return nil, false
	}
	var started *duplexToolTurn
	if dt.turn == nil {
		started = s.startToolTurnLocked(dt.offer.heldNames())
	}
	round := dt.open
	opened := round == nil
	if opened {
		round = dt.openRoundLocked()
	}
	dt.bindGenerationLocked(s.chunkGen)
	dt.open = nil
	c := &completion{round: round, turn: dt.turn, reported: *msg, dur: time.Since(round.start)}
	c.reported.CostInfo = addCost(msg.CostInfo, round.pending)
	dt.mu.Unlock()
	dt.replyDue.Store(false)
	s.syncStartedTurn(ctx, started)
	if opened {
		s.emitRoundStarted(ctx, round)
	}
	return c, true
}

// emitCompletion emits a closed call's provider.call.completed. Nil-safe.
func (s *DuplexProviderStage) emitCompletion(ctx context.Context, c *completion, noMessage bool) {
	if c != nil {
		s.emitRoundCompleted(ctx, &c.reported, c.round, c.dur, noMessage)
	}
}

// completeRound closes the chunk's call and reports it at once.
func (s *DuplexProviderStage) completeRound(
	ctx context.Context, msg *types.Message, noMessage bool,
) (*duplexToolTurn, roundRef, bool) {
	c, ok := s.closeRound(ctx, msg)
	if !ok {
		return nil, roundRef{}, false
	}
	s.emitCompletion(ctx, c, noMessage)
	return c.turn, c.round.rr, true
}

// charge adds cost to the session's spend and returns the budget error once
// it exceeds ToolPolicy.MaxCostUSD.
func (s *DuplexProviderStage) charge(turn *duplexToolTurn, cost *types.CostInfo) error {
	dt := s.tools
	dt.mu.Lock()
	defer dt.mu.Unlock()
	t := turn.toolTurn
	t.cumulativeCost = dt.spent
	err := t.AddCost(cost)
	dt.spent = t.cumulativeCost
	return err
}

// callInProgress reports whether a provider call is open.
func (dt *duplexTools) callInProgress() bool {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return dt.open != nil
}

// setTerminal marks the session as closing for a limit and reports whether
// this call did.
func (dt *duplexTools) setTerminal() bool {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	if dt.terminal {
		return false
	}
	dt.terminal = true
	return true
}

// isTerminal reports whether a limit is closing the session.
func (dt *duplexTools) isTerminal() bool {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return dt.terminal
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

// addRefusal counts a batch refused for stopped turn and returns the count.
func (dt *duplexTools) addRefusal(turn *duplexToolTurn) int {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	turn.refusals++
	return turn.refusals
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

// settledCall is a turn's completed provider call: its tool turn and round,
// the message (nil when the turn produced none), and the budget error its
// cost hit.
type settledCall struct {
	turn      *duplexToolTurn
	rr        roundRef
	msg       *types.Message
	chargeErr error
	// done is the call's completion, reported once the turn's message is
	// known to be emitted or not.
	done *completion
}

// settleTurnCall completes the provider call a turn's end completes and
// charges its cost, whatever follows (a limit closing the session, an output
// guardrail denying the turn). A turn's end with no message completes its
// call too. Returns nil without tool execution or when the element ends no
// turn.
func (s *DuplexProviderStage) settleTurnCall(ctx context.Context, elem *StreamElement) *settledCall {
	dt := s.tools
	if dt == nil || !elem.EndOfStream {
		return nil
	}
	msg := elem.Message
	if msg != nil && msg.Role != roleAssistant {
		return nil
	}
	noMessage := msg == nil
	if noMessage {
		msg = &types.Message{Role: roleAssistant, CostInfo: s.chunkCost}
		if elem.Meta.FinishReason != nil {
			msg.FinishReason = *elem.Meta.FinishReason
		}
	}
	done, ok := s.closeRound(ctx, msg)
	if !ok {
		return nil // a completed call's end: its cost is late cost
	}
	turn, rr := done.turn, done.round.rr
	s.costReported = true // the message's cost, the chunk's when it had one
	if !noMessage {
		dt.stampToolsOffered(turn, msg)
		stampWorkflowStateMeta(dt.resolver, msg)
	}
	settled := &settledCall{turn: turn, rr: rr, chargeErr: s.charge(turn, msg.CostInfo), done: done}
	if !noMessage {
		settled.msg = msg
	}
	return settled
}

// toolJob is the round for the worker a settled turn leaves: its calls, the
// refusal of a spent budget, or nil (the tool turn ends). Nil-safe.
func (s *DuplexProviderStage) toolJob(settled *settledCall) *duplexToolJob {
	if settled == nil {
		return nil
	}
	dt := s.tools
	var calls []types.MessageToolCall
	if settled.msg != nil {
		calls = settled.msg.ToolCalls
	}
	if settled.chargeErr != nil {
		return s.budgetSpent(settled.turn, calls, settled.rr, settled.chargeErr)
	}
	if len(calls) == 0 || dt.isTerminal() {
		dt.endTurn(settled.turn)
		return closingRefusal(settled.turn, calls, settled.rr)
	}
	return &duplexToolJob{turn: settled.turn, calls: calls, rr: settled.rr}
}

// errSessionClosing is the cause recorded for a call made once a limit is
// closing the session.
var errSessionClosing = errors.New("a limit is closing the session")

// closingRefusal is the job for calls made once a limit is closing the
// session: they are refused, recorded and not sent, so each still has its
// result. Nil without calls.
func closingRefusal(turn *duplexToolTurn, calls []types.MessageToolCall, rr roundRef) *duplexToolJob {
	if len(calls) == 0 {
		return nil
	}
	return &duplexToolJob{turn: turn, calls: calls, rr: rr, refuse: errSessionClosing, terminal: true}
}

// budgetSpent is the job for a provider call that took the session past
// MaxCostUSD: its calls are refused, the budget is reported once, and the
// session is closed so no further paid turn follows.
func (s *DuplexProviderStage) budgetSpent(
	turn *duplexToolTurn, calls []types.MessageToolCall, rr roundRef, err error,
) *duplexToolJob {
	if !s.tools.setTerminal() {
		return closingRefusal(turn, calls, rr)
	}
	s.tools.stop(turn, err)
	return &duplexToolJob{turn: turn, calls: calls, rr: rr, refuse: err, report: err, terminal: true}
}

// chargeInterruptedTurn is the response goroutine's part of a turn a barge-in
// cut short: the provider call it was completes, its cost counts toward the
// budget, and the tool turn ends (a user turn is under way). Returns the job
// reporting a spent budget, or nil.
func (s *DuplexProviderStage) chargeInterruptedTurn(
	ctx context.Context, chunk *providers.StreamChunk, elem *StreamElement,
) *duplexToolJob {
	if s.tools == nil || !elem.Meta.Interrupted {
		return nil
	}
	if !s.tools.interruptsOpenCall(chunk.Generation) ||
		(elem.Message == nil && chunk.CostInfo == nil && !s.tools.callInProgress()) {
		// Nothing under way to interrupt, or the interruption names a call
		// already completed: its cost, if any, is that call's late cost.
		return nil
	}
	msg := elem.Message
	if msg == nil {
		msg = &types.Message{Role: roleAssistant, FinishReason: types.FinishReasonInterrupted}
	}
	withCost := *msg
	withCost.CostInfo = chunk.CostInfo
	turn, rr, ok := s.completeRound(ctx, &withCost, elem.Message == nil)
	if !ok {
		return nil
	}
	s.tools.endTurn(turn)
	s.costReported = true
	if err := s.charge(turn, chunk.CostInfo); err != nil {
		return s.budgetSpent(turn, nil, rr, err)
	}
	return nil
}

// emitRoundStarted emits provider.call.started for a realtime round, as
// ProviderStage.emitProviderCallStarted does for a round.
func (s *DuplexProviderStage) emitRoundStarted(ctx context.Context, round *duplexRound) {
	defer close(round.startedCh)
	rr := round.rr
	if s.emitter == nil {
		return
	}
	data := &events.ProviderCallStartedData{
		ToolCount: len(s.tools.offer.heldNames()),
		Labels:    s.tools.cfg.Labels,
		Round:     rr.round,
		CallID:    rr.providerCallID,
	}
	if s.provider != nil {
		data.Provider = s.provider.ID()
		data.Model = s.provider.Model()
	}
	s.emitter.ProviderCallStartedCtx(ctx, data)
}

// emitRoundCompletedNow emits provider.call.completed for round at once.
func (s *DuplexProviderStage) emitRoundCompletedNow(
	ctx context.Context, msg *types.Message, round *duplexRound, dur time.Duration, noMessage bool,
) {
	if s.emitter == nil {
		return
	}
	if round.startedCh != nil {
		<-round.startedCh // never ahead of the call's provider.call.started
	}
	data := &events.ProviderCallCompletedData{
		Realtime:      true,
		Duration:      dur,
		ToolCallCount: len(msg.ToolCalls),
		FinishReason:  msg.FinishReason,
		Source:        s.tools.cfg.Source,
		Labels:        s.tools.cfg.Labels,
		Round:         round.rr.round,
		CallID:        round.rr.providerCallID,
		NoMessage:     noMessage,
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
	s.tools.inflight.Add(1)
	select {
	case s.tools.jobs <- job:
		return nil
	case <-ctx.Done():
		s.tools.inflight.Add(-1)
		return ctx.Err()
	}
}

// roundOutstanding reports whether a tool round is queued, running, or
// waiting to be delivered, or the reply its responses started has not
// completed. A provider call with no tool round behind it does not count.
// False without tool execution.
func (dt *duplexTools) roundOutstanding() bool {
	if dt == nil {
		return false
	}
	return dt.inflight.Load() > 0 || dt.replyDue.Load()
}

// startToolWorker starts the worker that runs the session's tool rounds, on a
// context of its own derived from ctx. Nil-safe without tool execution.
func (s *DuplexProviderStage) startToolWorker(ctx context.Context) {
	dt := s.tools
	if dt == nil {
		return
	}
	workerCtx, stop := context.WithCancel(ctx)
	dt.stopWorker = stop
	dt.workerDone = make(chan struct{})
	go s.runToolWorker(workerCtx)
}

// runToolWorker runs queued rounds one at a time until ctx ends, handing each
// one's results to the input goroutine. A round it cannot hand over before ctx
// ends is kept in undelivered, for finishToolRounds to record.
func (s *DuplexProviderStage) runToolWorker(ctx context.Context) {
	dt := s.tools
	defer close(dt.workerDone)
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case job := <-dt.jobs:
			if ctx.Err() != nil {
				return // never run once stopped: finishToolRounds refuses its calls
			}
			res := s.runToolJob(ctx, job)
			res.turn = job.turn
			select {
			case dt.results <- res:
			case <-ctx.Done():
				dt.keepUndelivered(res)
				return
			}
		}
	}
}

// keepUndelivered keeps a round the worker finished once stopped, for
// finishToolRounds to record, unless finishToolRounds has already gone on
// without the worker: it is then dropped.
func (dt *duplexTools) keepUndelivered(res *duplexToolResult) {
	dt.workerMu.Lock()
	defer dt.workerMu.Unlock()
	if dt.abandoned {
		logger.Warn("DuplexProviderStage: a tool round finished after the session ended; its result is dropped")
		return
	}
	dt.undelivered = append(dt.undelivered, res)
}

// takeUndelivered returns the rounds the worker kept, and stops it keeping
// any more.
func (dt *duplexTools) takeUndelivered() []*duplexToolResult {
	dt.workerMu.Lock()
	defer dt.workerMu.Unlock()
	dt.abandoned = true
	out := dt.undelivered
	dt.undelivered = nil
	return out
}

// finishToolRounds ends the session's tool execution once nothing else is
// emitting: it stops the worker and waits for it to return, for the stage's
// input grace at most (a running tool is canceled; a round it returns in time
// is recorded, one it returns later is dropped), answers every call left with
// no result (a round cut short or still queued, a call held for approval)
// with a refusal, emits what was held behind them, and completes the provider
// call still open, so its provider.call.started has its
// provider.call.completed, which, with every completion still waiting for its
// usage, goes out now. Nil-safe without tool execution.
func (s *DuplexProviderStage) finishToolRounds(ctx context.Context, output chan<- StreamElement) {
	dt := s.tools
	if dt == nil {
		return
	}
	dt.markClosed()
	if dt.stopWorker != nil {
		dt.stopWorker()
		select {
		case <-dt.workerDone:
		case <-time.After(s.inputGrace()):
			logger.Warn("DuplexProviderStage: a tool call did not return when canceled; the session ends without it")
		}
	}
	deadline := newCloseDeadline()
	for _, res := range dt.takeUndelivered() {
		// A result not taken in time is not recorded; its calls are answered
		// below.
		_ = s.recordToolRoundBy(ctx, res, output, sendMode{recorded: true, deadline: deadline})
	}
	s.exchange.closeOut(ctx, output, deadline)
	s.completeOpenCall(ctx, nil, false)
	// The session has ended: completions waiting for usage go out now.
	s.flushHeldCompletions()
}

// runToolJob runs one round, or refuses its calls, and returns its results.
// Whether the round runs is decided now, not when it was queued: an earlier
// round may have stopped its turn since.
func (s *DuplexProviderStage) runToolJob(ctx context.Context, job *duplexToolJob) *duplexToolResult {
	switch {
	case len(job.calls) == 0:
		return &duplexToolResult{err: job.report, closeSession: job.terminal}
	case job.refuse != nil:
		res := toolRoundResult(refuseUnanswered(job.calls, nil, nil, job.refuse), nil, job.report)
		if job.terminal {
			// Recorded, not sent: a tool response starts another paid reply,
			// and the session closes instead.
			res.elem.Meta.ToolResponses = nil
			res.closeSession = true
		}
		return res
	}
	if err := s.tools.stopped(job.turn); err != nil {
		return s.refuseStoppedTurn(job, err)
	}
	if limit := job.turn.maxRounds; job.rr.round > limit {
		// Past the limit without having stopped on it: the limit round held
		// every call it made, or did not reach the worker first.
		err := &MaxRoundsError{Rounds: limit}
		var report error
		if s.tools.stop(job.turn, err) {
			report = err
		}
		return toolRoundResult(refuseUnanswered(job.calls, nil, nil, err), nil, report)
	}
	return s.runRound(ctx, job)
}

// runRound runs a round's calls through the tool runner, which refuses those
// to tools the session lacks, the conversation has withdrawn or the model
// may not call (roundRefusals), then brings the session's tools in line with
// what the round changed, before its responses are sent. A limit, a
// termination tool or a failed execution stops the tool turn. A model that
// keeps making only refused calls is bounded as for any calls: by the
// repeated-call breaker and the round limit, then the stopped turn's
// refusal cap (refuseStoppedTurn).
func (s *DuplexProviderStage) runRound(ctx context.Context, job *duplexToolJob) *duplexToolResult {
	out := job.turn.RunRoundPastPending(ctx, job.calls, job.rr)
	s.syncSessionTools(ctx, job.turn, job.turn.excluded, true)
	if out.AllRefused {
		if res, stopped := s.countRefusedRound(job); stopped {
			return res
		}
	}
	if !out.Stop && out.Err == nil {
		return toolRoundResult(out.Results, out.Pending, nil)
	}
	cause := out.Err
	if cause == nil {
		cause = errTerminationToolCalled
	}
	s.tools.stop(job.turn, cause)
	return toolRoundResult(refuseUnanswered(job.calls, out.Results, out.Pending, cause), out.Pending, out.Err)
}

// countRefusedRound counts a round whose every call was refused as to a tool
// the model may not call, as a stopped turn's refusals are counted: a model
// that keeps calling unavailable tools, with new arguments each time so the
// repeated-call breaker never trips, is not answered until the round limit.
// Past maxStoppedTurnRefusals such batches the turn is stopped, and the
// session closed, with an error naming the tool, and stopped is set.
func (s *DuplexProviderStage) countRefusedRound(job *duplexToolJob) (res *duplexToolResult, stopped bool) {
	if s.tools.addRefusal(job.turn) <= maxStoppedTurnRefusals {
		return nil, false
	}
	err := fmt.Errorf("tool %s is not available in this realtime session", job.calls[0].Name)
	s.tools.stop(job.turn, err)
	return s.refuseStoppedTurn(job, err), true
}

// refuseStoppedTurn answers the calls of a stopped tool turn with refusals.
// Past maxStoppedTurnRefusals batches the model is not answered again: the
// session is closed, with an error naming why.
func (s *DuplexProviderStage) refuseStoppedTurn(job *duplexToolJob, cause error) *duplexToolResult {
	refusals := refuseUnanswered(job.calls, nil, nil, cause)
	if s.tools.addRefusal(job.turn) <= maxStoppedTurnRefusals {
		return toolRoundResult(refusals, nil, nil)
	}
	s.tools.setTerminal()
	res := toolRoundResult(refusals, nil, fmt.Errorf(
		"duplex provider stage: closing the session: the model kept calling tools after its tool turn was stopped: %w",
		cause))
	res.elem.Meta.ToolResponses = nil // recorded, not sent: the session closes instead
	res.closeSession = true
	return res
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
		out = append(out, toolRefusal(tc, errMsg))
	}
	return out
}

// providerToolResponses converts tool result messages to the responses the
// session is sent. A result with media carries its parts as well as its
// text; a text-only result (every refusal and failure is one) carries text
// only.
func providerToolResponses(results []types.Message) []providers.ToolResponse {
	out := make([]providers.ToolResponse, 0, len(results))
	for i := range results {
		tr := results[i].ToolResult
		if tr == nil {
			continue
		}
		resp := providers.ToolResponse{
			ToolCallID: tr.ID,
			Result:     tr.GetTextContent(),
			IsError:    tr.Error != "",
		}
		if tr.HasMedia() {
			resp.Parts = tr.Parts
		}
		out = append(out, resp)
	}
	return out
}

// prepareToolResponseText returns the responses to send to session. A session
// that can send media parts gets them as they are. One that cannot sends their
// text only: a response whose parts carry no text gets a placeholder naming
// the part types instead of an empty result, and the stage warns once (see
// warnDroppedToolParts).
func (s *DuplexProviderStage) prepareToolResponseText(
	session any, responses []providers.ToolResponse,
) []providers.ToolResponse {
	if ps, ok := session.(providers.ToolResponsePartsSupport); ok && ps.SupportsToolResponseParts() {
		return responses
	}
	s.warnDroppedToolParts(responses)
	out := make([]providers.ToolResponse, len(responses))
	for i, resp := range responses {
		if resp.Result == "" {
			if dropped := droppedPartTypes(responses[i : i+1]); len(dropped) > 0 {
				resp.Result = fmt.Sprintf("[tool returned %s content this session cannot receive]",
					strings.Join(dropped, ", "))
			}
		}
		out[i] = resp
	}
	return out
}

// warnDroppedToolParts logs, once per stage, that the session cannot send
// the media parts of the tool responses it is about to be sent and sends
// their text only, naming the provider and the part types dropped.
func (s *DuplexProviderStage) warnDroppedToolParts(responses []providers.ToolResponse) {
	dropped := droppedPartTypes(responses)
	if len(dropped) == 0 || !s.toolPartsWarned.CompareAndSwap(false, true) {
		return
	}
	providerID := ""
	if s.provider != nil {
		providerID = s.provider.ID()
	}
	logger.Warn("DuplexProviderStage: realtime session cannot send non-text tool result parts; "+
		"sending their text only",
		"provider", providerID, "part_types", dropped)
}

// droppedPartTypes returns the distinct non-text part types in responses, in
// the order they first appear.
func droppedPartTypes(responses []providers.ToolResponse) []string {
	var out []string
	seen := map[string]bool{}
	for i := range responses {
		for _, p := range responses[i].Parts {
			if p.Type == types.ContentTypeText || seen[p.Type] {
				continue
			}
			seen[p.Type] = true
			out = append(out, p.Type)
		}
	}
	return out
}

// deliverToolRound is the input goroutine's part of a finished round: the
// result messages go downstream (and into the hooks' history), the calls held
// pending go out on a control element, a limit is reported as an error
// element, the responses go to the session (starting the provider call they
// continue), and a terminal limit closes the session. A round that finishes
// once the session is closing still has its result messages recorded (its
// tools ran and the assistant message that called them was already sent, so
// the history keeps a result for each call), its held calls and its error
// surfaced; only its responses are not sent. A round that finishes after the
// input goroutine stopped delivering is recorded by finishToolRounds.
func (s *DuplexProviderStage) deliverToolRound(
	ctx context.Context, res *duplexToolResult, output chan<- StreamElement,
) error {
	dt := s.tools
	// Delivered once this returns: a reply its responses start is then
	// counted by the provider call it opened (openNextToolRound).
	defer dt.inflight.Add(-1)
	if err := s.recordToolRound(ctx, res, output); err != nil {
		return err
	}
	closing := dt.isClosed()
	// The limit is reported before the responses go to the session: they
	// start the model's reply to the stopped calls, which the session can
	// stream back at once, and the report must come ahead of it.
	if res.err != nil {
		if err := s.emit(ctx, NewErrorElement(res.err), output); err != nil {
			return err
		}
	}
	if responses := res.elem.Meta.ToolResponses; len(responses) > 0 { // not sent once closing (sendToolRound)
		s.sendToolRound(ctx, res, responses)
	}
	if res.closeSession && !closing {
		s.closeSessionIfOpen()
	}
	return nil
}

// recordToolRound emits a finished round's result messages (into the hooks'
// history too) and the calls it holds for the caller. The results go out even
// once ctx is canceled (emitRecorded): the assistant message that made the
// calls is already out, and the transcript keeps a result for each.
func (s *DuplexProviderStage) recordToolRound(
	ctx context.Context, res *duplexToolResult, output chan<- StreamElement,
) error {
	return s.recordToolRoundBy(ctx, res, output, sendMode{recorded: true})
}

// recordToolRoundBy is recordToolRound sending in mode.
func (s *DuplexProviderStage) recordToolRoundBy(
	ctx context.Context, res *duplexToolResult, output chan<- StreamElement, mode sendMode,
) error {
	for i := range res.elem.Meta.ToolResultMessages {
		msg := &res.elem.Meta.ToolResultMessages[i]
		s.recordHistory(msg) // first, so a held result's entry follows it (toolExchangeOrder)
		if err := s.exchange.send(ctx, StreamElement{Message: msg}, output, mode); err != nil {
			return err
		}
	}
	if pending := res.elem.Meta.PendingTools; len(pending) > 0 {
		var held StreamElement
		held.Meta.PendingTools = pending
		if err := s.exchange.send(ctx, held, output, mode); err != nil {
			return err
		}
	}
	return nil
}

// sendToolRound sends a round's responses to the session unless it is
// closing. A round that does not close the session starts the provider call
// its responses continue first, so the call's start precedes its reply.
func (s *DuplexProviderStage) sendToolRound(
	ctx context.Context, res *duplexToolResult, responses []providers.ToolResponse,
) {
	dt := s.tools
	dt.sendMu.Lock()
	defer dt.sendMu.Unlock()
	if dt.closed {
		return
	}
	if !res.closeSession && res.turn != nil {
		s.openNextToolRound(ctx, res.turn)
	}
	if !res.closeSession {
		dt.replyDue.Store(true)
	}
	s.sendToolResponses(ctx, responses)
}

// serveToolRoundsAfterInput keeps delivering finished rounds once input has
// ended, until ctx ends: the session is still answering, and its tool calls
// still need their results. It reports a clean exit on done when ctx ends, and
// the error when a round cannot be delivered, as the input loop does.
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
				done <- err
				return
			}
		}
	}
}
