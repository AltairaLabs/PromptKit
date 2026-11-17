//go:build integration

package sdk_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// Live: the caller barges in about 15 s into a long OpenAI Realtime reply
// that finished generating. The stored reply keeps its full text and is
// marked interrupted, with heard_ms matching how much of it had played.
//
//	go -C sdk test -tags integration -run 'TestLiveRealtimeHeardReply' -count=1 -v .
func TestLiveRealtimeHeardReply_StoredAsHeard(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	rc := rtpCases()[0]
	speech := rtpSpeech(t, "Wait, stop. I have a question.", rc.inputRate)
	store := statestore.NewMemoryStore()
	conv, err := sdk.OpenDuplex(rtlWritePack(t, rtpPackJSON), "chat",
		sdk.WithProvider(rc.provider()), sdk.WithSkipSchemaValidation(), sdk.WithStateStore(store),
		sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio, ChunkSize: 3200,
				SampleRate: rc.inputRate, Channels: 1, BitDepth: 16, Encoding: rc.encoding},
			Metadata: rc.metadata,
		}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	p := &rtpPlayback{}
	p.watch(t, conv)
	mic := &rtpMic{}
	micCtx, micStop := context.WithCancel(context.Background())
	t.Cleanup(micStop)
	go mic.run(micCtx, conv, rc.inputRate)

	require.NoError(t, conv.SendText(context.Background(), rtpStoryPrompt))
	require.True(t, rtlWaitUntil(90*time.Second, func() bool { _, _, done, _ := p.snapshot(); return done }),
		"the reply never finished generating")
	until, delivered, _, _ := p.snapshot()
	playStart := until.Add(-delivered) // the reply played back to back from its first chunk
	if wait := time.Until(playStart.Add(15 * time.Second)); wait > 0 {
		time.Sleep(wait)
	}
	mic.say(speech)
	require.True(t, rtlWaitUntil(15*time.Second, func() bool { _, _, _, at := p.snapshot(); return !at.IsZero() }),
		"no barge-in")
	_, _, _, at := p.snapshot()
	wantHeard := at.Sub(playStart)
	t.Logf("reply audio %v; barge-in %v into it", delivered, wantHeard)

	var reply *types.Message
	require.True(t, rtlWaitUntil(10*time.Second, func() bool {
		state, err := store.Load(context.Background(), conv.ID())
		if err != nil || state == nil {
			return false
		}
		for i := range state.Messages {
			if state.Messages[i].Role == "assistant" {
				m := state.Messages[i]
				reply = &m
				return true
			}
		}
		return false
	}), "the reply was never stored")
	t.Logf("stored: finish=%q meta=%v len=%d", reply.FinishReason, reply.Meta, len(reply.GetContent()))
	assert.Equal(t, types.FinishReasonInterrupted, string(reply.FinishReason))
	assert.Equal(t, "barge-in-playout", reply.Meta[types.MetaInterruptedCause])
	heardMs, _ := reply.Meta["heard_ms"].(int64)
	audioMs, _ := reply.Meta["audio_ms"].(int64)
	assert.InDelta(t, float64(wantHeard.Milliseconds()), float64(heardMs), 1500)
	assert.InDelta(t, float64(delivered.Milliseconds()), float64(audioMs), 500)
	assert.NotEmpty(t, reply.GetContent(), "the full text is kept")
}
