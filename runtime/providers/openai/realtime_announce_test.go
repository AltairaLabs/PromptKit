package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// Announce has the model speak a text as its own reply: one response.create
// whose instructions are to say it, with no tools and no user item, so the
// conversation gains no user turn.
func TestRealtimeSession_AnnounceAsksForTheTextAsAReply(t *testing.T) {
	events := make(chan map[string]any, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := bargeInUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{"type": "session.created", "session": map[string]any{"id": "s", "model": "m"}})
		if _, _, err := conn.ReadMessage(); err != nil { // session.update
			return
		}
		for {
			ev := readClientEvent(conn)
			if ev == nil {
				return
			}
			events <- ev
		}
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	config := DefaultRealtimeSessionConfig()
	session, err := newRealtimeSession(ctx, "test-key", &config, realtimeSessionOpts{endpoint: bargeInWSURL(srv)})
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	var a providers.Announcer = session
	spoken, err := a.Announce(ctx, "Sorry, I can't say that word.")
	require.NoError(t, err)
	assert.True(t, spoken, "an audio session speaks it")

	select {
	case ev := <-events:
		assert.Equal(t, "response.create", ev["type"], "no user item precedes the response")
		resp, _ := ev["response"].(map[string]any)
		assert.Equal(t, providers.AnnounceInstructions("Sorry, I can't say that word."), resp["instructions"])
		assert.Equal(t, "none", resp["tool_choice"])
	case <-ctx.Done():
		t.Fatal("no client event")
	}
}
