package stage

import (
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A realtime session can report cost that no turn of its own carries: a
// usage-only chunk (Gemini Live flushes usage no turn's end took when the
// session ends), or the completion of a turn a barge-in already ended, which
// arrives after its Interrupted chunk. Like ProviderStage, the stage charges
// every reported cost to the budget (MaxCostUSD) when it arrives
// (chargeInterruptedTurn), and the session's cost totals are its messages'
// CostInfo, so the cost must land on a message too. It is carried onto the
// next assistant message the stage emits: the reply that follows the
// interruption. When the session ends first it goes on the partial reply
// emitted then, or on a cost-only assistant message marked interrupted (kept
// out of model context like any interrupted reply).

// usageOnly reports whether chunk carries nothing but cost (no turn end, no
// interruption, and no message built from it).
func usageOnly(chunk *providers.StreamChunk, elem *StreamElement) bool {
	return chunk.FinishReason == nil && !chunk.Interrupted && elem.Message == nil
}

// placeCost puts the chunk's cost on the chunk's message, or carries it when
// the element has none, and adds carried cost to the next assistant message.
// Call after the chunk's cost is charged. Response goroutine only.
func (s *DuplexProviderStage) placeCost(chunk *providers.StreamChunk, elem *StreamElement) {
	msg := elem.Message
	if chunk.CostInfo != nil && (msg == nil || msg.CostInfo != chunk.CostInfo) {
		s.carriedCost = addCost(s.carriedCost, chunk.CostInfo)
		return
	}
	if msg != nil && msg.Role == roleAssistant && s.carriedCost != nil {
		msg.CostInfo = addCost(msg.CostInfo, s.carriedCost)
		s.carriedCost = nil
	}
}

// withCarriedCost adds the carried cost to msg, the partial reply emitted
// when the session ends, or to a cost-only interrupted reply when there is
// none. Returns msg unchanged when nothing is carried. Response goroutine
// only.
func (s *DuplexProviderStage) withCarriedCost(msg *types.Message, cause string) *types.Message {
	if s.carriedCost == nil {
		return msg
	}
	if msg == nil {
		msg = &types.Message{
			Role:         roleAssistant,
			FinishReason: types.FinishReasonInterrupted,
			Meta: map[string]interface{}{
				finishReasonMetaKey:        finishReasonInterrupted,
				types.MetaInterruptedCause: cause,
			},
		}
	}
	msg.CostInfo = addCost(msg.CostInfo, s.carriedCost)
	s.carriedCost = nil
	return msg
}

// addCost is a and b rolled up into one CostInfo; nil parts count as zero.
func addCost(a, b *types.CostInfo) *types.CostInfo {
	if a == nil {
		return b
	}
	sum := base.AggregateCost(a, b)
	return &sum
}
