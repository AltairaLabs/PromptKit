package sdk_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// staticCredential authenticates the fake realtime server's handshake.
type staticCredential struct{}

func (staticCredential) Apply(_ context.Context, req *http.Request) error {
	req.Header.Set("Authorization", "Bearer test")
	return nil
}

func (staticCredential) Type() string { return "azure" }

// fakeTextRealtime is an OpenAI Realtime stand-in for a text-only session:
// it answers each response.create with the text deltas given, then
// response.output_text.done carrying the whole text, then response.done.
func fakeTextRealtime(t *testing.T, deltas []string, full string) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		send := func(ev map[string]any) bool { return conn.WriteJSON(ev) == nil }
		if !send(map[string]any{"type": "session.created",
			"session": map[string]any{"id": "sess_1", "model": "gpt-realtime"}}) {
			return
		}
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var ev struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(data, &ev) != nil || ev.Type != "response.create" {
				continue
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
			if !send(map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_1"}}) {
				return
			}
			for _, d := range deltas {
				if !send(with(map[string]any{"type": "response.output_text.delta", "delta": d})) {
					return
				}
			}
			if !send(with(map[string]any{"type": "response.output_text.done", "text": full})) ||
				!send(map[string]any{"type": "response.done", "response": map[string]any{
					"id": "resp_1", "status": "completed",
					"usage": map[string]any{"total_tokens": 30, "input_tokens": 20, "output_tokens": 10},
				}}) {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

const textStreamPackJSON = `{
	"id": "rt-text-stream",
	"version": "1.0.0",
	"description": "Realtime text streaming",
	"prompts": {"chat": {"id": "chat", "name": "Chat", "system_template": "You are terse."}}
}`

// A text-only OpenAI Realtime reply streams: each response.output_text.delta
// reaches Response() as its own chunk, in order, as it arrives, and the
// recorded reply is the text once (the closing output_text.done repeats the
// whole text, which must not be appended again).
func TestOpenDuplex_RealtimeTextOnlyReplyStreamsDeltas(t *testing.T) {
	deltas := []string{"Hel", "lo ", "there"}
	srv := fakeTextRealtime(t, deltas, "Hello there")
	provider := openai.NewProviderFromConfig(&openai.ProviderConfig{
		ID: "rt", Model: "gpt-realtime", Credential: staticCredential{}, Platform: "azure",
		PlatformConfig: &providers.PlatformConfig{Type: "azure", Endpoint: srv.URL + "/"},
	})
	packPath := filepath.Join(t.TempDir(), "pack.json")
	require.NoError(t, os.WriteFile(packPath, []byte(textStreamPackJSON), 0o600))
	conv, err := sdk.OpenDuplex(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{
				Type: types.ContentTypeAudio, ChunkSize: 4800,
				SampleRate: 24000, Channels: 1, BitDepth: 16, Encoding: "pcm16",
			},
			Metadata: map[string]interface{}{"modalities": []string{"text"}},
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	ch, err := conv.Response()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, conv.SendText(ctx, "hi"))

	var got []string
	for done := false; !done; {
		select {
		case c, ok := <-ch:
			if !ok {
				done = true
				break
			}
			if c.Delta != "" {
				got = append(got, c.Delta)
			}
			done, _ = c.Metadata["assistant_turn_complete"].(bool)
		case <-ctx.Done():
			t.Fatalf("the reply never finished; deltas so far %q", got)
		}
	}

	assert.Equal(t, deltas, got)
	msgs := conv.Messages(ctx)
	require.NotEmpty(t, msgs)
	last := msgs[len(msgs)-1]
	assert.Equal(t, "assistant", last.Role)
	assert.Equal(t, "Hello there", last.GetContent())
}
