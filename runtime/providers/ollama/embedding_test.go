package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

func TestNewEmbeddingProvider(t *testing.T) {
	t.Run("creates provider with defaults", func(t *testing.T) {
		p := NewEmbeddingProvider()

		assert.Equal(t, DefaultEmbeddingModel, p.Model())
		assert.Equal(t, DefaultOllamaURL, p.BaseURL)
		assert.Equal(t, dimensionsNomicText, p.EmbeddingDimensions())
		assert.Equal(t, "ollama-embedding", p.ID())
		assert.Equal(t, maxEmbeddingBatch, p.MaxBatchSize())
	})

	t.Run("applies options", func(t *testing.T) {
		p := NewEmbeddingProvider(
			WithEmbeddingModel(EmbeddingModelMxbaiLarge),
			WithEmbeddingBaseURL("http://gpu-server:11434"),
		)

		assert.Equal(t, EmbeddingModelMxbaiLarge, p.Model())
		assert.Equal(t, "http://gpu-server:11434", p.BaseURL)
		assert.Equal(t, dimensionsMxbai, p.EmbeddingDimensions())
	})

	t.Run("custom dimensions override model default", func(t *testing.T) {
		p := NewEmbeddingProvider(
			WithEmbeddingModel(EmbeddingModelNomicText),
			WithEmbeddingDimensions(512), // custom fine-tuned model
		)

		assert.Equal(t, 512, p.EmbeddingDimensions())
	})

	t.Run("unknown model gets default dimensions", func(t *testing.T) {
		p := NewEmbeddingProvider(
			WithEmbeddingModel("custom-model"),
		)

		assert.Equal(t, dimensionsNomicText, p.EmbeddingDimensions())
	})
}

func TestEmbeddingProvider_Embed(t *testing.T) {
	t.Run("embeds single text", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/embed", r.URL.Path)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			// No Authorization header expected
			assert.Empty(t, r.Header.Get("Authorization"))

			var req ollamaEmbedRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			assert.Equal(t, DefaultEmbeddingModel, req.Model)
			// Single text should be sent as string, not array
			assert.IsType(t, "", req.Input)

			resp := ollamaEmbedResponse{
				Model:      DefaultEmbeddingModel,
				Embeddings: [][]float32{{0.1, 0.2, 0.3}},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p := NewEmbeddingProvider(WithEmbeddingBaseURL(server.URL))

		result, err := p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"Hello world"},
		})
		require.NoError(t, err)
		assert.Len(t, result.Embeddings, 1)
		assert.Equal(t, []float32{0.1, 0.2, 0.3}, result.Embeddings[0])
		assert.Equal(t, DefaultEmbeddingModel, result.Model)
	})

	t.Run("embeds multiple texts", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req ollamaEmbedRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			// Multiple texts should be sent as array
			inputArr, ok := req.Input.([]any)
			require.True(t, ok, "batch input should be array")
			assert.Len(t, inputArr, 3)

			resp := ollamaEmbedResponse{
				Model: DefaultEmbeddingModel,
				Embeddings: [][]float32{
					{0.1, 0.2, 0.3},
					{0.4, 0.5, 0.6},
					{0.7, 0.8, 0.9},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p := NewEmbeddingProvider(WithEmbeddingBaseURL(server.URL))

		result, err := p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"one", "two", "three"},
		})
		require.NoError(t, err)
		assert.Len(t, result.Embeddings, 3)
	})

	t.Run("empty request returns empty response", func(t *testing.T) {
		p := NewEmbeddingProvider()

		result, err := p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{},
		})
		require.NoError(t, err)
		assert.Empty(t, result.Embeddings)
	})

	t.Run("model override in request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req ollamaEmbedRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			assert.Equal(t, "mxbai-embed-large", req.Model)

			resp := ollamaEmbedResponse{
				Model:      "mxbai-embed-large",
				Embeddings: [][]float32{{0.1}},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p := NewEmbeddingProvider(WithEmbeddingBaseURL(server.URL))

		result, err := p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"test"},
			Model: "mxbai-embed-large",
		})
		require.NoError(t, err)
		assert.Equal(t, "mxbai-embed-large", result.Model)
	})

	t.Run("server error returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error": "model not found"}`))
		}))
		defer server.Close()

		p := NewEmbeddingProvider(WithEmbeddingBaseURL(server.URL))

		_, err := p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"test"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "500")
	})

	t.Run("embedding count mismatch returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			resp := ollamaEmbedResponse{
				Model:      DefaultEmbeddingModel,
				Embeddings: [][]float32{{0.1}}, // only 1, but 2 texts sent
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p := NewEmbeddingProvider(WithEmbeddingBaseURL(server.URL))

		_, err := p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"one", "two"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected 2 embeddings, got 1")
	})

	t.Run("connection refused returns error", func(t *testing.T) {
		p := NewEmbeddingProvider(WithEmbeddingBaseURL("http://localhost:1"))

		_, err := p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"test"},
		})
		require.Error(t, err)
	})

	t.Run("invalid JSON response returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{invalid json`))
		}))
		defer server.Close()

		p := NewEmbeddingProvider(WithEmbeddingBaseURL(server.URL))

		_, err := p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"test"},
		})
		require.Error(t, err)
	})
}

func TestEmbeddingProvider_Interface(t *testing.T) {
	// Compile-time check that EmbeddingProvider implements providers.EmbeddingProvider
	var _ providers.EmbeddingProvider = (*EmbeddingProvider)(nil)
}

func TestDimensionsForModel(t *testing.T) {
	tests := []struct {
		model string
		want  int
	}{
		{EmbeddingModelNomicText, dimensionsNomicText},
		{EmbeddingModelMxbaiLarge, dimensionsMxbai},
		{EmbeddingModelAllMiniLM, dimensionsAllMiniLM},
		{"unknown-model", dimensionsNomicText},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			assert.Equal(t, tt.want, dimensionsForModel(tt.model))
		})
	}
}
