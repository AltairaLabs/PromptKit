package stage

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A realtime session can report a cost that no message the stage builds
// carries: usage reported once its turn has ended (Gemini 3 reports a
// tool-call turn's usage after the tool calls; usage flushed as the session
// ends), the completion of a turn a barge-in already ended, or a turn's end
// with a cost but nothing to build a message from. None of it is lost:
//
//   - Spend: it is charged to the budget (MaxCostUSD) when it arrives.
//   - Events: one provider.call.completed per call. A tool-call turn completed
//     without a cost has its event held until the cost arrives, or until the next
//     call starts or the session ends; a call a barge-in cut short has its
//     event held until its completion reports what it cost (or the session
//     ends). The late cost is reported on the call it belongs to. A cost
//     whose call's event has already gone out (it reported a cost of its own)
//     is charged and kept in the history, with no event of its own: the stage
//     never reports a call that did not start.
//   - History: it is added to the next assistant message the stage emits, so
//     the messages' CostInfo (what a resumed conversation's budget is rebuilt
//     from) adds up to the session's spend. Only cost still carried when the
//     session ends with no later message stays out of the history.

// completedCall is a completed provider call whose provider.call.completed is
// held for a cost the provider reports later.
type completedCall struct {
	round *duplexRound
	msg   types.Message
	dur   time.Duration
}

// settleCompleted reports a completed provider call, or holds its event: for
// a call a barge-in cut short, or a tool-call turn that reported no cost yet.
func (s *DuplexProviderStage) settleCompleted(ctx context.Context, round *duplexRound, msg *types.Message) {
	call := &completedCall{round: round, msg: *msg, dur: time.Since(round.start)}
	dt := s.tools
	var flush *completedCall
	dt.mu.Lock()
	switch {
	case msg.FinishReason == types.FinishReasonInterrupted:
		flush, dt.interrupted = dt.interrupted, call
	case msg.CostInfo == nil && len(msg.ToolCalls) > 0:
		// Gemini 3 reports a tool-call turn's usage after its tool calls.
		flush, dt.deferred = dt.deferred, call
	default:
		flush = call
	}
	dt.mu.Unlock()
	s.emitCompletedCall(ctx, flush)
}

// emitCompletedCall emits a held call's provider.call.completed. Nil-safe.
func (s *DuplexProviderStage) emitCompletedCall(ctx context.Context, call *completedCall) {
	if call != nil {
		s.emitRoundCompleted(ctx, &call.msg, call.round, call.dur)
	}
}

// flushDeferredCall reports the call held for a cost that did not come.
func (s *DuplexProviderStage) flushDeferredCall(ctx context.Context) {
	dt := s.tools
	dt.mu.Lock()
	call := dt.deferred
	dt.deferred = nil
	dt.mu.Unlock()
	s.emitCompletedCall(ctx, call)
}

// flushCompletedCalls reports every held call: the session is ending.
// Nil-safe without tool execution.
func (s *DuplexProviderStage) flushCompletedCalls(ctx context.Context) {
	dt := s.tools
	if dt == nil {
		return
	}
	dt.mu.Lock()
	deferred, interrupted := dt.deferred, dt.interrupted
	dt.deferred, dt.interrupted = nil, nil
	dt.mu.Unlock()
	s.emitCompletedCall(ctx, interrupted)
	s.emitCompletedCall(ctx, deferred)
}

// reportLateCost charges an unplaced cost and reports it on the call it
// belongs to: an interrupted call's completion on that call, a turn's end
// with nothing to build a message from on the call it completes, and usage
// reported on its own on the call completed without a cost. An interrupted
// call's completion without a cost still releases its event. Returns the
// job reporting a spent budget, or nil.
func (s *DuplexProviderStage) reportLateCost(
	ctx context.Context, chunk *providers.StreamChunk, elem *StreamElement, unplaced bool,
) *duplexToolJob {
	dt := s.tools
	if dt == nil || elem.Meta.Interrupted { // chargeInterruptedTurn took it
		return nil
	}
	cost := chunk.CostInfo
	if !unplaced && !elem.Meta.InterruptedTurnComplete {
		return nil
	}
	turnEnd := chunk.FinishReason != nil && !elem.Meta.InterruptedTurnComplete
	var call *completedCall
	dt.mu.Lock()
	turn := dt.turn
	switch {
	case elem.Meta.InterruptedTurnComplete:
		call, dt.interrupted = dt.interrupted, nil
	case turnEnd && dt.open != nil:
		call = &completedCall{round: dt.open, msg: types.Message{Role: roleAssistant,
			FinishReason: *chunk.FinishReason}, dur: time.Since(dt.open.start)}
		dt.open = nil
	default:
		call, dt.deferred = dt.deferred, nil
	}
	dt.mu.Unlock()
	if call != nil {
		call.msg.CostInfo = addCost(call.msg.CostInfo, cost)
	}
	s.emitCompletedCall(ctx, call)
	if turnEnd && turn != nil {
		dt.endTurn(turn) // a reply without tool calls ends the tool turn
	}
	if cost == nil {
		return nil
	}
	if dt.isTerminal() {
		s.chargeSpend(cost)
		return nil
	}
	if turn == nil {
		turn = &duplexToolTurn{toolTurn: s.toolRunner().newTurn(nil, 0)}
	}
	if err := s.charge(turn, cost); err != nil {
		return s.budgetSpent(turn, nil, roundRef{}, err)
	}
	return nil
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
