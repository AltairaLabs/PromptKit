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

// callRateWindow enforces ToolPolicy.MaxCallsPerMinute: for each conversation
// and tool, the times of the calls admitted in the last minute. It lives on
// the provider stage, so the window spans every turn of a conversation the
// stage runs. Safe for concurrent use: a round's calls are checked in
// parallel.
type callRateWindow struct {
	mu        sync.Mutex
	calls     map[callRateKey][]time.Time
	lastSweep time.Time
	now       func() time.Time // injectable for tests
}

func newCallRateWindow() *callRateWindow {
	return &callRateWindow{calls: map[callRateKey][]time.Time{}, now: time.Now}
}

// allow reports whether conversationID may call tool once more under a limit
// of limit calls per minute, and records the call when it may.
func (w *callRateWindow) allow(conversationID, tool string, limit int) bool {
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
func (w *callRateWindow) sweep(now, cutoff time.Time) {
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
