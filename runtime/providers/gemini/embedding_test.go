package gemini

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

func TestNewGeminiEmbeddingProvider(t *testing.T) {
	t.Run("creates provider with defaults", func(t *testing.T) {
		p, err := NewEmbeddingProvider(
			WithGeminiEmbeddingAPIKey("test-key"),
		)
		require.NoError(t, err)

		assert.Equal(t, DefaultGeminiEmbeddingModel, p.Model())
		assert.Equal(t, geminiEmbeddingBaseURL, p.BaseURL)
		assert.Equal(t, dimensionsEmbedding004, p.EmbeddingDimensions())
		assert.Equal(t, "gemini-embedding", p.ID())
	})

	t.Run("applies options", func(t *testing.T) {
		p, err := NewEmbeddingProvider(
			WithGeminiEmbeddingAPIKey("test-key"),
			WithGeminiEmbeddingModel(EmbeddingModel001),
			WithGeminiEmbeddingBaseURL("https://custom.api.com"),
		)
		require.NoError(t, err)

		assert.Equal(t, EmbeddingModel001, p.Model())
		assert.Equal(t, "https://custom.api.com", p.BaseURL)
		assert.Equal(t, dimensionsEmbedding001, p.EmbeddingDimensions())
	})

	t.Run("fails without API key", func(t *testing.T) {
		t.Setenv("GEMINI_API_KEY", "")
		t.Setenv("GOOGLE_API_KEY", "")

		_, err := NewEmbeddingProvider()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "API key not found")
	})
}

func TestGeminiEmbeddingProvider_Embed(t *testing.T) {
	t.Run("embeds single text", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "embedContent")
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			var req geminiEmbedRequest
			err := json.NewDecoder(r.Body).Decode(&req)
			require.NoError(t, err)
			assert.Equal(t, 1, len(req.Content.Parts))
			assert.Equal(t, "hello world", req.Content.Parts[0].Text)

			resp := geminiEmbedResponse{
				Embedding: &geminiEmbeddingData{
					Values: []float32{0.1, 0.2, 0.3},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithGeminiEmbeddingAPIKey("test-key"),
			WithGeminiEmbeddingBaseURL(server.URL),
		)
		require.NoError(t, err)

		resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"hello world"},
		})
		require.NoError(t, err)

		assert.Len(t, resp.Embeddings, 1)
		assert.Equal(t, []float32{0.1, 0.2, 0.3}, resp.Embeddings[0])
		assert.Equal(t, DefaultGeminiEmbeddingModel, resp.Model)
	})

	t.Run("embeds multiple texts with batch endpoint", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "batchEmbedContents")

			var req geminiBatchEmbedRequest
			json.NewDecoder(r.Body).Decode(&req)
			assert.Equal(t, 3, len(req.Requests))

			resp := geminiBatchEmbedResponse{
				Embeddings: []geminiEmbeddingData{
					{Values: []float32{0.1, 0.2}},
					{Values: []float32{0.3, 0.4}},
					{Values: []float32{0.5, 0.6}},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithGeminiEmbeddingAPIKey("test-key"),
			WithGeminiEmbeddingBaseURL(server.URL),
		)
		require.NoError(t, err)

		resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"one", "two", "three"},
		})
		require.NoError(t, err)

		assert.Len(t, resp.Embeddings, 3)
		assert.Equal(t, []float32{0.1, 0.2}, resp.Embeddings[0])
		assert.Equal(t, []float32{0.3, 0.4}, resp.Embeddings[1])
		assert.Equal(t, []float32{0.5, 0.6}, resp.Embeddings[2])
	})

	t.Run("handles empty input", func(t *testing.T) {
		p, err := NewEmbeddingProvider(
			WithGeminiEmbeddingAPIKey("test-key"),
		)
		require.NoError(t, err)

		resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{},
		})
		require.NoError(t, err)

		assert.Len(t, resp.Embeddings, 0)
	})

	t.Run("uses model override", func(t *testing.T) {
		var receivedModel string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req geminiEmbedRequest
			json.NewDecoder(r.Body).Decode(&req)
			receivedModel = req.Model

			resp := geminiEmbedResponse{
				Embedding: &geminiEmbeddingData{Values: []float32{0.1}},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithGeminiEmbeddingAPIKey("test-key"),
			WithGeminiEmbeddingBaseURL(server.URL),
			WithGeminiEmbeddingModel(EmbeddingModel004),
		)
		require.NoError(t, err)

		_, err = p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"test"},
			Model: EmbeddingModel001, // Override
		})
		require.NoError(t, err)

		assert.Contains(t, receivedModel, EmbeddingModel001)
	})

	t.Run("handles API error response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			resp := geminiEmbedResponse{
				Error: &geminiEmbedError{
					Code:    400,
					Message: "Invalid input",
					Status:  "INVALID_ARGUMENT",
				},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithGeminiEmbeddingAPIKey("test-key"),
			WithGeminiEmbeddingBaseURL(server.URL),
		)
		require.NoError(t, err)

		_, err = p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"test"},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "status 400")
	})

	t.Run("handles error in response body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			resp := geminiEmbedResponse{
				Error: &geminiEmbedError{
					Code:    429,
					Message: "Rate limit exceeded",
					Status:  "RESOURCE_EXHAUSTED",
				},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithGeminiEmbeddingAPIKey("test-key"),
			WithGeminiEmbeddingBaseURL(server.URL),
		)
		require.NoError(t, err)

		_, err = p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"test"},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Rate limit exceeded")
	})

	t.Run("handles missing embedding in response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := geminiEmbedResponse{
				// No embedding field
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithGeminiEmbeddingAPIKey("test-key"),
			WithGeminiEmbeddingBaseURL(server.URL),
		)
		require.NoError(t, err)

		_, err = p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"test"},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no embedding in response")
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithGeminiEmbeddingAPIKey("test-key"),
			WithGeminiEmbeddingBaseURL(server.URL),
		)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err = p.Embed(ctx, providers.EmbeddingRequest{
			Texts: []string{"test"},
		})
		assert.Error(t, err)
	})

	t.Run("handles batch count mismatch", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Return wrong number of embeddings
			resp := geminiBatchEmbedResponse{
				Embeddings: []geminiEmbeddingData{
					{Values: []float32{0.1}},
					// Missing second embedding
				},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithGeminiEmbeddingAPIKey("test-key"),
			WithGeminiEmbeddingBaseURL(server.URL),
		)
		require.NoError(t, err)

		_, err = p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"one", "two", "three"},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected 3 embeddings")
	})
}

func TestGeminiEmbeddingProvider_Batching(t *testing.T) {
	t.Run("batches large requests", func(t *testing.T) {
		batchCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			batchCount++
			var req geminiBatchEmbedRequest
			json.NewDecoder(r.Body).Decode(&req)

			embeddings := make([]geminiEmbeddingData, len(req.Requests))
			for i := range req.Requests {
				embeddings[i] = geminiEmbeddingData{
					Values: []float32{float32(batchCount), float32(i)},
				}
			}

			resp := geminiBatchEmbedResponse{Embeddings: embeddings}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithGeminiEmbeddingAPIKey("test-key"),
			WithGeminiEmbeddingBaseURL(server.URL),
		)
		require.NoError(t, err)

		// Create more texts than the batch limit (100)
		texts := make([]string, 105)
		for i := range texts {
			texts[i] = "text"
		}

		resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: texts,
		})
		require.NoError(t, err)

		// Should have made 2 API calls (100 + 5)
		assert.Equal(t, 2, batchCount)
		assert.Len(t, resp.Embeddings, 105)
	})

	t.Run("handles batch error", func(t *testing.T) {
		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount == 2 {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Internal error"))
				return
			}

			var req geminiBatchEmbedRequest
			json.NewDecoder(r.Body).Decode(&req)

			embeddings := make([]geminiEmbeddingData, len(req.Requests))
			for i := range req.Requests {
				embeddings[i] = geminiEmbeddingData{Values: []float32{0.1}}
			}

			resp := geminiBatchEmbedResponse{Embeddings: embeddings}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithGeminiEmbeddingAPIKey("test-key"),
			WithGeminiEmbeddingBaseURL(server.URL),
		)
		require.NoError(t, err)

		texts := make([]string, 105)
		for i := range texts {
			texts[i] = "text"
		}

		_, err = p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: texts,
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "batch 1 failed")
	})
}

func TestGeminiEmbeddingProvider_EmbeddingDimensions(t *testing.T) {
	tests := []struct {
		model    string
		expected int
	}{
		{EmbeddingModel001, 768},
		{EmbeddingModel004, 768},
		{"unknown-model", 768},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p, err := NewEmbeddingProvider(
				WithGeminiEmbeddingAPIKey("test-key"),
				WithGeminiEmbeddingModel(tt.model),
			)
			require.NoError(t, err)

			assert.Equal(t, tt.expected, p.EmbeddingDimensions())
		})
	}
}

func TestGeminiEmbeddingProvider_MaxBatchSize(t *testing.T) {
	p, err := NewEmbeddingProvider(
		WithGeminiEmbeddingAPIKey("test-key"),
	)
	require.NoError(t, err)

	assert.Equal(t, 100, p.MaxBatchSize())
}

func TestGeminiEmbeddingProvider_EstimateCost(t *testing.T) {
	p, err := NewEmbeddingProvider(
		WithGeminiEmbeddingAPIKey("test-key"),
	)
	require.NoError(t, err)

	// Gemini embeddings are currently free
	cost := p.EstimateCost(1_000_000)
	assert.Equal(t, 0.0, cost)
}

func TestGeminiEmbeddingProvider_Model(t *testing.T) {
	p, err := NewEmbeddingProvider(
		WithGeminiEmbeddingAPIKey("test-key"),
		WithGeminiEmbeddingModel(EmbeddingModel001),
	)
	require.NoError(t, err)

	assert.Equal(t, EmbeddingModel001, p.Model())
}

func TestGeminiEmbeddingProvider_WithHTTPClient(t *testing.T) {
	customClient := &http.Client{}
	p, err := NewEmbeddingProvider(
		WithGeminiEmbeddingAPIKey("test-key"),
		WithGeminiEmbeddingHTTPClient(customClient),
	)
	require.NoError(t, err)

	assert.Equal(t, customClient, p.HTTPClient)
}

func TestGeminiDimensionsForModel(t *testing.T) {
	tests := []struct {
		model    string
		expected int
	}{
		{EmbeddingModel001, 768},
		{EmbeddingModel004, 768},
		{"custom-model", 768},
		{"", 768},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			assert.Equal(t, tt.expected, geminiDimensionsForModel(tt.model))
		})
	}
}

// Verify interface compliance
var _ providers.EmbeddingProvider = (*EmbeddingProvider)(nil)
