package sdk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/v2/config"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
)

// geminiTTSStandIn answers generateContent with raw 24 kHz PCM and records
// each request's path, API key and voice.
func geminiTTSStandIn(t *testing.T, pcm []byte) (*httptest.Server, <-chan [3]string) {
	t.Helper()
	got := make(chan [3]string, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			GenerationConfig struct {
				SpeechConfig struct {
					VoiceConfig struct {
						PrebuiltVoiceConfig struct {
							VoiceName string `json:"voiceName"`
						} `json:"prebuiltVoiceConfig"`
					} `json:"voiceConfig"`
				} `json:"speechConfig"`
			} `json:"generationConfig"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		got <- [3]string{r.URL.Path, r.Header.Get("x-goog-api-key"),
			body.GenerationConfig.SpeechConfig.VoiceConfig.PrebuiltVoiceConfig.VoiceName}
		_ = json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{
			"content": map[string]any{"parts": []any{map[string]any{"inlineData": map[string]any{
				"mimeType": "audio/L16;codec=pcm;rate=24000", "data": base64.StdEncoding.EncodeToString(pcm),
			}}}},
		}}})
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

// WithTTSProvider and a `role: tts` provider file with `type: gemini` both
// give the conversation a Gemini TTS service built from the spec.
func TestGeminiTTS_FromSpecAndProviderFile(t *testing.T) {
	pcm := []byte{1, 2, 3, 4}
	synthesize := func(t *testing.T, c *config) []byte {
		t.Helper()
		require.NotNil(t, c.ttsService)
		rc, err := c.ttsService.Synthesize(context.Background(), "hello", tts.SynthesisConfig{Voice: "Kore"})
		require.NoError(t, err)
		defer rc.Close()
		b, err := io.ReadAll(rc)
		require.NoError(t, err)
		return b
	}

	t.Run("WithTTSProvider", func(t *testing.T) {
		srv, got := geminiTTSStandIn(t, pcm)
		c := &config{}
		require.NoError(t, WithTTSProvider(ProviderSpec{
			ID: "cue", Type: "gemini", Model: "spec-tts", BaseURL: srv.URL,
			Credential: &pkgconfig.CredentialConfig{APIKey: "g-key"},
		})(c))
		assert.Equal(t, pcm, synthesize(t, c))
		assert.Equal(t, [3]string{"/models/spec-tts:generateContent", "g-key", "Kore"}, <-got)
	})

	t.Run("provider file", func(t *testing.T) {
		srv, got := geminiTTSStandIn(t, pcm)
		path := writeProviderYAML(t, t.TempDir(), "cue.provider.yaml", `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: cue
spec:
  id: cue
  type: gemini
  role: tts
  model: file-tts
  base_url: `+srv.URL+`
  credential:
    api_key: file-key
`)
		c := &config{}
		require.NoError(t, WithProviderFile(path)(c))
		assert.Equal(t, pcm, synthesize(t, c))
		assert.Equal(t, [3]string{"/models/file-tts:generateContent", "file-key", "Kore"}, <-got)
	})
}
