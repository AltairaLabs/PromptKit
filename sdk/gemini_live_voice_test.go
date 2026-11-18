package sdk_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// geminiLiveSetupRecorder is a Gemini Live stand-in that hands every setup
// message it receives to the test, completes setup, and then reads the
// session until it closes.
func geminiLiveSetupRecorder(t *testing.T) (*httptest.Server, <-chan []byte) {
	t.Helper()
	setups := make(chan []byte, 4)
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
		setups <- data
		if conn.WriteJSON(map[string]any{"setupComplete": map[string]any{}}) != nil {
			return
		}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv, setups
}

// openGeminiLiveVoice opens a duplex conversation on a Gemini Live provider
// whose live_url is the stand-in, with the streaming metadata given, sends a
// line of text so the session opens, and returns the setup message's voice.
func openGeminiLiveVoice(t *testing.T, metadata map[string]interface{}) string {
	t.Helper()
	srv, setups := geminiLiveSetupRecorder(t)
	provider, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "gemini-live", Type: "gemini", Model: "live-model",
		AdditionalConfig: map[string]any{"live_url": "ws" + strings.TrimPrefix(srv.URL, "http") + "/live"},
	})
	require.NoError(t, err)
	packPath := filepath.Join(t.TempDir(), "pack.json")
	require.NoError(t, os.WriteFile(packPath, []byte(textStreamPackJSON), 0o600))
	conv, err := sdk.OpenDuplex(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{
				Type: types.ContentTypeAudio, ChunkSize: 3200,
				SampleRate: 16000, Channels: 1, BitDepth: 16, Encoding: "pcm16",
			},
			Metadata: metadata,
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = conv.Response()
	require.NoError(t, err)
	require.NoError(t, conv.SendText(ctx, "hi"))

	var raw []byte
	select {
	case raw = <-setups:
	case <-ctx.Done():
		t.Fatal("the Gemini Live stand-in never received a setup message")
	}
	var setup struct {
		Setup struct {
			GenerationConfig struct {
				SpeechConfig struct {
					VoiceConfig struct {
						PrebuiltVoiceConfig struct {
							VoiceName string `json:"voiceName"`
						} `json:"prebuiltVoiceConfig"`
					} `json:"voiceConfig"`
				} `json:"speechConfig"`
			} `json:"generationConfig"`
		} `json:"setup"`
	}
	require.NoError(t, json.Unmarshal(raw, &setup), "setup message %s", raw)
	return setup.Setup.GenerationConfig.SpeechConfig.VoiceConfig.PrebuiltVoiceConfig.VoiceName
}

// The streaming config's metadata["voice"] — the route that sets the OpenAI
// realtime voice — sets the Gemini Live voice in the session's setup message;
// without it the session keeps Gemini Live's default voice.
func TestOpenDuplex_GeminiLiveVoiceFromStreamingMetadata(t *testing.T) {
	audio := []string{"AUDIO"}
	assert.Equal(t, "Kore",
		openGeminiLiveVoice(t, map[string]interface{}{"response_modalities": audio, "voice": "Kore"}))
	assert.Equal(t, "Puck",
		openGeminiLiveVoice(t, map[string]interface{}{"response_modalities": audio}))
}
