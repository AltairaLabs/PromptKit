package openai

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// This file verifies the barge-in path of the realtime session, the fix for
// theory T1 from the voice barge-in investigation.
//
// T1 (the bug): the session's receiveLoop was single-threaded and every handler
// sent directly to the bounded responseCh. When the downstream consumer is slow
// — as the real-time audio pacing stage always is — the back-pressure reached
// the socket, so a LATER control event (input_audio_buffer.speech_started, how
// server-side VAD signals barge-in) was not seen until the buffered audio ahead
// of it drained — hundreds of ms, observed at 317ms in this harness.
//
// The fix: receiveLoop hands chunks to a pump goroutine (decoupling socket
// reads from the paced consumer), and speech_started fires an OUT-OF-BAND
// BargeIn() notification that bypasses the responseCh FIFO entirely.
//
// The test drives the REAL RealtimeSession against a local fake WebSocket server
// (no network, no API key): it bursts a buffered audio response, then
// speech_started, and asserts the BargeIn() signal arrives promptly EVEN WHEN
// the Response() consumer is slow.

var bargeInUpgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool { return true },
}

const (
	// bargeInNumDeltas is how many audio chunks the fake server bursts ahead of
	// the barge-in signal — enough to fill both bounded buffers
	// (responseChannelSize each) and leave a backlog the slow consumer must
	// drain before speech_started can be processed.
	bargeInNumDeltas = 40
	// bargeInConsumerDelay paces the slow consumer, modeling real-time audio
	// playback. 40 chunks * 10ms => ~400ms backlog drain.
	bargeInConsumerDelay = 10 * time.Millisecond
	// bargeInDeltaBytes is the raw PCM size per audio delta. Kept small so all
	// 40 deltas + speech_started fit in the socket buffers and the server's
	// writes never block — speechSentAt then reflects ~t0, isolating the
	// client-side queue stall.
	bargeInDeltaBytes = 320
)

// newBargeInServer returns a fake OpenAI Realtime WebSocket server that, after
// the handshake, bursts numDeltas audio deltas followed by a single
// input_audio_buffer.speech_started, reporting the send time on speechSentCh.
func newBargeInServer(t *testing.T, numDeltas int, speechSentCh chan<- time.Time) *httptest.Server {
	t.Helper()
	audioB64 := base64.StdEncoding.EncodeToString(make([]byte, bargeInDeltaBytes))

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := bargeInUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// 1. session.created — consumed by the session's waitForSessionCreated
		//    before receiveLoop starts.
		if err := conn.WriteJSON(map[string]any{
			"type":    "session.created",
			"session": map[string]any{"id": "sess_test", "model": "gpt-realtime"},
		}); err != nil {
			return
		}

		// 2. Drain the client's session.update so its Send never blocks.
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}

		// 2a. A response is now in progress — required for the session to treat a
		// later speech_started as barge-in rather than a normal turn start.
		if err := conn.WriteJSON(map[string]any{
			"type":     "response.created",
			"response": map[string]any{"id": "resp_1", "status": "in_progress"},
		}); err != nil {
			return
		}

		// 3. Burst a buffered audio response: N deltas back-to-back, unpaced.
		for i := 0; i < numDeltas; i++ {
			if err := conn.WriteJSON(map[string]any{
				"type":          "response.audio.delta",
				"item_id":       "item_resp",
				"response_id":   "resp_1",
				"content_index": 0,
				"delta":         audioB64,
			}); err != nil {
				return
			}
		}

		// 4. Server-side VAD barge-in, immediately after the audio.
		sentAt := time.Now()
		if err := conn.WriteJSON(map[string]any{
			"type":    "input_audio_buffer.speech_started",
			"item_id": "item_user",
		}); err != nil {
			return
		}
		select {
		case speechSentCh <- sentAt:
		default:
		}

		// 5. Keep the connection open until the client closes it.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
}

func bargeInWSURL(s *httptest.Server) string {
	return "ws" + strings.TrimPrefix(s.URL, "http")
}

// runBargeIn opens a real RealtimeSession against the fake server, drains
// Response() at consumerDelay per chunk, and returns the delay between the
// server sending speech_started and the out-of-band BargeIn() signal firing.
func runBargeIn(t *testing.T, consumerDelay time.Duration) time.Duration {
	t.Helper()

	speechSentCh := make(chan time.Time, 1)
	srv := newBargeInServer(t, bargeInNumDeltas, speechSentCh)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	config := DefaultRealtimeSessionConfig()
	session, err := newRealtimeSession(ctx, "test-key", &config, realtimeSessionOpts{
		endpoint: bargeInWSURL(srv),
	})
	if err != nil {
		t.Fatalf("newRealtimeSession: %v", err)
	}
	defer func() { _ = session.Close() }()

	// Slow consumer: drains Response() at consumerDelay per chunk, modeling the
	// real-time audio pacing stage. This back-pressures Response() but must NOT
	// delay the out-of-band BargeIn() signal.
	go func() {
		for {
			select {
			case _, ok := <-session.Response():
				if !ok {
					return
				}
				if consumerDelay > 0 {
					time.Sleep(consumerDelay)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	var speechSentAt time.Time
	select {
	case speechSentAt = <-speechSentCh:
	case <-ctx.Done():
		t.Fatal("server never sent speech_started")
	}

	select {
	case <-session.BargeIn():
		return time.Since(speechSentAt)
	case <-ctx.Done():
		t.Fatal("BargeIn() never fired (barge-in stalled behind buffered audio)")
		return 0
	}
}

// maxBargeInLatency bounds how long after the server emits speech_started the
// out-of-band BargeIn() signal may take to fire. The decoupled receive loop
// means this is independent of the (slow, paced) Response() consumer; the
// pre-fix in-band path took ~317ms under the same backlog.
const maxBargeInLatency = 100 * time.Millisecond

// TestRealtimeBargeIn_DropsTrailingAudioAfterBargeIn verifies that after
// barge-in the session stops emitting the interrupted response's audio (the
// still-arriving "trailing" deltas are skipped), and that a NEW response started
// afterward flows normally — i.e. the drop state clears.
func TestRealtimeBargeIn_DropsTrailingAudioAfterBargeIn(t *testing.T) {
	audioB64 := base64.StdEncoding.EncodeToString(make([]byte, bargeInDeltaBytes))
	delta := func(conn *websocket.Conn, respID string, idx int) error {
		return conn.WriteJSON(map[string]any{
			"type": "response.audio.delta", "item_id": "i", "response_id": respID,
			"content_index": idx, "delta": audioB64,
		})
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := bargeInUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		write := func(v any) bool { return conn.WriteJSON(v) == nil }

		if !write(map[string]any{"type": "session.created", "session": map[string]any{"id": "s", "model": "gpt-realtime"}}) {
			return
		}
		if _, _, err := conn.ReadMessage(); err != nil { // session.update
			return
		}
		// R1: starts, streams, then is interrupted mid-stream.
		write(map[string]any{"type": "response.created", "response": map[string]any{"id": "R1", "status": "in_progress"}})
		for i := 0; i < 5; i++ {
			_ = delta(conn, "R1", i)
		}
		write(map[string]any{"type": "input_audio_buffer.speech_started", "item_id": "u"})
		// Trailing R1 audio after barge-in — MUST be dropped. Processed in-order
		// after speech_started in the session's single receive loop, so dropping
		// is already set when these arrive.
		for i := 5; i < 10; i++ {
			_ = delta(conn, "R1", i)
		}
		write(map[string]any{"type": "response.done", "response": map[string]any{"id": "R1", "status": "cancelled"}})
		// R2: the user's new turn response — MUST flow (drop state cleared).
		write(map[string]any{"type": "response.created", "response": map[string]any{"id": "R2", "status": "in_progress"}})
		for i := 100; i < 103; i++ {
			_ = delta(conn, "R2", i)
		}
		write(map[string]any{"type": "response.done", "response": map[string]any{"id": "R2", "status": "completed"}})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	config := DefaultRealtimeSessionConfig()
	session, err := newRealtimeSession(ctx, "test-key", &config, realtimeSessionOpts{endpoint: bargeInWSURL(srv)})
	if err != nil {
		t.Fatalf("newRealtimeSession: %v", err)
	}
	defer func() { _ = session.Close() }()

	var gotIdx []int
	finishes := 0
	for finishes < 2 {
		select {
		case chunk, ok := <-session.Response():
			if !ok {
				t.Fatalf("Response() closed after %d finishes", finishes)
			}
			if chunk.MediaData != nil {
				idx, _ := chunk.Metadata["content_index"].(int)
				gotIdx = append(gotIdx, idx)
			}
			if chunk.FinishReason != nil {
				finishes++
			}
		case <-ctx.Done():
			t.Fatalf("timed out; audio indices so far: %v", gotIdx)
		}
	}

	for _, idx := range gotIdx {
		if idx >= 5 && idx < 10 {
			t.Errorf("trailing interrupted-response audio (content_index=%d) was delivered; should be dropped", idx)
		}
	}
	r2 := 0
	for _, idx := range gotIdx {
		if idx >= 100 {
			r2++
		}
	}
	if r2 != 3 {
		t.Errorf("expected 3 post-barge-in (R2) audio chunks delivered, got %d (all indices: %v)", r2, gotIdx)
	}
	select {
	case <-session.BargeIn():
	default:
		t.Error("BargeIn() did not fire for speech_started during an active response")
	}
}

func TestRealtimeBargeIn_SignalsOutOfBandUnderBackpressure(t *testing.T) {
	delaySlow := runBargeIn(t, bargeInConsumerDelay)
	delayFast := runBargeIn(t, 0)

	t.Logf("BargeIn() latency: slow-consumer=%v fast-consumer=%v (deltas=%d, per-chunk=%v)",
		delaySlow, delayFast, bargeInNumDeltas, bargeInConsumerDelay)

	// The fix: barge-in is surfaced out-of-band, so a slow real-time-paced
	// Response() consumer must NOT delay it. Pre-fix this was ~317ms.
	if delaySlow > maxBargeInLatency {
		t.Errorf("barge-in stalled behind buffered audio: slow-consumer latency %v > %v "+
			"(receive loop is re-coupled to the paced consumer — T1 regression)",
			delaySlow, maxBargeInLatency)
	}
	if delayFast > maxBargeInLatency {
		t.Errorf("barge-in latency with fast consumer %v > %v", delayFast, maxBargeInLatency)
	}
}
