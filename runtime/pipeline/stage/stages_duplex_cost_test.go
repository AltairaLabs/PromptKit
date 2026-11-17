package stage

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
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

// callEvents collects provider.call.completed events.
type callEvents struct {
	mu   sync.Mutex
	done []*events.ProviderCallCompletedData
}

func newCallEvents() (*events.Emitter, *callEvents) {
	bus := events.NewEventBus()
	c := &callEvents{}
	bus.SubscribeAll(func(e *events.Event) {
		if d, ok := e.Data.(*events.ProviderCallCompletedData); ok {
			c.mu.Lock()
			c.done = append(c.done, d)
			c.mu.Unlock()
		}
	})
	return events.NewEmitter(bus, "run", "sess", "conv"), c
}

// await returns the first n events once they have arrived.
func (c *callEvents) await(t *testing.T, n int) []*events.ProviderCallCompletedData {
	t.Helper()
	require.Eventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return len(c.done) >= n
	}, 2*time.Second, time.Millisecond, "expected %d provider.call.completed", n)
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*events.ProviderCallCompletedData(nil), c.done...)
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

// Gemini 3 reports a tool-call turn's usage after its tool calls. The tool
// call's provider.call.completed waits for it and is emitted once, with it;
// the conversation's messages carry it on the next reply, so they add up to
// the spend a resumed conversation's budget is rebuilt from.
func TestDuplexCost_LateUsageGoesOnItsCallAndTheNextMessage(t *testing.T) {
	emitter, ev := newCallEvents()
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`), gate: make(chan struct{})}
	h := startRealtime(t, realtimeOpts{registry: newWeatherRegistry(t, exec), emitter: emitter})
	h.emit(toolCallTurn(weatherCall("c1")))
	h.emit([]providers.StreamChunk{{CostInfo: usd(600, 12)}})
	ev.await(t, 1) // the tool call's call, with its usage, before the tool finishes
	close(exec.gate)
	h.awaitSent(1)
	h.emit(textTurn("It is 21 degrees", usd(20, 5)))
	calls := ev.await(t, 2)
	msgs := costedReplies(h.finish())

	require.Len(t, ev.await(t, 2), 2, "one event per call")
	assert.Equal(t, 1, calls[0].ToolCallCount)
	assertCall(t, calls[0], 600, 12)
	assertCall(t, calls[1], 20, 5)
	assert.NotEqual(t, calls[0].CallID, calls[1].CallID)
	require.Len(t, msgs, 2)
	assert.Nil(t, msgs[0].CostInfo)
	assertCostInfo(t, usd(620, 17), msgs[1].CostInfo)
	assert.InDelta(t, h.spent(), sumHistoryCost(derefMessages(msgs)), 1e-12)
	assert.InDelta(t, 0.637, h.spent(), 1e-12)
}

func derefMessages(msgs []*types.Message) []types.Message {
	out := make([]types.Message, len(msgs))
	for i, m := range msgs {
		out[i] = *m
	}
	return out
}

// A call completed without a cost whose cost never comes is reported when
// the next call starts.
func TestDuplexCost_CallWithoutCostIsReportedWhenTheNextStarts(t *testing.T) {
	h, ev := costHarness(t, 0)
	h.emit(toolCallTurn(weatherCall("c1")))
	h.awaitSent(1)
	h.emit(textTurn("It is 21 degrees", usd(20, 5)))
	calls := ev.await(t, 2)
	h.finish()

	require.Len(t, ev.await(t, 2), 2)
	assertCall(t, calls[0], 0, 0)
	assertCall(t, calls[1], 20, 5)
}

// With the host running the tools (no registry), a Gemini 3 two-tool chain's
// messages still carry every usage but what is reported as the session ends
// with no message after it.
func TestDuplexCost_HostRunToolChainKeepsEveryCost(t *testing.T) {
	h := startRealtime(t, realtimeOpts{noToolSet: true})
	h.emit(toolCallTurn(weatherCall("c1")))
	h.emit([]providers.StreamChunk{{CostInfo: usd(919, 12)}})
	h.emit(toolCallTurn(weatherCall("c2")))
	h.emit([]providers.StreamChunk{{CostInfo: usd(952, 13)}})
	h.emit(textTurn("Rome and Oslo are hazy", usd(986, 129)))
	h.emit([]providers.StreamChunk{{CostInfo: usd(7, 1)}}) // session-end remainder
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

// A turn's end that carries a cost but nothing to build a message from is
// still charged.
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

// A barge-in's partial reply carries what the turn cost so far. Its
// completion's cost goes on the interrupted call's event (held until then,
// one event) and on the next message.
func TestDuplexCost_InterruptedTurnCost(t *testing.T) {
	stop := "stop"
	h, ev := costHarness(t, 0)
	h.emit([]providers.StreamChunk{
		{Content: "Once upon", Delta: "Once upon"},
		{Interrupted: true, CostInfo: usd(80, 4)},
		{FinishReason: &stop, CostInfo: usd(0, 6)},
	})
	h.emit(textTurn("Yes?", usd(30, 2)))
	calls := ev.await(t, 2)
	msgs := costedReplies(h.finish())

	require.Len(t, ev.await(t, 2), 2, "the interrupted call once, the reply once")
	assert.Equal(t, types.FinishReasonInterrupted, calls[0].FinishReason)
	assertCall(t, calls[0], 80, 10)
	assertCall(t, calls[1], 30, 2)
	require.Len(t, msgs, 2)
	assertCostInfo(t, usd(80, 4), msgs[0].CostInfo)
	assertCostInfo(t, usd(30, 8), msgs[1].CostInfo)
}

// The interrupted call's completion can arrive after the caller's next turn
// began: its cost is still the interrupted call's, not the new call's.
func TestDuplexCost_InterruptedCompletionAfterTheNextTurnBegins(t *testing.T) {
	stop := "stop"
	h, ev := costHarness(t, 0)
	h.emit([]providers.StreamChunk{{Content: "Once upon", Delta: "Once upon"}, {Interrupted: true}})
	earlier := h.toolTurn()
	user := types.Message{Role: roleUser, Content: "stop, tell me about Rome"}
	h.input <- StreamElement{Message: &user}
	h.awaitNewToolTurn(earlier)
	h.emit([]providers.StreamChunk{{FinishReason: &stop, CostInfo: usd(50, 3)}})
	h.emit(textTurn("Rome is hazy", usd(30, 2)))
	calls := ev.await(t, 2)
	h.finish()

	require.Len(t, ev.await(t, 2), 2, "no phantom call")
	assert.Equal(t, types.FinishReasonInterrupted, calls[0].FinishReason)
	assertCall(t, calls[0], 50, 3)
	assertCall(t, calls[1], 30, 2)
	assert.NotEqual(t, calls[0].CallID, calls[1].CallID)
}

// A barge-in before the reply produced anything or reported a cost: the
// completion that follows reports the interrupted call, once.
func TestDuplexCost_EmptyInterruptionIsReportedOnItsCompletion(t *testing.T) {
	stop := "stop"
	h, ev := costHarness(t, 0)
	h.emit([]providers.StreamChunk{{Delta: "x", Metadata: map[string]interface{}{"type": "output_transcription"}}})
	h.emit([]providers.StreamChunk{{Interrupted: true}, {FinishReason: &stop, CostInfo: usd(0, 6)}})
	h.emit(textTurn("Yes?", usd(30, 2)))
	calls := ev.await(t, 2)
	h.finish()

	require.Len(t, ev.await(t, 2), 2)
	assertCall(t, calls[0], 0, 6)
	assertCall(t, calls[1], 30, 2)
}
