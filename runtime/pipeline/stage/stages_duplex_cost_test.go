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

// Usage reported after its turn ended (as Gemini 3.x reports a tool-call
// turn's) belongs to the call completed last: it is reported on that call.
func TestDuplexCost_LateUsageIsReportedOnTheCallItBelongsTo(t *testing.T) {
	h, ev := costHarness(t, 0)
	h.emit(textTurn("hello", usd(20, 5)))
	h.emit([]providers.StreamChunk{{CostInfo: usd(600, 12)}})
	calls := ev.await(t, 2)
	msgs := costedReplies(h.finish())

	require.Len(t, ev.await(t, 2), 2, "one call, and its late usage")
	assertCall(t, calls[0], 20, 5)
	assertCall(t, calls[1], 600, 12)
	assert.Equal(t, calls[0].CallID, calls[1].CallID, "on the call the usage belongs to")
	require.Len(t, msgs, 1, "nothing is added to the conversation")
	assert.Equal(t, usd(20, 5), msgs[0].CostInfo)
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

// A turn's end that carries cost but nothing to build a message from (no
// tokens counted) is still charged, and completes its call.
func TestDuplexCost_TurnEndWithoutAMessageIsCharged(t *testing.T) {
	h, ev := costHarness(t, 0.01)
	stop := "stop"
	h.emit([]providers.StreamChunk{{FinishReason: &stop, CostInfo: &types.CostInfo{TotalCost: 0.02}}})
	elems := drainUntilClosed(t, h.output)
	require.NoError(t, <-h.done)

	errs := streamErrors(elems)
	require.Len(t, errs, 1)
	assert.Equal(t, "provider stage: cost budget exceeded (0.0200 > 0.0100 USD)", errs[0].Error())
	calls := ev.await(t, 1)
	assert.InDelta(t, 0.02, calls[0].Cost, 1e-12)
	assert.Equal(t, "stop", calls[0].FinishReason)
}

// A barge-in's partial reply carries what the turn cost so far; the cost its
// completion reports afterwards goes on the interrupted call, not on the
// reply that follows.
func TestDuplexCost_InterruptedTurnCost(t *testing.T) {
	stop := "stop"
	h, ev := costHarness(t, 0)
	h.emit([]providers.StreamChunk{
		{Content: "Once upon", Delta: "Once upon"},
		{Interrupted: true, CostInfo: usd(80, 4)},
		{FinishReason: &stop, CostInfo: usd(0, 6)},
	})
	h.emit(textTurn("Yes?", usd(30, 2)))
	calls := ev.await(t, 3)
	msgs := costedReplies(h.finish())

	require.Len(t, ev.await(t, 3), 3, "the interrupted call twice, the reply once")
	require.Len(t, msgs, 2)
	assert.True(t, msgs[0].IsInterrupted())
	assert.Equal(t, usd(80, 4), msgs[0].CostInfo)
	assert.Equal(t, usd(30, 2), msgs[1].CostInfo, "the reply carries only its own cost")
	assertCall(t, calls[0], 80, 4)
	assertCall(t, calls[1], 0, 6)
	assert.Equal(t, calls[0].CallID, calls[1].CallID, "the completion's cost is the interrupted call's")
	assertCall(t, calls[2], 30, 2)
}

// Usage reported as the session ends is charged and reported; it adds no
// message to the conversation.
func TestDuplexCost_UsageAtSessionEndAddsNoMessage(t *testing.T) {
	h, ev := costHarness(t, 0)
	h.emit(textTurn("hello", usd(20, 5)))
	h.emit([]providers.StreamChunk{{CostInfo: usd(600, 12)}})
	msgs := costedReplies(h.finish())

	require.Len(t, msgs, 1)
	assertCall(t, ev.await(t, 2)[1], 600, 12)
}

// A barge-in before the reply produced anything or reported a cost leaves its
// call open: the completion that follows reports it, once.
func TestDuplexCost_EmptyInterruptionLeavesTheCallToItsCompletion(t *testing.T) {
	stop := "stop"
	h, ev := costHarness(t, 0)
	h.emit([]providers.StreamChunk{{Interrupted: true}, {FinishReason: &stop, CostInfo: usd(0, 6)}})
	h.emit(textTurn("Yes?", usd(30, 2)))
	calls := ev.await(t, 2)
	h.finish()

	require.Len(t, ev.await(t, 2), 2)
	assertCall(t, calls[0], 0, 6)
	assertCall(t, calls[1], 30, 2)
}

// A cut-short reply with no cost reported yet still completes its call.
func TestDuplexCost_InterruptedReplyCompletesItsCall(t *testing.T) {
	h, ev := costHarness(t, 0)
	h.emit([]providers.StreamChunk{{Content: "Once", Delta: "Once"}, {Interrupted: true}})
	h.emit(textTurn("Yes?", usd(30, 2)))
	calls := ev.await(t, 2)
	h.finish()

	require.Len(t, ev.await(t, 2), 2)
	assert.Equal(t, types.FinishReasonInterrupted, calls[0].FinishReason)
	assertCall(t, calls[0], 0, 0)
	assertCall(t, calls[1], 30, 2)
}
