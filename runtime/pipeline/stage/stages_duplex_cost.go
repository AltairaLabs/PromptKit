package stage

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Cost attribution for realtime sessions. Invariants:
//
//   - Every cost-bearing chunk a built-in realtime session sends names its
//     generation (StreamChunk.Generation): one per provider call (a reply, a
//     tool-call turn, each reply to tool results; one per OpenAI Realtime
//     response).
//   - The stage binds a generation to the provider call (round) open when the
//     generation's first output arrives, or that a chunk of it completes, so
//     open, interrupted and closing calls are all attributable. Ending a turn
//     (completing the call, ending the tool turn) follows the chunk's turn-end
//     flags; attribution only decides which call a cost goes to.
//   - Every cost is charged to the spend (MaxCostUSD) once, when it arrives;
//     a budget it exceeds refuses on the call it belongs to.
//   - Every call that starts completes exactly once: when its turn ends
//     (with or without a message), when an interruption names it, when the
//     next user turn or a new generation supersedes it (as interrupted), or
//     when the session closes. A limit closing the session or an output
//     guardrail denying the turn does not stop the call completing or its
//     cost being charged.
//   - Events: cost for a call still open is carried by its
//     provider.call.completed; cost for a call already completed is a
//     provider.call.usage event for it. A call's total is its completed Cost
//     plus its usage events'. Output of a completed call (a canceled
//     response's trailing chunks) is dropped; only its cost counts. A cost
//     with Generation 0 (a provider that does not number calls) goes to the
//     call started last; a cost for a generation too old to remember is a
//     usage event with no CallID.
//   - Messages: a cost lands on exactly one emitted message: its own, or, when
//     it has none, the next assistant message emitted; at close, the last
//     message emitted then that can carry a cost. Only cost with no message
//     emitted after it is in no message.

// maxTrackedGenerations bounds the generations remembered. The open call's
// generation is never forgotten; usage for one forgotten is reported with no
// CallID (a session-level provider.call.usage).
const maxTrackedGenerations = 64

// genState is what the stage knows of a generation.
type genState int

const (
	genUnbound   genState = iota // not seen yet (or 0: the provider does not number them)
	genOpen                      // bound to the call open now
	genCompleted                 // bound to a call that has completed
	genForgotten                 // bound once, no longer remembered
)

// lookupLocked returns the call gen is bound to and its state. Called with
// dt.mu held.
func (dt *duplexTools) lookupLocked(gen int64) (*duplexRound, genState) {
	if gen == 0 {
		return nil, genUnbound
	}
	if r, ok := dt.gens[gen]; ok {
		if r == dt.open {
			return r, genOpen
		}
		return r, genCompleted
	}
	if gen <= dt.forgottenUpTo {
		return nil, genForgotten
	}
	return nil, genUnbound
}

// bindGenerationLocked binds gen to the open call, when gen is unbound and
// the open call has none. Called with dt.mu held.
func (dt *duplexTools) bindGenerationLocked(gen int64) {
	if _, st := dt.lookupLocked(gen); st != genUnbound || gen == 0 || dt.open == nil || dt.open.gen != 0 {
		return
	}
	if dt.gens == nil {
		dt.gens = map[int64]*duplexRound{}
	}
	dt.open.gen = gen
	dt.gens[gen] = dt.open
	dt.genOrder = append(dt.genOrder, gen)
	if len(dt.genOrder) > maxTrackedGenerations {
		for i, old := range dt.genOrder {
			if dt.gens[old] != dt.open {
				delete(dt.gens, old)
				dt.forgottenUpTo = max(dt.forgottenUpTo, old)
				dt.genOrder = append(dt.genOrder[:i], dt.genOrder[i+1:]...)
				break
			}
		}
	}
}

// bindGeneration binds a chunk's generation to its call before anything else
// is decided about the chunk, so every generation (one whose first chunk is
// only its cost, or only its end, included) has a call of its own. A new
// generation while the open call is bound to another means the provider has
// moved on: that call completes, as interrupted, with no message (what it
// accumulated is discarded). A new generation's call starts a tool turn of
// its own (round 1) when a user turn came in meanwhile, when the previous
// call was superseded, or when no turn is under way. Response goroutine
// only. Nil-safe without tool execution.
func (s *DuplexProviderStage) bindGeneration(ctx context.Context, gen int64) {
	dt := s.tools
	if dt == nil || gen == 0 {
		return
	}
	dt.mu.Lock()
	if _, st := dt.lookupLocked(gen); st != genUnbound || (dt.open != nil && dt.open.gen == 0) {
		dt.bindGenerationLocked(gen)
		dt.mu.Unlock()
		return
	}
	var superseded *completion
	if dt.open != nil {
		old := dt.open
		superseded = &completion{round: old, turn: old.turn, dur: time.Since(old.start),
			reported: types.Message{Role: roleAssistant, FinishReason: types.FinishReasonInterrupted, CostInfo: old.pending}}
		dt.open = nil
		delete(s.parked, old.gen)
		if s.accGen == old.gen { // what it accumulated is not anyone's message
			s.discardAccumulation()
			s.accGen = 0
		}
	}
	var started *duplexToolTurn
	if dt.turn == nil || dt.newTurnDue || superseded != nil {
		started = s.startToolTurnLocked(dt.offer.heldNames())
		dt.newTurnDue = false
	}
	opened := dt.openRoundLocked()
	dt.bindGenerationLocked(gen)
	dt.mu.Unlock()
	if superseded != nil {
		dt.replyDue.Store(false)
		s.emitCompletion(ctx, superseded, true)
	}
	s.syncStartedTurn(ctx, started)
	s.emitRoundStarted(ctx, opened)
}

// interruptsOpenCall reports whether an interruption tagged gen interrupts
// the open call: gen is the open call's (bindGeneration has bound every
// numbered generation by now), or 0 (not numbered). An interruption naming a
// completed call interrupts nothing now.
func (dt *duplexTools) interruptsOpenCall(gen int64) bool {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	_, st := dt.lookupLocked(gen)
	return gen == 0 || st == genOpen
}

// staleChunk reports whether chunk belongs to a call that already completed
// (a canceled response's trailing deltas or tool calls, a superseded call's
// end or late usage, an interruption of a finished reply still playing):
// only its cost counts, and an interruption still flushes the caller's
// playback.
func (s *DuplexProviderStage) staleChunk(chunk *providers.StreamChunk) bool {
	dt := s.tools
	if dt == nil || chunk.Generation == 0 || isInputTranscriptionChunk(chunk) {
		return false
	}
	dt.mu.Lock()
	defer dt.mu.Unlock()
	_, st := dt.lookupLocked(chunk.Generation)
	return st == genCompleted || st == genForgotten
}

// handleStaleChunk handles a chunk of a call that already completed: an
// interruption is still forwarded; otherwise only its cost counts.
func (s *DuplexProviderStage) handleStaleChunk(
	ctx context.Context, chunk *providers.StreamChunk, output chan<- StreamElement,
) error {
	if chunk.Interrupted {
		return s.forwardStaleInterruption(ctx, chunk, output)
	}
	return s.reportStaleCost(ctx, chunk)
}

// forwardStaleInterruption passes an interruption naming a completed call
// to the caller, so its playback stops, and reports its cost on that call.
// The open call, its accumulation and its turn are left as they are.
func (s *DuplexProviderStage) forwardStaleInterruption(
	ctx context.Context, chunk *providers.StreamChunk, output chan<- StreamElement,
) error {
	reason := finishReasonInterrupted
	elem := StreamElement{}
	elem.Meta.Interrupted = true
	elem.Meta.FinishReason = &reason
	if err := sendStreamElement(ctx, elem, output); err != nil {
		return err
	}
	return s.reportStaleCost(ctx, chunk)
}

// reportStaleCost charges a stale chunk's cost on its call and carries it for
// the history. The chunk is not forwarded.
func (s *DuplexProviderStage) reportStaleCost(ctx context.Context, chunk *providers.StreamChunk) error {
	// What a completed call accumulates later is not anyone's message.
	warnStaleToolCalls(chunk)
	delete(s.parked, chunk.Generation)
	if s.accGen == chunk.Generation {
		s.discardAccumulation()
		s.accGen = 0
	}
	if chunk.CostInfo == nil {
		return nil
	}
	job := s.attributeCost(ctx, chunk)
	s.carriedCost = addCost(s.carriedCost, chunk.CostInfo)
	return s.queueToolRound(ctx, job)
}

// warnStaleToolCalls logs the tool calls a chunk of a completed provider call
// carries: they are not run (a canceled response's trailing function calls
// are not answered), and the operator can see which were dropped.
func warnStaleToolCalls(chunk *providers.StreamChunk) {
	if len(chunk.ToolCalls) == 0 {
		return
	}
	names := make([]string, len(chunk.ToolCalls))
	ids := make([]string, len(chunk.ToolCalls))
	for i, tc := range chunk.ToolCalls {
		names[i], ids[i] = tc.Name, tc.ID
	}
	logger.Warn("duplex provider stage: dropped tool calls of a completed provider call; they are not run",
		"generation", chunk.Generation, "tools", names, "call_ids", ids)
}

// isInputTranscriptionChunk reports whether chunk carries the caller's
// transcribed speech, which belongs to no provider call.
func isInputTranscriptionChunk(chunk *providers.StreamChunk) bool {
	_, isInput := classifyTranscriptionChunk(chunk)
	return isInput
}

// genAccumulation is a generation's output accumulated so far, parked while
// another generation's is being accumulated.
type genAccumulation struct {
	text, reasoning string
	opaque          []types.OpaqueReasoning
	media           []byte
	toolCalls       []types.MessageToolCall
}

// switchGeneration makes the accumulation the stage works on gen's: the
// current generation's is parked and gen's restored, so each generation's
// message is built only from its own output. Response goroutine only.
func (s *DuplexProviderStage) switchGeneration(gen int64) {
	if gen == 0 || gen == s.accGen {
		return
	}
	if s.accGen != 0 {
		if s.parked == nil {
			s.parked = map[int64]*genAccumulation{}
		}
		s.parked[s.accGen] = &genAccumulation{
			text: s.accumulatedText.String(), reasoning: s.accumulatedReasoning.String(),
			opaque: s.accumulatedOpaqueReasoning, media: s.accumulatedMedia, toolCalls: s.accumulatedToolCalls,
		}
	}
	s.discardAccumulation()
	if p := s.parked[gen]; p != nil {
		s.accumulatedText.WriteString(p.text)
		s.accumulatedReasoning.WriteString(p.reasoning)
		s.accumulatedOpaqueReasoning, s.accumulatedMedia, s.accumulatedToolCalls = p.opaque, p.media, p.toolCalls
		delete(s.parked, gen)
	}
	s.accGen = gen
}

// switchToOpenCall makes the accumulation the open call's generation's, for a
// path that takes the open call's partial reply (an error, the session
// closing).
func (s *DuplexProviderStage) switchToOpenCall() {
	dt := s.tools
	if dt == nil {
		return
	}
	dt.mu.Lock()
	var gen int64
	if dt.open != nil {
		gen = dt.open.gen
	}
	dt.mu.Unlock()
	s.switchGeneration(gen)
}

// discardAccumulation drops what the turn accumulated (text, reasoning,
// media, tool calls) without ending the turn.
func (s *DuplexProviderStage) discardAccumulation() {
	s.accumulatedText.Reset()
	s.accumulatedReasoning.Reset()
	s.accumulatedOpaqueReasoning = nil
	s.accumulatedMedia = nil
	s.accumulatedToolCalls = nil
}

// attributeCost charges a cost no message or interruption reported and puts
// it on its call: the call its generation is bound to (on that call's
// completion while it is open, else a provider.call.usage); for an
// unnumbered chunk (Generation 0), the call started last; for a forgotten
// generation, a session-level usage event with no CallID. Returns the job
// reporting a spent budget, or nil. Response goroutine only.
func (s *DuplexProviderStage) attributeCost(ctx context.Context, chunk *providers.StreamChunk) *duplexToolJob {
	dt := s.tools
	if dt == nil {
		return nil
	}
	cost := chunk.CostInfo
	dt.mu.Lock()
	dt.bindGenerationLocked(chunk.Generation) // a generation seen first with its cost
	round, st := dt.lookupLocked(chunk.Generation)
	if chunk.Generation == 0 {
		round = dt.last
	}
	if st == genForgotten {
		round = nil
	}
	open := round != nil && round == dt.open
	if open {
		round.pending = addCost(round.pending, cost)
	}
	turn := dt.turn
	var rr roundRef
	if round != nil {
		rr = round.rr
		if round.turn != nil {
			turn = round.turn
		}
	}
	dt.mu.Unlock()
	if !open {
		usageOf := round
		if usageOf == nil {
			usageOf = &duplexRound{} // a session-level record: no call to name
		}
		s.emitRoundUsage(ctx, cost, usageOf)
	}
	return s.chargeLate(turn, cost, rr)
}

// chargeLate charges a late cost against turn (a fresh one when nil), or
// only to the spend once a limit is closing the session. A budget it
// exceeds refuses on rr, the call the cost belongs to.
func (s *DuplexProviderStage) chargeLate(turn *duplexToolTurn, cost *types.CostInfo, rr roundRef) *duplexToolJob {
	if s.tools.isTerminal() {
		s.chargeSpend(cost)
		return nil
	}
	if turn == nil {
		turn = &duplexToolTurn{toolTurn: s.toolRunner().newTurn(nil, 0)}
	}
	if err := s.charge(turn, cost); err != nil {
		return s.budgetSpent(turn, nil, rr, err)
	}
	return nil
}

// emitRoundUsage emits provider.call.usage: cost reported for round's call
// after it completed.
func (s *DuplexProviderStage) emitRoundUsage(ctx context.Context, cost *types.CostInfo, round *duplexRound) {
	if s.emitter == nil || cost == nil {
		return
	}
	data := &events.ProviderCallUsageData{
		InputTokens:  cost.InputTokens,
		OutputTokens: cost.OutputTokens,
		CachedTokens: cost.CachedTokens,
		Cost:         cost.TotalCost,
		Source:       s.tools.cfg.Source,
		Labels:       s.tools.cfg.Labels,
		Round:        round.rr.round,
		CallID:       round.rr.providerCallID,
	}
	if s.provider != nil {
		data.Provider = s.provider.ID()
		data.Model = s.provider.Model()
	}
	s.emitter.ProviderCallUsageCtx(ctx, data)
}

// completeOpenCall completes the provider call still open when the turn or
// the session ends, with msg (the partial reply taken then) or as
// interrupted, carrying the cost reported for it while it was open. emitted
// says whether msg is emitted. Nil-safe without tool execution.
func (s *DuplexProviderStage) completeOpenCall(ctx context.Context, msg *types.Message, emitted bool) {
	dt := s.tools
	if dt == nil {
		return
	}
	dt.mu.Lock()
	round := dt.open
	dt.open = nil
	var pending *types.CostInfo
	if round != nil {
		pending = round.pending
	}
	dt.mu.Unlock()
	if round == nil {
		return
	}
	dt.replyDue.Store(false)
	reported := types.Message{Role: roleAssistant, FinishReason: types.FinishReasonInterrupted}
	if msg != nil {
		reported = *msg
	}
	reported.CostInfo = addCost(reported.CostInfo, pending)
	s.emitRoundCompleted(ctx, &reported, round, time.Since(round.start), !emitted)
}

// chargeSpend adds cost to the session's spend once a limit is closing the
// session: nothing is checked against the budget any more.
func (s *DuplexProviderStage) chargeSpend(cost *types.CostInfo) {
	if cost == nil {
		return
	}
	dt := s.tools
	dt.mu.Lock()
	defer dt.mu.Unlock()
	dt.spent += cost.TotalCost
}

// placeCarriedCost carries an unplaced cost for the history, and adds what
// is carried to the next assistant message the stage emits. Call after the
// chunk's cost is charged. Response goroutine only.
func (s *DuplexProviderStage) placeCarriedCost(chunk *providers.StreamChunk, elem *StreamElement, unplaced bool) {
	if unplaced {
		s.carriedCost = addCost(s.carriedCost, chunk.CostInfo)
		return
	}
	if elem.Message != nil && elem.Message.Role == roleAssistant {
		s.takeCarriedCost(elem.Message)
	}
}

// takeCarriedCost adds the carried cost to msg, an assistant message about to
// be emitted. Response goroutine only.
func (s *DuplexProviderStage) takeCarriedCost(msg *types.Message) {
	if s.carriedCost == nil {
		return
	}
	msg.CostInfo = addCost(msg.CostInfo, s.carriedCost)
	s.carriedCost = nil
}

// addCost is a and b rolled up into one CostInfo; a nil part counts as zero.
func addCost(a, b *types.CostInfo) *types.CostInfo {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	}
	sum := base.AggregateCost(a, b)
	return &sum
}
