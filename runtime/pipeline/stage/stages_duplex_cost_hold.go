package stage

import (
	"context"
	"slices"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A provider can end a call before it reports the call's usage: Gemini Live
// reports a tool-call turn's usage, and an interrupted reply's, on the
// turnComplete that follows. Completing the call at once would report it at
// $0 and its usage as a provider.call.usage a few milliseconds later. So a
// completion with no cost waits, up to completionUsageWait, for the call's
// usage, and carries it when it arrives in time. Only the event waits: the
// interruption, the tool calls, the next call and the messages go on at once.
//
// Completions still go out in the order the calls completed: one that does
// not wait (it has its cost) sends those waiting before it first, and usage
// for a waiting call sends those before it first. Usage arriving after the
// wait is a provider.call.usage, as before, and never precedes the call's
// completion. The session's end sends every completion still waiting.

// completionUsageWait is how long a provider call's completion with no cost
// waits for the call's usage. Usage a provider reports after a call ends
// comes milliseconds after its end; the wait bounds how late the completion
// of a call whose usage never comes is.
const completionUsageWait = 300 * time.Millisecond

// heldCompletion is a provider.call.completed waiting for its call's usage.
type heldCompletion struct {
	ctx       context.Context
	round     *duplexRound
	msg       types.Message
	dur       time.Duration
	noMessage bool
	timer     *time.Timer
}

// completionWait is usageWait, or completionUsageWait when it is unset.
func (s *DuplexProviderStage) completionWait() time.Duration {
	if s.usageWait == 0 {
		return completionUsageWait
	}
	return s.usageWait
}

// hasCost reports whether c reports any usage or cost (cached tokens are
// part of the input tokens).
func hasCost(c *types.CostInfo) bool {
	return c != nil && (c.TotalCost != 0 || c.InputTokens != 0 || c.OutputTokens != 0)
}

// emitRoundCompleted emits provider.call.completed for a realtime round that
// took dur, as ProviderStage.emitProviderCallCompleted does for a round. A
// completion with no cost waits for the call's usage (holdCompletionLocked);
// one that does not wait goes out after those waiting.
func (s *DuplexProviderStage) emitRoundCompleted(
	ctx context.Context, msg *types.Message, round *duplexRound, dur time.Duration, noMessage bool,
) {
	if s.emitter == nil {
		return
	}
	dt := s.tools
	dt.completionMu.Lock()
	defer dt.completionMu.Unlock()
	if s.holdCompletionLocked(ctx, msg, round, dur, noMessage) {
		return
	}
	s.sendHeld(dt.takeHeld(nil), nil)
	s.emitRoundCompletedNow(ctx, msg, round, dur, noMessage)
}

// holdCompletionLocked holds round's completion, when it has no cost, until
// the call's usage arrives (sendWithUsage) or the wait ends, and reports
// whether it did. Called with dt.completionMu held.
func (s *DuplexProviderStage) holdCompletionLocked(
	ctx context.Context, msg *types.Message, round *duplexRound, dur time.Duration, noMessage bool,
) bool {
	if hasCost(msg.CostInfo) {
		return false
	}
	dt := s.tools
	dt.mu.Lock()
	defer dt.mu.Unlock()
	h := &heldCompletion{ctx: ctx, round: round, msg: *msg, dur: dur, noMessage: noMessage}
	h.timer = time.AfterFunc(s.completionWait(), func() {
		dt.completionMu.Lock()
		defer dt.completionMu.Unlock()
		s.sendHeld(dt.takeHeld(round), nil)
	})
	dt.held = append(dt.held, h)
	return true
}

// takeHeld takes the held completions up to and including round's (all of
// them when round is nil), in the order they were held; none when round's
// is not held. Called with dt.completionMu held.
func (dt *duplexTools) takeHeld(round *duplexRound) []*heldCompletion {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	n := len(dt.held)
	if round != nil {
		n = slices.IndexFunc(dt.held, func(h *heldCompletion) bool { return h.round == round }) + 1
	}
	taken := slices.Clone(dt.held[:n])
	dt.held = slices.Delete(dt.held, 0, n)
	return taken
}

// sendHeld emits held completions in order; the one for usage's call, when
// usage is set, carries it. Called with dt.completionMu held.
func (s *DuplexProviderStage) sendHeld(held []*heldCompletion, usage *heldUsage) {
	for _, h := range held {
		h.timer.Stop()
		if usage != nil && h.round == usage.round {
			h.msg.CostInfo = addCost(h.msg.CostInfo, usage.cost)
		}
		s.emitRoundCompletedNow(h.ctx, &h.msg, h.round, h.dur, h.noMessage)
	}
}

// heldUsage is usage reported for a call after it completed.
type heldUsage struct {
	round *duplexRound
	cost  *types.CostInfo
}

// reportUsage reports cost for round's call, which has completed: on its
// completion when that is still waiting for it, else as a
// provider.call.usage after the completion. A nil round is a session-level
// usage.
func (s *DuplexProviderStage) reportUsage(ctx context.Context, cost *types.CostInfo, round *duplexRound) {
	dt := s.tools
	dt.completionMu.Lock()
	defer dt.completionMu.Unlock()
	if round != nil {
		if held := dt.takeHeld(round); len(held) > 0 {
			s.sendHeld(held, &heldUsage{round: round, cost: cost})
			return
		}
	} else {
		round = &duplexRound{} // a session-level record: no call to name
	}
	s.emitRoundUsage(ctx, cost, round)
}

// flushHeldCompletions emits every completion still waiting for its usage,
// as it is: the session has ended, and no usage can arrive.
func (s *DuplexProviderStage) flushHeldCompletions() {
	dt := s.tools
	dt.completionMu.Lock()
	defer dt.completionMu.Unlock()
	s.sendHeld(dt.takeHeld(nil), nil)
}
