package openai

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
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
	close(ps.reply) // the reply arrives only once playout is held

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
		session.ReleaseAudio(&held[i])
	}
	if dropped {
		session.HeldAudioDropped(0)
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
	run.heldInterrupted = session.HeldAudioInterrupted(0)
	if r.dropAfter {
		session.HeldAudioDropped(0)
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
	s.ReleaseAudio(nil)
	s.ReleaseAudio(&providers.StreamChunk{})
	s.ReleaseAudio(&providers.StreamChunk{MediaData: &providers.StreamMediaData{}})
	assert.False(t, s.playoutEstimate().Playing())
	s.ReleaseAudio(&providers.StreamChunk{MediaData: &providers.StreamMediaData{
		Data: make([]byte, 480), SampleRate: 24000, Channels: 1}})
	assert.True(t, s.playoutEstimate().Playing())
}

// newSupersedeServer is a fake realtime server that, once reply is closed,
// delivers a second of reply R1 and completes it, then starts R2 with a
// second of its own audio, completing R2 only once speak is closed.
func newSupersedeServer(t *testing.T) *playoutServer {
	t.Helper()
	ps := newPlayoutServerState()
	second := base64.StdEncoding.EncodeToString(make([]byte, 2*DefaultRealtimeSampleRate))
	ps.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := bargeInUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		write := func(v map[string]any) { _ = conn.WriteJSON(v) }
		write(map[string]any{"type": "session.created", "session": map[string]any{"id": "s", "model": "gpt-realtime"}})
		if _, _, err := conn.ReadMessage(); err != nil { // session.update
			return
		}
		go ps.recordClient(conn)
		<-ps.reply
		for _, id := range []string{"R1", "R2"} {
			write(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress"}})
			write(map[string]any{"type": "response.output_audio.delta", "item_id": "item_" + id,
				"response_id": id, "content_index": 0, "delta": second})
			if id == "R2" {
				<-ps.speak
			}
			write(map[string]any{"type": "response.done", "response": map[string]any{"id": id, "status": "completed"}})
		}
		<-r.Context().Done()
	}))
	t.Cleanup(ps.srv.Close)
	return ps
}

// The pipeline dropping a reply's held audio once the next reply has begun
// cuts the dropped reply's copy at once; the reply in progress is cut only
// once it too is dropped.
func TestRealtimeHeldPlayout_DroppingASupersededReplyCutsItAtOnce(t *testing.T) {
	ps := newSupersedeServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	config := DefaultRealtimeSessionConfig()
	session, err := newRealtimeSession(ctx, "test-key", &config, realtimeSessionOpts{endpoint: bargeInWSURL(ps.srv)})
	require.NoError(t, err)
	defer func() { _ = session.Close() }()
	session.HoldPlayout()
	close(ps.reply) // the replies arrive only once playout is held
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
	audioOf := func(id string) {
		for {
			if c := next(); c.MediaData != nil && c.Metadata[keyResponseID] == id {
				return
			}
		}
	}
	audioOf("R1")
	audioOf("R2") // R2 is in progress
	session.HeldAudioDropped(0)
	require.NoError(t, session.CommitAudioBuffer())
	var clients []map[string]any
	select {
	case clients = <-ps.client:
	case <-ctx.Done():
		t.Fatal("the server never saw the commit")
	}
	close(ps.speak)
	assert.Equal(t, []map[string]any{{"type": "conversation.item.truncate", "item_id": "item_R1",
		"content_index": float64(0), "audio_end_ms": float64(0)}}, truncateEvents(clients),
		"R1 is cut while R2 is in progress, and R2 is not")
}

// Audio of an item already cut to what was heard no longer waits for the
// caller: later speech is a new turn, not a barge-in on it.
func TestRealtimeHeldPlayout_CutItemsWaitForNothing(t *testing.T) {
	s := &RealtimeSession{}
	s.heldItems = []heldItem{{key: heldItemKey{itemID: "a"}, gen: 1, delivered: time.Second, cut: true}}
	assert.False(t, s.interruptHeldAudio())
	assert.False(t, s.HeldAudioInterrupted(1))
	s.heldItems = append(s.heldItems, heldItem{key: heldItemKey{itemID: "b"}, gen: 2, delivered: time.Second})
	assert.True(t, s.markHeldInterrupted())
	assert.False(t, s.HeldAudioInterrupted(1), "only the reply still waiting")
	assert.True(t, s.HeldAudioInterrupted(2))
}

// Once the caller spoke over the held audio, releasing more of it is refused
// and counts nothing as heard.
func TestRealtimeHeldPlayout_ReleaseAfterInterruptionIsRefused(t *testing.T) {
	s := &RealtimeSession{now: func() time.Time { return time.Unix(0, 0) }}
	s.HoldPlayout()
	s.heldInterrupted = map[int64]bool{1: true}
	assert.False(t, s.ReleaseAudio(&providers.StreamChunk{MediaData: &providers.StreamMediaData{
		Data: make([]byte, 480), SampleRate: 24000, Channels: 1}}))
	assert.False(t, s.playoutEstimate().Playing())
}

// Dropping a reply's hold cuts that reply's audio alone: a later reply that
// ended meanwhile, released and heard after, keeps its copy.
func TestRealtimeHeldPlayout_DroppingAReplyCutsOnlyItsOwnAudio(t *testing.T) {
	ps := newSupersedeServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	config := DefaultRealtimeSessionConfig()
	session, err := newRealtimeSession(ctx, "test-key", &config, realtimeSessionOpts{endpoint: bargeInWSURL(ps.srv)})
	require.NoError(t, err)
	defer func() { _ = session.Close() }()
	session.HoldPlayout()
	close(ps.reply) // the replies arrive only once playout is held
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
	gens := map[string]int64{}
	close(ps.speak) // R2 completes too
	for {
		c := next()
		if c.MediaData != nil {
			gens[c.Metadata[keyResponseID].(string)] = c.Generation
		}
		if c.FinishReason != nil && c.Metadata[keyResponseID] == "R2" {
			break
		}
	}
	require.NotEqual(t, gens["R1"], gens["R2"])
	session.HeldAudioDropped(gens["R1"])
	require.NoError(t, session.CommitAudioBuffer())
	var clients []map[string]any
	select {
	case clients = <-ps.client:
	case <-ctx.Done():
		t.Fatal("the server never saw the commit")
	}
	assert.Equal(t, []map[string]any{{"type": "conversation.item.truncate", "item_id": "item_R1",
		"content_index": float64(0), "audio_end_ms": float64(0)}}, truncateEvents(clients), "R2 keeps its copy")
}

// Dropping one reply's hold leaves a newer reply's interruption standing: the
// caller spoke over that one too.
func TestRealtimeHeldPlayout_DroppingAReplyLeavesANewerOnesInterruption(t *testing.T) {
	s := &RealtimeSession{heldInterrupted: map[int64]bool{1: true, 2: true}}
	s.HeldAudioDropped(1)
	assert.False(t, s.HeldAudioInterrupted(1))
	assert.True(t, s.HeldAudioInterrupted(2))
	assert.False(t, s.ReleaseAudio(&providers.StreamChunk{Generation: 2}))
	assert.True(t, s.ReleaseAudio(&providers.StreamChunk{Generation: 1}))
}
