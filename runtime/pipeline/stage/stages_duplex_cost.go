package stage

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A realtime session can report a cost that no message the stage builds
// carries: usage reported once its turn has ended (Gemini 3 reports a
// tool-call turn's usage after the tool calls; usage after an interruption;
// an interrupted turn's completion), or a turn's end with a cost but nothing
// to build a message from. None of it is lost:
//
//   - Attribution: a session tags each chunk with the generation it belongs
//     to (StreamChunk.Generation). The stage remembers which call each
//     generation completed, so late usage lands on its own call whatever
//     happened in between.
//   - Events: provider.call.completed goes out when the call completes, as on
//     ProviderStage. Usage reported for a call after that is a
//     provider.call.usage event for the call; a call's total cost is its
//     completed event's Cost plus its usage events'. A turn's end with a cost
//     and no message completes its call with that cost.
//   - Spend: it is charged to the budget (MaxCostUSD) when it arrives; a
//     budget it exceeds refuses on the call it belongs to.
//   - History: it is added to the next assistant message the stage emits, so
//     the messages' CostInfo (what a resumed conversation's budget is rebuilt
//     from) adds up to the session's spend. At session close it goes on the
//     message emitted last; when none is, it is in no message.

// generationCall is the call a generation completed, and its tool turn.
type generationCall struct {
	round *duplexRound
	turn  *duplexToolTurn
}

// maxTrackedGenerations bounds the generations remembered for late usage.
const maxTrackedGenerations = 16

// recordGeneration remembers that the chunk being handled, of generation
// s.chunkGen, completed round. Response goroutine only.
func (s *DuplexProviderStage) recordGeneration(round *duplexRound, turn *duplexToolTurn) {
	gen := s.chunkGen
	if gen == 0 {
		return
	}
	dt := s.tools
	dt.mu.Lock()
	defer dt.mu.Unlock()
	if dt.gens == nil {
		dt.gens = map[int64]generationCall{}
	}
	if _, ok := dt.gens[gen]; !ok {
		dt.genOrder = append(dt.genOrder, gen)
		if len(dt.genOrder) > maxTrackedGenerations {
			delete(dt.gens, dt.genOrder[0])
			dt.genOrder = dt.genOrder[1:]
		}
	}
	dt.gens[gen] = generationCall{round: round, turn: turn}
}

// generationCallFor returns the call generation gen completed.
func (dt *duplexTools) generationCallFor(gen int64) (generationCall, bool) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	gc, ok := dt.gens[gen]
	return gc, ok && gen != 0
}

// reportLateCost charges an unplaced cost and reports it on the call it
// belongs to. Returns the job reporting a spent budget, or nil.
func (s *DuplexProviderStage) reportLateCost(
	ctx context.Context, chunk *providers.StreamChunk, elem *StreamElement, unplaced bool,
) *duplexToolJob {
	dt := s.tools
	if dt == nil || !unplaced || elem.Meta.Interrupted { // chargeInterruptedTurn took an interruption's
		return nil
	}
	cost := chunk.CostInfo
	if gc, ok := dt.generationCallFor(chunk.Generation); ok {
		s.emitRoundUsage(ctx, cost, gc.round)
		return s.chargeLate(gc.turn, cost, gc.round.rr)
	}
	if chunk.FinishReason != nil && !elem.Meta.InterruptedTurnComplete && dt.callInProgress() {
		// A turn's end with nothing to build a message from: its call
		// completes with the cost.
		msg := &types.Message{Role: roleAssistant, FinishReason: *chunk.FinishReason, CostInfo: cost}
		turn, rr := s.completeRound(ctx, msg)
		dt.endTurn(turn)
		return s.chargeLate(turn, cost, rr)
	}
	return s.chargeLate(nil, cost, roundRef{})
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
// ends, with msg (the partial reply kept then) or as interrupted. Nil-safe
// without tool execution.
func (s *DuplexProviderStage) completeOpenCall(ctx context.Context, msg *types.Message) {
	dt := s.tools
	if dt == nil {
		return
	}
	dt.mu.Lock()
	round := dt.open
	dt.open = nil
	dt.mu.Unlock()
	if round == nil {
		return
	}
	if msg == nil {
		msg = &types.Message{Role: roleAssistant, FinishReason: types.FinishReasonInterrupted}
	}
	s.emitRoundCompleted(ctx, msg, round, time.Since(round.start))
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
