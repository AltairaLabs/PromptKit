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

// A model given as a full resource name is sent to POST /v1/{name}:predict,
// the form Vertex documents for both a publisher model and an endpoint (where
// a tuned or self-deployed model is served), never to the models collection.
func TestResourceName_PredictURL(t *testing.T) {
	for _, tc := range []struct {
		model, path string
		batch, dims int
	}{
		{
			model: "projects/p1/locations/europe-west4/publishers/google/models/text-embedding-005",
			path:  "/v1/projects/p1/locations/europe-west4/publishers/google/models/text-embedding-005:predict",
			batch: textEmbeddingMaxBatch, dims: textEmbeddingDimensions,
		},
		{
			model: "projects/p1/locations/us-central1/endpoints/1234567890",
			path:  "/v1/projects/p1/locations/us-central1/endpoints/1234567890:predict",
			batch: unknownModelMaxBatch, dims: 0,
		},
	} {
		t.Run(tc.path, func(t *testing.T) {
			srv, got := predictServer(t, fixtureVector(768, 1), 1)
			p, err := providers.CreateEmbeddingProviderFromSpec(vertexSpecAt(srv.URL, tc.model, nil))
			require.NoError(t, err)
			assert.Equal(t, tc.batch, p.MaxBatchSize())
			assert.Equal(t, tc.dims, p.EmbeddingDimensions())

			_, err = p.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"x"}})

			require.NoError(t, err)
			require.Len(t, *got, 1)
			assert.Equal(t, tc.path, (*got)[0].path)
		})
	}
}

// With the standard regional base URL, a resource name keeps the host and
// API version and replaces the publisher's models collection.
func TestResourceName_ReplacesTheModelsCollection(t *testing.T) {
	p, err := NewEmbeddingProvider(WithWiring(providers.EmbeddingWiring{
		Model:   "projects/p1/locations/us-central1/endpoints/42",
		BaseURL: "https://us-central1-aiplatform.googleapis.com/v1/projects/p1/locations/us-central1/publishers/google/models",
	}))
	require.NoError(t, err)

	assert.Equal(t, "https://us-central1-aiplatform.googleapis.com/v1/projects/p1/locations/us-central1/endpoints/42:predict",
		p.predictURL(p.ProviderModel))
	assert.Equal(t, "https://us-central1-aiplatform.googleapis.com/v1/projects/p1/locations/us-central1/"+
		"publishers/google/models/text-embedding-005:predict", p.predictURL("text-embedding-005"))

	p.BaseURL = "https://vertex.internal.example/v1"
	assert.Equal(t, "https://vertex.internal.example/v1/projects/p1/locations/us-central1/endpoints/42:predict",
		p.predictURL(p.ProviderModel), "an endpoint ending in /v1 is not given a second one")
}

// A model-registry resource (projects/.../models/...) has no :predict method;
// it is served through an endpoint, so it is refused with the form to use.
func TestResourceName_RegistryModelIsRefused(t *testing.T) {
	_, err := providers.CreateEmbeddingProviderFromSpec(vertexSpecAt("http://unused",
		"projects/p1/locations/us-central1/models/987", nil))

	require.Error(t, err)
	assert.Equal(t, `vertex embedding model "projects/p1/locations/us-central1/models/987" `+
		`has no :predict method; deploy it and use its endpoint `+
		`(projects/<project>/locations/<location>/endpoints/<id>) as the model`, err.Error())
}
