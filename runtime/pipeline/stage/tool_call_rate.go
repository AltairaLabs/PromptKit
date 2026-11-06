package stage

import (
	"fmt"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// callRateWindowLength is the sliding window ToolPolicy.MaxCallsPerMinute
// counts calls over.
const callRateWindowLength = time.Minute

// CallRateReservationLifetime is how long a reserved slot lasts when nothing
// commits or releases it: a call held for approval, or a client tool's call,
// that is never answered. Once it lapses, the slot is free. It matches the
// SDK's default pending-call TTL (sdk/tools.DefaultPendingTTL), after which an
// in-memory held call expires and cannot be approved. A held call that is approved
// after its reservation has lapsed still commits a slot, because the approval
// is what admits it, so with a longer pending TTL the window can then briefly
// exceed the limit.
const CallRateReservationLifetime = 5 * time.Minute

// callRateKey names one tool's window in one conversation.
type callRateKey struct {
	conversationID string
	tool           string
}

// callRateReservation is a slot held by a call that has been made but not yet
// admitted to run or refused.
type callRateReservation struct {
	callID string
	seq    uint64 // tells apart reservations that share a call ID
	at     time.Time
}

// callRateSlots is one tool's window in one conversation: the times calls were
// admitted to run, in ascending order, and the reservations still open.
type callRateSlots struct {
	committed []time.Time
	reserved  []callRateReservation
}

// callRateCounts is the state a CallRateWindow and its Scoped views share.
type callRateCounts struct {
	mu        sync.Mutex
	slots     map[callRateKey]*callRateSlots
	lastSweep time.Time
	seq       uint64
	now       func() time.Time // injectable for tests
}

// CallRateWindow enforces ToolPolicy.MaxCallsPerMinute. For each conversation
// and tool, it tracks the slots taken in the last minute, using three steps:
//
//   - Reserve: when the model makes a call, the provider stage reserves a slot.
//     If the committed and reserved slots already reach the limit, the call is
//     refused at once. A call held for approval and a call the executor
//     defers (ToolStatusPending, such as a client tool) keep their
//     reservation.
//   - Commit: when the call is admitted to run, its slot is committed and
//     counts for one minute from then. The stage commits a call it runs. The
//     host commits a held or deferred call when it is approved or answered
//     (Commit).
//   - Release: a call that is rejected gives its slot back (Release).
//
// A reservation that is never resolved lapses after
// CallRateReservationLifetime.
//
// A provider stage holds one window (ProviderConfig.CallRate, or its own), so
// the window spans every turn of a conversation the stage runs. Safe for
// concurrent use: a round's calls are checked in parallel.
//
// tools.WithRateLimit is the other sliding window, and a different limit. It
// is one per registry, shared by its children, counts every conversation's
// calls to a tool together under a fixed limit, and refuses with
// tools.ErrRateLimitExceeded. This one is keyed on the conversation, takes
// the limit of the policy in force for each call, drops idle conversations
// (its keys are unbounded, the registry's are tool names), and is resolved by
// the host for calls that run outside the stage.
type CallRateWindow struct {
	counts *callRateCounts
	// scope, when scoped, replaces every conversation ID the window is
	// called with (see Scoped).
	scope  string
	scoped bool
}

// NewCallRateWindow returns an empty window.
func NewCallRateWindow() *CallRateWindow {
	return &CallRateWindow{counts: &callRateCounts{slots: map[callRateKey]*callRateSlots{}, now: time.Now}}
}

// Scoped returns a view of w in which every call counts under scope, whatever
// conversation ID it is reserved, committed or released under. The view shares
// w's counts. Use it to give one limit to conversations that act as one, such
// as the conversations a workflow opens for its states, which each have their
// own ID. Nil-safe: a nil window's view is nil.
func (w *CallRateWindow) Scoped(scope string) *CallRateWindow {
	if w == nil {
		return nil
	}
	return &CallRateWindow{counts: w.counts, scope: scope, scoped: true}
}

// Commit admits a call to tool in conversationID: its reserved slot, if it
// still has one, becomes a call that counts against MaxCallsPerMinute for one
// minute from now. The host calls it when a held call is approved or a
// deferred call is answered. It does not check the limit, because the
// reservation already did and the approval or answer is the admission. A call
// whose reservation lapsed, or that was reserved by another process, still
// commits. Nil-safe.
func (w *CallRateWindow) Commit(conversationID, tool, callID string) {
	if w == nil {
		return
	}
	w.resolve(conversationID, tool, func(s *callRateSlots, now time.Time) {
		s.dropReservation(func(r callRateReservation) bool { return r.callID == callID })
		s.committed = append(s.committed, now)
	})
}

// Release gives back the slot reserved for a call to tool in conversationID
// that was rejected, so it does not count. It does nothing when the call holds
// no reservation. Where call IDs repeat (a provider that numbers calls
// call_0, call_1, ...), it releases the oldest reservation under callID.
// Nil-safe.
func (w *CallRateWindow) Release(conversationID, tool, callID string) {
	if w == nil {
		return
	}
	w.resolve(conversationID, tool, func(s *callRateSlots, _ time.Time) {
		s.dropReservation(func(r callRateReservation) bool { return r.callID == callID })
	})
}

// reserve reserves a slot for a call to tool in conversationID under a limit
// of limit calls per minute, and reports false, reserving nothing, when the
// committed and reserved slots already reach it. The token names the
// reservation for commitReserved.
func (w *CallRateWindow) reserve(conversationID, tool, callID string, limit int) (token uint64, ok bool) {
	w.resolve(conversationID, tool, func(s *callRateSlots, now time.Time) {
		if len(s.committed)+len(s.reserved) >= limit {
			return
		}
		w.counts.seq++
		token, ok = w.counts.seq, true
		s.reserved = append(s.reserved, callRateReservation{callID: callID, seq: token, at: now})
	})
	return token, ok
}

// commitReserved commits the reservation token names, made by reserve.
func (w *CallRateWindow) commitReserved(conversationID, tool string, token uint64) {
	w.resolve(conversationID, tool, func(s *callRateSlots, now time.Time) {
		s.dropReservation(func(r callRateReservation) bool { return r.seq == token })
		s.committed = append(s.committed, now)
	})
}

// resolve is the single record helper: under the lock, it sweeps idle
// windows, prunes the expired slots of conversationID's window for tool, and
// passes that window to fn.
func (w *CallRateWindow) resolve(conversationID, tool string, fn func(s *callRateSlots, now time.Time)) {
	c := w.counts
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	c.sweep(now)
	if w.scoped {
		conversationID = w.scope
	}
	key := callRateKey{conversationID: conversationID, tool: tool}
	s := c.slots[key]
	if s == nil {
		s = &callRateSlots{}
		c.slots[key] = s
	}
	s.prune(now)
	fn(s, now)
	if s.empty() {
		delete(c.slots, key)
	}
}

// sweep drops windows with no slot left in them, at most once per window
// length, so a long-lived stage does not keep every conversation it ran.
// Caller holds mu.
func (c *callRateCounts) sweep(now time.Time) {
	if now.Sub(c.lastSweep) < callRateWindowLength {
		return
	}
	c.lastSweep = now
	for key, s := range c.slots {
		s.prune(now)
		if s.empty() {
			delete(c.slots, key)
		}
	}
}

// prune drops the committed slots older than the window and the reservations
// older than their lifetime.
func (s *callRateSlots) prune(now time.Time) {
	cutoff := now.Add(-callRateWindowLength)
	start := 0
	for start < len(s.committed) && !s.committed[start].After(cutoff) {
		start++
	}
	s.committed = s.committed[start:]
	lapse := now.Add(-CallRateReservationLifetime)
	s.dropAll(func(r callRateReservation) bool { return !r.at.After(lapse) })
}

// dropReservation removes the oldest reservation match accepts.
func (s *callRateSlots) dropReservation(match func(callRateReservation) bool) {
	for i, r := range s.reserved {
		if match(r) {
			s.reserved = append(s.reserved[:i:i], s.reserved[i+1:]...)
			return
		}
	}
}

// dropAll removes every reservation match accepts.
func (s *callRateSlots) dropAll(match func(callRateReservation) bool) {
	kept := s.reserved[:0]
	for _, r := range s.reserved {
		if !match(r) {
			kept = append(kept, r)
		}
	}
	s.reserved = kept
}

func (s *callRateSlots) empty() bool { return len(s.committed) == 0 && len(s.reserved) == 0 }

// callRateSlot is the slot a call reserved under ToolPolicy.MaxCallsPerMinute,
// which the stage commits once it runs the call. The zero value holds none.
type callRateSlot struct {
	window         *CallRateWindow
	conversationID string
	tool           string
	token          uint64
}

// commit commits the slot. It does nothing for the zero value.
func (r callRateSlot) commit() {
	if r.window != nil {
		r.window.commitReserved(r.conversationID, r.tool, r.token)
	}
}

// reserveCallRate reserves a slot for toolCall under ToolPolicy.MaxCallsPerMinute.
// When the limit is reached it returns the refusal the model is answered
// with, in the shape of the other tool-policy refusals, and limited is true.
// With no limit set, the slot is the zero value.
func (s *ProviderStage) reserveCallRate(
	toolCall types.MessageToolCall,
) (slot callRateSlot, refusal types.Message, limited bool) {
	p := s.policy()
	if p == nil || p.MaxCallsPerMinute <= 0 || s.callRate == nil {
		return callRateSlot{}, types.Message{}, false
	}
	var conversationID string
	if s.turnState != nil {
		conversationID = s.turnState.ConversationID
	}
	token, ok := s.callRate.reserve(conversationID, toolCall.Name, toolCall.ID, p.MaxCallsPerMinute)
	if ok {
		return callRateSlot{window: s.callRate, conversationID: conversationID, tool: toolCall.Name, token: token},
			types.Message{}, false
	}
	errMsg := fmt.Sprintf("Tool %s not executed: its limit of %d calls per minute is reached",
		toolCall.Name, p.MaxCallsPerMinute)
	result := types.NewTextToolResult(toolCall.ID, toolCall.Name, errMsg)
	result.Error = errMsg
	result.ErrorType = types.ToolErrorApproval
	return callRateSlot{}, types.NewToolResultMessage(result), true
}
