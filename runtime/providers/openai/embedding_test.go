package openai

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
		p, err := NewEmbeddingProvider(
			WithEmbeddingAPIKey("test-key"),
		)
		require.NoError(t, err)

		assert.Equal(t, DefaultEmbeddingModel, p.Model())
		assert.Equal(t, "https://api.openai.com/v1", p.BaseURL)
		assert.Equal(t, dimensions3Small, p.EmbeddingDimensions())
		assert.Equal(t, "openai-embedding", p.ID())
	})

	t.Run("applies options", func(t *testing.T) {
		p, err := NewEmbeddingProvider(
			WithEmbeddingAPIKey("test-key"),
			WithEmbeddingModel(EmbeddingModel3Large),
			WithEmbeddingBaseURL("https://custom.api.com"),
		)
		require.NoError(t, err)

		assert.Equal(t, EmbeddingModel3Large, p.Model())
		assert.Equal(t, "https://custom.api.com", p.BaseURL)
		assert.Equal(t, dimensions3Large, p.EmbeddingDimensions())
	})

	t.Run("fails without API key", func(t *testing.T) {
		// Clear env vars for this test
		t.Setenv("OPENAI_API_KEY", "")
		t.Setenv("OPENAI_TOKEN", "")

		_, err := NewEmbeddingProvider()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "API key not found")
	})
}

func TestEmbeddingProvider_Embed(t *testing.T) {
	t.Run("embeds single text", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/embeddings", r.URL.Path)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			assert.Contains(t, r.Header.Get("Authorization"), "Bearer ")

			var req embeddingRequest
			err := json.NewDecoder(r.Body).Decode(&req)
			require.NoError(t, err)
			assert.Equal(t, 1, len(req.Input))
			assert.Equal(t, "hello world", req.Input[0])

			resp := embeddingResponse{
				Object: "list",
				Data: []embeddingData{
					{
						Object:    "embedding",
						Index:     0,
						Embedding: []float32{0.1, 0.2, 0.3},
					},
				},
				Model: "text-embedding-3-small",
				Usage: embeddingUsage{
					PromptTokens: 2,
					TotalTokens:  2,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithEmbeddingAPIKey("test-key"),
			WithEmbeddingBaseURL(server.URL),
		)
		require.NoError(t, err)

		resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"hello world"},
		})
		require.NoError(t, err)

		assert.Len(t, resp.Embeddings, 1)
		assert.Equal(t, []float32{0.1, 0.2, 0.3}, resp.Embeddings[0])
		assert.Equal(t, "text-embedding-3-small", resp.Model)
		assert.Equal(t, 2, resp.Usage.TotalTokens)
	})

	t.Run("embeds multiple texts", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req embeddingRequest
			json.NewDecoder(r.Body).Decode(&req)

			resp := embeddingResponse{
				Object: "list",
				Data: []embeddingData{
					{Index: 0, Embedding: []float32{0.1, 0.2}},
					{Index: 1, Embedding: []float32{0.3, 0.4}},
					{Index: 2, Embedding: []float32{0.5, 0.6}},
				},
				Model: "text-embedding-3-small",
				Usage: embeddingUsage{TotalTokens: 10},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithEmbeddingAPIKey("test-key"),
			WithEmbeddingBaseURL(server.URL),
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
			WithEmbeddingAPIKey("test-key"),
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
			var req embeddingRequest
			json.NewDecoder(r.Body).Decode(&req)
			receivedModel = req.Model

			resp := embeddingResponse{
				Data:  []embeddingData{{Index: 0, Embedding: []float32{0.1}}},
				Model: req.Model,
				Usage: embeddingUsage{TotalTokens: 1},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithEmbeddingAPIKey("test-key"),
			WithEmbeddingBaseURL(server.URL),
			WithEmbeddingModel(EmbeddingModel3Small),
		)
		require.NoError(t, err)

		_, err = p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"test"},
			Model: EmbeddingModel3Large, // Override
		})
		require.NoError(t, err)

		assert.Equal(t, EmbeddingModel3Large, receivedModel)
	})

	t.Run("handles API error response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			resp := embeddingResponse{
				Error: &embeddingAPIError{
					Message: "Invalid input",
					Type:    "invalid_request_error",
				},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithEmbeddingAPIKey("test-key"),
			WithEmbeddingBaseURL(server.URL),
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
			resp := embeddingResponse{
				Error: &embeddingAPIError{
					Message: "Rate limit exceeded",
					Type:    "rate_limit_error",
				},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithEmbeddingAPIKey("test-key"),
			WithEmbeddingBaseURL(server.URL),
		)
		require.NoError(t, err)

		_, err = p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"test"},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Rate limit exceeded")
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Delay to allow context cancellation
			<-r.Context().Done()
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithEmbeddingAPIKey("test-key"),
			WithEmbeddingBaseURL(server.URL),
		)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err = p.Embed(ctx, providers.EmbeddingRequest{
			Texts: []string{"test"},
		})
		assert.Error(t, err)
	})

	t.Run("preserves order with out-of-order response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Return embeddings in reverse order
			resp := embeddingResponse{
				Data: []embeddingData{
					{Index: 2, Embedding: []float32{0.5, 0.6}},
					{Index: 0, Embedding: []float32{0.1, 0.2}},
					{Index: 1, Embedding: []float32{0.3, 0.4}},
				},
				Model: "text-embedding-3-small",
				Usage: embeddingUsage{TotalTokens: 6},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithEmbeddingAPIKey("test-key"),
			WithEmbeddingBaseURL(server.URL),
		)
		require.NoError(t, err)

		resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: []string{"a", "b", "c"},
		})
		require.NoError(t, err)

		// Should be reordered correctly
		assert.Equal(t, []float32{0.1, 0.2}, resp.Embeddings[0])
		assert.Equal(t, []float32{0.3, 0.4}, resp.Embeddings[1])
		assert.Equal(t, []float32{0.5, 0.6}, resp.Embeddings[2])
	})
}

func TestEmbeddingProvider_EmbeddingDimensions(t *testing.T) {
	tests := []struct {
		model    string
		expected int
	}{
		{EmbeddingModelAda002, 1536},
		{EmbeddingModel3Small, 1536},
		{EmbeddingModel3Large, 3072},
		{"unknown-model", 1536}, // Default
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p, err := NewEmbeddingProvider(
				WithEmbeddingAPIKey("test-key"),
				WithEmbeddingModel(tt.model),
			)
			require.NoError(t, err)

			assert.Equal(t, tt.expected, p.EmbeddingDimensions())
		})
	}
}

func TestEmbeddingProvider_MaxBatchSize(t *testing.T) {
	p, err := NewEmbeddingProvider(
		WithEmbeddingAPIKey("test-key"),
	)
	require.NoError(t, err)

	assert.Equal(t, 2048, p.MaxBatchSize())
}

func TestEmbeddingProvider_EstimateCost(t *testing.T) {
	tests := []struct {
		model    string
		tokens   int
		expected float64
	}{
		{EmbeddingModelAda002, 1_000_000, 0.10},
		{EmbeddingModel3Small, 1_000_000, 0.02},
		{EmbeddingModel3Large, 1_000_000, 0.13},
		{EmbeddingModel3Small, 100_000, 0.002},
		{EmbeddingModel3Small, 1000, 0.00002},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p, err := NewEmbeddingProvider(
				WithEmbeddingAPIKey("test-key"),
				WithEmbeddingModel(tt.model),
			)
			require.NoError(t, err)

			cost := p.EstimateCost(tt.tokens)
			assert.InDelta(t, tt.expected, cost, 0.0001)
		})
	}
}

func TestEmbeddingProvider_Model(t *testing.T) {
	p, err := NewEmbeddingProvider(
		WithEmbeddingAPIKey("test-key"),
		WithEmbeddingModel(EmbeddingModel3Large),
	)
	require.NoError(t, err)

	assert.Equal(t, EmbeddingModel3Large, p.Model())
}

func TestEmbeddingProvider_WithHTTPClient(t *testing.T) {
	customClient := &http.Client{}
	p, err := NewEmbeddingProvider(
		WithEmbeddingAPIKey("test-key"),
		WithEmbeddingHTTPClient(customClient),
	)
	require.NoError(t, err)

	assert.Equal(t, customClient, p.HTTPClient)
}

func TestDimensionsForModel(t *testing.T) {
	tests := []struct {
		model    string
		expected int
	}{
		{EmbeddingModelAda002, 1536},
		{EmbeddingModel3Small, 1536},
		{EmbeddingModel3Large, 3072},
		{"custom-model", 1536}, // Defaults to 3-small
		{"", 1536},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			assert.Equal(t, tt.expected, dimensionsForModel(tt.model))
		})
	}
}

func TestEmbeddingProvider_Batching(t *testing.T) {
	t.Run("batches large requests", func(t *testing.T) {
		batchCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			batchCount++
			var req embeddingRequest
			json.NewDecoder(r.Body).Decode(&req)

			// Generate embeddings for this batch
			data := make([]embeddingData, len(req.Input))
			for i := range req.Input {
				data[i] = embeddingData{
					Index:     i,
					Embedding: []float32{float32(batchCount), float32(i)},
				}
			}

			resp := embeddingResponse{
				Data:  data,
				Model: req.Model,
				Usage: embeddingUsage{TotalTokens: len(req.Input) * 5},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithEmbeddingAPIKey("test-key"),
			WithEmbeddingBaseURL(server.URL),
		)
		require.NoError(t, err)

		// Create more texts than the batch limit (2048)
		// We'll use a smaller number for testing but verify batching works
		texts := make([]string, 2050) // Just over the limit
		for i := range texts {
			texts[i] = "text"
		}

		resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{
			Texts: texts,
		})
		require.NoError(t, err)

		// Should have made 2 API calls (2048 + 2)
		assert.Equal(t, 2, batchCount)
		assert.Len(t, resp.Embeddings, 2050)

		// Verify tokens were accumulated
		assert.NotNil(t, resp.Usage)
		assert.Equal(t, 2050*5, resp.Usage.TotalTokens)
	})

	t.Run("handles batch error", func(t *testing.T) {
		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount == 2 {
				// Second batch fails
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Internal error"))
				return
			}

			var req embeddingRequest
			json.NewDecoder(r.Body).Decode(&req)

			data := make([]embeddingData, len(req.Input))
			for i := range req.Input {
				data[i] = embeddingData{Index: i, Embedding: []float32{0.1}}
			}

			resp := embeddingResponse{
				Data:  data,
				Model: "test",
				Usage: embeddingUsage{TotalTokens: 10},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, err := NewEmbeddingProvider(
			WithEmbeddingAPIKey("test-key"),
			WithEmbeddingBaseURL(server.URL),
		)
		require.NoError(t, err)

		texts := make([]string, 2050)
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

// Verify interface compliance
var _ providers.EmbeddingProvider = (*EmbeddingProvider)(nil)
