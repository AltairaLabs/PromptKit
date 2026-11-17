package stage

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
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
//   - Events: cost for a call still open is carried by its
//     provider.call.completed; cost for a call already completed is a
//     provider.call.usage event for it. A call's total is its completed Cost
//     plus its usage events'. A cost whose generation the stage does not know
//     (only a third-party provider sends one) goes to the call started last;
//     with no call at all, it is a usage event with no CallID.
//   - Messages: a cost lands on exactly one emitted message: its own, or, when
//     it has none, the next assistant message emitted; at close, the last
//     message emitted then that can carry a cost. Only cost with no message
//     emitted after it is in no message.

// maxTrackedGenerations bounds the generations remembered.
const maxTrackedGenerations = 16

// bindGeneration binds gen to the call open now, if neither is bound yet.
// Response goroutine only. Nil-safe without tool execution.
func (s *DuplexProviderStage) bindGeneration(gen int64) {
	dt := s.tools
	if dt == nil {
		return
	}
	dt.mu.Lock()
	defer dt.mu.Unlock()
	dt.bindGenerationLocked(gen)
}

// bindGenerationLocked binds gen to the open call. Called with dt.mu held.
func (dt *duplexTools) bindGenerationLocked(gen int64) {
	if gen == 0 || dt.open == nil || dt.open.gen != 0 {
		return
	}
	if _, bound := dt.gens[gen]; bound {
		return
	}
	if dt.gens == nil {
		dt.gens = map[int64]*duplexRound{}
	}
	dt.open.gen = gen
	dt.gens[gen] = dt.open
	dt.genOrder = append(dt.genOrder, gen)
	if len(dt.genOrder) > maxTrackedGenerations {
		delete(dt.gens, dt.genOrder[0])
		dt.genOrder = dt.genOrder[1:]
	}
}

// attributeCost charges a cost no message or interruption reported and puts
// it on its call: the call bound to the chunk's generation, or the call
// started last when the generation is unknown. On a call still open it goes
// on its completion; on a completed call it is a provider.call.usage. Returns
// the job reporting a spent budget, or nil. Response goroutine only.
func (s *DuplexProviderStage) attributeCost(ctx context.Context, chunk *providers.StreamChunk) *duplexToolJob {
	dt := s.tools
	if dt == nil {
		return nil
	}
	cost := chunk.CostInfo
	dt.mu.Lock()
	round := dt.gens[chunk.Generation]
	if round == nil || chunk.Generation == 0 {
		round = dt.last
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
			usageOf = &duplexRound{} // no call was ever started
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

// completeOpenCall completes the provider call still open when the session
// ends, with msg (the partial reply kept then) or as interrupted, carrying
// the cost reported for it while it was open. Nil-safe without tool
// execution.
func (s *DuplexProviderStage) completeOpenCall(ctx context.Context, msg *types.Message) {
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
	reported := types.Message{Role: roleAssistant, FinishReason: types.FinishReasonInterrupted}
	if msg != nil {
		reported = *msg
	}
	reported.CostInfo = addCost(reported.CostInfo, pending)
	s.emitRoundCompleted(ctx, &reported, round, time.Since(round.start))
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
