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
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// recordingRealtime is an OpenAI Realtime stand-in that completes the
// handshake and sends every conversation.item.create the client sends to
// items, as the item it carries.
func recordingRealtime(t *testing.T, items chan<- map[string]any) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if conn.WriteJSON(map[string]any{"type": "session.created",
			"session": map[string]any{"id": "sess_1", "model": "gpt-realtime"}}) != nil {
			return
		}
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var ev struct {
				Type string         `json:"type"`
				Item map[string]any `json:"item"`
			}
			if json.Unmarshal(data, &ev) == nil && ev.Type == "conversation.item.create" {
				items <- ev.Item
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// ResumeDuplex on an OpenAI Realtime provider gives the new session the
// persisted conversation's text turns, as conversation items, before the
// caller's first input.
func TestResumeDuplex_OpenAIRealtimeSeedsTheConversation(t *testing.T) {
	items := make(chan map[string]any, 16)
	srv := recordingRealtime(t, items)
	provider := openai.NewProviderFromConfig(&openai.ProviderConfig{
		ID: "rt", Model: "gpt-realtime", Credential: staticCredential{}, Platform: "azure",
		PlatformConfig: &providers.PlatformConfig{Type: "azure", Endpoint: srv.URL + "/"},
	})
	store := statestore.NewMemoryStore()
	require.NoError(t, store.Save(context.Background(), &statestore.ConversationState{ID: "voice-1",
		Messages: []types.Message{
			{Role: "user", Content: "my name is Ada"},
			{Role: "assistant", Content: "hi Ada"},
		}}))
	packPath := filepath.Join(t.TempDir(), "pack.json")
	require.NoError(t, os.WriteFile(packPath, []byte(textStreamPackJSON), 0o600))

	conv, err := sdk.ResumeDuplex("voice-1", packPath, "chat",
		sdk.WithProvider(provider), sdk.WithSkipSchemaValidation(), sdk.WithStateStore(store),
		sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{
				Type: types.ContentTypeAudio, ChunkSize: 4800,
				SampleRate: 24000, Channels: 1, BitDepth: 16, Encoding: "pcm16",
			},
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, conv.SendText(ctx, "what is my name"))

	var got []map[string]any
	for len(got) < 3 {
		select {
		case item := <-items:
			delete(item, "id")
			got = append(got, item)
		case <-ctx.Done():
			t.Fatalf("the session received %d conversation items, want 3: %v", len(got), got)
		}
	}
	textItem := func(role, contentType, text string) map[string]any {
		return map[string]any{"type": "message", "role": role,
			"content": []any{map[string]any{"type": contentType, "text": text}}}
	}
	assert.Equal(t, []map[string]any{
		textItem("user", "input_text", "my name is Ada"),
		textItem("assistant", "output_text", "hi Ada"),
		textItem("user", "input_text", "what is my name"),
	}, got)
}
