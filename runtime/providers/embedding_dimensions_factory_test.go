package providers_test

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

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/ollama"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/voyageai"
)

// dimsWireCase describes how one provider type carries a requested size on
// the wire and shapes its response, so a single fake server serves them all.
type dimsWireCase struct {
	typ      string
	dimField string // request field that carries a declared size
	respond  func(vectors [][]float32) any
}

func vectorResponseData(vectors [][]float32) []map[string]any {
	data := make([]map[string]any, len(vectors))
	for i, v := range vectors {
		data[i] = map[string]any{"embedding": v, "index": i}
	}
	return data
}

var dimsWireCases = []dimsWireCase{
	{typ: "openai", dimField: "dimensions", respond: func(v [][]float32) any {
		return map[string]any{"data": vectorResponseData(v)}
	}},
	{typ: "voyageai", dimField: "output_dimension", respond: func(v [][]float32) any {
		return map[string]any{"data": vectorResponseData(v)}
	}},
	{typ: "ollama", dimField: "dimensions", respond: func(v [][]float32) any {
		return map[string]any{"embeddings": v}
	}},
	{typ: "gemini", dimField: "outputDimensionality", respond: func(v [][]float32) any {
		return map[string]any{"embedding": map[string]any{"values": v[0]}}
	}},
}

// dimsServer returns vectors of the requested size when the request carries
// one and honorRequest is set, else of nativeSize — a fixed-size model that
// ignores the field. It records the last request body.
func dimsServer(t *testing.T, c dimsWireCase, nativeSize int, honorRequest bool) (*httptest.Server, *map[string]any) {
	t.Helper()
	var last map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		last = map[string]any{}
		require.NoError(t, json.Unmarshal(raw, &last))
		size := nativeSize
		if n, ok := last[c.dimField].(float64); ok && honorRequest {
			size = int(n)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(c.respond([][]float32{make([]float32, size)}))
	}))
	t.Cleanup(srv.Close)
	return srv, &last
}

func newDimsProvider(t *testing.T, c dimsWireCase, baseURL string, extra map[string]any) providers.EmbeddingProvider {
	t.Helper()
	p, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type:             c.typ,
		Model:            "self-hosted-model-the-table-does-not-know",
		BaseURL:          baseURL,
		Credential:       credentials.NewAPIKeyCredential("k"),
		AdditionalConfig: extra,
	})
	require.NoError(t, err)
	return p
}

func embedOne(p providers.EmbeddingProvider) ([][]float32, error) {
	resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"hello"}})
	return resp.Embeddings, err
}

func TestEmbeddingFactories_UnknownModelSizeIsObservedNotGuessed(t *testing.T) {
	for _, c := range dimsWireCases {
		t.Run(c.typ, func(t *testing.T) {
			srv, last := dimsServer(t, c, 384, true)
			p := newDimsProvider(t, c, srv.URL, nil)

			assert.Equal(t, 0, p.EmbeddingDimensions(), "an unknown model's size must not be guessed")
			vecs, err := embedOne(p)
			require.NoError(t, err)
			require.Len(t, vecs[0], 384)
			assert.Equal(t, 384, p.EmbeddingDimensions())
			assert.NotContains(t, (*last), c.dimField, "no size may be sent when none was declared")
		})
	}
}

func TestEmbeddingFactories_DeclaredSizeIsReportedAndSent(t *testing.T) {
	for _, c := range dimsWireCases {
		t.Run(c.typ, func(t *testing.T) {
			srv, last := dimsServer(t, c, 768, true)
			p := newDimsProvider(t, c, srv.URL, map[string]any{"dimensions": 256})

			assert.Equal(t, 256, p.EmbeddingDimensions())
			vecs, err := embedOne(p)
			require.NoError(t, err)
			require.Len(t, vecs[0], 256)
			assert.EqualValues(t, 256, (*last)[c.dimField])
		})
	}
}

func TestEmbeddingFactories_DeclaredSizeTheModelIgnoresFails(t *testing.T) {
	for _, c := range dimsWireCases {
		t.Run(c.typ, func(t *testing.T) {
			srv, _ := dimsServer(t, c, 768, false)
			p := newDimsProvider(t, c, srv.URL, map[string]any{"dimensions": 1536})

			_, err := embedOne(p)
			require.Error(t, err, "vectors of the wrong size must never be returned as success")
			assert.True(t, strings.Contains(err.Error(), "returned 768-dimension embeddings, expected 1536"), err.Error())
		})
	}
}
