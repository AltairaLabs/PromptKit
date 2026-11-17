package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A text-only realtime session (output_modalities ["text"]) is answered with
// the GA event names response.output_text.delta / .done. Their text must reach
// Response() as the pre-GA response.text.* did; before the fix both were
// dropped as unhandled, so a text-only session's replies were empty. The text
// streams: each delta is its own chunk, as it arrives.
func TestRealtimeSession_GAOutputTextEventsReachResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := bargeInUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		send := func(ev map[string]any) bool { return conn.WriteJSON(ev) == nil }
		if !send(map[string]any{"type": "session.created",
			"session": map[string]any{"id": "sess_test", "model": "gpt-realtime"}}) {
			return
		}
		if _, _, err := conn.ReadMessage(); err != nil { // session.update
			return
		}
		item := map[string]any{"item_id": "item_1", "response_id": "resp_1", "output_index": 0, "content_index": 0}
		with := func(extra map[string]any) map[string]any {
			out := map[string]any{}
			for k, v := range item {
				out[k] = v
			}
			for k, v := range extra {
				out[k] = v
			}
			return out
		}
		_ = send(map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_1"}}) &&
			send(with(map[string]any{"type": "response.output_text.delta", "delta": "Hello"})) &&
			send(with(map[string]any{"type": "response.output_text.delta", "delta": " there"})) &&
			send(with(map[string]any{"type": "response.output_text.done", "text": "Hello there"}))
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	config := DefaultRealtimeSessionConfig()
	config.Modalities = []string{"text"}
	session, err := newRealtimeSession(ctx, "test-key", &config, realtimeSessionOpts{
		endpoint: "ws" + strings.TrimPrefix(srv.URL, "http"),
	})
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	// Each delta is a chunk whose Content and Delta are that delta; the done
	// chunk carries the whole text in Metadata["text"] and no Content, so
	// Content adds up to the text once.
	type textChunk struct{ content, delta, doneText string }
	var got []textChunk
	for done := false; !done; {
		select {
		case c, ok := <-session.Response():
			require.True(t, ok, "Response() closed before the text arrived")
			d, _ := c.Metadata["text_done"].(bool)
			text, _ := c.Metadata["text"].(string)
			if c.Content != "" || c.Delta != "" || d {
				got = append(got, textChunk{c.Content, c.Delta, text})
			}
			done = d
		case <-ctx.Done():
			t.Fatalf("the text never reached Response(); chunks so far %+v", got)
		}
	}
	assert.Equal(t, []textChunk{
		{content: "Hello", delta: "Hello"},
		{content: " there", delta: " there"},
		{doneText: "Hello there"},
	}, got)
}
