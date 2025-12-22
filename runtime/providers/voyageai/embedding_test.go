package voyageai

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
	t.Run("fails without API key", func(t *testing.T) {
		t.Setenv("VOYAGE_API_KEY", "")

		_, err := NewEmbeddingProvider()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "VOYAGE_API_KEY")
	})

	t.Run("creates with API key from env", func(t *testing.T) {
		t.Setenv("VOYAGE_API_KEY", "test-key")

		p, err := NewEmbeddingProvider()
		require.NoError(t, err)
		assert.Equal(t, DefaultModel, p.Model())
		assert.Equal(t, Dimensions1024, p.EmbeddingDimensions())
	})

	t.Run("creates with explicit API key", func(t *testing.T) {
		t.Setenv("VOYAGE_API_KEY", "")

		p, err := NewEmbeddingProvider(WithAPIKey("explicit-key"))
		require.NoError(t, err)
		assert.NotNil(t, p)
	})

	t.Run("applies options", func(t *testing.T) {
		t.Setenv("VOYAGE_API_KEY", "test-key")

		p, err := NewEmbeddingProvider(
			WithModel(ModelVoyage3Large),
			WithDimensions(Dimensions2048),
			WithInputType(InputTypeQuery),
		)
		require.NoError(t, err)
		assert.Equal(t, ModelVoyage3Large, p.Model())
		assert.Equal(t, Dimensions2048, p.EmbeddingDimensions())
		assert.Equal(t, InputTypeQuery, p.inputType)
	})
}

func TestEmbeddingProvider_Embed(t *testing.T) {
	t.Run("returns empty for empty input", func(t *testing.T) {
		t.Setenv("VOYAGE_API_KEY", "test-key")

		p, err := NewEmbeddingProvider()
		require.NoError(t, err)

		resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{},
		})
		require.NoError(t, err)
		assert.Empty(t, resp.Embeddings)
		assert.Equal(t, DefaultModel, resp.Model)
	})

	t.Run("embeds texts successfully", func(t *testing.T) {
		// Create mock server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify request
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/embeddings", r.URL.Path)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

			var req embeddingRequest
			err := json.NewDecoder(r.Body).Decode(&req)
			require.NoError(t, err)
			assert.Equal(t, DefaultModel, req.Model)
			assert.Equal(t, []string{"hello world", "test input"}, req.Input)

			// Return mock response
			resp := embeddingResponse{
				Object: "list",
				Data: []embeddingData{
					{Object: "embedding", Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
					{Object: "embedding", Embedding: []float32{0.4, 0.5, 0.6}, Index: 1},
				},
				Model: DefaultModel,
				Usage: embeddingUsage{TotalTokens: 10},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithAPIKey("test-key"),
			WithBaseURL(server.URL),
		)
		require.NoError(t, err)

		resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"hello world", "test input"},
		})
		require.NoError(t, err)
		assert.Len(t, resp.Embeddings, 2)
		assert.Equal(t, []float32{0.1, 0.2, 0.3}, resp.Embeddings[0])
		assert.Equal(t, []float32{0.4, 0.5, 0.6}, resp.Embeddings[1])
		assert.Equal(t, DefaultModel, resp.Model)
		assert.Equal(t, 10, resp.Usage.TotalTokens)
	})

	t.Run("handles API error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"detail": "Invalid model"}`))
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithAPIKey("test-key"),
			WithBaseURL(server.URL),
		)
		require.NoError(t, err)

		_, err = p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"test"},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Invalid model")
	})

	t.Run("uses model override from request", func(t *testing.T) {
		var receivedModel string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req embeddingRequest
			json.NewDecoder(r.Body).Decode(&req)
			receivedModel = req.Model

			resp := embeddingResponse{
				Object: "list",
				Data: []embeddingData{
					{Object: "embedding", Embedding: []float32{0.1}, Index: 0},
				},
				Model: req.Model,
				Usage: embeddingUsage{TotalTokens: 5},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithAPIKey("test-key"),
			WithBaseURL(server.URL),
		)
		require.NoError(t, err)

		_, err = p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"test"},
			Model: ModelVoyageCode3,
		})
		require.NoError(t, err)
		assert.Equal(t, ModelVoyageCode3, receivedModel)
	})

	t.Run("includes input_type when set", func(t *testing.T) {
		var receivedInputType string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req embeddingRequest
			json.NewDecoder(r.Body).Decode(&req)
			receivedInputType = req.InputType

			resp := embeddingResponse{
				Object: "list",
				Data: []embeddingData{
					{Object: "embedding", Embedding: []float32{0.1}, Index: 0},
				},
				Model: DefaultModel,
				Usage: embeddingUsage{TotalTokens: 5},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithAPIKey("test-key"),
			WithBaseURL(server.URL),
			WithInputType(InputTypeQuery),
		)
		require.NoError(t, err)

		_, err = p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"search query"},
		})
		require.NoError(t, err)
		assert.Equal(t, InputTypeQuery, receivedInputType)
	})

	t.Run("includes output_dimension when non-default", func(t *testing.T) {
		var receivedDimension int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req embeddingRequest
			json.NewDecoder(r.Body).Decode(&req)
			receivedDimension = req.OutputDimension

			resp := embeddingResponse{
				Object: "list",
				Data: []embeddingData{
					{Object: "embedding", Embedding: make([]float32, 512), Index: 0},
				},
				Model: DefaultModel,
				Usage: embeddingUsage{TotalTokens: 5},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithAPIKey("test-key"),
			WithBaseURL(server.URL),
			WithDimensions(Dimensions512),
		)
		require.NoError(t, err)

		_, err = p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"test"},
		})
		require.NoError(t, err)
		assert.Equal(t, Dimensions512, receivedDimension)
	})
}

func TestEmbeddingProvider_Methods(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "test-key")

	p, err := NewEmbeddingProvider(
		WithModel(ModelVoyage35Lite),
		WithDimensions(Dimensions2048),
	)
	require.NoError(t, err)

	t.Run("ID", func(t *testing.T) {
		assert.Equal(t, "voyageai-embedding", p.ID())
	})

	t.Run("Model", func(t *testing.T) {
		assert.Equal(t, ModelVoyage35Lite, p.Model())
	})

	t.Run("EmbeddingDimensions", func(t *testing.T) {
		assert.Equal(t, Dimensions2048, p.EmbeddingDimensions())
	})

	t.Run("MaxBatchSize", func(t *testing.T) {
		assert.Equal(t, 128, p.MaxBatchSize())
	})
}

func TestEmbeddingProvider_InterfaceCompliance(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "test-key")

	p, err := NewEmbeddingProvider()
	require.NoError(t, err)

	// Verify it implements the interface
	var _ providers.EmbeddingProvider = p
}
