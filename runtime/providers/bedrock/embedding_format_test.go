package bedrock_test

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bedrockSpecAt is a bedrock embedding spec whose platform endpoint is url, so
// the request goes through the real factory, transport and provider.
func bedrockSpecAt(url, model string, extra map[string]any) providers.EmbeddingProviderSpec {
	return providers.EmbeddingProviderSpec{
		Type:             "bedrock",
		Model:            model,
		Platform:         "bedrock",
		PlatformConfig:   &credentials.PlatformConfig{Type: "bedrock", Endpoint: url},
		Credential:       credentials.NewAPIKeyCredential("stub"),
		AdditionalConfig: extra,
	}
}

// A cross-region inference profile does not start with "cohere.embed", so the
// name rule refuses it. Declaring the format lets it through, and the request
// takes the Cohere shape on the profile's own invoke path.
func TestDeclaredCohereFormat_InferenceProfileSendsCohereBody(t *testing.T) {
	srv, c := fakeBedrock(t, func(int) string { return twoCohereVectors })
	p, err := providers.CreateEmbeddingProviderFromSpec(bedrockSpecAt(srv.URL,
		"us.cohere.embed-english-v3", map[string]any{"embedding_format": "cohere"}))
	require.NoError(t, err)
	require.Equal(t, 0, p.EmbeddingDimensions(), "a model the name rule does not know has no known size")

	got, err := p.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"a", "b"}})

	require.NoError(t, err)
	require.Len(t, c.bodies, 1, "cohere batches: one call for both texts")
	assert.Equal(t, "/model/us.cohere.embed-english-v3/invoke", c.paths[0])
	assert.Equal(t, map[string]any{"texts": []any{"a", "b"}, "input_type": "search_document"}, c.bodies[0])
	assert.Equal(t, [][]float32{fixtureVector(fixtureDims, 1, 2), fixtureVector(fixtureDims, 3, 4)}, got.Embeddings)
	assert.Equal(t, 96, p.MaxBatchSize())
	assert.Equal(t, fixtureDims, p.EmbeddingDimensions(), "the size the first response had")
}

// An application inference profile ARN names no family at all. With the Titan
// format declared it gets Titan's body, and a declared size is sent, since
// nothing in the name says the model rejects it.
func TestDeclaredTitanFormat_ARNSendsTitanBodyWithDimensions(t *testing.T) {
	const arn = "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/abc123"
	srv, c := fakeBedrock(t, func(int) string { return titanVector(256) })
	p, err := providers.CreateEmbeddingProviderFromSpec(bedrockSpecAt(srv.URL, arn,
		map[string]any{"embedding_format": "titan", "dimensions": 256}))
	require.NoError(t, err)

	_, err = p.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"hello", "world"}})

	require.NoError(t, err)
	require.Len(t, c.bodies, 2, "titan embeds one text per call")
	assert.Equal(t, map[string]any{"inputText": "hello", "dimensions": float64(256)}, c.bodies[0])
	assert.Equal(t, 1, p.MaxBatchSize())
	assert.Equal(t, 256, p.EmbeddingDimensions())
}

// A declared Titan format with no declared size takes the size from the first
// response instead of claiming the v2 default for a model it cannot identify.
func TestDeclaredTitanFormat_UndeclaredSizeIsObserved(t *testing.T) {
	srv, c := fakeBedrock(t, func(int) string { return titanVector(512) })
	p, err := providers.CreateEmbeddingProviderFromSpec(bedrockSpecAt(srv.URL,
		"global.amazon.titan-embed-text-v2:0", map[string]any{"embedding_format": "titan"}))
	require.NoError(t, err)
	require.Equal(t, 0, p.EmbeddingDimensions(), "unknown until the first response")

	_, err = p.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"hello"}})

	require.NoError(t, err)
	assert.Equal(t, map[string]any{"inputText": "hello"}, c.bodies[0])
	assert.Equal(t, 512, p.EmbeddingDimensions())
}

// The declared format wins over the name rule: a model id that reads as Titan
// is still sent the Cohere body when the config says cohere.
func TestDeclaredFormat_WinsOverModelName(t *testing.T) {
	srv, c := fakeBedrock(t, func(int) string { return cohereBody(fixtureVector(fixtureDims, 1)) })
	p, err := providers.CreateEmbeddingProviderFromSpec(bedrockSpecAt(srv.URL,
		"amazon.titan-embed-text-v2:0", map[string]any{"embedding_format": "cohere"}))
	require.NoError(t, err)

	_, err = p.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"a"}})

	require.NoError(t, err)
	assert.Equal(t, map[string]any{"texts": []any{"a"}, "input_type": "search_document"}, c.bodies[0])
}

// A declared batch size replaces the family's.
func TestDeclaredBatchSize_ReplacesFamilyDefault(t *testing.T) {
	p, err := providers.CreateEmbeddingProviderFromSpec(bedrockSpecAt("http://unused",
		"cohere.embed-english-v3", map[string]any{"batch_size": 20}))

	require.NoError(t, err)
	assert.Equal(t, 20, p.MaxBatchSize())
}

// Without a declaration the name rule still decides, and a model it does not
// know is still refused; the error says how to declare the format.
func TestUndeclaredFormat_UnknownModelErrorNamesTheSetting(t *testing.T) {
	_, err := providers.CreateEmbeddingProviderFromSpec(bedrockSpecAt("http://unused",
		"us.cohere.embed-english-v3", nil))

	require.Error(t, err)
	assert.Equal(t, `unsupported bedrock embedding model "us.cohere.embed-english-v3": `+
		`expected an amazon.titan-embed* or cohere.embed* model, `+
		`or set additional_config.embedding_format to titan or cohere`, err.Error())
}

func TestDeclaredFormat_UnknownValueIsRejected(t *testing.T) {
	_, err := providers.CreateEmbeddingProviderFromSpec(bedrockSpecAt("http://unused",
		"us.cohere.embed-english-v3", map[string]any{"embedding_format": "openai"}))

	require.Error(t, err)
	assert.Equal(t, `bedrock embedding: additional_config.embedding_format must be titan or cohere, got "openai"`,
		err.Error())
}

// A declared format keeps the known size of a model id the name rule
// recognizes: Titan v1 still reports 1536.
func TestDeclaredFormat_KnownModelKeepsItsSize(t *testing.T) {
	p, err := providers.CreateEmbeddingProviderFromSpec(bedrockSpecAt("http://unused",
		"amazon.titan-embed-text-v1", map[string]any{"embedding_format": "titan"}))

	require.NoError(t, err)
	assert.Equal(t, 1536, p.EmbeddingDimensions())
}

// A declared batch size above the format's API limit is clamped to it: Cohere
// takes at most 96 texts, Titan one.
func TestDeclaredBatchSize_ClampedToTheFormatLimit(t *testing.T) {
	for _, tc := range []struct {
		model  string
		extra  map[string]any
		expect int
	}{
		{"cohere.embed-english-v3", map[string]any{"batch_size": 500}, 96},
		{"amazon.titan-embed-text-v2:0", map[string]any{"batch_size": 500}, 1},
		{"us.cohere.embed-v4:0", map[string]any{"batch_size": 500, "embedding_format": "cohere"}, 96},
	} {
		p, err := providers.CreateEmbeddingProviderFromSpec(bedrockSpecAt("http://unused", tc.model, tc.extra))
		require.NoError(t, err)
		assert.Equal(t, tc.expect, p.MaxBatchSize(), tc.model)
	}
}
