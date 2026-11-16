package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// sessionUpdateTranscription opens a realtime session with the given request
// metadata against a fake server and returns the session.update's
// audio.input.transcription object as the server received it (nil if absent).
func sessionUpdateTranscription(t *testing.T, metadata map[string]any) map[string]any {
	t.Helper()
	updates := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := bargeInUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{
			"type":    "session.created",
			"session": map[string]any{"id": "sess_test", "model": "gpt-realtime"},
		})
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var m map[string]any
			if json.Unmarshal(data, &m) == nil && m["type"] == "session.update" {
				updates <- m
			}
		}
	}))
	t.Cleanup(srv.Close)

	p := NewProviderFromConfig(&ProviderConfig{
		ID: "rt", Model: "gpt-realtime", Credential: &tokenCredential{}, Platform: "azure",
		PlatformConfig: &providers.PlatformConfig{Type: "azure", Endpoint: srv.URL + "/"},
	})
	req := audioStreamRequest()
	req.Metadata = metadata
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := p.CreateStreamSession(ctx, req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	var update map[string]any
	select {
	case update = <-updates:
	case <-time.After(3 * time.Second):
		t.Fatal("no session.update received")
	}
	sess, _ := update["session"].(map[string]any)
	audio, _ := sess["audio"].(map[string]any)
	input, _ := audio["input"].(map[string]any)
	tr, _ := input["transcription"].(map[string]any)
	return tr
}

// The input transcription model is selectable from metadata (#2692); whisper-1
// stays the default, and turning transcription off still wins.
func TestRealtime_InputTranscriptionModelFromMetadata(t *testing.T) {
	cases := map[string]struct {
		metadata  map[string]any
		wantModel any // nil means no transcription object at all
	}{
		"default":  {map[string]any{}, "whisper-1"},
		"selected": {map[string]any{"input_transcription_model": "gpt-4o-transcribe"}, "gpt-4o-transcribe"},
		"empty string keeps default": {
			map[string]any{"input_transcription_model": ""}, "whisper-1",
		},
		"disabled ignores the model": {
			map[string]any{"input_transcription": false, "input_transcription_model": "gpt-4o-transcribe"}, nil,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tr := sessionUpdateTranscription(t, tc.metadata)
			if tc.wantModel == nil {
				require.Nil(t, tr, "transcription must be absent when disabled")
				return
			}
			require.Equal(t, tc.wantModel, tr["model"])
		})
	}
}
