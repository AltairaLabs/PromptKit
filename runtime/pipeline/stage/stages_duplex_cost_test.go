package stage

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Cost a realtime session reports outside a turn of its own is charged when
// it arrives and lands on a message, so the session's totals include it.

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

func assertCost(t *testing.T, want, got *types.CostInfo) {
	t.Helper()
	require.NotNil(t, got)
	assert.Equal(t, want.InputTokens, got.InputTokens, "input tokens")
	assert.Equal(t, want.OutputTokens, got.OutputTokens, "output tokens")
	assert.InDelta(t, want.TotalCost, got.TotalCost, 1e-12, "total cost")
}

func TestDuplexCost_UsageOnlyChunkLandsOnTheNextReply(t *testing.T) {
	h := startRealtime(t, realtimeOpts{noToolSet: true})
	h.emit([]providers.StreamChunk{{CostInfo: usd(100, 0)}})
	h.emit(textTurn("hello", usd(20, 5)))
	msgs := costedReplies(h.finish())

	require.Len(t, msgs, 1)
	assert.Equal(t, "hello", msgs[0].Content)
	assertCost(t, usd(120, 5), msgs[0].CostInfo)
}

func TestDuplexCost_UsageOnlyChunkIsChargedWhenItArrives(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	h := startRealtime(t, realtimeOpts{
		registry: newWeatherRegistry(t, exec),
		policy:   &pipeline.ToolPolicy{MaxCostUSD: 0.01},
	})
	h.emit([]providers.StreamChunk{{CostInfo: usd(15, 5)}})
	elems := drainUntilClosed(t, h.output)
	require.NoError(t, <-h.done)

	assert.True(t, h.sess().isClosed(), "the budget is spent: the session is closed")
	errs := streamErrors(elems)
	require.Len(t, errs, 1)
	assert.Equal(t, "provider stage: cost budget exceeded (0.0200 > 0.0100 USD)", errs[0].Error())
	msgs := costedReplies(elems)
	require.Len(t, msgs, 1, "the cost is on a message")
	assertCost(t, usd(15, 5), msgs[0].CostInfo)
}

// A barge-in's partial reply carries what the turn cost so far; the cost its
// completion reports afterwards goes on the reply that follows.
func TestDuplexCost_InterruptedTurnCostIsOnMessages(t *testing.T) {
	stop := "stop"
	h := startRealtime(t, realtimeOpts{noToolSet: true})
	h.emit([]providers.StreamChunk{
		{Content: "Once upon", Delta: "Once upon"},
		{Interrupted: true, CostInfo: usd(80, 4)},
		{FinishReason: &stop, CostInfo: usd(0, 6)},
	})
	h.emit(textTurn("Yes?", usd(30, 2)))
	msgs := costedReplies(h.finish())

	require.Len(t, msgs, 2)
	assert.Equal(t, "Once upon", msgs[0].Content)
	assert.True(t, msgs[0].IsInterrupted())
	assertCost(t, usd(80, 4), msgs[0].CostInfo)
	assert.Equal(t, "Yes?", msgs[1].Content)
	assertCost(t, usd(30, 8), msgs[1].CostInfo)
}

// Usage reported as the session ends (Gemini Live flushes usage no turn's end
// took) lands on a cost-only interrupted message, kept out of model context.
func TestDuplexCost_UsageAtSessionEndLandsOnACostOnlyMessage(t *testing.T) {
	h := startRealtime(t, realtimeOpts{noToolSet: true})
	h.emit(textTurn("hello", usd(20, 5)))
	h.emit([]providers.StreamChunk{{CostInfo: usd(600, 12)}})
	msgs := costedReplies(h.finish())

	require.Len(t, msgs, 2)
	assertCost(t, usd(20, 5), msgs[0].CostInfo)
	assert.Empty(t, msgs[1].Content)
	assert.True(t, msgs[1].IsInterrupted())
	assert.Equal(t, "provider closed the session", msgs[1].Meta[types.MetaInterruptedCause])
	assertCost(t, usd(600, 12), msgs[1].CostInfo)
}
