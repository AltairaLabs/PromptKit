package stage

import (
	"errors"
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

// callRateCounts is the state a CallRateWindow and its Scoped views share:
// for each conversation and tool, the times calls were admitted, in ascending
// order.
type callRateCounts struct {
	mu        sync.Mutex
	admitted  map[callRateKey][]time.Time
	lastSweep time.Time
	now       func() time.Time // injectable for tests
}

// CallRateLimitError is the refusal of a call over ToolPolicy.MaxCallsPerMinute.
// Its message is what the model is told in place of the call's result.
type CallRateLimitError struct {
	Tool  string
	Limit int
}

func (e *CallRateLimitError) Error() string {
	return fmt.Sprintf("Tool %s not executed: its limit of %d calls per minute is reached", e.Tool, e.Limit)
}

// CallRateWindow enforces ToolPolicy.MaxCallsPerMinute. For each conversation
// and tool, it counts the calls admitted to run in the last minute. A call is
// checked against the limit, and counted, only when it is admitted (Admit):
//
//   - a call the provider stage runs is admitted immediately before the stage
//     executes it;
//   - a call held for approval is admitted when the host approves it;
//   - a call its executor defers (ToolStatusPending, such as a client tool) is
//     admitted when the host uses its result.
//
// A call that is refused, rejected, abandoned or never answered is never
// counted. Nothing is reserved, so nothing is left to give back. The cost is
// that a held or deferred call is checked late: a human may approve a call
// that then hits the limit, and a client may run a call whose result is then
// refused.
//
// The stage admits a call through tools.Registry.ExecuteAsyncAdmitted, after
// the registry's own checks pass (the tool exists, its arguments are valid,
// the registry's rate limit allows it), so a call those refuse is never
// counted. Whether an executor defers a call is known only when it returns:
// the stage takes a deferred call's admission back then. While the executor
// runs, the call counts, so another call to the same tool in the same round
// can be refused for the limit in that moment.
//
// A provider stage holds one window (ProviderConfig.CallRate, or its own), so
// the window spans every turn of a conversation the stage runs. Safe for
// concurrent use: a round's calls are admitted in parallel.
//
// tools.WithRateLimit is the other sliding window, and a different limit. It
// is one per registry, shared by its children, counts every conversation's
// calls to a tool together under a fixed limit, and refuses with
// tools.ErrRateLimitExceeded. This one is keyed on the conversation, takes
// the limit of the policy in force for each call, drops idle conversations
// (its keys are unbounded, the registry's are tool names), and is also used by
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
	return &CallRateWindow{counts: &callRateCounts{admitted: map[callRateKey][]time.Time{}, now: time.Now}}
}

// Scoped returns a view of w in which every call counts under scope, whatever
// conversation ID it is admitted under. The view shares w's counts. Use it to
// give one limit to conversations that act as one, such as the conversations
// a workflow opens for its states, which each have their own ID. Nil-safe: a
// nil window's view is nil.
func (w *CallRateWindow) Scoped(scope string) *CallRateWindow {
	if w == nil {
		return nil
	}
	return &CallRateWindow{counts: w.counts, scope: scope, scoped: true}
}

// Admit admits a call to tool in conversationID under a limit of limit calls
// per minute. If the calls admitted in the last minute already reach the
// limit, it returns a *CallRateLimitError and counts nothing; otherwise the
// call counts for one minute from now. The host calls it for a call that runs
// outside the provider stage: a held call when it is approved, a deferred call
// when its result is used. A nil window or a limit of 0 or less admits every
// call.
func (w *CallRateWindow) Admit(conversationID, tool string, limit int) error {
	if w == nil || limit <= 0 {
		return nil
	}
	_, err := w.admit(conversationID, tool, limit)
	return err
}

// admit is Admit, also returning the time the call was admitted at, for
// withdraw.
func (w *CallRateWindow) admit(conversationID, tool string, limit int) (at time.Time, err error) {
	w.record(conversationID, tool, func(admitted []time.Time, now time.Time) []time.Time {
		if len(admitted) >= limit {
			err = &CallRateLimitError{Tool: tool, Limit: limit}
			return admitted
		}
		at = now
		return append(admitted, now)
	})
	return at, err
}

// withdraw takes back the admission made at at, for a call the stage admitted
// whose executor then deferred it, to be admitted when its result is used. It
// runs before the stage returns the call's result.
func (w *CallRateWindow) withdraw(conversationID, tool string, at time.Time) {
	w.record(conversationID, tool, func(admitted []time.Time, _ time.Time) []time.Time {
		for i := len(admitted) - 1; i >= 0; i-- {
			if admitted[i].Equal(at) {
				return append(admitted[:i:i], admitted[i+1:]...)
			}
		}
		return admitted
	})
}

// record is the single record helper: under the lock, it sweeps idle
// windows, prunes the expired admissions of conversationID's window for tool,
// and replaces them with what fn returns.
func (w *CallRateWindow) record(
	conversationID, tool string, fn func(admitted []time.Time, now time.Time) []time.Time,
) {
	c := w.counts
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	c.sweep(now)
	if w.scoped {
		conversationID = w.scope
	}
	key := callRateKey{conversationID: conversationID, tool: tool}
	admitted := fn(prune(c.admitted[key], now), now)
	if len(admitted) == 0 {
		delete(c.admitted, key)
		return
	}
	c.admitted[key] = admitted
}

// sweep drops windows with no admission left in them, at most once per window
// length, so a long-lived stage does not keep every conversation it ran.
// Caller holds mu.
func (c *callRateCounts) sweep(now time.Time) {
	if now.Sub(c.lastSweep) < callRateWindowLength {
		return
	}
	c.lastSweep = now
	for key, admitted := range c.admitted {
		if len(prune(admitted, now)) == 0 {
			delete(c.admitted, key)
		}
	}
}

// prune drops the admissions older than the window.
func prune(admitted []time.Time, now time.Time) []time.Time {
	cutoff := now.Add(-callRateWindowLength)
	start := 0
	for start < len(admitted) && !admitted[start].After(cutoff) {
		start++
	}
	return admitted[start:]
}

// callRateAdmission admits one call the stage runs under
// ToolPolicy.MaxCallsPerMinute. The zero value admits without counting.
type callRateAdmission struct {
	window         *CallRateWindow
	conversationID string
	tool           string
	limit          int
	at             time.Time
	admitted       bool
}

// admit admits the call. The registry calls it once the call has passed the
// registry's own checks, immediately before the executor runs.
func (a *callRateAdmission) admit() error {
	if a.window == nil {
		return nil
	}
	at, err := a.window.admit(a.conversationID, a.tool, a.limit)
	if err == nil {
		a.at, a.admitted = at, true
	}
	return err
}

// withdraw takes the admission back, for a call its executor deferred.
func (a *callRateAdmission) withdraw() {
	if a.admitted {
		a.window.withdraw(a.conversationID, a.tool, a.at)
		a.admitted = false
	}
}

// callRateAdmission returns the admission for toolCall: the zero value when
// no limit is set.
func (s *ProviderStage) callRateAdmission(toolCall types.MessageToolCall) *callRateAdmission {
	p := s.policy()
	if p == nil || p.MaxCallsPerMinute <= 0 || s.callRate == nil {
		return &callRateAdmission{}
	}
	var conversationID string
	if s.turnState != nil {
		conversationID = s.turnState.ConversationID
	}
	return &callRateAdmission{
		window: s.callRate, conversationID: conversationID, tool: toolCall.Name, limit: p.MaxCallsPerMinute,
	}
}

// callRateRefusal reports whether err is the window's refusal of toolCall
// and, if so, returns the result the model is answered with, in the shape of
// the other tool-policy refusals.
func callRateRefusal(toolCall types.MessageToolCall, err error) (types.Message, bool) {
	var limitErr *CallRateLimitError
	if !errors.As(err, &limitErr) {
		return types.Message{}, false
	}
	errMsg := limitErr.Error()
	result := types.NewTextToolResult(toolCall.ID, toolCall.Name, errMsg)
	result.Error = errMsg
	result.ErrorType = types.ToolErrorApproval
	return types.NewToolResultMessage(result), true
}
