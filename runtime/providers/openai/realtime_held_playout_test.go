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

// heldRun is a held-playout run: a reply of seconds of audio, a chunk a
// second, delivered at t0 to a session whose playout is held; its first
// releaseN chunks released at t0+release; with dropped, the rest dropped
// then; the caller speaking at t0+speakAfter (with midReply, while the reply
// is still generating); and with dropAfter, what is held dropped after the
// speech.
type heldRun struct {
	seconds, releaseN            int
	release, speakAfter          time.Duration
	dropped, midReply, dropAfter bool
}

// runHeldPlayout runs r and reports what the caller and the server saw.
func runHeldPlayout(t *testing.T, r heldRun) playoutRun {
	t.Helper()
	seconds, releaseN, release, speakAfter := r.seconds, r.releaseN, r.release, r.speakAfter
	dropped, midReply := r.dropped, r.midReply
	ps := newPlayoutServer(t, []int{seconds}, midReply)
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
	if !midReply {
		require.NotNil(t, next().FinishReason)
	}
	clock.set(t0.Add(release))
	for i := range held[:releaseN] {
		session.AudioReleased(&held[i])
	}
	if dropped {
		session.HeldAudioDropped()
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
	run.heldInterrupted = session.HeldAudioInterrupted()
	if r.dropAfter {
		session.HeldAudioDropped()
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
	run := runHeldPlayout(t, heldRun{seconds: 3, releaseN: 3, release: 2 * time.Second, speakAfter: 2500 * time.Millisecond, dropped: true})

	assert.True(t, run.barged, "the released reply still plays: barge-in")
	assert.Equal(t, 1, interruptedChunks(run.chunks))
	assert.Equal(t, []map[string]any{{
		"type": "conversation.item.truncate", "item_id": "item_R1",
		"content_index": float64(0), "audio_end_ms": float64(500),
	}}, truncateEvents(run.clients), "cut to the 0.5 s heard since release")
}

// Audio released in full and played out waits for nothing: the caller
// speaking after it is a new turn.
func TestRealtimeHeldPlayout_ReleasedAudioThatPlayedOutWaitsForNothing(t *testing.T) {
	run := runHeldPlayout(t, heldRun{seconds: 3, releaseN: 3, release: 0, speakAfter: 5 * time.Second})

	assert.False(t, run.barged)
	assert.False(t, run.heldInterrupted)
	assert.Empty(t, truncateEvents(run.clients))
}

// truncateTo is the conversation.item.truncate event cutting the model's
// copy of the test server's reply to ms.
func truncateTo(ms float64) map[string]any {
	return map[string]any{
		"type": "conversation.item.truncate", "item_id": "item_R1",
		"content_index": float64(0), "audio_end_ms": ms,
	}
}

// Audio held and dropped (a guardrail failed it) was never heard: the model's
// copy of the reply is cut to nothing, and the caller speaking afterwards is
// a new turn, not a barge-in.
func TestRealtimeHeldPlayout_DroppedAudioIsCutFromTheModel(t *testing.T) {
	run := runHeldPlayout(t, heldRun{seconds: 3, releaseN: 0, release: 0, speakAfter: time.Second, dropped: true})

	assert.False(t, run.barged)
	assert.False(t, run.heldInterrupted)
	assert.Zero(t, interruptedChunks(run.chunks))
	assert.Equal(t, []map[string]any{truncateTo(0)}, truncateEvents(run.clients))
}

// A reply dropped after a second of it was released, once that second had
// played, is cut to the second heard.
func TestRealtimeHeldPlayout_PartlyReleasedThenDroppedIsCutToWhatWasReleased(t *testing.T) {
	run := runHeldPlayout(t, heldRun{seconds: 3, releaseN: 1, release: 0, speakAfter: 5 * time.Second, dropped: true})

	assert.False(t, run.barged)
	assert.Equal(t, []map[string]any{truncateTo(1000)}, truncateEvents(run.clients))
}

// A caller who barges in on the released part of a reply while its rest is
// held is cut to what they heard of the released part, once.
func TestRealtimeHeldPlayout_BargeInOnTheReleasedPartCutsToWhatWasHeard(t *testing.T) {
	run := runHeldPlayout(t, heldRun{seconds: 3, releaseN: 1, release: 0, speakAfter: 500 * time.Millisecond})

	assert.True(t, run.barged)
	assert.Equal(t, 1, interruptedChunks(run.chunks))
	assert.Equal(t, []map[string]any{truncateTo(500)}, truncateEvents(run.clients))
}

// Audio still held when the caller starts speaking (the reply finished
// generating and its judge runs) is waiting for them: their speech is a
// barge-in on it. The session reports the interruption, so the pipeline
// releases none of it, and cuts the model's copy to the nothing heard.
func TestRealtimeHeldPlayout_SpeakingOverHeldAudioBargesIn(t *testing.T) {
	run := runHeldPlayout(t, heldRun{seconds: 3, releaseN: 0, release: 0, speakAfter: time.Second})

	assert.True(t, run.barged)
	assert.True(t, run.heldInterrupted)
	assert.Equal(t, 1, interruptedChunks(run.chunks))
	assert.Equal(t, []map[string]any{truncateTo(0)}, truncateEvents(run.clients), "nothing of the reply was heard")
}

// Speech while a held reply is still generating is a barge-in on it: the
// session reports the held audio interrupted, so none of it is released, and
// cuts the model's copy to the nothing heard.
func TestRealtimeHeldPlayout_SpeakingOverAHeldReplyStillGenerating(t *testing.T) {
	run := runHeldPlayout(t, heldRun{seconds: 3, releaseN: 0, release: 0, speakAfter: time.Second, midReply: true})

	assert.True(t, run.barged)
	assert.True(t, run.heldInterrupted)
	assert.Equal(t, []map[string]any{truncateTo(0)}, truncateEvents(run.clients))
}

// A caller who barges in on a reply released in full interrupts no held
// audio: none was waiting.
func TestRealtimeHeldPlayout_BargeInOnAFullyReleasedReplyInterruptsNothingHeld(t *testing.T) {
	run := runHeldPlayout(t, heldRun{seconds: 3, releaseN: 3, speakAfter: 500 * time.Millisecond})

	assert.True(t, run.barged)
	assert.False(t, run.heldInterrupted)
	assert.Equal(t, []map[string]any{truncateTo(500)}, truncateEvents(run.clients))
}

// The pipeline dropping the held rest of a reply the caller barged in on
// cuts it no further: the barge-in already cut it to what was heard.
func TestRealtimeHeldPlayout_DroppingAfterABargeInCutsOnce(t *testing.T) {
	run := runHeldPlayout(t, heldRun{seconds: 3, releaseN: 1, speakAfter: 500 * time.Millisecond, dropAfter: true})

	assert.Equal(t, []map[string]any{truncateTo(500)}, truncateEvents(run.clients))
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
