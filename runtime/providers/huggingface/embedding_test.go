package huggingface_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/huggingface"
)

// hfEmbedServer answers feature-extraction requests with one vector of the
// given size per input, where every element is the input's index — so order
// can be checked. It records the paths and batch sizes it saw.
type hfEmbedServer struct {
	mu      sync.Mutex
	paths   []string
	batches []int
	size    int
	body    string // when set, returned verbatim instead
}

func (s *hfEmbedServer) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Inputs []string `json:"inputs"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.Path)
		offset := 0
		for _, b := range s.batches {
			offset += b
		}
		s.batches = append(s.batches, len(req.Inputs))
		s.mu.Unlock()
		if s.body != "" {
			_, _ = w.Write([]byte(s.body))
			return
		}
		out := make([][]float32, len(req.Inputs))
		for i := range out {
			v := make([]float32, s.size)
			for j := range v {
				v[j] = float32(offset + i)
			}
			out[i] = v
		}
		_ = json.NewEncoder(w).Encode(out)
	})
}

func newHFEmbedder(t *testing.T, srv *httptest.Server, extra map[string]any) providers.EmbeddingProvider {
	t.Helper()
	p, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type:             "huggingface",
		Model:            "sentence-transformers/all-MiniLM-L6-v2",
		BaseURL:          srv.URL,
		Credential:       credentials.NewAPIKeyCredential("hf_test"),
		AdditionalConfig: extra,
	})
	require.NoError(t, err)
	return p
}

func texts(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "text"
	}
	return out
}

// The bare /models/{id} route lets HF pick the model's default pipeline, and
// for sentence-transformers models that is sentence-similarity, which answers
// a plain input list with HTTP 400. The feature-extraction pipeline is pinned.
func TestEmbed_UsesTheFeatureExtractionPipeline(t *testing.T) {
	s := &hfEmbedServer{size: 384}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()

	resp, err := newHFEmbedder(t, srv, nil).Embed(context.Background(),
		providers.EmbeddingRequest{Texts: []string{"hello", "world"}})

	require.NoError(t, err)
	assert.Len(t, resp.Embeddings, 2)
	assert.Equal(t, []string{"/models/sentence-transformers/all-MiniLM-L6-v2/pipeline/feature-extraction"}, s.paths)
}

func TestEmbed_SplitsLargeRequestsAndKeepsOrder(t *testing.T) {
	s := &hfEmbedServer{size: 4}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()

	resp, err := newHFEmbedder(t, srv, nil).Embed(context.Background(), providers.EmbeddingRequest{Texts: texts(70)})

	require.NoError(t, err)
	assert.Equal(t, []int{32, 32, 6}, s.batches)
	require.Len(t, resp.Embeddings, 70)
	for i, v := range resp.Embeddings {
		assert.InDelta(t, float32(i), v[0], 0, "vector %d is out of order", i)
	}
}

func TestEmbed_SizeIsObservedNotGuessed(t *testing.T) {
	s := &hfEmbedServer{size: 384}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()
	p := newHFEmbedder(t, srv, nil)

	assert.Equal(t, 0, p.EmbeddingDimensions(), "no size may be reported before one is known")
	_, err := p.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"hello"}})
	require.NoError(t, err)
	assert.Equal(t, 384, p.EmbeddingDimensions())
}

func TestEmbed_DeclaredSizeTheModelDoesNotProduceFails(t *testing.T) {
	s := &hfEmbedServer{size: 384}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()

	_, err := newHFEmbedder(t, srv, map[string]any{"dimensions": 256}).Embed(context.Background(),
		providers.EmbeddingRequest{Texts: []string{"hello"}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned 384-dimension embeddings, expected 256")
}

func TestEmbed_PerTokenOutputIsAnError(t *testing.T) {
	s := &hfEmbedServer{body: `[[[0.1,0.2],[0.3,0.4]]]`}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()

	_, err := newHFEmbedder(t, srv, nil).Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"hi"}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "sentence-transformers/all-MiniLM-L6-v2")
}

func TestEmbed_DedicatedEndpointIsPostedToDirectly(t *testing.T) {
	s := &hfEmbedServer{size: 8}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()

	_, err := newHFEmbedder(t, srv, map[string]any{"dedicated": true}).Embed(context.Background(),
		providers.EmbeddingRequest{Texts: []string{"hello"}})

	require.NoError(t, err)
	assert.Equal(t, []string{"/"}, s.paths)
}

func TestFactory_RouterWithoutATokenFails(t *testing.T) {
	t.Setenv("HF_TOKEN", "")
	t.Setenv("HUGGING_FACE_HUB_TOKEN", "")

	_, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{Type: "huggingface"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "HF_TOKEN")
}
