package duplex

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The provider-call lifecycle (generation binding, stale-chunk filtering,
// cost attribution, provider.call.* events) does not depend on the stage
// running tools: a host that runs them itself, or has none, gets the same
// messages and events.

// noRegistryOpts are the ways a stage is left without tool execution.
func noRegistryOpts() map[string]realtimeOpts {
	return map[string]realtimeOpts{
		"never set": {noToolSet: true},
		"nil registry": {
			policy: &pipeline.ToolPolicy{MaxRounds: 1},
			cfg:    DuplexToolConfig{Labels: map[string]string{"team": "voice"}, Source: "selfplay"},
		},
	}
}

// streamedTexts returns the text elements in elems, in order.
func streamedTexts(elems []core.StreamElement) []string {
	var out []string
	for _, e := range elems {
		if e.Text != nil {
			out = append(out, *e.Text)
		}
	}
	return out
}

// A barge-in cuts gen1 short; its trailing deltas and its interrupted end are
// stale. They are neither streamed nor a message: the conversation is the cut
// reply and the next one, and each call has its started and completed
// events, the stale end's cost a usage event on the interrupted call.
func TestDuplexCallTracker_NoRegistryDropsACutReplysTail(t *testing.T) {
	for name, opts := range noRegistryOpts() {
		t.Run(name, func(t *testing.T) {
			emitter, ev := newCallEvents()
			opts.emitter = emitter
			h := startRealtime(t, opts)
			interrupted := types.FinishReasonInterrupted
			h.emit(gen(1, []providers.StreamChunk{
				{Content: "hello", Delta: "hello"},
				{Interrupted: true},
				{Content: "TAIL", Delta: "TAIL"},
				{FinishReason: &interrupted, CostInfo: usd(0, 6)},
			}))
			h.emit(gen(2, textTurn("second", usd(20, 5))))
			done, usage := ev.await(t, 2, 1)
			elems := h.finish()

			msgs := costedReplies(elems)
			require.Len(t, msgs, 2, "the cut reply and the next; the tail is no message")
			assert.Equal(t, "hello", msgs[0].Content)
			assert.Equal(t, types.FinishReasonInterrupted, msgs[0].FinishReason)
			assert.Equal(t, "second", msgs[1].Content)
			assert.Equal(t, "stop", msgs[1].FinishReason)
			assert.Equal(t, []string{"hello", "second"}, streamedTexts(elems))

			ev.mu.Lock()
			started := append(ev.started[:0:0], ev.started...)
			ev.mu.Unlock()
			require.Len(t, started, 2, "one provider.call.started per call")
			require.Len(t, done, 2, "one provider.call.completed per call")
			assert.Equal(t, started[0].CallID, done[0].CallID)
			assert.Equal(t, started[1].CallID, done[1].CallID)
			assert.NotEqual(t, done[0].CallID, done[1].CallID)
			assert.Equal(t, types.FinishReasonInterrupted, done[0].FinishReason)
			assertCall(t, done[0], 0, 0)
			assert.Equal(t, "stop", done[1].FinishReason)
			assertCall(t, done[1], 20, 5)
			require.Len(t, usage, 1)
			assertUsage(t, usage[0], done[0].CallID, 0, 6)
			// The spend counts both calls' cost, the stale end's too.
			assert.InDelta(t, 0.031, h.spent(), 1e-12)
			// The events carry the labels and source the host configured.
			assert.Equal(t, opts.cfg.Labels, started[0].Labels)
			assert.Equal(t, opts.cfg.Labels, done[1].Labels)
			source := opts.cfg.Source
			if source == "" {
				source = "agent" // the default
			}
			assert.Equal(t, source, done[1].Source)
			assert.Equal(t, source, usage[0].Source)
		})
	}
}

// After a barge-in, the cut generation ending normally ("ok", stop) is still
// the cut call's: dropped, not a second message.
func TestDuplexCallTracker_NoRegistryDropsACutReplysLateEnd(t *testing.T) {
	for name, opts := range noRegistryOpts() {
		t.Run(name, func(t *testing.T) {
			emitter, ev := newCallEvents()
			opts.emitter = emitter
			h := startRealtime(t, opts)
			h.emit(gen(1, []providers.StreamChunk{{Content: "hello", Delta: "hello"}, {Interrupted: true}}))
			h.emit(gen(1, textTurn("ok", nil)))
			done, _ := ev.await(t, 1, 0)
			h.settle()
			elems := h.finish()

			msgs := costedReplies(elems)
			require.Len(t, msgs, 1, "the late end of the cut call is no message")
			assert.Equal(t, "hello", msgs[0].Content)
			assert.Equal(t, types.FinishReasonInterrupted, msgs[0].FinishReason)
			assert.Equal(t, []string{"hello"}, streamedTexts(elems))
			require.Len(t, done, 1)
			assert.Equal(t, types.FinishReasonInterrupted, done[0].FinishReason)
		})
	}
}

// A provider that does not number its generations ends an interrupted turn
// with an empty turnComplete the tracker cannot tie to a call: it is marked
// interrupted_turn_complete and ends no turn. One that numbers them never
// gets here (TestDuplexProviderStage_InterruptionHandling): the call tracker
// drops it as the end of the call the interruption completed.
func TestDuplexProviderStage_UnnumberedInterruptedTurnComplete(t *testing.T) {
	s := NewDuplexProviderStage(nil, nil)
	stop := "stop"
	s.accumulatedText.WriteString("Sure, I'd be ha-")
	cut := s.chunkToElement(&providers.StreamChunk{Interrupted: true})
	require.NotNil(t, cut.Message)
	assert.Equal(t, "Sure, I'd be ha-", cut.Message.Content)

	end := s.chunkToElement(&providers.StreamChunk{FinishReason: &stop})
	assert.True(t, end.Meta.InterruptedTurnComplete)
	assert.False(t, end.EndOfStream)
	assert.Nil(t, end.Message)

	numbered := NewDuplexProviderStage(nil, nil)
	numbered.chunkToElement(&providers.StreamChunk{Interrupted: true, Generation: 1})
	other := numbered.chunkToElement(&providers.StreamChunk{FinishReason: &stop, Generation: 2})
	assert.False(t, other.Meta.InterruptedTurnComplete, "a numbered end is its own call's, not skipped")
	assert.True(t, other.EndOfStream)
}
