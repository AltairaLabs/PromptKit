package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBaseEmbeddingProvider(t *testing.T) {
	p := NewBaseEmbeddingProvider(
		"test-provider",
		"test-model",
		"https://api.test.com",
		1024,
		100,
		30*time.Second,
	)

	assert.Equal(t, "test-provider", p.ID())
	assert.Equal(t, "test-model", p.Model())
	assert.Equal(t, "https://api.test.com", p.BaseURL)
	assert.Equal(t, 1024, p.EmbeddingDimensions())
	assert.Equal(t, 100, p.MaxBatchSize())
	assert.NotNil(t, p.HTTPClient)
}

func TestBaseEmbeddingProvider_EmptyResponseForModel(t *testing.T) {
	p := NewBaseEmbeddingProvider("test", "default-model", "", 1024, 100, time.Second)

	t.Run("uses provided model", func(t *testing.T) {
		resp := p.EmptyResponseForModel("custom-model")
		assert.Empty(t, resp.Embeddings)
		assert.Equal(t, "custom-model", resp.Model)
	})

	t.Run("uses default model when empty", func(t *testing.T) {
		resp := p.EmptyResponseForModel("")
		assert.Empty(t, resp.Embeddings)
		assert.Equal(t, "default-model", resp.Model)
	})
}

func TestBaseEmbeddingProvider_ResolveModel(t *testing.T) {
	p := NewBaseEmbeddingProvider("test", "default-model", "", 1024, 100, time.Second)

	t.Run("returns request model when provided", func(t *testing.T) {
		model := p.ResolveModel("custom-model")
		assert.Equal(t, "custom-model", model)
	})

	t.Run("returns default model when empty", func(t *testing.T) {
		model := p.ResolveModel("")
		assert.Equal(t, "default-model", model)
	})
}

func TestBaseEmbeddingProvider_HandleEmptyRequest(t *testing.T) {
	p := NewBaseEmbeddingProvider("test", "test-model", "", 1024, 100, time.Second)

	t.Run("returns empty response for empty texts", func(t *testing.T) {
		resp, isEmpty := p.HandleEmptyRequest(EmbeddingRequest{Texts: []string{}})
		assert.True(t, isEmpty)
		assert.Empty(t, resp.Embeddings)
		assert.Equal(t, "test-model", resp.Model)
	})

	t.Run("returns false for non-empty texts", func(t *testing.T) {
		_, isEmpty := p.HandleEmptyRequest(EmbeddingRequest{Texts: []string{"hello"}})
		assert.False(t, isEmpty)
	})
}

func TestBaseEmbeddingProvider_EmbedWithEmptyCheck(t *testing.T) {
	p := NewBaseEmbeddingProvider("test", "test-model", "", 1024, 100, time.Second)
	ctx := context.Background()

	t.Run("handles empty request", func(t *testing.T) {
		called := false
		embedFn := func(_ context.Context, _ []string, _ string) (EmbeddingResponse, error) {
			called = true
			return EmbeddingResponse{}, nil
		}

		resp, err := p.EmbedWithEmptyCheck(ctx, EmbeddingRequest{Texts: []string{}}, embedFn)
		assert.NoError(t, err)
		assert.Empty(t, resp.Embeddings)
		assert.False(t, called)
	})

	t.Run("calls embed function for non-empty request", func(t *testing.T) {
		called := false
		expectedTexts := []string{"hello", "world"}
		embedFn := func(_ context.Context, texts []string, model string) (EmbeddingResponse, error) {
			called = true
			assert.Equal(t, expectedTexts, texts)
			assert.Equal(t, "test-model", model)
			return EmbeddingResponse{
				Embeddings: [][]float32{{0.1, 0.2}, {0.3, 0.4}},
				Model:      model,
			}, nil
		}

		resp, err := p.EmbedWithEmptyCheck(ctx, EmbeddingRequest{Texts: expectedTexts}, embedFn)
		assert.NoError(t, err)
		assert.True(t, called)
		assert.Len(t, resp.Embeddings, 2)
	})

	t.Run("uses request model when provided", func(t *testing.T) {
		embedFn := func(_ context.Context, _ []string, model string) (EmbeddingResponse, error) {
			assert.Equal(t, "custom-model", model)
			return EmbeddingResponse{Model: model}, nil
		}

		resp, err := p.EmbedWithEmptyCheck(ctx, EmbeddingRequest{
			Texts: []string{"test"},
			Model: "custom-model",
		}, embedFn)
		assert.NoError(t, err)
		assert.Equal(t, "custom-model", resp.Model)
	})
}

func TestBaseEmbeddingProvider_DoEmbeddingRequest(t *testing.T) {
	t.Run("successful request with API key", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, ApplicationJSON, r.Header.Get(ContentTypeHeader))
			assert.Equal(t, BearerPrefix+"test-api-key", r.Header.Get(AuthorizationHeader))

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}))
		defer server.Close()

		p := NewBaseEmbeddingProvider("test", "model", server.URL, 1024, 100, time.Second)
		p.APIKey = "test-api-key"

		body, err := p.DoEmbeddingRequest(context.Background(), HTTPRequestConfig{
			URL:       server.URL,
			Body:      []byte(`{"input": "test"}`),
			UseAPIKey: true,
		})
		require.NoError(t, err)
		assert.Contains(t, string(body), "ok")
	})

	t.Run("successful request without API key", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Empty(t, r.Header.Get(AuthorizationHeader))
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"result": "success"}`))
		}))
		defer server.Close()

		p := NewBaseEmbeddingProvider("test", "model", server.URL, 1024, 100, time.Second)
		p.APIKey = "key"

		body, err := p.DoEmbeddingRequest(context.Background(), HTTPRequestConfig{
			URL:       server.URL,
			Body:      []byte(`{}`),
			UseAPIKey: false,
		})
		require.NoError(t, err)
		assert.Contains(t, string(body), "success")
	})

	t.Run("custom content type", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "text/plain", r.Header.Get(ContentTypeHeader))
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		p := NewBaseEmbeddingProvider("test", "model", server.URL, 1024, 100, time.Second)

		_, err := p.DoEmbeddingRequest(context.Background(), HTTPRequestConfig{
			URL:         server.URL,
			Body:        []byte(`test`),
			ContentType: "text/plain",
		})
		require.NoError(t, err)
	})

	t.Run("handles HTTP error status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error": "bad request"}`))
		}))
		defer server.Close()

		p := NewBaseEmbeddingProvider("test", "model", server.URL, 1024, 100, time.Second)

		_, err := p.DoEmbeddingRequest(context.Background(), HTTPRequestConfig{
			URL:  server.URL,
			Body: []byte(`{}`),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 400")
		assert.Contains(t, err.Error(), "bad request")
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		defer server.Close()

		p := NewBaseEmbeddingProvider("test", "model", server.URL, 1024, 100, time.Second)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := p.DoEmbeddingRequest(ctx, HTTPRequestConfig{
			URL:  server.URL,
			Body: []byte(`{}`),
		})
		require.Error(t, err)
	})
}

func TestExtractOrderedEmbeddings(t *testing.T) {
	type testData struct {
		Index     int
		Embedding []float32
	}

	t.Run("extracts embeddings in correct order", func(t *testing.T) {
		data := []testData{
			{Index: 1, Embedding: []float32{0.3, 0.4}},
			{Index: 0, Embedding: []float32{0.1, 0.2}},
			{Index: 2, Embedding: []float32{0.5, 0.6}},
		}

		embeddings, err := ExtractOrderedEmbeddings(
			data,
			func(d testData) int { return d.Index },
			func(d testData) []float32 { return d.Embedding },
			3,
		)
		require.NoError(t, err)

		assert.Equal(t, []float32{0.1, 0.2}, embeddings[0])
		assert.Equal(t, []float32{0.3, 0.4}, embeddings[1])
		assert.Equal(t, []float32{0.5, 0.6}, embeddings[2])
	})

	t.Run("handles empty data", func(t *testing.T) {
		embeddings, err := ExtractOrderedEmbeddings(
			[]testData{},
			func(d testData) int { return d.Index },
			func(d testData) []float32 { return d.Embedding },
			0,
		)
		require.NoError(t, err)
		assert.Empty(t, embeddings)
	})

	t.Run("ignores out of bounds indices", func(t *testing.T) {
		data := []testData{
			{Index: 0, Embedding: []float32{0.1}},
			{Index: 5, Embedding: []float32{0.5}},  // out of bounds
			{Index: -1, Embedding: []float32{0.0}}, // negative
		}

		embeddings, err := ExtractOrderedEmbeddings(
			data,
			func(d testData) int { return d.Index },
			func(d testData) []float32 { return d.Embedding },
			2,
		)
		require.NoError(t, err)

		assert.Equal(t, []float32{0.1}, embeddings[0])
		assert.Nil(t, embeddings[1]) // Not filled
	})
}

func TestHTTPConstants(t *testing.T) {
	assert.Equal(t, "Content-Type", ContentTypeHeader)
	assert.Equal(t, "Authorization", AuthorizationHeader)
	assert.Equal(t, "application/json", ApplicationJSON)
	assert.Equal(t, "Bearer ", BearerPrefix)
}

func TestMarshalRequest(t *testing.T) {
	t.Run("marshals valid struct", func(t *testing.T) {
		req := struct {
			Name string `json:"name"`
		}{Name: "test"}

		body, err := MarshalRequest(req)
		require.NoError(t, err)
		assert.Contains(t, string(body), "test")
	})

	t.Run("handles unmarshalable type", func(t *testing.T) {
		// Channels can't be marshaled
		req := make(chan int)
		_, err := MarshalRequest(req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to marshal request")
	})
}

func TestUnmarshalResponse(t *testing.T) {
	t.Run("unmarshals valid JSON", func(t *testing.T) {
		body := []byte(`{"name": "test", "value": 42}`)
		var resp struct {
			Name  string `json:"name"`
			Value int    `json:"value"`
		}

		err := UnmarshalResponse(body, &resp)
		require.NoError(t, err)
		assert.Equal(t, "test", resp.Name)
		assert.Equal(t, 42, resp.Value)
	})

	t.Run("handles invalid JSON", func(t *testing.T) {
		body := []byte(`{invalid json}`)
		var resp map[string]interface{}

		err := UnmarshalResponse(body, &resp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal response")
	})
}

func TestLogEmbeddingRequest(t *testing.T) {
	// Just verify it doesn't panic
	start := time.Now()
	LogEmbeddingRequest("Test", "model-v1", 5, start)
}

func TestLogEmbeddingRequestWithTokens(t *testing.T) {
	// Just verify it doesn't panic
	start := time.Now()
	LogEmbeddingRequestWithTokens("Test", "model-v1", 5, 100, start)
}
