package duplex

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A provider can end a call before it reports the call's usage (Gemini Live
// reports a tool-call turn's usage, and an interrupted reply's, on the
// turnComplete after). For a provider that declares it
// (providers.LateUsageReporter), the call's provider.call.completed waits a
// short while for that usage, so it carries the call's whole cost; nothing
// else waits. Any other provider's completions go out at once.

// lateUsageToolProvider is the harness's provider declaring that it reports
// usage after a call ends.
type lateUsageToolProvider struct {
	*realtimeToolProvider
}

func (*lateUsageToolProvider) ReportsUsageAfterCallEnds() bool { return true }

// costHarnessWaiting is costHarness on a provider that reports usage after a
// call ends, with a completion waiting wait for its call's usage.
func costHarnessWaiting(t *testing.T, wait time.Duration) (*realtimeHarness, *callEvents) {
	t.Helper()
	emitter, ev := newCallEvents()
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	h := startRealtime(t, realtimeOpts{
		registry: newWeatherRegistry(t, exec), policy: &pipeline.ToolPolicy{}, emitter: emitter,
		lateUsage: true, usageWait: wait,
	})
	return h, ev
}

// completedCount is how many provider.call.completed events are out.
func (c *callEvents) completedCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.done)
}

// awaitInterruption reads the stage's output until an interruption is
// forwarded, and returns what it read.
func (h *realtimeHarness) awaitInterruption() []core.StreamElement {
	h.t.Helper()
	var read []core.StreamElement
	deadline := time.After(2 * time.Second)
	for {
		select {
		case e, ok := <-h.output:
			require.True(h.t, ok, "output closed before the interruption was forwarded")
			read = append(read, e)
			if e.Meta.Interrupted {
				return read
			}
		case <-deadline:
			require.FailNow(h.t, "the interruption was never forwarded")
		}
	}
}

// A tool-call turn's usage arriving after its tool calls: the tool runs at
// once (its response is sent before the usage arrives, with the wait far
// longer than the test), and the call's completion carries the usage. No
// provider.call.usage is emitted for it.
func TestDuplexCost_CompletionWaitsForTheToolCallTurnsUsage(t *testing.T) {
	h, ev := costHarnessWaiting(t, time.Minute)
	h.emit(gen(1, toolCallTurn(weatherCall("c1"))))
	h.awaitSent(1) // the tool ran without waiting for the usage
	assert.Equal(t, 0, ev.completedCount(), "the completion waits for the call's usage")
	h.emit(gen(1, []providers.StreamChunk{{CostInfo: usd(2937, 6)}}))
	done, _ := ev.await(t, 1, 0)
	assert.Equal(t, 1, done[0].ToolCallCount)
	assertCall(t, done[0], 2937, 6)

	h.emit(gen(2, textTurn("It is 21 degrees", usd(20, 5))))
	done, usage := ev.await(t, 2, 0)
	msgs := costedReplies(h.finish())

	require.Len(t, done, 2)
	assertCall(t, done[1], 20, 5)
	assert.Empty(t, usage, "no late usage: the completion carried it")
	require.Len(t, msgs, 2)
	assertCostInfo(t, usd(2957, 11), msgs[1].CostInfo)
	assert.InDelta(t, 2.968, h.spent(), 1e-12)
}

// An interruption (a guardrail cut, a barge-in) ends the call before the
// provider's turnComplete reports its usage: the interruption reaches the
// caller at once, and the call's completion carries the usage that follows.
func TestDuplexCost_InterruptedCompletionWaitsForItsUsage(t *testing.T) {
	stop := "stop"
	h, ev := costHarnessWaiting(t, time.Minute)
	h.emit(gen(1, []providers.StreamChunk{{Content: "Once upon", Delta: "Once upon"}, {Interrupted: true}}))
	read := h.awaitInterruption()
	assert.Equal(t, 0, ev.completedCount(), "the completion waits for the call's usage")
	h.emit(gen(1, []providers.StreamChunk{{FinishReason: &stop, CostInfo: usd(50, 3)}}))
	done, _ := ev.await(t, 1, 0)
	h.emit(gen(2, textTurn("Yes?", usd(30, 2))))
	done, usage := ev.await(t, 2, 0)
	elems := append(read, h.finish()...)

	require.Len(t, done, 2)
	assert.Equal(t, "interrupted", done[0].FinishReason)
	assertCall(t, done[0], 50, 3)
	assertCall(t, done[1], 30, 2)
	assert.Empty(t, usage, "no late usage: the completion carried it")
	msgs := costedReplies(elems)
	require.Len(t, msgs, 2)
	assert.True(t, msgs[0].IsInterrupted())
	assertCostInfo(t, usd(80, 5), sumCosts(msgs))
}

// Usage arriving after the wait: the completion goes out at $0 when the wait
// ends, and the usage follows it as a provider.call.usage for the call.
func TestDuplexCost_UsageAfterTheWaitIsLateUsage(t *testing.T) {
	h, ev := costHarnessWaiting(t, 20*time.Millisecond)
	h.emit(gen(1, toolCallTurn(weatherCall("c1"))))
	done, _ := ev.await(t, 1, 0) // the wait ended without the usage
	assertCall(t, done[0], 0, 0)
	h.emit(gen(1, []providers.StreamChunk{{CostInfo: usd(600, 12)}}))
	done, usage := ev.await(t, 1, 1)
	h.finish()

	require.Len(t, done, 1)
	require.Len(t, usage, 1)
	assertUsage(t, usage[0], done[0].CallID, 600, 12)
}

// A completion still waiting for its usage when the session ends goes out
// then, at $0: the session's end does not wait out the deadline.
func TestDuplexCost_CloseFlushesAWaitingCompletion(t *testing.T) {
	h, ev := costHarnessWaiting(t, time.Minute)
	h.emit(gen(1, toolCallTurn(weatherCall("c1"))))
	h.awaitSent(1)
	h.settle()
	require.Equal(t, 0, ev.completedCount(), "the completion waits for the call's usage")
	h.finish()

	done, usage := ev.await(t, 2, 0)
	require.Len(t, done, 2, "the tool-call turn's call and the reply's, open at close")
	assert.Equal(t, 1, done[0].ToolCallCount)
	assertCall(t, done[0], 0, 0)
	assert.Equal(t, "interrupted", done[1].FinishReason)
	assert.Empty(t, usage)
}

// A completion that reports usage goes out at once, even at $0 (a model
// with no price): only a completion that reports nothing waits.
func TestDuplexCost_CompletionWithUsageAndNoPriceDoesNotWait(t *testing.T) {
	for name, cost := range map[string]*types.CostInfo{
		"input tokens":  {InputTokens: 40},
		"output tokens": {OutputTokens: 7},
		"a cost":        {TotalCost: 0.002},
	} {
		t.Run(name, func(t *testing.T) {
			h, ev := costHarnessWaiting(t, time.Minute)
			h.emit(gen(1, textTurn("hi", cost)))
			done, _ := ev.await(t, 1, 0)
			h.finish()

			assert.Equal(t, cost.InputTokens, done[0].InputTokens)
			assert.Equal(t, cost.OutputTokens, done[0].OutputTokens)
			assert.InDelta(t, cost.TotalCost, done[0].Cost, 1e-12)
		})
	}
}

// A provider that does not declare it reports usage after a call ends: its
// completion goes out at once, ahead of the tool's events, even with no cost
// and a long wait configured, and usage after it is a provider.call.usage.
func TestDuplexCost_ProviderWithoutLateUsageDoesNotWait(t *testing.T) {
	bus := events.NewEventBus()
	var mu sync.Mutex
	var order []events.EventType
	var completed []*events.ProviderCallCompletedData
	var usage []*events.ProviderCallUsageData
	bus.SubscribeAll(func(e *events.Event) {
		mu.Lock()
		defer mu.Unlock()
		switch d := e.Data.(type) {
		case *events.ProviderCallCompletedData:
			order = append(order, e.Type)
			completed = append(completed, d)
		case *events.ProviderCallUsageData:
			order = append(order, e.Type)
			usage = append(usage, d)
		case *events.ToolCallEventData:
			order = append(order, e.Type)
		}
	})
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	h := startRealtime(t, realtimeOpts{
		registry: newWeatherRegistry(t, exec), emitter: events.NewEmitter(bus, "run", "sess", "conv"),
		usageWait: time.Minute,
	})
	h.emit(gen(1, toolCallTurn(weatherCall("c1"))))
	h.awaitSent(1)
	h.emit(gen(1, []providers.StreamChunk{{CostInfo: usd(600, 12)}}))
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(usage) == 1
	}, 2*time.Second, time.Millisecond, "the usage was never reported")
	h.finish()

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(order), 4)
	assert.Equal(t, []events.EventType{
		events.EventProviderCallCompleted,
		events.EventToolCallStarted,
		events.EventToolCallCompleted,
	}, order[:3], "the completion did not wait")
	assert.Equal(t, 1, completed[0].ToolCallCount)
	assert.Zero(t, completed[0].Cost)
	assert.Zero(t, completed[0].InputTokens)
	assertUsage(t, usage[0], completed[0].CallID, 600, 12)
}
