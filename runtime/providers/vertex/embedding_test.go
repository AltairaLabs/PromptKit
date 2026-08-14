package vertex

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// capturedRequest records what the provider sent, so the assertions are about
// the Vertex wire format rather than about a mock.
type capturedRequest struct {
	path string
	body map[string]any
}

// predictServer serves Vertex :predict responses with one embedding per
// instance, and records every request.
func predictServer(t *testing.T, values []float32, tokens int) (*httptest.Server, *[]capturedRequest) {
	t.Helper()
	var got []capturedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var body map[string]any
		require.NoError(t, json.Unmarshal(raw, &body))
		got = append(got, capturedRequest{path: r.URL.Path, body: body})

		instances, _ := body["instances"].([]any)
		preds := make([]map[string]any, 0, len(instances))
		for range instances {
			preds = append(preds, map[string]any{
				"embeddings": map[string]any{
					"values":     values,
					"statistics": map[string]any{"token_count": tokens},
				},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"predictions": preds}))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestEmbeddingProvider_SendsVertexPredictBody(t *testing.T) {
	srv, got := predictServer(t, []float32{0.1, 0.2, 0.3}, 7)

	p, err := NewEmbeddingProvider(
		WithWiring(providers.EmbeddingWiring{
			Model: "text-embedding-005", BaseURL: srv.URL, Client: srv.Client(),
		}),
	)
	require.NoError(t, err)

	resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{
		Texts: []string{"hello", "world"},
	})
	require.NoError(t, err)

	require.Len(t, *got, 1, "text-embedding models batch natively — one call for both texts")
	req := (*got)[0]

	assert.True(t, strings.HasSuffix(req.path, "/text-embedding-005:predict"),
		"the model belongs in the path with the :predict verb, got %q", req.path)

	instances, ok := req.body["instances"].([]any)
	require.True(t, ok, "Vertex expects an instances array, got %v", req.body)
	require.Len(t, instances, 2)
	first, ok := instances[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "hello", first["content"], "each instance carries its text under content")

	require.Len(t, resp.Embeddings, 2)
	assert.Equal(t, []float32{0.1, 0.2, 0.3}, resp.Embeddings[0],
		"values must be read from predictions[].embeddings.values")
	require.NotNil(t, resp.Usage)
	assert.Equal(t, 14, resp.Usage.TotalTokens, "token_count sums across predictions")
}

// task_type steers Vertex between indexing and query embeddings, and the two
// are not interchangeable for retrieval quality.
func TestEmbeddingProvider_SendsTaskType(t *testing.T) {
	srv, got := predictServer(t, []float32{1}, 1)

	p, err := NewEmbeddingProvider(
		WithWiring(providers.EmbeddingWiring{
			Model: "text-embedding-005", BaseURL: srv.URL, Client: srv.Client(),
		}),
		WithTaskType("RETRIEVAL_QUERY"),
	)
	require.NoError(t, err)

	_, err = p.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"q"}})
	require.NoError(t, err)

	instances := (*got)[0].body["instances"].([]any)
	first := instances[0].(map[string]any)
	assert.Equal(t, "RETRIEVAL_QUERY", first["task_type"])
}

// outputDimensionality is a request parameter, not a client-side truncation.
func TestEmbeddingProvider_SendsOutputDimensionality(t *testing.T) {
	srv, got := predictServer(t, []float32{1}, 1)

	p, err := NewEmbeddingProvider(
		WithWiring(providers.EmbeddingWiring{
			Model: "text-embedding-005", BaseURL: srv.URL, Client: srv.Client(), Dimensions: 256,
		}),
	)
	require.NoError(t, err)

	_, err = p.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"x"}})
	require.NoError(t, err)

	params, ok := (*got)[0].body["parameters"].(map[string]any)
	require.True(t, ok, "dimensions must travel in parameters, got %v", (*got)[0].body)
	assert.EqualValues(t, 256, params["outputDimensionality"])
	assert.Equal(t, 256, p.EmbeddingDimensions(), "the provider reports what it asked for")
}

// Without an explicit override the reported dimensionality follows the model
// family, so a caller reading GetDimensions before the first call is not lied to.
func TestNewEmbeddingProvider_FamilyDefaults(t *testing.T) {
	tests := []struct {
		model string
		dims  int
		batch int
	}{
		{"text-embedding-005", textEmbeddingDimensions, textEmbeddingMaxBatch},
		{"text-embedding-004", textEmbeddingDimensions, textEmbeddingMaxBatch},
		{"textembedding-gecko@003", textEmbeddingDimensions, textEmbeddingMaxBatch},
		{"gemini-embedding-001", geminiEmbeddingDimensions, geminiEmbeddingMaxBatch},
	}
	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			p, err := NewEmbeddingProvider(WithWiring(providers.EmbeddingWiring{Model: tc.model}))
			require.NoError(t, err)
			assert.Equal(t, tc.dims, p.EmbeddingDimensions())
			assert.Equal(t, tc.batch, p.MaxBatchSize())
		})
	}
}

// An unknown model is rejected at construction rather than producing a request
// the endpoint rejects later.
func TestNewEmbeddingProvider_RejectsUnknownModel(t *testing.T) {
	_, err := NewEmbeddingProvider(WithWiring(providers.EmbeddingWiring{Model: "gpt-4o"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported vertex embedding model")
}

// A response with fewer predictions than instances is a protocol violation, and
// silently returning short results would corrupt a vector index.
func TestEmbeddingProvider_RejectsPredictionCountMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"predictions":[{"embeddings":{"values":[1,2]}}]}`))
	}))
	defer srv.Close()

	p, err := NewEmbeddingProvider(
		WithWiring(providers.EmbeddingWiring{
			Model: "text-embedding-005", BaseURL: srv.URL, Client: srv.Client(),
		}))
	require.NoError(t, err)

	_, err = p.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"a", "b"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned 1 embeddings for 2 texts")
}

// A per-request model override must reach the URL: the base URL is the models
// collection, not a single model, which is what makes the override possible.
func TestEmbeddingProvider_PerRequestModelOverride(t *testing.T) {
	srv, got := predictServer(t, []float32{1}, 1)

	p, err := NewEmbeddingProvider(
		WithWiring(providers.EmbeddingWiring{
			Model: "text-embedding-005", BaseURL: srv.URL, Client: srv.Client(),
		}))
	require.NoError(t, err)

	_, err = p.Embed(context.Background(), providers.EmbeddingRequest{
		Texts: []string{"x"}, Model: "text-embedding-004",
	})
	require.NoError(t, err)

	assert.True(t, strings.HasSuffix((*got)[0].path, "/text-embedding-004:predict"),
		"got %q", (*got)[0].path)
}
