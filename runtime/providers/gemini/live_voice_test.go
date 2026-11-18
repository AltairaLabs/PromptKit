package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
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

	built, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "gemini-live", Type: "gemini", Model: "gemini-live-model",
		AdditionalConfig: map[string]any{"live_url": "ws" + strings.TrimPrefix(srv.URL, "http") + "/gw/gemini/live"},
	})
	require.NoError(t, err)
	p := built.(*ToolProvider)
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

// additional_config.live_url sends Live sessions to that URL exactly, path
// included, so a gateway or a test stand-in can front Live. base_url does not
// move Live: a REST-only proxy keeps working with Live on Google.
func TestLive_LiveURLCarriesTheSessionWithItsPath(t *testing.T) {
	path, _ := liveSetupAt(t, map[string]interface{}{"response_modalities": []string{"AUDIO"}})
	assert.Equal(t, "/gw/gemini/live", path)
}

func TestLive_BaseURLDoesNotMoveLive(t *testing.T) {
	for _, base := range []string{"", "https://generativelanguage.googleapis.com/v1beta",
		"https://gateway.example.com/gemini/v1beta"} {
		p := NewProvider("g", "m", base, providers.ProviderDefaults{}, false)
		wsURL, _, err := p.liveDial(&StreamSessionConfig{})
		require.NoError(t, err)
		assert.Equal(t, geminiLiveAPIURL, wsURL, "base_url %q", base)
	}
}

func geminiSpec(live any) providers.ProviderSpec {
	return providers.ProviderSpec{ID: "g", Type: "gemini", Model: "m",
		AdditionalConfig: map[string]any{"live_url": live}}
}

// A live_url must be a WebSocket URL; ws:// is allowed with a warning that the
// API key travels in plaintext, anything else is refused when the provider is built.
func TestLive_LiveURLValidation(t *testing.T) {
	var logs bytes.Buffer
	logger.SetLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { logger.SetLogger(nil) })

	p, err := providers.CreateProviderFromSpec(geminiSpec("wss://gw.example.com/a/b?x=1"))
	require.NoError(t, err)
	wsURL, _, err := p.(*ToolProvider).liveDial(&StreamSessionConfig{})
	require.NoError(t, err)
	assert.Equal(t, "wss://gw.example.com/a/b?x=1", wsURL)
	assert.Empty(t, logs.String())

	_, err = providers.CreateProviderFromSpec(geminiSpec("ws://gw.example.com/live"))
	require.NoError(t, err)
	assert.Contains(t, logs.String(), "live_url is not TLS: the API key is sent in plaintext")

	for _, bad := range []any{"http://gw.example.com/live", "https://gw.example.com/live", "gw.example.com/live",
		"wss://", 42} {
		_, err := providers.CreateProviderFromSpec(geminiSpec(bad))
		require.Error(t, err, "%v", bad)
		assert.Contains(t, err.Error(), "additional_config.live_url must be a ws:// or wss:// URL", "%v", bad)
	}
}
