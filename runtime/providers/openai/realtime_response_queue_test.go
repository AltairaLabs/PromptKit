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

// OpenAI Realtime allows one response at a time: a response.create while one
// is in progress is answered with conversation_already_has_active_response.
// Captured live (realtime-showcase text mode): a guardrail cut the reply and
// the session asked for the guardrail's message to be spoken (Announce); the
// caller typed their next line while that was still being spoken, and the
// session's response.create for it was rejected, which ended the session.

// oneResponseServer is a Realtime server that enforces one response at a
// time. Each response.create it accepts starts a response (response.created,
// one audio delta) that ends (response.done) once the test says so on end;
// one it receives while a response is in progress is rejected with the
// server's error. It reports each client event type it read, in order, with
// "rejected" after a rejected create.
type oneResponseServer struct {
	srv    *httptest.Server
	events chan string
	end    chan struct{}
}

func newOneResponseServer(t *testing.T) *oneResponseServer {
	t.Helper()
	return newOneResponseServerWith(t, false)
}

// newOneResponseServerWith is newOneResponseServer; with startOnItem the
// server starts a response of its own on the first item the client adds.
func newOneResponseServerWith(t *testing.T, startOnItem bool) *oneResponseServer {
	t.Helper()
	os := &oneResponseServer{events: make(chan string, 64), end: make(chan struct{}, 8)}
	delta := base64.StdEncoding.EncodeToString(make([]byte, 480))
	os.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		client := make(chan map[string]any, 16)
		go func() {
			defer close(client)
			for {
				ev := readClientEvent(conn)
				if ev == nil {
					return
				}
				client <- ev
			}
		}()
		n, active := 0, ""
		start := func() {
			n++
			active = []string{"", "R1", "R2", "R3"}[min(n, 3)]
			write(map[string]any{"type": "response.created", "response": map[string]any{"id": active, "status": "in_progress"}})
			write(map[string]any{"type": "response.output_audio.delta", "item_id": "item_" + active,
				"response_id": active, "content_index": 0, "delta": delta})
		}
		for {
			select {
			case ev, ok := <-client:
				if !ok {
					return
				}
				typ, _ := ev["type"].(string)
				os.events <- typ
				if typ == realtimeEventItemCreate && startOnItem && n == 0 {
					start() // the server starts a response of its own, as server VAD does
					continue
				}
				if typ != realtimeEventResponseCreate {
					continue
				}
				if active != "" {
					os.events <- "rejected"
					write(map[string]any{"type": "error", "error": map[string]any{
						"type": "invalid_request_error", "code": "conversation_already_has_active_response",
						"message":  "Conversation already has an active response in progress: " + active,
						"event_id": ev["event_id"]}})
					continue
				}
				start()
			case <-os.end:
				if active != "" {
					write(map[string]any{"type": "response.done", "response": map[string]any{"id": active, "status": "completed"}})
					active = ""
				}
			case <-r.Context().Done():
				return
			}
		}
	}))
	t.Cleanup(os.srv.Close)
	return os
}

// await returns the next client event type the server read.
func (os *oneResponseServer) await(t *testing.T) string {
	t.Helper()
	select {
	case e := <-os.events:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("no client event")
		return ""
	}
}

// quiet asserts the server reads no client event for a while.
func (os *oneResponseServer) quiet(t *testing.T) {
	t.Helper()
	select {
	case e := <-os.events:
		t.Fatalf("unexpected client event %q", e)
	case <-time.After(150 * time.Millisecond):
	}
}

// openOneResponseSession opens an audio session on os.
func openOneResponseSession(t *testing.T, os *oneResponseServer) *RealtimeSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	config := DefaultRealtimeSessionConfig()
	session, err := newRealtimeSession(ctx, "test-key", &config, realtimeSessionOpts{endpoint: bargeInWSURL(os.srv)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// turnEnds reads Response() until n responses have ended and returns their
// chunks, failing on an error chunk.
func turnEnds(t *testing.T, session *RealtimeSession, n int) []providers.StreamChunk {
	t.Helper()
	var ends []providers.StreamChunk
	deadline := time.After(3 * time.Second)
	for len(ends) < n {
		select {
		case c, ok := <-session.Response():
			require.True(t, ok, "Response() closed")
			require.NoError(t, c.Error, "the session reported an error")
			if c.FinishReason != nil {
				ends = append(ends, c)
			}
		case <-deadline:
			t.Fatalf("got %d response ends, want %d", len(ends), n)
		}
	}
	return ends
}

// Text typed while a response is in progress (here the guardrail's spoken
// message) is added at once; its response.create waits for that response to
// end, so the server never rejects it, and the reply to the text follows.
func TestRealtimeSendText_WhileAResponseIsInProgressWaitsForItsEnd(t *testing.T) {
	os := newOneResponseServer(t)
	session := openOneResponseSession(t, os)

	announced, err := session.Announce(context.Background(), "Sorry, I can't say that word.")
	require.NoError(t, err)
	require.True(t, announced)
	assert.Equal(t, realtimeEventResponseCreate, os.await(t))

	require.NoError(t, session.SendText(context.Background(), "What is the capital of France?"))
	assert.Equal(t, realtimeEventItemCreate, os.await(t), "the text is added at once")
	os.quiet(t) // no response.create while the spoken message is in progress

	os.end <- struct{}{} // the spoken message ends
	assert.Equal(t, realtimeEventResponseCreate, os.await(t), "the text's response follows the spoken message")
	os.end <- struct{}{}
	ends := turnEnds(t, session, 2)
	assert.Equal(t, "R1", ends[0].Metadata[keyResponseID])
	assert.Equal(t, "R2", ends[1].Metadata[keyResponseID])
}

// A response.create sent but not yet answered with response.created counts
// as in progress: text typed then waits too.
func TestRealtimeSendText_AfterAnUnansweredResponseCreateWaits(t *testing.T) {
	os := newOneResponseServer(t)
	session := openOneResponseSession(t, os)

	_, err := session.Announce(context.Background(), "Sorry, I can't say that word.")
	require.NoError(t, err)
	require.NoError(t, session.SendText(context.Background(), "What is the capital of France?"))
	assert.Equal(t, realtimeEventResponseCreate, os.await(t))
	assert.Equal(t, realtimeEventItemCreate, os.await(t))
	os.quiet(t)

	os.end <- struct{}{}
	assert.Equal(t, realtimeEventResponseCreate, os.await(t))
	os.end <- struct{}{}
	assert.Len(t, turnEnds(t, session, 2), 2)
}

// Tool results sent while a response is in progress wait for it the same way.
func TestRealtimeSendToolResponses_WhileAResponseIsInProgressWaitsForItsEnd(t *testing.T) {
	os := newOneResponseServer(t)
	session := openOneResponseSession(t, os)

	_, err := session.Announce(context.Background(), "One moment.")
	require.NoError(t, err)
	assert.Equal(t, realtimeEventResponseCreate, os.await(t))
	require.NoError(t, session.SendToolResponses(context.Background(),
		[]providers.ToolResponse{{ToolCallID: "call_1", Result: `{"temp":21}`}}))
	assert.Equal(t, realtimeEventItemCreate, os.await(t))
	os.quiet(t)

	os.end <- struct{}{}
	assert.Equal(t, realtimeEventResponseCreate, os.await(t))
	os.end <- struct{}{}
	assert.Len(t, turnEnds(t, session, 2), 2)
}

// A response.create the server rejects because it had just started a
// response of its own (server VAD, as the create went out) is not a session
// error: it is sent again once that response is done.
func TestRealtimeSendText_RejectedAsBusyIsRequestedAgainAfterTheResponse(t *testing.T) {
	os := newOneResponseServerWith(t, true)
	session := openOneResponseSession(t, os)

	require.NoError(t, session.SendText(context.Background(), "What is the capital of France?"))
	assert.Equal(t, realtimeEventItemCreate, os.await(t))
	assert.Equal(t, realtimeEventResponseCreate, os.await(t))
	assert.Equal(t, "rejected", os.await(t))
	os.quiet(t)

	os.end <- struct{}{} // the server's own response ends
	assert.Equal(t, realtimeEventResponseCreate, os.await(t), "requested again")
	os.end <- struct{}{}
	ends := turnEnds(t, session, 2)
	assert.Equal(t, "R2", ends[1].Metadata[keyResponseID])
}
