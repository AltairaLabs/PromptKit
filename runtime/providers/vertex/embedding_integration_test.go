//go:build integration

// Live Google Vertex AI embedding tests. Exercises the full path the unit tests
// stub out: an OAuth2 bearer token applied by the platform RoundTripper, the
// publishers/google/models/{model}:predict URL, and the real predictions
// response shape.
//
// Run locally:
//
//	gcloud auth application-default login
//	export GCP_PROJECT=<project-with-vertex-ai-enabled>
//	export GCP_REGION=us-central1
//	go test -tags=integration ./runtime/providers/vertex/... -v
//
// Tests skip when GCP credentials or GCP_PROJECT are unavailable. Live calls
// additionally require the Vertex AI API to be enabled on the project.
package vertex_test

import (
	"context"
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/AltairaLabs/PromptKit/runtime/providers/vertex"
)

func integrationRegion() string {
	if r := os.Getenv("GCP_REGION"); r != "" {
		return r
	}
	return "us-central1"
}

func integrationProject(t *testing.T) string {
	t.Helper()
	project := os.Getenv("GCP_PROJECT")
	if project == "" {
		t.Skip("GCP_PROJECT not set")
	}
	return project
}

// liveProvider builds the provider exactly as a caller would — through the
// factory and the platform transport — so the test covers the real wiring
// rather than a hand-assembled struct.
func liveProvider(t *testing.T, model string) providers.EmbeddingProvider {
	t.Helper()
	project := integrationProject(t)

	cred, err := credentials.NewGCPCredential(context.Background(), project, integrationRegion())
	if err != nil {
		t.Skipf("GCP credentials not available: %v", err)
	}

	p, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		ID:       "vertex-live",
		Type:     "vertex",
		Model:    model,
		Platform: "vertex",
		PlatformConfig: &credentials.PlatformConfig{
			Type: "vertex", Project: project, Region: integrationRegion(),
		},
		Credential: cred,
	})
	require.NoError(t, err)
	return p
}

func TestLive_EmbedSingleText(t *testing.T) {
	p := liveProvider(t, "text-embedding-005")

	resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{
		Texts: []string{"the quick brown fox"},
	})
	require.NoError(t, err)

	require.Len(t, resp.Embeddings, 1)
	assert.NotEmpty(t, resp.Embeddings[0], "a live embedding must not come back empty")
	assert.Len(t, resp.Embeddings[0], p.EmbeddingDimensions(),
		"the reported dimensionality must match what the service actually returns")
	require.NotNil(t, resp.Usage)
	assert.Positive(t, resp.Usage.TotalTokens)
}

// Batching is the claim most likely to be wrong against the live service: the
// unit tests only prove the request shape, not that Vertex accepts several
// instances in one call and returns them in order.
func TestLive_EmbedBatchPreservesOrder(t *testing.T) {
	p := liveProvider(t, "text-embedding-005")

	texts := []string{"alpha", "beta", "gamma"}
	resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{Texts: texts})
	require.NoError(t, err)

	require.Len(t, resp.Embeddings, len(texts))
	for i := range texts {
		assert.NotEmpty(t, resp.Embeddings[i], "embedding %d empty", i)
	}
	assert.NotEqual(t, resp.Embeddings[0], resp.Embeddings[1],
		"distinct inputs must produce distinct vectors — equal ones would mean the "+
			"instances were collapsed or the response was misread")
}

// outputDimensionality is applied server-side, so this is the only way to know
// the parameter is accepted rather than ignored.
func TestLive_OutputDimensionalityIsHonored(t *testing.T) {
	project := integrationProject(t)
	cred, err := credentials.NewGCPCredential(context.Background(), project, integrationRegion())
	if err != nil {
		t.Skipf("GCP credentials not available: %v", err)
	}

	p, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type:     "vertex",
		Model:    "text-embedding-005",
		Platform: "vertex",
		PlatformConfig: &credentials.PlatformConfig{
			Type: "vertex", Project: project, Region: integrationRegion(),
		},
		Credential:       cred,
		AdditionalConfig: map[string]any{"dimensions": 256},
	})
	require.NoError(t, err)

	resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"x"}})
	require.NoError(t, err)
	require.Len(t, resp.Embeddings, 1)
	assert.Len(t, resp.Embeddings[0], 256,
		"Vertex must return the requested dimensionality, not the family default")
}
