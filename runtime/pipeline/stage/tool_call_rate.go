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

// callRateKey names one tool's window in one conversation.
type callRateKey struct {
	conversationID string
	tool           string
}

// CallRateWindow enforces ToolPolicy.MaxCallsPerMinute: for each conversation
// and tool, the times of the calls admitted to run in the last minute. A
// provider stage holds one (ProviderConfig.CallRate, or its own), so the
// window spans every turn of a conversation the stage runs. A call held for
// approval is not counted by the stage; Admit counts it once it is approved.
// Safe for concurrent use: a round's calls are checked in parallel.
//
// tools.WithRateLimit is the other sliding window, and a different limit: one
// per registry, shared by its children, counting every conversation's calls
// to a tool together under a fixed limit, and refusing with
// tools.ErrRateLimitExceeded. This one is keyed on the conversation, takes
// the limit of the policy in force for each call, drops idle conversations
// (its keys are unbounded, the registry's are tool names), and counts calls
// admitted outside the stage (Admit).
type CallRateWindow struct {
	mu        sync.Mutex
	calls     map[callRateKey][]time.Time
	lastSweep time.Time
	now       func() time.Time // injectable for tests
}

// NewCallRateWindow returns an empty window.
func NewCallRateWindow() *CallRateWindow {
	return &CallRateWindow{calls: map[callRateKey][]time.Time{}, now: time.Now}
}

// Admit records a call to tool in conversationID that was admitted to run
// outside the provider stage — a held call a human approved — so it counts
// against MaxCallsPerMinute like a call the stage ran. It records without
// checking the limit: the approval is the admission. Nil-safe.
func (w *CallRateWindow) Admit(conversationID, tool string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	cutoff := now.Add(-callRateWindowLength)
	w.sweep(now, cutoff)
	key := callRateKey{conversationID: conversationID, tool: tool}
	w.calls[key] = append(unexpired(w.calls[key], cutoff), now)
}

// allow reports whether conversationID may call tool once more under a limit
// of limit calls per minute, and records the call when it may.
func (w *CallRateWindow) allow(conversationID, tool string, limit int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	cutoff := now.Add(-callRateWindowLength)
	w.sweep(now, cutoff)

	key := callRateKey{conversationID: conversationID, tool: tool}
	recent := unexpired(w.calls[key], cutoff)
	if len(recent) >= limit {
		w.calls[key] = recent
		return false
	}
	w.calls[key] = append(recent, now)
	return true
}

// sweep drops windows with no call left in them, at most once per window
// length, so a long-lived stage does not keep every conversation it ran.
func (w *CallRateWindow) sweep(now, cutoff time.Time) {
	if now.Sub(w.lastSweep) < callRateWindowLength {
		return
	}
	w.lastSweep = now
	for key, times := range w.calls {
		if len(unexpired(times, cutoff)) == 0 {
			delete(w.calls, key)
		}
	}
}

// unexpired returns the times after cutoff; times is in ascending order.
func unexpired(times []time.Time, cutoff time.Time) []time.Time {
	start := 0
	for start < len(times) && !times[start].After(cutoff) {
		start++
	}
	return times[start:]
}

// callRateLimited answers a call refused by ToolPolicy.MaxCallsPerMinute,
// in the shape of the other tool-policy refusals, or reports false when the
// call is within the limit (or none is set).
func (s *ProviderStage) callRateLimited(toolCall types.MessageToolCall) (types.Message, bool) {
	p := s.policy()
	if p == nil || p.MaxCallsPerMinute <= 0 || s.callRate == nil {
		return types.Message{}, false
	}
	var conversationID string
	if s.turnState != nil {
		conversationID = s.turnState.ConversationID
	}
	if s.callRate.allow(conversationID, toolCall.Name, p.MaxCallsPerMinute) {
		return types.Message{}, false
	}
	errMsg := fmt.Sprintf("Tool %s not executed: its limit of %d calls per minute is reached",
		toolCall.Name, p.MaxCallsPerMinute)
	result := types.NewTextToolResult(toolCall.ID, toolCall.Name, errMsg)
	result.Error = errMsg
	result.ErrorType = types.ToolErrorApproval
	return types.NewToolResultMessage(result), true
}
