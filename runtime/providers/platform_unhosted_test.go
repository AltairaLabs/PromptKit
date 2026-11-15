package providers_test

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/imagen"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/ollama"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/vllm"
)

func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	logger.SetLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { logger.SetLogger(nil) })
	return &logs
}

// A self-hosted provider type runs on no platform. A spec that names one is
// still built against its own endpoint (it worked that way, the platform never
// read), and the platform having no effect is now a warning instead of silence.
func TestUnhostedProviderTypes_WarnThatThePlatformIsIgnored(t *testing.T) {
	for _, typ := range []string{"ollama", "vllm"} {
		t.Run(typ, func(t *testing.T) {
			logs := captureWarnings(t)
			p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
				ID: "local", Type: typ, Model: "llama3", BaseURL: "http://127.0.0.1:1",
				Platform: "azure", PlatformConfig: &providers.PlatformConfig{Type: "azure", Endpoint: "https://x"},
			})
			require.NoError(t, err)
			assert.Equal(t, "local", p.ID())
			assert.Contains(t, logs.String(), "provider type does not run on this platform; the platform is ignored")
			assert.Contains(t, logs.String(), "type="+typ+" platform=azure")
		})
	}
}

// Imagen is retired on Vertex, and the provider only speaks the Gemini API: on
// Vertex it sent the GCP token to the Gemini API host. Refuse it there.
func TestImagen_VertexPlatformIsRejected(t *testing.T) {
	_, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "img", Type: "imagen", Model: "imagen-4.0-generate-001",
		Platform: "vertex", PlatformConfig: &providers.PlatformConfig{Type: "vertex", Project: "p", Region: "us-central1"},
	})
	var typed *providers.UnsupportedProviderPlatformError
	require.True(t, errors.As(err, &typed), "got %T: %v", err, err)
	assert.Equal(t, "imagen", typed.ProviderType)
	assert.Equal(t, "vertex", typed.Platform)
}

// On a platform that never hosted it either, imagen keeps its Gemini API
// behavior and warns.
func TestImagen_OtherPlatformIsIgnoredWithAWarning(t *testing.T) {
	logs := captureWarnings(t)
	_, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "img", Type: "imagen", Model: "imagen-4.0-generate-001",
		Platform: "azure", PlatformConfig: &providers.PlatformConfig{Type: "azure", Endpoint: "https://x"},
	})
	require.NoError(t, err)
	assert.Contains(t, logs.String(), "type=imagen platform=azure")
}

// Ollama embeddings likewise run on no platform: built as before, with a warning.
func TestOllamaEmbedding_PlatformIsIgnoredWithAWarning(t *testing.T) {
	logs := captureWarnings(t)
	_, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		ID: "emb", Type: "ollama", Model: "nomic-embed-text", BaseURL: "http://127.0.0.1:1",
		Platform: "vertex", PlatformConfig: &providers.PlatformConfig{Type: "vertex", Project: "p", Region: "r"},
	})
	require.NoError(t, err)
	assert.Contains(t, logs.String(), "type=ollama platform=vertex")
}
