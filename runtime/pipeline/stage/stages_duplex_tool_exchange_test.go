package stage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// These tests check the transcript a realtime session leaves for the tool
// calls the stage runs: every call message is followed at once by a result for
// each of its calls, however the session ends and whatever input arrives while
// the round runs, so a text provider accepts the conversation when it resumes.

// transcriptOf renders the messages in elems, in order, as role and content:
// "assistant:calls c1 c2", "tool:c1=<result or error>", "user:text".
func transcriptOf(elems []StreamElement) []string {
	var out []string
	for i := range elems {
		m := elems[i].Message
		if m == nil {
			continue
		}
		switch {
		case len(m.ToolCalls) > 0:
			line := m.Role + ":calls"
			for _, tc := range m.ToolCalls {
				line += " " + tc.ID
			}
			out = append(out, line)
		case m.ToolResult != nil:
			text := m.ToolResult.Error
			if text == "" {
				text = m.ToolResult.GetTextContent()
			}
			out = append(out, m.Role+":"+m.ToolResult.ID+"="+text)
		default:
			out = append(out, m.Role+":"+m.GetContent())
		}
	}
	return out
}

// awaitRunning waits until the tool has been called n times.
func awaitRunning(t *testing.T, exec *realtimeToolExec, n int) {
	t.Helper()
	require.Eventually(t, func() bool { return len(exec.calls()) >= n }, 2*time.Second, time.Millisecond,
		"the tool was never called %d times", n)
}

// cancelAndCollect cancels the stage's context, as a caller closing the
// conversation does, and returns what it emitted once Process has returned.
func (h *realtimeHarness) cancelAndCollect() []StreamElement {
	h.t.Helper()
	h.cancel()
	select {
	case <-h.done:
	case <-time.After(3 * time.Second):
		h.t.Fatal("Process did not return after its context was canceled")
	}
	return drainUntilClosed(h.t, h.output)
}

const sessionClosedRefusal = "Tool weather not executed: the session closed before it ran"

// A session closed while a round runs leaves the round's call answered: the
// tool sees the cancellation, and its outcome is stored right after the call.
func TestDuplexToolExchange_CloseDuringRoundStoresTheResult(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`), gate: make(chan struct{})}
	h := startRealtime(t, realtimeOpts{registry: newWeatherRegistry(t, exec)})

	h.emit(toolCallTurn(weatherCall("c1")))
	awaitRunning(t, exec, 1)
	elems := h.cancelAndCollect()

	assert.Equal(t, []string{"assistant:calls c1", "tool:c1=context canceled"}, transcriptOf(elems))
	assert.Empty(t, h.sess().sentResponses(), "nothing is sent to the closing session")
}

// A round still queued behind a running one when the session closes never
// runs, but its calls are answered too, each right after its own call message,
// which comes after the running round's result.
func TestDuplexToolExchange_QueuedRoundCutByCloseIsAnswered(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`), gate: make(chan struct{})}
	h := startRealtime(t, realtimeOpts{registry: newWeatherRegistry(t, exec)})

	h.emit(toolCallTurn(weatherCall("c1")))
	awaitRunning(t, exec, 1)
	h.emit(toolCallTurn(weatherCall("c2"), weatherCall("c3")))
	h.settle()
	elems := h.cancelAndCollect()

	assert.Equal(t, []string{
		"assistant:calls c1", "tool:c1=context canceled",
		"assistant:calls c2 c3", "tool:c2=" + sessionClosedRefusal, "tool:c3=" + sessionClosedRefusal,
	}, transcriptOf(elems))
	assert.Equal(t, []string{"weather"}, exec.calls(), "the queued round never ran")
}

// Process returns only once the tool worker has: a tool that does not watch
// its context finishes before Process returns, and its result is stored.
func TestDuplexToolExchange_ProcessJoinsTheToolWorker(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`), delay: 200 * time.Millisecond}
	h := startRealtime(t, realtimeOpts{registry: newWeatherRegistry(t, exec)})

	h.emit(toolCallTurn(weatherCall("c1")))
	awaitRunning(t, exec, 1)
	elems := h.cancelAndCollect()

	assert.Equal(t, 1, exec.done(), "the running tool finished before Process returned")
	assert.Equal(t, []string{"assistant:calls c1", "tool:c1=" + weatherResult}, transcriptOf(elems))
}

// A user turn that arrives while an earlier turn's round runs is stored after
// the round's results, not between the call and its results.
func TestDuplexToolExchange_UserTurnDuringRoundFollowsTheResults(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`), gate: make(chan struct{})}
	h := startRealtime(t, realtimeOpts{
		registry: newWeatherRegistry(t, exec),
		script:   [][]providers.StreamChunk{textTurn("It is 21 in Paris", nil)},
	})

	h.emit(toolCallTurn(weatherCall("c1")))
	awaitRunning(t, exec, 1)
	earlier := h.toolTurn()
	user := types.Message{Role: roleUser, Content: "and in Rome?"}
	h.input <- StreamElement{Message: &user}
	h.awaitNewToolTurn(earlier)
	close(exec.gate)
	h.awaitSent(1)
	h.settle()
	elems := h.finish()

	assert.Equal(t, []string{
		"assistant:calls c1", "tool:c1=" + weatherResult, "user:and in Rome?", "assistant:It is 21 in Paris",
	}, transcriptOf(elems))
}

// A round held for the caller (approval) does not hold up the transcript: the
// held call is answered later, by the caller, and what follows it is stored
// as it comes.
func TestDuplexToolExchange_HeldCallDoesNotHoldTheTranscript(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	gate := func(context.Context, string, string, map[string]any) (*tools.PendingToolInfo, error) {
		return &tools.PendingToolInfo{Reason: "approval", Message: "approve weather"}, nil
	}
	h := startRealtime(t, realtimeOpts{registry: newWeatherRegistry(t, exec), cfg: DuplexToolConfig{ApprovalGate: gate}})

	h.emit(toolCallTurn(weatherCall("c1")))
	h.settle()
	require.Eventually(t, func() bool { return !h.stage.tools.roundOutstanding() }, 2*time.Second, time.Millisecond,
		"the held round was never delivered")
	earlier := h.toolTurn()
	user := types.Message{Role: roleUser, Content: "still there?"}
	h.input <- StreamElement{Message: &user}
	h.awaitNewToolTurn(earlier)
	elems := h.finish()

	assert.Equal(t, []string{"assistant:calls c1", "user:still there?"}, transcriptOf(elems))
	assert.Empty(t, exec.calls())
}

// blockingAllowHook allows BeforeCall once release is closed; entered is
// closed as BeforeCall begins.
type blockingAllowHook struct {
	release chan struct{}
	entered chan struct{}
}

func (h *blockingAllowHook) Name() string { return "blocking_allow" }

func (h *blockingAllowHook) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	close(h.entered)
	<-h.release
	return hooks.Allow
}

func (h *blockingAllowHook) AfterCall(context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse) hooks.Decision {
	return hooks.Allow
}

// A provider call the input goroutine opens after the session ended (a typed
// turn an input hook let through late) is completed before output closes:
// every provider.call.started has its provider.call.completed.
func TestDuplexToolExchange_CallOpenedAfterTheSessionEndedIsCompleted(t *testing.T) {
	hook := &blockingAllowHook{release: make(chan struct{}), entered: make(chan struct{})}
	s, sess, _ := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(hook)))
	emitter, ev := newCallEvents()
	s.emitter = emitter
	s.SetToolExecution(registryWithTools(t, "weather"), nil, DuplexToolConfig{}, nil)
	s.inputGracePeriod = 5 * time.Millisecond
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 8)
	text := "hello"
	input <- StreamElement{Text: &text}

	done := make(chan error, 1)
	go func() { done <- s.Process(context.Background(), input, output) }()
	<-hook.entered
	close(sess.respCh) // the provider ends the session while the hook runs
	require.NoError(t, <-done)
	close(hook.release)
	drainUntilClosed(t, output)

	require.Eventually(t, func() bool {
		ev.mu.Lock()
		defer ev.mu.Unlock()
		return len(ev.started) == 1 && len(ev.done) == 1
	}, 2*time.Second, time.Millisecond, "the call opened late was never completed")
	ev.mu.Lock()
	defer ev.mu.Unlock()
	assert.Equal(t, ev.started[0].CallID, ev.done[0].CallID)
	assert.Equal(t, types.FinishReasonInterrupted, ev.done[0].FinishReason)
	assert.True(t, ev.done[0].NoMessage)
}
