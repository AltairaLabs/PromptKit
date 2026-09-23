//go:build integration

// Live Voyage AI embedding tests. They check the sizes the provider reports
// against what the API actually returns, which fake servers cannot.
//
// Run locally:
//
//	export VOYAGE_API_KEY=<key>
//	go test -tags=integration ./runtime/providers/voyageai/... -v
//
// Tests skip when VOYAGE_API_KEY is unset.
package voyageai_test

import (
	"context"
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/voyageai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func liveVoyage(t *testing.T, model string, extra map[string]any) providers.EmbeddingProvider {
	t.Helper()
	key := os.Getenv("VOYAGE_API_KEY")
	if key == "" {
		t.Skip("VOYAGE_API_KEY not set")
	}
	p, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type:             "voyageai",
		Model:            model,
		Credential:       credentials.NewAPIKeyCredential(key),
		AdditionalConfig: extra,
	})
	require.NoError(t, err)
	return p
}

func embedTwo(t *testing.T, p providers.EmbeddingProvider) [][]float32 {
	t.Helper()
	resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"hello", "world"}})
	require.NoError(t, err)
	require.Len(t, resp.Embeddings, 2)
	return resp.Embeddings
}

// Every size the provider claims to know must match what the API returns.
func TestLiveKnownModelSizesMatchTheAPI(t *testing.T) {
	for _, model := range []string{
		voyageai.ModelVoyage35, voyageai.ModelVoyage35Lite, voyageai.ModelVoyage3Large,
		voyageai.ModelVoyageCode3, voyageai.ModelVoyageFinance2, voyageai.ModelVoyageLaw2,
	} {
		t.Run(model, func(t *testing.T) {
			p := liveVoyage(t, model, nil)
			claimed := p.EmbeddingDimensions()
			require.NotZero(t, claimed, "a model in the table must report a size up front")

			vecs := embedTwo(t, p)
			assert.Len(t, vecs[0], claimed)
		})
	}
}

func TestLiveDeclaredDimensionsAreHonored(t *testing.T) {
	p := liveVoyage(t, voyageai.ModelVoyage35, map[string]any{"dimensions": 256})

	vecs := embedTwo(t, p)

	assert.Len(t, vecs[0], 256)
	assert.Equal(t, 256, p.EmbeddingDimensions())
}

// voyage-3-lite is live but not in the table, so its size is observed.
func TestLiveUnknownModelSizeIsObserved(t *testing.T) {
	p := liveVoyage(t, "voyage-3-lite", nil)
	require.Equal(t, 0, p.EmbeddingDimensions())

	vecs := embedTwo(t, p)

	assert.Equal(t, len(vecs[0]), p.EmbeddingDimensions())
	assert.NotZero(t, p.EmbeddingDimensions())
}
