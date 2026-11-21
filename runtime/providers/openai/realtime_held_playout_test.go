package openai

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A pipeline that holds a reply's audio back until its guardrails judged it
// (providers.PlayoutHolder) has the session count that audio as playing only
// from when it is released: a barge-in cuts the model's copy of the reply to
// what the caller heard since release, and audio never released is never
// counted as heard.

// runHeldPlayout delivers a reply of seconds of audio at t0 to a session
// whose playout is held, releases the audio at t0+release (none when release
// is negative), then has the caller speak at t0+speakAfter.
func runHeldPlayout(t *testing.T, seconds int, release, speakAfter time.Duration) playoutRun {
	t.Helper()
	ps := newPlayoutServer(t, []int{seconds}, false)
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := &playoutClock{t: t0}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	config := DefaultRealtimeSessionConfig()
	session, err := newRealtimeSession(ctx, "test-key", &config,
		realtimeSessionOpts{endpoint: bargeInWSURL(ps.srv), now: clock.now})
	require.NoError(t, err)
	defer func() { _ = session.Close() }()
	session.HoldPlayout()

	next := func() providers.StreamChunk {
		select {
		case c, ok := <-session.Response():
			require.True(t, ok, "Response() closed")
			return c
		case <-ctx.Done():
			t.Fatal("timed out")
			return providers.StreamChunk{}
		}
	}
	var held []providers.StreamChunk
	for len(held) < seconds {
		if c := next(); c.MediaData != nil {
			held = append(held, c)
		}
	}
	require.NotNil(t, next().FinishReason)
	if release >= 0 {
		clock.set(t0.Add(release))
		for i := range held {
			session.AudioReleased(&held[i])
		}
	}
	clock.set(t0.Add(speakAfter))
	close(ps.speak)
	var run playoutRun
	for {
		c := next()
		run.chunks = append(run.chunks, c)
		if c.FinishReason != nil && c.Metadata[keyResponseID] == "R2" {
			break
		}
	}
	select {
	case <-session.BargeIn():
		run.barged = true
	default:
	}
	require.NoError(t, session.CommitAudioBuffer())
	select {
	case run.clients = <-ps.client:
	case <-ctx.Done():
		t.Fatal("the server never saw the commit")
	}
	return run
}

// Released 2 s after it arrived, a 3 s reply the caller interrupts 2.5 s
// after arrival was heard for 0.5 s, not 2.5 s.
func TestRealtimeHeldPlayout_HeardCountsFromRelease(t *testing.T) {
	run := runHeldPlayout(t, 3, 2*time.Second, 2500*time.Millisecond)

	assert.True(t, run.barged, "the released reply still plays: barge-in")
	assert.Equal(t, 1, interruptedChunks(run.chunks))
	assert.Equal(t, []map[string]any{{
		"type": "conversation.item.truncate", "item_id": "item_R1",
		"content_index": float64(0), "audio_end_ms": float64(500),
	}}, truncateEvents(run.clients), "cut to the 0.5 s heard since release")
}

// Audio held and never released (a guardrail dropped it) never played: the
// caller speaking is a new turn, not a barge-in.
func TestRealtimeHeldPlayout_DroppedAudioIsNeverHeard(t *testing.T) {
	run := runHeldPlayout(t, 3, -1, time.Second)

	assert.False(t, run.barged)
	assert.Zero(t, interruptedChunks(run.chunks))
	assert.Empty(t, truncateEvents(run.clients))
}

// A released chunk with no audio adds nothing to what plays.
func TestRealtimeHeldPlayout_ReleasingNoAudioPlaysNothing(t *testing.T) {
	s := &RealtimeSession{now: func() time.Time { return time.Unix(0, 0) }}
	s.HoldPlayout()
	s.AudioReleased(nil)
	s.AudioReleased(&providers.StreamChunk{})
	s.AudioReleased(&providers.StreamChunk{MediaData: &providers.StreamMediaData{}})
	assert.False(t, s.playoutEstimate().Playing())
	s.AudioReleased(&providers.StreamChunk{MediaData: &providers.StreamMediaData{
		Data: make([]byte, 480), SampleRate: 24000, Channels: 1}})
	assert.True(t, s.playoutEstimate().Playing())
}
