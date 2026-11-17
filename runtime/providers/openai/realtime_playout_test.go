package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// The realtime API delivers a reply's audio faster than real time, so most of
// a long reply is still playing after response.done. Speech in that window is
// barge-in: the reply's audio is dropped, the interruption is reported, and
// the model's copy of the reply is cut to what the caller heard.

// playoutClock is a test clock the session reads for its playout estimate.
type playoutClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *playoutClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *playoutClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

// playoutServer is a fake realtime server. It delivers one reply of
// replyAudio (one second per delta), ending it with response.done unless
// speechMidReply, then waits for speak, sends speech_started, and finishes
// with a second reply R2. It records the client events it reads until
// input_audio_buffer.commit, which the test sends once it has read R2.
type playoutServer struct {
	srv    *httptest.Server
	speak  chan struct{}
	client chan []map[string]any
}

func newPlayoutServer(t *testing.T, items []int, speechMidReply bool) *playoutServer {
	t.Helper()
	ps := &playoutServer{speak: make(chan struct{}), client: make(chan []map[string]any, 1)}
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

		write(map[string]any{"type": "response.created", "response": map[string]any{"id": "R1", "status": "in_progress"}})
		for n, seconds := range items {
			for i := 0; i < seconds; i++ {
				write(map[string]any{"type": "response.output_audio.delta", "item_id": playoutItemID(n),
					"response_id": "R1", "content_index": 0, "delta": second})
			}
		}
		done := map[string]any{"type": "response.done", "response": map[string]any{"id": "R1", "status": "completed"}}
		if !speechMidReply {
			write(done)
		}
		<-ps.speak
		write(map[string]any{"type": "input_audio_buffer.speech_started", "item_id": "item_user"})
		if speechMidReply {
			done["response"] = map[string]any{"id": "R1", "status": "cancelled"}
			write(done)
		}
		write(map[string]any{"type": "response.created", "response": map[string]any{"id": "R2", "status": "in_progress"}})
		write(map[string]any{"type": "response.done", "response": map[string]any{"id": "R2", "status": "completed"}})
		<-r.Context().Done()
	}))
	t.Cleanup(ps.srv.Close)
	return ps
}

// playoutItemID names the reply's n-th audio item: item_R1, item_R1b, ...
func playoutItemID(n int) string {
	if n == 0 {
		return "item_R1"
	}
	return "item_R1" + string(rune('a'+n))
}

func (ps *playoutServer) recordClient(conn *websocket.Conn) {
	var got []map[string]any
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var ev map[string]any
		_ = json.Unmarshal(data, &ev)
		if ev["type"] == "input_audio_buffer.commit" {
			ps.client <- got
			return
		}
		got = append(got, ev)
	}
}

// playoutRun is what the caller observed: the chunks before R2 completed,
// whether BargeIn() fired, and the client events the server read.
type playoutRun struct {
	chunks  []providers.StreamChunk
	barged  bool
	clients []map[string]any
}

// runPlayout plays a reply of seconds of audio delivered at t0, then has the
// caller speak at t0+speakAfter.
func runPlayout(t *testing.T, items []int, speakAfter time.Duration, speechMidReply bool) playoutRun {
	t.Helper()
	ps := newPlayoutServer(t, items, speechMidReply)
	seconds := 0
	for _, n := range items {
		seconds += n
	}
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := &playoutClock{t: t0}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	config := DefaultRealtimeSessionConfig()
	session, err := newRealtimeSession(ctx, "test-key", &config,
		realtimeSessionOpts{endpoint: bargeInWSURL(ps.srv), now: clock.now})
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	var run playoutRun
	next := func() providers.StreamChunk {
		select {
		case c, ok := <-session.Response():
			require.True(t, ok, "Response() closed")
			return c
		case <-ctx.Done():
			t.Fatalf("timed out; chunks so far %d", len(run.chunks))
			return providers.StreamChunk{}
		}
	}
	// The first reply's audio has all arrived (and, unless the caller speaks
	// mid-reply, its response.done).
	for audio := 0; audio < seconds; {
		if c := next(); c.MediaData != nil {
			audio++
		}
	}
	if !speechMidReply {
		require.NotNil(t, next().FinishReason)
	}
	clock.set(t0.Add(speakAfter))
	close(ps.speak)
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

func interruptedChunks(chunks []providers.StreamChunk) int {
	n := 0
	for _, c := range chunks {
		if c.Interrupted {
			n++
		}
	}
	return n
}

func truncateEvents(clients []map[string]any) []map[string]any {
	var out []map[string]any
	for _, ev := range clients {
		if ev["type"] == "conversation.item.truncate" {
			delete(ev, "event_id")
			out = append(out, ev)
		}
	}
	return out
}

func TestRealtimePlayout_SpeechWhileReplyStillPlaysBargesIn(t *testing.T) {
	run := runPlayout(t, []int{3}, 1200*time.Millisecond, false)

	assert.True(t, run.barged, "BargeIn() fired")
	assert.Equal(t, 1, interruptedChunks(run.chunks), "the interruption is reported on Response()")
	assert.Equal(t, []map[string]any{{
		"type": "conversation.item.truncate", "item_id": "item_R1",
		"content_index": float64(0), "audio_end_ms": float64(1200),
	}}, truncateEvents(run.clients), "the reply is cut to the 1.2s the caller heard")
}

func TestRealtimePlayout_SpeechAfterReplyFinishedPlayingIsATurnStart(t *testing.T) {
	run := runPlayout(t, []int{3}, 3*time.Second, false)

	assert.False(t, run.barged, "no barge-in once the reply has played out")
	assert.Zero(t, interruptedChunks(run.chunks))
	assert.Empty(t, truncateEvents(run.clients))
}

// Speech during generation was already barge-in; the reply is now also cut
// to what was heard, and its end is reported by the cancelled response.done,
// not an Interrupted chunk.
func TestRealtimePlayout_SpeechDuringGenerationTruncatesTheReply(t *testing.T) {
	run := runPlayout(t, []int{2}, 500*time.Millisecond, true)

	assert.True(t, run.barged)
	assert.Zero(t, interruptedChunks(run.chunks))
	assert.Equal(t, []map[string]any{{
		"type": "conversation.item.truncate", "item_id": "item_R1",
		"content_index": float64(0), "audio_end_ms": float64(500),
	}}, truncateEvents(run.clients))
}

// The caller stops 2 s into a reply of item A (5 s) then item B (1 s): A is
// cut to the 2 s they heard and B, which never played, to nothing.
func TestRealtimePlayout_EachUnheardItemIsTruncated(t *testing.T) {
	run := runPlayout(t, []int{5, 1}, 2*time.Second, false)

	assert.True(t, run.barged)
	assert.Equal(t, []map[string]any{
		{"type": "conversation.item.truncate", "item_id": "item_R1",
			"content_index": float64(0), "audio_end_ms": float64(2000)},
		{"type": "conversation.item.truncate", "item_id": "item_R1b",
			"content_index": float64(0), "audio_end_ms": float64(0)},
	}, truncateEvents(run.clients))
}

// A truncate the server rejects is logged; the turn goes on. Any other server
// error still reaches Response(). A truncate the server acknowledges is
// forgotten.
func TestRealtimePlayout_TruncateAnswers(t *testing.T) {
	s := &RealtimeSession{emitCh: make(chan providers.StreamChunk, 2)}
	s.truncates.sent("evt_7", "item_a")
	s.truncates.sent("evt_9", "item_b")
	s.handleMessage([]byte(`{"type":"error","error":{"type":"invalid_request_error",` +
		`"message":"audio_end_ms is too long","event_id":"evt_7"}}`))
	assert.Empty(t, s.emitCh)
	assert.Equal(t, 1, s.truncates.len())

	s.handleMessage([]byte(`{"type":"conversation.item.truncated","item_id":"item_b","content_index":0,"audio_end_ms":10}`))
	assert.Zero(t, s.truncates.len(), "the acknowledged truncate is forgotten")

	s.handleMessage([]byte(`{"type":"error","error":{"type":"server_error","message":"boom","event_id":"evt_9"}}`))
	require.Len(t, s.emitCh, 1, "an answered truncate's ID no longer swallows errors")
	assert.EqualError(t, (<-s.emitCh).Error, "server error: boom")
}

func TestRealtimePlayout_PendingTruncatesAreBounded(t *testing.T) {
	var tr truncateTracker
	for i := 0; i <= maxPendingTruncates; i++ {
		tr.sent(fmt.Sprintf("evt_%d", i), fmt.Sprintf("item_%d", i))
	}
	assert.Equal(t, maxPendingTruncates, tr.len())
	assert.False(t, tr.rejected("evt_0"), "the oldest was forgotten")
	assert.True(t, tr.rejected(fmt.Sprintf("evt_%d", maxPendingTruncates)))
	assert.False(t, tr.rejected(""))
}

// Two truncates pending for one item: an ack takes the older, and an error
// still names the newer by its event ID.
func TestRealtimePlayout_TwoTruncatesForOneItem(t *testing.T) {
	var tr truncateTracker
	tr.sent("evt_1", "item_a")
	tr.sent("evt_2", "item_a")
	tr.done("item_a")

	assert.Equal(t, 1, tr.len())
	assert.False(t, tr.rejected("evt_1"), "the ack took the older truncate")
	assert.True(t, tr.rejected("evt_2"))
	assert.Zero(t, tr.len())
}

// Every chunk of a response names the response's generation, its
// response.done (the cost) included, even when it arrives after the next
// response began.
func TestRealtimeSession_ChunksCarryTheirResponsesGeneration(t *testing.T) {
	emitCh := make(chan providers.StreamChunk, 16)
	s := &RealtimeSession{emitCh: emitCh, StreamPump: providers.NewStreamPump(context.Background(), emitCh, 16)}
	audio := base64.StdEncoding.EncodeToString(make([]byte, 480))
	for _, ev := range []string{
		`{"type":"response.created","response":{"id":"R1"}}`,
		`{"type":"response.output_audio.delta","response_id":"R1","item_id":"i1","delta":"` + audio + `"}`,
		`{"type":"response.created","response":{"id":"R2"}}`,
		`{"type":"response.done","response":{"id":"R1","status":"cancelled","usage":{"total_tokens":3}}}`,
		`{"type":"response.done","response":{"id":"R2","status":"completed","usage":{"total_tokens":5}}}`,
		// R1's trailing delta, after R2 began and both ended: still R1's.
		`{"type":"response.output_audio_transcript.delta","response_id":"R1","item_id":"i1","delta":"late"}`,
	} {
		s.handleMessage([]byte(ev))
	}
	close(emitCh)
	var got []int64
	for c := range emitCh {
		got = append(got, c.Generation)
	}
	assert.Equal(t, []int64{1, 1, 2, 1}, got,
		"the audio, R1's done and R1's trailing delta are generation 1, R2's done is 2")
}
