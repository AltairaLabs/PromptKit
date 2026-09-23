package bedrock_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/bedrock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func titanVector(n int) string {
	b, _ := json.Marshal(map[string]any{"embedding": make([]float32, n), "inputTextTokenCount": 1})
	return string(b)
}

// A declared size must reach Titan v2's body — the only Titan model that
// accepts it — or the endpoint returns its 1024 default and the declared size
// is a lie.
func TestTitanV2_SendsDeclaredDimensions(t *testing.T) {
	srv, c := fakeBedrock(t, func(int) string { return titanVector(256) })
	p, err := bedrock.NewEmbeddingProvider(bedrock.WithWiring(providers.EmbeddingWiring{
		Model: "amazon.titan-embed-text-v2:0", BaseURL: srv.URL, PlatformAuth: true, Dimensions: 256,
	}))
	require.NoError(t, err)

	resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"hello"}})

	require.NoError(t, err)
	assert.Len(t, resp.Embeddings[0], 256)
	assert.EqualValues(t, 256, c.bodies[0]["dimensions"])
}

func TestTitanV1_NeverSendsDimensions(t *testing.T) {
	srv, c := fakeBedrock(t, func(int) string { return titanVector(1536) })
	p, err := bedrock.NewEmbeddingProvider(bedrock.WithWiring(providers.EmbeddingWiring{
		Model: "amazon.titan-embed-text-v1", BaseURL: srv.URL, PlatformAuth: true, Dimensions: 1536,
	}))
	require.NoError(t, err)

	_, err = p.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"hello"}})

	require.NoError(t, err)
	assert.NotContains(t, c.bodies[0], "dimensions", "Titan v1 rejects the field")
}

func TestTitanV2_UndeclaredSizeNotSent(t *testing.T) {
	srv, c := fakeBedrock(t, func(int) string { return titanVector(1024) })

	_, err := titanProvider(t, srv.URL).Embed(context.Background(),
		providers.EmbeddingRequest{Texts: []string{"hello"}})

	require.NoError(t, err)
	assert.NotContains(t, c.bodies[0], "dimensions")
}
