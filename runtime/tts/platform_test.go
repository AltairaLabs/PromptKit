package tts

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// A provider type that runs on no platform keeps working against its own API
// when a platform is declared (resolved with its own key, sent to its own
// URL), and the platform being ignored is a warning, not silence.
func TestCreateFromSpec_UndeclaredPlatformIsIgnoredWithAWarning(t *testing.T) {
	var logs bytes.Buffer
	logger.SetLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { logger.SetLogger(nil) })

	var gotKey, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotPath = r.Header.Get("xi-api-key"), r.URL.Path
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("MP3"))
	}))
	t.Cleanup(srv.Close)

	platform := &credentials.PlatformConfig{Type: "azure", Endpoint: "https://res.openai.azure.com"}
	cred, err := ResolveCredentialForPlatform(context.Background(), "elevenlabs", "",
		&credentials.CredentialConfig{APIKey: "eleven-key"}, platform)
	require.NoError(t, err)
	svc, err := CreateFromSpec(ProviderSpec{
		ID: "voice", Type: "elevenlabs", BaseURL: srv.URL, Credential: cred, Platform: platform,
	})
	require.NoError(t, err)

	rc, err := svc.Synthesize(context.Background(), "hi", SynthesisConfig{Voice: "v1"})
	require.NoError(t, err)
	audio, _ := io.ReadAll(rc)
	_ = rc.Close()
	assert.Equal(t, "MP3", string(audio))
	assert.Equal(t, "eleven-key", gotKey)
	assert.Equal(t, "/text-to-speech/v1", gotPath)
	assert.Contains(t, logs.String(), "provider type does not run on this platform; the platform is ignored")
	assert.Contains(t, logs.String(), "type=elevenlabs platform=azure")
}

// A type that declares the platform is built without that warning.
func TestCreateFromSpec_DeclaredPlatformDoesNotWarn(t *testing.T) {
	var logs bytes.Buffer
	logger.SetLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { logger.SetLogger(nil) })

	_, err := CreateFromSpec(ProviderSpec{
		ID: "voice", Type: "openai", Model: "my-tts", Credential: credentials.NewAPIKeyCredential("k"),
		Platform: &credentials.PlatformConfig{Type: "azure", Endpoint: "https://res.openai.azure.com"},
	})
	require.NoError(t, err)
	assert.Empty(t, logs.String())
}

// OpenAI on azure needs the deployment: without a model there is no URL to
// address, so construction fails rather than calling a guessed deployment.
func TestCreateFromSpec_AzureOpenAINeedsTheDeployment(t *testing.T) {
	_, err := CreateFromSpec(ProviderSpec{
		ID: "voice", Type: "openai", Credential: credentials.NewAPIKeyCredential("k"),
		Platform: &credentials.PlatformConfig{Type: "azure", Endpoint: "https://res.openai.azure.com"},
	})
	require.Error(t, err)
	assert.Equal(t, `openai "voice" on azure needs model: the deployment name`, err.Error())
}
