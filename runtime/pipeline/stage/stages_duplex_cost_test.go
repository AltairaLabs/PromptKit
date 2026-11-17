package stage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Cost a realtime session reports that no emitted message carries is charged
// to the budget and reported on provider.call.completed; nothing is added to
// the conversation.

// usd is a headline-only cost of in/out tokens at $0.001 each.
func usd(in, out int) *types.CostInfo {
	return &types.CostInfo{
		InputTokens: in, OutputTokens: out,
		InputCostUSD: float64(in) / 1000, OutputCostUSD: float64(out) / 1000,
		TotalCost: float64(in+out) / 1000,
	}
}

// costedReplies returns the assistant messages in elems, in order.
func costedReplies(elems []StreamElement) []*types.Message {
	var out []*types.Message
	for _, e := range elems {
		if e.Message != nil && e.Message.Role == roleAssistant {
			out = append(out, e.Message)
		}
	}
	return out
}

// callEvents collects provider.call.started, completed and usage events.
type callEvents struct {
	mu         sync.Mutex
	started    []*events.ProviderCallStartedData
	done       []*events.ProviderCallCompletedData
	usage      []*events.ProviderCallUsageData
	toolStarts []*events.ToolCallEventData
}

func newCallEvents() (*events.Emitter, *callEvents) {
	bus := events.NewEventBus()
	c := &callEvents{}
	bus.SubscribeAll(func(e *events.Event) {
		c.mu.Lock()
		defer c.mu.Unlock()
		switch d := e.Data.(type) {
		case *events.ProviderCallStartedData:
			c.started = append(c.started, d)
		case *events.ProviderCallCompletedData:
			c.done = append(c.done, d)
		case *events.ProviderCallUsageData:
			c.usage = append(c.usage, d)
		case *events.ToolCallEventData:
			if e.Type == events.EventToolCallStarted {
				c.toolStarts = append(c.toolStarts, d)
			}
		}
	})
	return events.NewEmitter(bus, "run", "sess", "conv"), c
}

// await returns the completed events once there are n of them, and the usage
// events once there are u.
func (c *callEvents) await(t *testing.T, n, u int) ([]*events.ProviderCallCompletedData, []*events.ProviderCallUsageData) {
	t.Helper()
	require.Eventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return len(c.done) >= n && len(c.usage) >= u
	}, 2*time.Second, time.Millisecond, "expected %d provider.call.completed and %d provider.call.usage", n, u)
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*events.ProviderCallCompletedData(nil), c.done...),
		append([]*events.ProviderCallUsageData(nil), c.usage...)
}

func costHarness(t *testing.T, budget float64) (*realtimeHarness, *callEvents) {
	t.Helper()
	emitter, ev := newCallEvents()
	var policy *pipeline.ToolPolicy
	if budget > 0 {
		policy = &pipeline.ToolPolicy{MaxCostUSD: budget}
	}
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	return startRealtime(t, realtimeOpts{registry: newWeatherRegistry(t, exec), policy: policy, emitter: emitter}), ev
}

func assertCall(t *testing.T, d *events.ProviderCallCompletedData, in, out int) {
	t.Helper()
	assert.Equal(t, in, d.InputTokens, "input tokens")
	assert.Equal(t, out, d.OutputTokens, "output tokens")
	assert.InDelta(t, float64(in+out)/1000, d.Cost, 1e-12, "cost")
}

func assertUsage(t *testing.T, d *events.ProviderCallUsageData, callID string, in, out int) {
	t.Helper()
	assert.Equal(t, callID, d.CallID, "the call the usage belongs to")
	assert.Equal(t, in, d.InputTokens, "input tokens")
	assert.Equal(t, out, d.OutputTokens, "output tokens")
	assert.InDelta(t, float64(in+out)/1000, d.Cost, 1e-12, "cost")
}

// gen tags chunks with the generation they belong to.
func gen(g int64, chunks []providers.StreamChunk) []providers.StreamChunk {
	for i := range chunks {
		chunks[i].Generation = g
	}
	return chunks
}

// sumCosts is the total of msgs' CostInfo.
func sumCosts(msgs []*types.Message) *types.CostInfo {
	var total *types.CostInfo
	for _, m := range msgs {
		total = addCost(total, m.CostInfo)
	}
	return total
}

func assertCostInfo(t *testing.T, want, got *types.CostInfo) {
	t.Helper()
	require.NotNil(t, got)
	assert.Equal(t, want.InputTokens, got.InputTokens, "input tokens")
	assert.Equal(t, want.OutputTokens, got.OutputTokens, "output tokens")
	assert.InDelta(t, want.TotalCost, got.TotalCost, 1e-12, "total cost")
}

func (h *realtimeHarness) spent() float64 {
	h.stage.tools.mu.Lock()
	defer h.stage.tools.mu.Unlock()
	return h.stage.tools.spent
}

func derefMessages(msgs []*types.Message) []types.Message {
	out := make([]types.Message, len(msgs))
	for i, m := range msgs {
		out[i] = *m
	}
	return out
}

// Gemini 3 reports a tool-call turn's usage after its tool calls; here the
// tool has run and the reply's call started before it arrives. It is still
// the tool call's: a provider.call.usage on that call. The completed events
// go out as each call completes, one per call, and the messages carry the
// usage on the next reply, adding up to the spend a resumed budget is
// rebuilt from.
func TestDuplexCost_LateUsageLandsOnItsCallAfterTheToolFinished(t *testing.T) {
	h, ev := costHarness(t, 0)
	h.emit(gen(1, toolCallTurn(weatherCall("c1"))))
	h.awaitSent(1) // the tool ran; the reply's call has started
	h.emit(gen(1, []providers.StreamChunk{{CostInfo: usd(600, 12)}}))
	h.emit(gen(2, textTurn("It is 21 degrees", usd(20, 5))))
	done, usage := ev.await(t, 2, 1)
	msgs := costedReplies(h.finish())

	require.Len(t, done, 2, "one completed event per call")
	assert.Equal(t, 1, done[0].ToolCallCount)
	assertCall(t, done[0], 0, 0)
	assertCall(t, done[1], 20, 5)
	require.Len(t, usage, 1)
	assertUsage(t, usage[0], done[0].CallID, 600, 12)
	require.Len(t, msgs, 2)
	assert.Nil(t, msgs[0].CostInfo)
	assertCostInfo(t, usd(620, 17), msgs[1].CostInfo)
	assert.InDelta(t, 0.637, h.spent(), 1e-12)
	assert.InDelta(t, h.spent(), sumHistoryCost(derefMessages(msgs)), 1e-12)
}

// Two barge-ins in a row: each interrupted call's completion, arriving after
// both, lands on its own call.
func TestDuplexCost_DoubleBargeInLandsEachCompletionOnItsCall(t *testing.T) {
	stop := "stop"
	h, ev := costHarness(t, 0)
	h.emit(gen(1, []providers.StreamChunk{{Content: "Once", Delta: "Once"}, {Interrupted: true}}))
	h.emit(gen(2, []providers.StreamChunk{{Content: "Sure", Delta: "Sure"}, {Interrupted: true}}))
	h.emit(gen(1, []providers.StreamChunk{{FinishReason: &stop, CostInfo: usd(0, 6)}}))
	h.emit(gen(2, []providers.StreamChunk{{FinishReason: &stop, CostInfo: usd(0, 9)}}))
	h.emit(gen(3, textTurn("Yes?", usd(30, 2))))
	done, usage := ev.await(t, 3, 2)
	msgs := costedReplies(h.finish())

	require.Len(t, done, 3)
	assert.Equal(t, types.FinishReasonInterrupted, done[0].FinishReason)
	assert.Equal(t, types.FinishReasonInterrupted, done[1].FinishReason)
	require.Len(t, usage, 2)
	assertUsage(t, usage[0], done[0].CallID, 0, 6)
	assertUsage(t, usage[1], done[1].CallID, 0, 9)
	assertCall(t, done[2], 30, 2)
	require.Len(t, msgs, 3)
	assertCostInfo(t, usd(30, 17), msgs[2].CostInfo)
}

// The interrupted call's completion can arrive after the caller's next turn
// began: it is still the interrupted call's, not the new call's.
func TestDuplexCost_InterruptedCompletionAfterTheNextTurnBegins(t *testing.T) {
	stop := "stop"
	h, ev := costHarness(t, 0)
	h.emit(gen(1, []providers.StreamChunk{{Content: "Once upon", Delta: "Once upon"}, {Interrupted: true}}))
	ev.await(t, 1, 0) // the interrupted call has completed
	user := types.Message{Role: roleUser, Content: "stop, tell me about Rome"}
	h.input <- StreamElement{Message: &user}
	h.awaitNewToolTurn(nil)
	h.emit(gen(1, []providers.StreamChunk{{FinishReason: &stop, CostInfo: usd(50, 3)}}))
	h.emit(gen(2, textTurn("Rome is hazy", usd(30, 2))))
	done, usage := ev.await(t, 2, 1)
	h.finish()

	require.Len(t, done, 2, "no phantom call")
	assert.Equal(t, types.FinishReasonInterrupted, done[0].FinishReason)
	assertCall(t, done[1], 30, 2)
	require.Len(t, usage, 1)
	assertUsage(t, usage[0], done[0].CallID, 50, 3)
}

// With the host running the tools (no registry), a Gemini 3 two-tool chain's
// messages carry every usage but what is reported as the session ends with
// no message after it.
func TestDuplexCost_HostRunToolChainKeepsEveryCost(t *testing.T) {
	h := startRealtime(t, realtimeOpts{noToolSet: true})
	h.emit(gen(1, toolCallTurn(weatherCall("c1"))))
	h.emit(gen(1, []providers.StreamChunk{{CostInfo: usd(919, 12)}}))
	h.emit(gen(2, toolCallTurn(weatherCall("c2"))))
	h.emit(gen(2, []providers.StreamChunk{{CostInfo: usd(952, 13)}}))
	h.emit(gen(3, textTurn("Rome and Oslo are hazy", usd(986, 129))))
	h.emit(gen(3, []providers.StreamChunk{{CostInfo: usd(7, 1)}})) // session-end remainder
	msgs := costedReplies(h.finish())

	require.Len(t, msgs, 3)
	assert.Nil(t, msgs[0].CostInfo)
	assertCostInfo(t, usd(919, 12), msgs[1].CostInfo)
	assertCostInfo(t, usd(952+986, 13+129), msgs[2].CostInfo)
	assertCostInfo(t, usd(919+952+986, 12+13+129), sumCosts(msgs))
}

func TestDuplexCost_UsageOnlyChunkIsChargedWhenItArrives(t *testing.T) {
	h, _ := costHarness(t, 0.01)
	h.emit([]providers.StreamChunk{{CostInfo: usd(15, 5)}})
	elems := drainUntilClosed(t, h.output)
	require.NoError(t, <-h.done)

	assert.True(t, h.sess().isClosed(), "the budget is spent: the session is closed")
	errs := streamErrors(elems)
	require.Len(t, errs, 1)
	assert.Equal(t, "provider stage: cost budget exceeded (0.0200 > 0.0100 USD)", errs[0].Error())
	assert.Empty(t, costedReplies(elems), "no message without content")
}

// A late cost that exceeds the budget refuses on the call it belongs to.
func TestDuplexCost_LateCostRefusesOnItsCall(t *testing.T) {
	h, ev := costHarness(t, 0.01)
	h.emit(gen(1, toolCallTurn(weatherCall("c1"))))
	h.awaitSent(1)
	h.emit(gen(1, []providers.StreamChunk{{CostInfo: usd(15, 5)}}))
	elems := drainUntilClosed(t, h.output)
	require.NoError(t, <-h.done)

	errs := streamErrors(elems)
	require.Len(t, errs, 1)
	assert.Equal(t, "provider stage: cost budget exceeded (0.0200 > 0.0100 USD)", errs[0].Error())
	done, usage := ev.await(t, 1, 1)
	assertUsage(t, usage[0], done[0].CallID, 15, 5)
}

// A turn's end that carries a cost but nothing to build a message from is
// charged.
func TestDuplexCost_TurnEndWithoutAMessageIsCharged(t *testing.T) {
	h, _ := costHarness(t, 0.01)
	stop := "stop"
	h.emit([]providers.StreamChunk{{FinishReason: &stop, CostInfo: &types.CostInfo{TotalCost: 0.02}}})
	elems := drainUntilClosed(t, h.output)
	require.NoError(t, <-h.done)

	errs := streamErrors(elems)
	require.Len(t, errs, 1)
	assert.Equal(t, "provider stage: cost budget exceeded (0.0200 > 0.0100 USD)", errs[0].Error())
}

// A barge-in's partial reply carries what the turn cost so far.
func TestDuplexCost_InterruptedPartialCarriesItsCost(t *testing.T) {
	h, ev := costHarness(t, 0)
	h.emit(gen(1, []providers.StreamChunk{
		{Content: "Once upon", Delta: "Once upon"},
		{Interrupted: true, CostInfo: usd(80, 4)},
	}))
	done, _ := ev.await(t, 1, 0)
	msgs := costedReplies(h.finish())

	assertCall(t, done[0], 80, 4)
	require.Len(t, msgs, 1)
	assertCostInfo(t, usd(80, 4), msgs[0].CostInfo)
}

// A call open when the session ends, with no reply, still completes.
func TestDuplexCost_OpenCallCompletesOnClose(t *testing.T) {
	h, ev := costHarness(t, 0)
	user := types.Message{Role: roleUser, Content: "hello?"}
	h.input <- StreamElement{Message: &user}
	h.awaitNewToolTurn(nil)
	h.finish()

	done, _ := ev.await(t, 1, 0)
	require.Len(t, done, 1)
	assert.Equal(t, types.FinishReasonInterrupted, done[0].FinishReason)
}

// At close the carried cost goes on the message emitted last: a blocked turn
// flushed after the partial reply.
func TestDuplexCost_CarriedCostGoesOnTheBlockedTurnFlushedAtClose(t *testing.T) {
	reg := hooks.NewRegistry(hooks.WithProviderHook(&enforceBeforeCallHook{replacement: "Not here."}))
	s, sess, _ := duplexStageWithHooks(t, reg)
	s.accumulatedText.WriteString("half a reply")
	text := "blocked input"
	require.False(t, s.handleInputElement(context.Background(), &StreamElement{Text: &text},
		make(chan StreamElement, 4), make(chan error, 1)))

	out := make(chan StreamElement, 16)
	loopDone := make(chan error, 1)
	go func() { loopDone <- s.forwardResponseElements(context.Background(), out) }()
	sess.respCh <- providers.StreamChunk{CostInfo: usd(5, 1)}
	close(sess.respCh)
	require.NoError(t, <-loopDone)
	close(out)

	var got []string
	for e := range out {
		if e.EndOfStream && e.Message != nil && e.Message.Role == roleAssistant {
			cost := "none"
			if e.Message.CostInfo != nil {
				cost = fmt.Sprintf("%d/%d", e.Message.CostInfo.InputTokens, e.Message.CostInfo.OutputTokens)
			}
			got = append(got, e.Message.Content+"="+cost)
		}
	}
	assert.Equal(t, []string{"half a reply=none", "Not here.=5/1"}, got)
}

// A turn's end with a cost and nothing to build a message from completes its
// call with that cost (marked as producing no message) and ends the tool
// turn; the cost goes on the next message.
func TestDuplexCost_TurnEndWithoutAMessageCompletesItsCall(t *testing.T) {
	h, ev := costHarness(t, 0)
	user := types.Message{Role: roleUser, Content: "hello?"}
	h.input <- StreamElement{Message: &user}
	h.awaitNewToolTurn(nil)
	stop := "stop"
	h.emit([]providers.StreamChunk{{FinishReason: &stop, CostInfo: &types.CostInfo{TotalCost: 0.02}, Generation: 1}})
	h.awaitToolTurnEnded()
	h.emit(gen(2, textTurn("hello", usd(20, 5))))
	ev.await(t, 2, 0)
	msgs := costedReplies(h.finish())

	done, usage := ev.await(t, 2, 0)
	require.Len(t, done, 2)
	assert.Empty(t, usage)
	assert.InDelta(t, 0.02, done[0].Cost, 1e-12)
	assert.True(t, done[0].NoMessage)
	assert.Equal(t, "stop", done[0].FinishReason)
	assertCall(t, done[1], 20, 5)
	assert.False(t, done[1].NoMessage)
	require.Len(t, msgs, 1)
	assert.InDelta(t, 0.045, msgs[0].CostInfo.TotalCost, 1e-12, "the next message carries it")
}

// An interruption that is a generation's first chunk gets a call of its own
// (every generation does), completed with no message; the reply after it is
// another call.
func TestDuplexCost_InterruptionAsAGenerationsFirstChunk(t *testing.T) {
	h, ev := costHarness(t, 0)
	h.emit([]providers.StreamChunk{{Interrupted: true, Generation: 1}})
	h.emit(gen(2, textTurn("g2|", usd(20, 5))))
	ev.await(t, 2, 0)
	h.finish()

	done, usage := ev.await(t, 2, 0)
	require.Len(t, done, 2)
	assert.Empty(t, usage)
	assert.Equal(t, types.FinishReasonInterrupted, done[0].FinishReason)
	assert.True(t, done[0].NoMessage)
	assertCall(t, done[1], 20, 5)
	assert.False(t, done[1].NoMessage)
}

// Usage for a generation too old to remember is a session-level usage event
// (no CallID); never added to a call it does not belong to.
func TestDuplexCost_ForgottenGenerationIsSessionLevel(t *testing.T) {
	h, ev := costHarness(t, 0)
	n := maxTrackedGenerations + 1
	for g := int64(1); g <= int64(n); g++ {
		h.emit(gen(g, textTurn("hi", usd(1, 0))))
	}
	ev.await(t, n, 0)
	h.emit(gen(1, []providers.StreamChunk{{CostInfo: usd(5, 0)}}))
	h.emit(gen(2, []providers.StreamChunk{{CostInfo: usd(7, 0)}}))
	done, _ := ev.await(t, n, 2)
	h.finish()

	_, usage := ev.await(t, n, 2)
	require.Len(t, usage, 2)
	assertUsage(t, usage[0], "", 5, 0)
	assertUsage(t, usage[1], done[1].CallID, 7, 0)
}

// A new generation while the open call is bound to another, with no user turn
// between, completes that call and starts a tool turn of its own: round 1.
func TestDuplexCost_NewGenerationStartsItsOwnToolTurn(t *testing.T) {
	h, ev := costHarness(t, 0)
	h.emit(gen(1, []providers.StreamChunk{{Content: "g1|", Delta: "g1|"}}))
	h.emit(gen(2, textTurn("g2|", usd(20, 5))))
	done, _ := ev.await(t, 2, 0)
	msgs := costedReplies(h.finish())

	ev.mu.Lock()
	started := append([]*events.ProviderCallStartedData(nil), ev.started...)
	ev.mu.Unlock()
	require.Len(t, started, 2)
	assert.Equal(t, []int{1, 1}, []int{started[0].Round, started[1].Round}, "each call starts its own tool turn")
	assert.True(t, done[0].NoMessage)
	assert.Equal(t, types.FinishReasonInterrupted, done[0].FinishReason)
	assertCall(t, done[1], 20, 5)
	require.Len(t, msgs, 1)
	assert.Equal(t, "g2|", msgs[0].Content, "the superseded generation's text is not carried over")
}

// A new generation's first chunk, of any kind (here an interruption), means
// the provider has moved on: the call of the generation before it completes
// as interrupted, with no message, and anything that generation reports
// later is that call's late cost, never another call's message.
func TestDuplexCost_NewGenerationEndsThePreviousCall(t *testing.T) {
	h, ev := costHarness(t, 0)
	h.emit(gen(1, []providers.StreamChunk{{Content: "g1|", Delta: "g1|"}}))
	h.emit([]providers.StreamChunk{{Interrupted: true, Generation: 7}})
	stop := "stop"
	h.emit(gen(1, []providers.StreamChunk{{FinishReason: &stop, CostInfo: usd(10, 1)}}))
	done, usage := ev.await(t, 2, 1)
	msgs := costedReplies(h.finish())

	require.Len(t, done, 2)
	assert.True(t, done[0].NoMessage, "generation 1's call: moved on from")
	assert.Equal(t, types.FinishReasonInterrupted, done[0].FinishReason)
	assert.True(t, done[1].NoMessage, "generation 7's call: interrupted before any output")
	require.Len(t, usage, 1)
	assertUsage(t, usage[0], done[0].CallID, 10, 1)
	assert.Empty(t, msgs, "generation 1's text is in no message")
}

// A provider error ends the turn: its open call completes, with the cost
// reported for it while it was open.
func TestDuplexCost_ErrorCompletesTheOpenCall(t *testing.T) {
	h, ev := costHarness(t, 0)
	h.emit(gen(1, []providers.StreamChunk{{Content: "g1|", Delta: "g1|"}}))
	h.emit(gen(1, []providers.StreamChunk{{CostInfo: usd(4, 2)}}))
	h.emit([]providers.StreamChunk{{Error: errors.New("socket reset")}})
	elems := drainUntilClosed(t, h.output)
	<-h.done

	done, _ := ev.await(t, 1, 0)
	require.Len(t, done, 1)
	assertCall(t, done[0], 4, 2)
	msgs := costedReplies(elems)
	require.Len(t, msgs, 1)
	assert.True(t, msgs[0].IsInterrupted())
	assertCostInfo(t, usd(4, 2), msgs[0].CostInfo)
}
