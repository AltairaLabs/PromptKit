package gemini

import (
	"context"
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

// liveSetupAt runs a Live API stand-in, opens a session through a provider
// whose base_url points at it, and returns the path the session dialed and
// the setup message it sent.
func liveSetupAt(t *testing.T, metadata map[string]interface{}) (path string, setup map[string]any) {
	t.Helper()
	type dial struct {
		path  string
		setup map[string]any
	}
	got := make(chan dial, 1)
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg map[string]any
		_ = json.Unmarshal(data, &msg)
		got <- dial{path: r.URL.Path, setup: msg}
		reply, _ := json.Marshal(ServerMessage{SetupComplete: &SetupComplete{}})
		_ = conn.WriteMessage(websocket.TextMessage, reply)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	p := NewProvider("gemini-live", "gemini-live-model", srv.URL+"/v1beta", providers.ProviderDefaults{}, false)
	req := liveAudioRequest()
	req.Metadata = metadata
	session, err := p.CreateStreamSession(context.Background(), req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	select {
	case d := <-got:
		return d.path, d.setup
	case <-time.After(5 * time.Second):
		t.Fatal("the Live stand-in never received a setup message")
	}
	return "", nil
}

// voiceNameOf returns setup.generationConfig.speechConfig.voiceConfig.prebuiltVoiceConfig.voiceName.
func voiceNameOf(t *testing.T, setup map[string]any) any {
	t.Helper()
	var cur any = setup
	for _, k := range []string{"setup", "generationConfig", "speechConfig", "voiceConfig", "prebuiltVoiceConfig"} {
		m, ok := cur.(map[string]any)
		require.Truef(t, ok, "setup message has no %q object: %v", k, setup)
		cur = m[k]
	}
	m, ok := cur.(map[string]any)
	require.True(t, ok, "setup message has no prebuiltVoiceConfig: %v", setup)
	return m["voiceName"]
}

// metadata["voice"] — the key the OpenAI realtime provider reads — picks the
// Gemini Live voice; unset, the voice stays the one sessions have always used.
func TestLive_StreamMetadataVoiceSetsTheSetupVoice(t *testing.T) {
	audio := []string{"AUDIO"}

	_, setup := liveSetupAt(t, map[string]interface{}{"response_modalities": audio, "voice": "Kore"})
	assert.Equal(t, "Kore", voiceNameOf(t, setup))

	_, setup = liveSetupAt(t, map[string]interface{}{"response_modalities": audio})
	assert.Equal(t, "Puck", voiceNameOf(t, setup))

	_, setup = liveSetupAt(t, map[string]interface{}{"response_modalities": audio, "voice": ""})
	assert.Equal(t, "Puck", voiceNameOf(t, setup), "an empty voice keeps the default")
}

// A provider whose base_url points somewhere other than the Gemini API opens
// its Live sessions there too, at the Live route, so a gateway (or a test
// stand-in) in front of the API also fronts Live.
func TestLive_BaseURLHostCarriesTheLiveSession(t *testing.T) {
	path, _ := liveSetupAt(t, map[string]interface{}{"response_modalities": []string{"AUDIO"}})
	assert.Equal(t, "/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent", path)
}

// The Gemini API's own base URL, or none, keeps the public Live endpoint.
func TestLive_DefaultBaseURLKeepsThePublicLiveEndpoint(t *testing.T) {
	for _, base := range []string{"", "https://generativelanguage.googleapis.com/v1beta"} {
		p := NewProvider("g", "m", base, providers.ProviderDefaults{}, false)
		cfg := StreamSessionConfig{}
		wsURL, _, err := p.liveDial(&cfg)
		require.NoError(t, err)
		assert.Equal(t, geminiLiveAPIURL, wsURL, "base_url %q", base)
	}
	p := NewProvider("g", "m", "https://gateway.example.com/gemini/v1beta", providers.ProviderDefaults{}, false)
	wsURL, _, err := p.liveDial(&StreamSessionConfig{})
	require.NoError(t, err)
	assert.Equal(t,
		"wss://gateway.example.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent", wsURL)
}
