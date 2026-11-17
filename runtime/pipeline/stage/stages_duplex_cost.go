package stage

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A realtime session can report cost that no message the stage emits
// carries: usage reported on its own once its turn has ended (Gemini 3.x
// reports a tool-call turn's usage after the tool calls; usage flushed when
// the session ends), the completion of a turn a barge-in already ended, or a
// turn's end with a cost but nothing to build a message from. As ProviderStage
// does for every reported cost, the stage charges it to the budget
// (MaxCostUSD) and reports it on provider.call.completed: for a turn's end,
// as that provider call's completion; otherwise on the call completed last,
// the one the usage belongs to. Nothing is added to the conversation: a
// message without content reads as a reply that never happened.

// reportUnplacedCost charges and reports the chunk's cost when chunkToElement
// put it on no message and no other path took it. Returns the job reporting
// a spent budget, or nil. Response goroutine only.
func (s *DuplexProviderStage) reportUnplacedCost(
	ctx context.Context, chunk *providers.StreamChunk, elem *StreamElement,
) *duplexToolJob {
	if s.tools == nil || s.tools.isTerminal() || chunk.CostInfo == nil || s.costPlaced ||
		elem.Meta.Interrupted { // chargeInterruptedTurn took it
		return nil
	}
	dt := s.tools
	turnEnd := chunk.FinishReason != nil
	dt.mu.Lock()
	round := dt.last
	switch {
	case turnEnd && dt.open != nil:
		round = dt.open // the call this chunk completes
		dt.open = nil
		dt.last = round
	case round == nil:
		round = &duplexRound{rr: roundRef{round: dt.round, providerCallID: newProviderCallID()}, start: time.Now()}
	}
	turn := dt.turn
	dt.mu.Unlock()

	msg := &types.Message{Role: roleAssistant, CostInfo: chunk.CostInfo}
	if turnEnd {
		msg.FinishReason = *chunk.FinishReason
	}
	s.emitRoundCompleted(ctx, msg, round)
	if turn == nil {
		turn = &duplexToolTurn{toolTurn: s.toolRunner().newTurn(nil, 0)}
	} else if turnEnd && !elem.Meta.InterruptedTurnComplete {
		dt.endTurn(turn) // a reply without tool calls ends the tool turn
	}
	if err := s.charge(turn, chunk.CostInfo); err != nil {
		return s.budgetSpent(turn, nil, round.rr, err)
	}
	return nil
}
