package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// InterruptResponse stops a reply on the caller's behalf (a guardrail blocked
// it), as a barge-in does: a response in progress is cancelled, its audio
// still to come is dropped, and the model's copy of the reply is cut to the
// audio the caller heard.

// interruptServer delivers one reply (R1) of seconds of audio, one second per
// delta, then waits for the client's response.cancel (or, when done, ends R1
// first and waits for any client event). It then sends one more audio delta
// of R1, which the client must drop, rejects the cancel when rejectCancel,
// and ends the reply. It records the client events until
// input_audio_buffer.commit.
type interruptServer struct {
	srv    *httptest.Server
	client chan []map[string]any
}

func newInterruptServer(t *testing.T, seconds int, done, rejectCancel bool) *interruptServer {
	t.Helper()
	is := &interruptServer{client: make(chan []map[string]any, 1)}
	second := base64.StdEncoding.EncodeToString(make([]byte, 2*DefaultRealtimeSampleRate))
	is.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		write(map[string]any{"type": "response.created", "response": map[string]any{"id": "R1", "status": "in_progress"}})
		for i := 0; i < seconds; i++ {
			write(map[string]any{"type": "response.output_audio.delta", "item_id": "item_R1",
				"response_id": "R1", "content_index": 0, "delta": second})
		}
		status := "cancelled"
		if done {
			status = "completed"
			write(map[string]any{"type": "response.done", "response": map[string]any{"id": "R1", "status": status}})
		}
		var got []map[string]any
		first := readClientEvent(conn)
		if first == nil {
			return
		}
		got = append(got, first)
		if !done {
			write(map[string]any{"type": "response.output_audio.delta", "item_id": "item_R1",
				"response_id": "R1", "content_index": 0, "delta": second})
			if rejectCancel {
				write(map[string]any{"type": "error", "error": map[string]any{"type": "invalid_request_error",
					"code": "response_cancel_not_active", "message": "no active response", "event_id": first["event_id"]}})
			}
			write(map[string]any{"type": "response.done", "response": map[string]any{"id": "R1", "status": status}})
		}
		for {
			ev := readClientEvent(conn)
			if ev == nil {
				return
			}
			if ev["type"] == "input_audio_buffer.commit" {
				is.client <- got
				<-r.Context().Done()
				return
			}
			got = append(got, ev)
		}
	}))
	t.Cleanup(is.srv.Close)
	return is
}

func readClientEvent(conn *websocket.Conn) map[string]any {
	_, data, err := conn.ReadMessage()
	if err != nil {
		return nil
	}
	var ev map[string]any
	_ = json.Unmarshal(data, &ev)
	return ev
}

// interruptRun is what the caller saw after interrupting: the chunks up to
// R1's end, and the client events the server read, without their IDs.
type interruptRun struct {
	chunks  []providers.StreamChunk
	clients []map[string]any
}

// runInterrupt plays a reply of seconds of audio delivered at t0 and
// interrupts it at t0+after.
func runInterrupt(t *testing.T, seconds int, after time.Duration, done, rejectCancel bool) interruptRun {
	t.Helper()
	is := newInterruptServer(t, seconds, done, rejectCancel)
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := &playoutClock{t: t0}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	config := DefaultRealtimeSessionConfig()
	session, err := newRealtimeSession(ctx, "test-key", &config,
		realtimeSessionOpts{endpoint: bargeInWSURL(is.srv), now: clock.now})
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

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
	for audio := 0; audio < seconds; {
		if c := next(); c.MediaData != nil {
			audio++
		}
	}
	if done {
		require.NotNil(t, next().FinishReason)
	}
	clock.set(t0.Add(after))
	require.NoError(t, session.InterruptResponse(ctx))

	var run interruptRun
	if !done {
		for {
			c := next()
			run.chunks = append(run.chunks, c)
			if c.FinishReason != nil {
				break
			}
		}
	}
	require.NoError(t, session.CommitAudioBuffer())
	select {
	case run.clients = <-is.client:
	case <-ctx.Done():
		t.Fatal("the server never saw the commit")
	}
	for _, ev := range run.clients {
		delete(ev, "event_id")
	}
	return run
}

// Interrupting a reply still generating cancels it and cuts the model's copy
// to the 700 ms the caller heard; the audio that still arrives is dropped, and
// the cancelled response.done ends the turn as interrupted.
func TestRealtimeInterrupt_CancelsAndTruncatesAReplyInProgress(t *testing.T) {
	run := runInterrupt(t, 2, 700*time.Millisecond, false, false)

	assert.Equal(t, []map[string]any{
		{"type": "response.cancel"},
		{"type": "conversation.item.truncate", "item_id": "item_R1",
			"content_index": float64(0), "audio_end_ms": float64(700)},
	}, run.clients)
	require.Len(t, run.chunks, 1, "the audio after the cancel is dropped; only the turn's end arrives")
	require.NotNil(t, run.chunks[0].FinishReason)
	assert.Equal(t, "interrupted", *run.chunks[0].FinishReason)
	assert.Nil(t, run.chunks[0].Error)
	assert.False(t, run.chunks[0].Interrupted, "the caller reports the interruption, not the session")
}

// A cancel the server rejects (the response ended first) is not a session
// error.
func TestRealtimeInterrupt_RejectedCancelIsNotAnError(t *testing.T) {
	run := runInterrupt(t, 2, 700*time.Millisecond, false, true)

	for _, c := range run.chunks {
		assert.Nil(t, c.Error)
	}
	require.NotEmpty(t, run.chunks)
	require.NotNil(t, run.chunks[len(run.chunks)-1].FinishReason)
}

// A reply that finished generating has nothing to cancel: it is only cut to
// what the caller heard while it played.
func TestRealtimeInterrupt_FinishedReplyIsOnlyTruncated(t *testing.T) {
	run := runInterrupt(t, 3, 1200*time.Millisecond, true, false)

	assert.Equal(t, []map[string]any{
		{"type": "conversation.item.truncate", "item_id": "item_R1",
			"content_index": float64(0), "audio_end_ms": float64(1200)},
	}, run.clients)
}

// A closed session cannot interrupt.
func TestRealtimeInterrupt_ClosedSession(t *testing.T) {
	s := &RealtimeSession{closed: true}
	assert.EqualError(t, s.InterruptResponse(context.Background()), errSessionClosed)
}

var _ providers.ResponseInterrupter = (*RealtimeSession)(nil)

// pendingCancelServer waits for start, then, for a new turn, sends
// speech_started (newTurn "speech") or reads the client's text turn
// (conversation.item.create and response.create, newTurn "text"), then sends
// response.created R1, one audio delta, and response.done
// (cancelled when the client cancels R1 first, else completed). It records
// the client events until input_audio_buffer.commit.
func pendingCancelServer(t *testing.T, start <-chan struct{}, newTurn string) *interruptServer {
	t.Helper()
	is := &interruptServer{client: make(chan []map[string]any, 1)}
	second := base64.StdEncoding.EncodeToString(make([]byte, 2*DefaultRealtimeSampleRate))
	is.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		<-start
		var got []map[string]any
		switch newTurn {
		case "speech":
			write(map[string]any{"type": "input_audio_buffer.speech_started", "item_id": "item_user2"})
		case "text":
			for i := 0; i < 2; i++ {
				ev := readClientEvent(conn)
				if ev == nil {
					return
				}
				got = append(got, ev)
			}
		}
		write(map[string]any{"type": "response.created", "response": map[string]any{"id": "R1", "status": "in_progress"}})
		status := "completed"
		if newTurn == "" {
			ev := readClientEvent(conn)
			if ev == nil {
				return
			}
			got = append(got, ev)
			status = "cancelled"
		}
		write(map[string]any{"type": "response.output_audio.delta", "item_id": "item_R1",
			"response_id": "R1", "content_index": 0, "delta": second})
		write(map[string]any{"type": "response.done", "response": map[string]any{"id": "R1", "status": status}})
		for {
			ev := readClientEvent(conn)
			if ev == nil {
				return
			}
			if ev["type"] == "input_audio_buffer.commit" {
				is.client <- got
				<-r.Context().Done()
				return
			}
			got = append(got, ev)
		}
	}))
	t.Cleanup(is.srv.Close)
	return is
}

// runPendingCancel interrupts before any response exists, then lets the
// server go on. It returns the chunks up to R1's end and the client events.
func runPendingCancel(t *testing.T, newTurn string) interruptRun {
	t.Helper()
	start := make(chan struct{})
	is := pendingCancelServer(t, start, newTurn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	config := DefaultRealtimeSessionConfig()
	session, err := newRealtimeSession(ctx, "test-key", &config, realtimeSessionOpts{endpoint: bargeInWSURL(is.srv)})
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	require.NoError(t, session.InterruptResponse(ctx))
	if newTurn == "text" {
		require.NoError(t, session.SendText(ctx, "hello"))
	}
	close(start)
	var run interruptRun
	for {
		var c providers.StreamChunk
		select {
		case c = <-session.Response():
		case <-ctx.Done():
			t.Fatal("timed out")
		}
		run.chunks = append(run.chunks, c)
		if c.FinishReason != nil {
			break
		}
	}
	require.NoError(t, session.CommitAudioBuffer())
	select {
	case run.clients = <-is.client:
	case <-ctx.Done():
		t.Fatal("the server never saw the commit")
	}
	for _, ev := range run.clients {
		delete(ev, "event_id")
	}
	return run
}

// A reply interrupted before the server created it (the caller's transcript
// blocked it first) is cancelled as soon as it is created, so it is neither
// generated nor billed, and none of its audio reaches the caller.
func TestRealtimeInterrupt_PendingCancelStopsTheNextResponse(t *testing.T) {
	run := runPendingCancel(t, "")

	assert.Equal(t, []map[string]any{{"type": "response.cancel"}}, run.clients)
	require.Len(t, run.chunks, 1, "the cancelled reply's audio is dropped")
	assert.Equal(t, "interrupted", *run.chunks[0].FinishReason)
}

// A pending cancel belongs to the turn it was made in: once the caller starts
// a new turn, the next response is that turn's and is not cancelled.
func TestRealtimeInterrupt_PendingCancelEndsWithTheTurn(t *testing.T) {
	for _, newTurn := range []string{"speech", "text"} {
		t.Run(newTurn, func(t *testing.T) {
			run := runPendingCancel(t, newTurn)

			for _, ev := range run.clients {
				assert.NotEqual(t, "response.cancel", ev["type"], "no cancel for the new turn's response")
			}
			assertNewTurnReplyPlays(t, run)
		})
	}
}

func assertNewTurnReplyPlays(t *testing.T, run interruptRun) {
	t.Helper()
	var audio int
	for _, c := range run.chunks {
		if c.MediaData != nil {
			audio++
		}
	}
	assert.Equal(t, 1, audio, "the new turn's reply plays")
	assert.Equal(t, "stop", *run.chunks[len(run.chunks)-1].FinishReason)
}
