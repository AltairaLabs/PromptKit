package openai

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// responseIDOf is a client event's response_id.
func responseIDOf(ev map[string]any) any { return ev["response_id"] }

// A render whose context ends is canceled at the server by its response ID,
// and what it cost is still returned with the context's error. Nothing of it
// reaches Response(), and the cancel is no session error.
func TestSpeakOutOfBand_ContextEndCancelsTheResponse(t *testing.T) {
	srv := newOOBServer(t, func(s *oobServer) { s.hold = make(chan struct{}) })
	session := openOOBSession(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	sp, err := session.SpeakOutOfBand(ctx, brunoLine)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	srv.awaitType(t, realtimeEventResponseCreate)
	cancelEv := srv.awaitType(t, realtimeEventResponseCancel)
	assert.Equal(t, "OOB1", responseIDOf(cancelEv))
	require.NotNil(t, sp.Cost, "the canceled render's cost is returned")
	assert.Positive(t, sp.Cost.TotalCost)
	// A cancel the server rejects (the response had ended) is no session error.
	srv.push <- map[string]any{"type": "error", "error": map[string]any{"type": "invalid_request_error",
		"code": "response_cancel_not_active", "message": "none", "event_id": cancelEv["event_id"]}}
	assertNoResponseChunk(t, session)
}

// A render whose context ends before the server names its response is
// canceled as soon as it does.
func TestSpeakOutOfBand_ContextEndBeforeCreatedCancelsOnCreated(t *testing.T) {
	srv := newOOBServer(t, func(s *oobServer) {
		s.hold = make(chan struct{})
		s.delayCreated = make(chan struct{})
	})
	session := openOOBSession(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	res := make(chan error, 1)
	go func() {
		_, err := session.SpeakOutOfBand(ctx, brunoLine)
		res <- err
	}()
	srv.awaitType(t, realtimeEventResponseCreate)
	time.Sleep(100 * time.Millisecond) // the context ends while the response is not created
	srv.quiet(t)
	close(srv.delayCreated)
	assert.Equal(t, "OOB1", responseIDOf(srv.awaitType(t, realtimeEventResponseCancel)))
	require.ErrorIs(t, <-res, context.DeadlineExceeded)
	assertNoResponseChunk(t, session)
}

// A server error naming the request after its response was created fails
// the render once; the response's later events still go nowhere.
func TestSpeakOutOfBand_RejectionAfterCreatedEndsTheRenderOnce(t *testing.T) {
	srv := newOOBServer(t, func(s *oobServer) { s.rejectAfterCreated = true })
	session := openOOBSession(t, srv)

	r := awaitSpeech(t, speak(session, brunoLine))
	require.EqualError(t, r.err, "the server rejected the out-of-band response: invalid_value: late")
	srv.awaitType(t, realtimeEventResponseCreate)

	require.NoError(t, session.SendText(context.Background(), "Hello?"))
	srv.awaitType(t, realtimeEventItemCreate)
	srv.awaitType(t, realtimeEventResponseCreate)
	srv.end <- "completed"
	for _, c := range responseChunks(t, session, 1) {
		if id, ok := c.Metadata[keyResponseID].(string); ok {
			assert.Equal(t, "R1", id)
		}
		assert.Equal(t, int64(1), c.Generation)
	}
}

// A session.update the server rejects leaves no voice to speak in: renders
// fail at once instead of waiting for a session.updated that is not coming.
func TestSpeakOutOfBand_RejectedSessionUpdateFailsRendersAtOnce(t *testing.T) {
	srv := newOOBServer(t, func(s *oobServer) { s.rejectUpdate = true })
	session := openOOBSession(t, srv)

	start := time.Now()
	r := awaitSpeech(t, speak(session, brunoLine))
	require.EqualError(t, r.err, "the server rejected the session configuration: invalid_value: bad voice")
	assert.Less(t, time.Since(start), time.Second)
	srv.quiet(t)
}

// A session that closes mid-render fails the render as closed, at once.
func TestSpeakOutOfBand_SessionClosingMidRender(t *testing.T) {
	srv := newOOBServer(t, func(s *oobServer) { s.hold = make(chan struct{}) })
	session := openOOBSession(t, srv)

	res := speak(session, brunoLine)
	srv.awaitType(t, realtimeEventResponseCreate)
	require.NoError(t, session.Close())
	r := awaitSpeech(t, res)
	require.ErrorIs(t, r.err, providers.ErrOutOfBandSessionClosed)

	_, err := session.SpeakOutOfBand(context.Background(), brunoLine)
	require.ErrorIs(t, err, providers.ErrOutOfBandSessionClosed)
}

// Out-of-band events interleaved with a reply's: the reply gets exactly its
// own audio, and the render exactly its own.
func TestSpeakOutOfBand_InterleavedWithAReply(t *testing.T) {
	srv := newOOBServer(t, func(s *oobServer) { s.hold = make(chan struct{}) })
	session := openOOBSession(t, srv)

	require.NoError(t, session.SendText(context.Background(), "Hello?"))
	srv.awaitType(t, realtimeEventItemCreate)
	srv.awaitType(t, realtimeEventResponseCreate) // R1 runs
	res := speak(session, brunoLine)
	srv.awaitType(t, realtimeEventResponseCreate) // OOB1 runs, held after its first delta
	srv.push <- map[string]any{"type": "response.output_audio.delta", "response_id": "R1", "item_id": "item_R1",
		"content_index": 0, "delta": base64.StdEncoding.EncodeToString(replyAudio)}
	close(srv.hold)
	r := awaitSpeech(t, res)
	require.NoError(t, r.err)
	assert.Equal(t, oobAudio, r.speech.Audio)
	srv.end <- "completed"

	var got []byte
	for _, c := range responseChunks(t, session, 1) {
		if c.MediaData != nil {
			got = append(got, c.MediaData.Data...)
		}
	}
	assert.Equal(t, append(append([]byte{}, replyAudio...), replyAudio...), got)
}

// Interrupting the reply while a render runs cancels and cuts the reply
// only; the render completes.
func TestSpeakOutOfBand_InterruptingTheReplyLeavesTheRender(t *testing.T) {
	srv := newOOBServer(t, func(s *oobServer) { s.hold = make(chan struct{}) })
	session := openOOBSession(t, srv)

	require.NoError(t, session.SendText(context.Background(), "Hello?"))
	srv.awaitType(t, realtimeEventItemCreate)
	srv.awaitType(t, realtimeEventResponseCreate)
	res := speak(session, brunoLine)
	srv.awaitType(t, realtimeEventResponseCreate)
	awaitReplyAudio(t, session)

	require.NoError(t, session.InterruptResponse(context.Background(), 0))
	assert.Equal(t, "R1", responseIDOf(srv.awaitType(t, realtimeEventResponseCancel)))
	assert.Equal(t, "item_R1", srv.awaitType(t, realtimeEventItemTruncate)["item_id"])
	close(srv.hold)
	r := awaitSpeech(t, res)
	require.NoError(t, r.err)
	assert.Equal(t, oobAudio, r.speech.Audio)
	srv.quiet(t)
}

// The caller barging in while a reply and a render run cuts the reply only.
func TestSpeakOutOfBand_BargeInLeavesTheRender(t *testing.T) {
	srv := newOOBServer(t, func(s *oobServer) { s.hold = make(chan struct{}) })
	session := openOOBSession(t, srv)

	require.NoError(t, session.SendText(context.Background(), "Hello?"))
	srv.awaitType(t, realtimeEventItemCreate)
	srv.awaitType(t, realtimeEventResponseCreate)
	res := speak(session, brunoLine)
	srv.awaitType(t, realtimeEventResponseCreate)
	awaitReplyAudio(t, session)

	srv.push <- map[string]any{"type": "input_audio_buffer.speech_started", "item_id": "user_2"}
	assert.Equal(t, "item_R1", srv.awaitType(t, realtimeEventItemTruncate)["item_id"])
	close(srv.hold)
	require.NoError(t, awaitSpeech(t, res).err)
	srv.quiet(t)
}

// CancelResponse names the reply in progress, never a render; with no
// reply in progress it sends nothing.
func TestCancelResponse_NamesTheReplyNeverARender(t *testing.T) {
	srv := newOOBServer(t, func(s *oobServer) { s.hold = make(chan struct{}) })
	session := openOOBSession(t, srv)

	res := speak(session, brunoLine)
	srv.awaitType(t, realtimeEventResponseCreate)
	waitOOBResponse(t, session, "OOB1")
	require.NoError(t, session.CancelResponse())
	srv.quiet(t) // only a render runs: nothing to cancel

	require.NoError(t, session.SendText(context.Background(), "Hello?"))
	srv.awaitType(t, realtimeEventItemCreate)
	srv.awaitType(t, realtimeEventResponseCreate)
	waitGeneration(t, session, 1)
	require.NoError(t, session.CancelResponse())
	assert.Equal(t, "R1", responseIDOf(srv.awaitType(t, realtimeEventResponseCancel)))
	close(srv.hold)
	require.NoError(t, awaitSpeech(t, res).err)
}

// awaitReplyAudio reads Response() until the reply's audio arrives: it is
// then in the playout estimate, so an interruption cuts it.
func awaitReplyAudio(t *testing.T, session *RealtimeSession) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case c := <-session.Response():
			if c.MediaData != nil {
				return
			}
		case <-deadline:
			t.Fatal("no reply audio")
		}
	}
}

// waitGeneration waits for the session to have numbered gen responses.
func waitGeneration(t *testing.T, session *RealtimeSession, gen int64) {
	t.Helper()
	require.Eventually(t, func() bool {
		session.recvMu.Lock()
		defer session.recvMu.Unlock()
		return session.gen == gen && session.responseActive
	}, 2*time.Second, 5*time.Millisecond)
}

// waitOOBResponse waits for the server's response.created of a render.
func waitOOBResponse(t *testing.T, session *RealtimeSession, id string) {
	t.Helper()
	require.Eventually(t, func() bool {
		session.recvMu.Lock()
		defer session.recvMu.Unlock()
		return session.oob.byResp[id] != nil
	}, 2*time.Second, 5*time.Millisecond)
}
