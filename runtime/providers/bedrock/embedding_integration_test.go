//go:build integration

// Live AWS Bedrock embedding tests. Exercises the full path the unit tests
// stub out: SigV4 signing of the request body through the platform
// RoundTripper, the /model/{modelId}/invoke URL with its ':' version suffix
// percent-encoded, and the real Titan/Cohere response shapes.
//
// Run locally:
//
//	export AWS_PROFILE=<profile-with-bedrock-access>
//	export AWS_REGION=us-west-2
//	go test -tags=integration ./runtime/providers/bedrock/... -v
//
// Tests skip when AWS credentials are unavailable. Live calls additionally
// require Bedrock model access for the chosen model in the chosen region;
// Cohere coverage is opt-in via BEDROCK_COHERE_MODEL because model access is
// granted per-model and most accounts enable Titan only.
package bedrock_test

import (
	"context"
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func integrationRegion() string {
	if r := os.Getenv("AWS_REGION"); r != "" {
		return r
	}
	return "us-west-2"
}

func skipIfNoAWS(t *testing.T) {
	t.Helper()
	if _, err := credentials.NewAWSCredential(context.Background(), integrationRegion()); err != nil {
		t.Skipf("AWS credentials not available: %v", err)
	}
}

// liveProvider builds the provider exactly as a caller would — through the
// factory and the platform transport — so the test covers the real wiring
// rather than a hand-assembled struct.
func liveProvider(t *testing.T, model string) providers.EmbeddingProvider {
	t.Helper()
	skipIfNoAWS(t)

	cred, err := credentials.NewAWSCredential(context.Background(), integrationRegion())
	require.NoError(t, err)

	p, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		ID:       "embed",
		Type:     "bedrock",
		Model:    model,
		Platform: "bedrock",
		PlatformConfig: &credentials.PlatformConfig{
			Type: "bedrock", Region: integrationRegion(),
		},
		Credential: cred,
	})
	require.NoError(t, err)
	return p
}

func TestLiveTitanEmbedsSingleText(t *testing.T) {
	p := liveProvider(t, "amazon.titan-embed-text-v2:0")

	got, err := p.Embed(context.Background(), providers.EmbeddingRequest{
		Texts: []string{"PromptKit runs on Bedrock."},
	})

	require.NoError(t, err)
	require.Len(t, got.Embeddings, 1)
	assert.Len(t, got.Embeddings[0], p.EmbeddingDimensions(),
		"live vector width must match the advertised dimensions")
	assert.NotZero(t, got.Usage.TotalTokens)
}

// The fan-out path is the one most likely to break against the real endpoint:
// each call is signed independently, and the body must be re-signed per text.
func TestLiveTitanFansOutMultipleTexts(t *testing.T) {
	p := liveProvider(t, "amazon.titan-embed-text-v2:0")

	got, err := p.Embed(context.Background(), providers.EmbeddingRequest{
		Texts: []string{"first document", "second document", "third document"},
	})

	require.NoError(t, err)
	require.Len(t, got.Embeddings, 3)
	for i, v := range got.Embeddings {
		assert.NotEmpty(t, v, "embedding %d is empty", i)
	}
	assert.NotEqual(t, got.Embeddings[0], got.Embeddings[1],
		"distinct inputs must produce distinct vectors — equal ones would mean "+
			"the same text was sent every time")
}

func TestLiveTitanV1(t *testing.T) {
	p := liveProvider(t, "amazon.titan-embed-text-v1")

	got, err := p.Embed(context.Background(), providers.EmbeddingRequest{
		Texts: []string{"version one"},
	})

	require.NoError(t, err)
	require.Len(t, got.Embeddings, 1)
	assert.Len(t, got.Embeddings[0], p.EmbeddingDimensions())
}

func TestLiveCohereBatches(t *testing.T) {
	model := os.Getenv("BEDROCK_COHERE_MODEL")
	if model == "" {
		t.Skip("set BEDROCK_COHERE_MODEL (e.g. cohere.embed-english-v3) to run Cohere coverage")
	}
	p := liveProvider(t, model)

	got, err := p.Embed(context.Background(), providers.EmbeddingRequest{
		Texts: []string{"alpha", "beta"},
	})

	require.NoError(t, err)
	require.Len(t, got.Embeddings, 2)
	assert.NotEqual(t, got.Embeddings[0], got.Embeddings[1])
}

// A model the account cannot reach must surface as a provider error rather
// than a panic or a silently empty result.
func TestLiveUnauthorizedModelSurfacesError(t *testing.T) {
	p := liveProvider(t, "amazon.titan-embed-text-v2:0")

	_, err := p.Embed(context.Background(), providers.EmbeddingRequest{
		Texts: []string{"probe"},
		Model: "amazon.titan-embed-does-not-exist-v9:0",
	})

	require.Error(t, err)
}
