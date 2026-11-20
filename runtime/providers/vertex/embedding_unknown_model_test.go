package vertex

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// vertexSpecAt is a vertex embedding spec whose platform endpoint is url, so
// the request goes through the real factory, transport and provider.
func vertexSpecAt(url, model string, extra map[string]any) providers.EmbeddingProviderSpec {
	return providers.EmbeddingProviderSpec{
		Type:             "vertex",
		Model:            model,
		Platform:         "vertex",
		PlatformConfig:   &credentials.PlatformConfig{Type: "vertex", Endpoint: url},
		Credential:       credentials.NewAPIKeyCredential("stub"),
		AdditionalConfig: extra,
	}
}

// text-multilingual-embedding-002 is a Vertex embedding model outside the
// three known prefixes. It used to be refused at construction. Now it builds,
// one text per request, and the size comes from the first response.
func TestUnknownModel_BuildsWithBatchOneAndObservedSize(t *testing.T) {
	srv, got := predictServer(t, fixtureVector(768, 0.5), 2)
	p, err := providers.CreateEmbeddingProviderFromSpec(
		vertexSpecAt(srv.URL, "text-multilingual-embedding-002", nil))
	require.NoError(t, err)
	require.Equal(t, 1, p.MaxBatchSize(), "an unknown model is sent one text per request")
	require.Equal(t, 0, p.EmbeddingDimensions(), "unknown until the first response")

	resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"hola"}})

	require.NoError(t, err)
	require.Len(t, *got, 1)
	assert.Equal(t, "/text-multilingual-embedding-002:predict", (*got)[0].path)
	assert.Equal(t, map[string]any{
		"instances": []any{map[string]any{"content": "hola", "task_type": DefaultTaskType}},
	}, (*got)[0].body, "no outputDimensionality when none is declared")
	assert.Equal(t, [][]float32{fixtureVector(768, 0.5)}, resp.Embeddings)
	assert.Equal(t, 768, p.EmbeddingDimensions())
}

// Declared dimensions and batch size stand in for the family an unknown model
// lacks: the size is requested and reported, and the batch size is reported.
func TestUnknownModel_DeclaredSizeAndBatchAreUsed(t *testing.T) {
	srv, got := predictServer(t, fixtureVector(256, 1), 1)
	p, err := providers.CreateEmbeddingProviderFromSpec(vertexSpecAt(srv.URL,
		"text-multilingual-embedding-002", map[string]any{"dimensions": 256, "batch_size": 50}))
	require.NoError(t, err)

	_, err = p.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"a", "b"}})

	require.NoError(t, err)
	assert.Equal(t, map[string]any{"outputDimensionality": float64(256)}, (*got)[0].body["parameters"])
	assert.Equal(t, 256, p.EmbeddingDimensions())
	assert.Equal(t, 50, p.MaxBatchSize())
}

// A declared batch size replaces a known family's default too.
func TestKnownModel_DeclaredBatchSizeWins(t *testing.T) {
	p, err := providers.CreateEmbeddingProviderFromSpec(vertexSpecAt("http://unused",
		"text-embedding-005", map[string]any{"batch_size": 16}))

	require.NoError(t, err)
	assert.Equal(t, 16, p.MaxBatchSize())
	assert.Equal(t, textEmbeddingDimensions, p.EmbeddingDimensions(), "the family size still applies")
}
