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

// testSendTimeout replaces partialResponseSendTimeout in the tests that wait
// it out, so they take milliseconds; the bounds they check are multiples of it.
const testSendTimeout = 50 * time.Millisecond

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

// Process waits for the tool worker only for the stage's grace period: a
// tool that does not watch its context does not hold the session's end. Its
// call is answered with the session-closed refusal, and what the tool returns
// later is discarded, never sent on the closed output.
func TestDuplexToolExchange_ProcessWaitsForTheToolWorkerABoundedTime(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`), delay: 200 * time.Millisecond}
	h := startRealtime(t, realtimeOpts{registry: newWeatherRegistry(t, exec)})
	h.stage.inputGracePeriod = 20 * time.Millisecond

	h.emit(toolCallTurn(weatherCall("c1")))
	awaitRunning(t, exec, 1)
	start := time.Now()
	elems := h.cancelAndCollect()

	assert.Less(t, time.Since(start), 150*time.Millisecond, "Process returned within its grace, not the tool's time")
	assert.Equal(t, []string{"assistant:calls c1", "tool:c1=" + sessionClosedRefusal}, transcriptOf(elems))
	require.Eventually(t, func() bool { return exec.done() == 1 }, 2*time.Second, time.Millisecond)
	time.Sleep(50 * time.Millisecond) // the worker hands its late result over, or drops it
	_, open := <-h.output
	assert.False(t, open, "nothing is sent after Process returned")
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

// heldGate holds every call for approval.
func heldGate(context.Context, string, string, map[string]any) (*tools.PendingToolInfo, error) {
	return &tools.PendingToolInfo{Reason: "approval", Message: "approve weather"}, nil
}

// A call held for approval awaits its result like any other: what follows it
// is stored after the result the caller gives, here after a user turn that
// came in meanwhile.
func TestDuplexToolExchange_ApprovedCallIsStoredBeforeTheTurnsAfterIt(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	h := startRealtime(t, realtimeOpts{
		registry: newWeatherRegistry(t, exec),
		cfg:      DuplexToolConfig{ApprovalGate: heldGate},
		script:   [][]providers.StreamChunk{textTurn("Approved: 21 in Paris", nil)},
	})

	h.emit(toolCallTurn(weatherCall("c1")))
	h.settle()
	require.Eventually(t, func() bool { return !h.stage.tools.roundOutstanding() }, 2*time.Second, time.Millisecond,
		"the held round was never delivered")
	earlier := h.toolTurn()
	user := types.Message{Role: roleUser, Content: "still there?"}
	h.input <- StreamElement{Message: &user}
	h.awaitNewToolTurn(earlier)
	approved := types.NewToolResultMessage(types.NewTextToolResult("c1", "weather", weatherResult))
	var answer StreamElement
	answer.Meta.ToolResultMessages = []types.Message{approved}
	answer.Meta.ToolResponses = []providers.ToolResponse{{ToolCallID: "c1", Result: weatherResult}}
	h.input <- answer
	h.awaitSent(1)
	h.settle()
	elems := h.finish()

	assert.Equal(t, []string{
		"assistant:calls c1", "tool:c1=" + weatherResult, "user:still there?", "assistant:Approved: 21 in Paris",
	}, transcriptOf(elems))
}

// A call still held for approval when the session ends gets the
// session-closed refusal, ahead of the messages held behind it.
func TestDuplexToolExchange_HeldCallUnansweredAtCloseIsRefused(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	h := startRealtime(t, realtimeOpts{registry: newWeatherRegistry(t, exec), cfg: DuplexToolConfig{ApprovalGate: heldGate}})

	h.emit(toolCallTurn(weatherCall("c1")))
	h.settle()
	require.Eventually(t, func() bool { return !h.stage.tools.roundOutstanding() }, 2*time.Second, time.Millisecond,
		"the held round was never delivered")
	earlier := h.toolTurn()
	user := types.Message{Role: roleUser, Content: "still there?"}
	h.input <- StreamElement{Message: &user}
	h.awaitNewToolTurn(earlier)
	elems := h.finish()

	assert.Equal(t, []string{"assistant:calls c1", "tool:c1=" + sessionClosedRefusal, "user:still there?"},
		transcriptOf(elems))
	assert.Empty(t, exec.calls())
}

// A turn end with no message (an empty reply) after a held user turn is held
// behind it, so a consumer never sees a turn end before the turn's message.
func TestDuplexToolExchange_TurnEndIsHeldBehindAHeldMessage(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`), gate: make(chan struct{})}
	h := startRealtime(t, realtimeOpts{registry: newWeatherRegistry(t, exec)})

	h.emit(toolCallTurn(weatherCall("c1")))
	awaitRunning(t, exec, 1)
	earlier := h.toolTurn()
	user := types.Message{Role: roleUser, Content: "and in Rome?"}
	h.input <- StreamElement{Message: &user}
	h.awaitNewToolTurn(earlier)
	stop := "stop"
	h.emit([]providers.StreamChunk{{FinishReason: &stop}}) // an empty reply: a turn end with no message
	h.settle()
	close(exec.gate)
	h.awaitSent(1)
	elems := h.finish()

	var order []string
	for _, e := range elems {
		switch {
		case e.Message != nil:
			order = append(order, transcriptOf([]StreamElement{e})...)
		case e.EndOfStream:
			order = append(order, "turn-end")
		}
	}
	assert.Equal(t, []string{"assistant:calls c1", "tool:c1=" + weatherResult, "user:and in Rome?", "turn-end"}, order)
}

// A tool-call message whose call the provider's barge-in already completed is
// never run: it is not emitted as a round the stage runs, so nothing after it
// is held.
func TestDuplexToolExchange_CallOfACompletedGenerationHoldsNothing(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	h := startRealtime(t, realtimeOpts{registry: newWeatherRegistry(t, exec)})

	h.emit([]providers.StreamChunk{{Content: "Let me", Generation: 1}})
	h.emit([]providers.StreamChunk{{Interrupted: true, Generation: 1}})
	late := toolCallTurn(weatherCall("late1"))
	late[0].Generation = 1
	h.emit(late)
	h.settle()
	user := types.Message{Role: roleUser, Content: "hello?"}
	h.input <- StreamElement{Message: &user}
	require.Eventually(t, func() bool { return len(h.output) >= 3 }, 2*time.Second, time.Millisecond,
		"the user turn was held") // the partial reply, its interruption, the user turn
	reply := textTurn("Hi.", nil)
	reply[0].Generation = 2
	h.emit(reply)
	h.settle()
	elems := h.finish()

	assert.Equal(t, []string{"assistant:Let me", "user:hello?", "assistant:Hi."}, transcriptOf(elems))
	assert.Empty(t, exec.calls())
}

// A tool-call message that arrives once a limit is closing the session is a
// round the stage answers: its calls are refused at once (recorded, not sent),
// not left awaiting the session's end.
func TestDuplexToolExchange_CallWhileTheSessionClosesIsRefusedAtOnce(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	h := startRealtime(t, realtimeOpts{registry: newWeatherRegistry(t, exec)})
	require.True(t, h.stage.calls.setTerminal(), "a limit is closing the session")

	h.emit(toolCallTurn(weatherCall("c1")))
	h.settle()
	elems := h.finish()

	assert.Equal(t, []string{"assistant:calls c1",
		"tool:c1=Tool weather not executed: the turn's tool calls were stopped: a limit is closing the session"},
		transcriptOf(elems))
	assert.Empty(t, exec.calls())
	assert.Empty(t, h.sess().sentResponses(), "the refusal is recorded, not sent")
}

func indexOf(xs []string, x string) int {
	for i := range xs {
		if xs[i] == x {
			return i
		}
	}
	return -1
}

// passHook is a provider hook that allows everything, so the stage keeps the
// hooks' history.
type passHook struct{}

func (passHook) Name() string { return "pass" }
func (passHook) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}
func (passHook) AfterCall(context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse) hooks.Decision {
	return hooks.Allow
}

// The hooks' history keeps the order the messages are emitted in: a user turn
// held behind a round's results follows them there too.
func TestDuplexToolExchange_HooksHistoryFollowsTheEmittedOrder(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`), gate: make(chan struct{})}
	h := startRealtime(t, realtimeOpts{registry: newWeatherRegistry(t, exec),
		hookReg: hooks.NewRegistry(hooks.WithProviderHook(passHook{}))})

	h.emit(toolCallTurn(weatherCall("c1")))
	awaitRunning(t, exec, 1)
	earlier := h.toolTurn()
	user := types.Message{Role: roleUser, Content: "and in Rome?"}
	h.input <- StreamElement{Message: &user}
	h.awaitNewToolTurn(earlier)
	close(exec.gate)
	h.awaitSent(1)
	h.finish()

	var got []string
	for i := range h.stage.history.snapshot() {
		m := h.stage.history.snapshot()[i]
		got = append(got, transcriptOf([]StreamElement{{Message: &m}})...)
	}
	assert.Equal(t, []string{"assistant:calls c1", "tool:c1=" + weatherResult, "user:and in Rome?"}, got)
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

// closeOut is bounded in total once the context is canceled, and stops at the
// first element downstream does not take: a call message that cannot be sent
// takes its results with it.
func TestToolExchangeOrder_CloseOutIsBoundedAndKeepsGroupsWhole(t *testing.T) {
	o := toolExchangeOrder{sendTimeout: testSendTimeout}
	out := make(chan StreamElement, 1)
	ctx, cancel := context.WithCancel(context.Background())
	first := StreamElement{Message: &types.Message{Role: roleAssistant, ToolCalls: []types.MessageToolCall{weatherCall("c1")}}}
	first.Meta.ToolCallsRunning = true
	require.NoError(t, o.send(ctx, first, out, sendMode{}))
	<-out
	for i := 0; i < 10; i++ {
		later := StreamElement{Message: &types.Message{Role: roleAssistant,
			ToolCalls: []types.MessageToolCall{weatherCall("c" + string(rune('2'+i)))}}}
		later.Meta.ToolCallsRunning = true
		require.NoError(t, o.send(ctx, later, out, sendMode{}))
	}
	cancel()

	start := time.Now()
	o.closeOut(ctx, out, o.newDeadline())
	assert.Less(t, time.Since(start), 2*testSendTimeout, "bounded in total, not per element")
	close(out)
	var got []StreamElement
	for e := range out {
		got = append(got, e)
	}
	assert.Equal(t, []string{"tool:c1=" + sessionClosedRefusal}, transcriptOf(got),
		"no result is sent for a call message that was not")
}

// Once the shared close deadline has passed, every later send with it gives
// up at once, not only the first.
func TestCloseDeadline_EverySendAfterItGivesUp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := newCloseDeadline(testSendTimeout)
	full := make(chan StreamElement) // never read
	mode := sendMode{recorded: true, deadline: d}
	require.ErrorIs(t, sendElement(ctx, StreamElement{}, full, mode), context.Canceled)
	start := time.Now()
	for i := 0; i < 3; i++ {
		require.ErrorIs(t, sendElement(ctx, StreamElement{}, full, mode), context.Canceled)
	}
	assert.Less(t, time.Since(start), testSendTimeout/2)
}

// A recorded element sent with no shared deadline is given the order's send
// timeout once the context is canceled: it gives up after that, not after the
// production timeout.
func TestToolExchangeOrder_RecordedSendGetsTheOrdersTimeout(t *testing.T) {
	o := toolExchangeOrder{sendTimeout: testSendTimeout}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	full := make(chan StreamElement) // never read
	start := time.Now()
	err := o.send(ctx, StreamElement{Message: &types.Message{Role: roleUser, Content: "u"}}, full,
		sendMode{recorded: true})
	elapsed := time.Since(start)
	require.ErrorIs(t, err, context.Canceled)
	assert.GreaterOrEqual(t, elapsed, testSendTimeout, "the recorded element was given its timeout")
	assert.Less(t, elapsed, partialResponseSendTimeout, "the order's timeout, not the production one")
}

// The close deadline bounds closeOut as a whole: a downstream that takes each
// element just inside the timeout does not stretch it to one timeout each.
func TestToolExchangeOrder_CloseOutDeadlineIsShared(t *testing.T) {
	o := toolExchangeOrder{sendTimeout: testSendTimeout}
	out := make(chan StreamElement)
	ctx, cancel := context.WithCancel(context.Background())
	call := StreamElement{Message: &types.Message{Role: roleAssistant, ToolCalls: []types.MessageToolCall{weatherCall("c1")}}}
	call.Meta.ToolCallsRunning = true
	go func() { <-out }()
	require.NoError(t, o.send(ctx, call, out, sendMode{}))
	for i := 0; i < 6; i++ {
		require.NoError(t, o.send(ctx, StreamElement{Message: &types.Message{Role: roleUser, Content: "u"}}, out, sendMode{}))
	}
	cancel()
	taken := make(chan int)
	go func() {
		n := 0
		for {
			select {
			case <-out:
				n++
			case <-time.After(3 * testSendTimeout):
				taken <- n
				return
			}
			time.Sleep(testSendTimeout * 2 / 5)
		}
	}()

	start := time.Now()
	o.closeOut(ctx, out, o.newDeadline())
	assert.Less(t, time.Since(start), testSendTimeout*3/2, "one timeout in all")
	assert.Less(t, <-taken, 7, "what was not taken in time is dropped")
}
