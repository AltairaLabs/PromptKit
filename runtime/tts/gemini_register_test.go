package tts

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
)

// A `type: gemini` spec builds a Gemini service that sends the spec's model,
// base URL and resolved API key.
func TestCreateFromSpec_Gemini(t *testing.T) {
	assert.True(t, slices.Contains(RegisteredTypes(), "gemini"))

	srv, calls := geminiTTSServer(t, http.StatusOK,
		geminiAudioResponse("audio/wav", geminiWAV(testPCM, 24000, "")))
	cred, err := ResolveCredential(context.Background(), "gemini", "",
		&credentials.CredentialConfig{APIKey: "spec-key"})
	require.NoError(t, err)
	svc, err := CreateFromSpec(ProviderSpec{
		ID: "cue", Type: "gemini", Model: "spec-model", BaseURL: srv.URL, Credential: cred,
		AdditionalConfig: map[string]any{"pricing": map[string]any{
			"currency": "usd", "items": []any{map[string]any{"unit": "character", "rate": 0.001}},
		}},
	})
	require.NoError(t, err)
	rc, err := svc.Synthesize(context.Background(), "Hi", SynthesisConfig{Voice: "Kore"})
	require.NoError(t, err)
	assert.Equal(t, testPCM, readAllAudio(t, rc))
	call := <-calls
	assert.Equal(t, "/models/spec-model:generateContent", call.path)
	assert.Equal(t, "spec-key", call.apiKey)

	gs := svc.(*GeminiService)
	require.NotNil(t, gs.Pricing())
	assert.InDelta(t, 0.001, gs.Pricing().Items[0].Rate, 1e-12)

	bare, err := CreateFromSpec(ProviderSpec{Type: "gemini"})
	require.NoError(t, err)
	assert.Equal(t, DefaultGeminiTTSModel, bare.(*GeminiService).Model)
	assert.Equal(t, geminiBaseURL, bare.(*GeminiService).BaseURL)
	assert.Nil(t, bare.(*GeminiService).Pricing())
}

// On a vertex platform the factory targets the project's location with the
// platform credential.
func TestCreateFromSpec_GeminiOnVertex(t *testing.T) {
	platform := &credentials.PlatformConfig{Type: "vertex", Project: "proj", Region: "europe-west4"}
	svc, err := CreateFromSpec(ProviderSpec{
		ID: "cue", Type: "gemini", Model: "vx-tts", Credential: bearerCred("t"), Platform: platform,
	})
	require.NoError(t, err)
	gs := svc.(*GeminiService)
	assert.Equal(t,
		"https://europe-west4-aiplatform.googleapis.com/v1/projects/proj/locations/europe-west4/publishers/google/models/vx-tts:generateContent",
		gs.generateContentURL(gs.Model))
	assert.Empty(t, gs.APIKey)

	_, err = CreateFromSpec(ProviderSpec{ID: "cue", Type: "gemini", Credential: bearerCred("t"),
		Platform: &credentials.PlatformConfig{Type: "vertex", Project: "proj"}})
	assert.EqualError(t, err, `tts "cue" on vertex needs platform.project and platform.region`)

	_, err = CreateFromSpec(ProviderSpec{ID: "cue", Type: "gemini", Platform: platform})
	assert.EqualError(t, err, `tts "cue" on vertex needs a platform credential`)
}
