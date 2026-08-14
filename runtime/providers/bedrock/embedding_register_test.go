package bedrock_test

import (
	"slices"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Side-effect import registers the factory under test.
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/bedrock"
)

func TestFactoryIsRegistered(t *testing.T) {
	assert.Contains(t, providers.RegisteredEmbeddingProviderTypes(), "bedrock")
}

// The end-to-end path #1774 reported as broken: type "bedrock" with a Bedrock
// platform block must now construct instead of returning "unsupported
// embedding provider type".
func TestFactoryBuildsProviderFromSpec(t *testing.T) {
	p, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		ID:             "embed",
		Type:           "bedrock",
		Model:          "amazon.titan-embed-text-v2:0",
		Platform:       "bedrock",
		PlatformConfig: &credentials.PlatformConfig{Type: "bedrock", Region: "us-west-2"},
		Credential:     credentials.NewAPIKeyCredential("stub"),
	})

	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "bedrock-embedding", p.ID())
}

func TestFactoryPassesInputTypeThrough(t *testing.T) {
	p, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type:             "bedrock",
		Model:            "cohere.embed-english-v3",
		Platform:         "bedrock",
		PlatformConfig:   &credentials.PlatformConfig{Type: "bedrock", Region: "us-west-2"},
		Credential:       credentials.NewAPIKeyCredential("stub"),
		AdditionalConfig: map[string]any{"input_type": "search_query"},
	})

	require.NoError(t, err)
	// Cohere batches, so the batch size proves the model family was honored
	// rather than defaulted to Titan's single-text limit.
	assert.Greater(t, p.MaxBatchSize(), 1)
}

func TestFactoryPassesDimensionsThrough(t *testing.T) {
	p, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type:             "bedrock",
		Model:            "amazon.titan-embed-text-v2:0",
		Platform:         "bedrock",
		PlatformConfig:   &credentials.PlatformConfig{Type: "bedrock", Region: "us-west-2"},
		Credential:       credentials.NewAPIKeyCredential("stub"),
		AdditionalConfig: map[string]any{"dimensions": 256},
	})

	require.NoError(t, err)
	assert.Equal(t, 256, p.EmbeddingDimensions())
}

// Without a platform block there is no SigV4 signer, so every request would go
// unsigned and be rejected. Failing at construction beats failing per request.
func TestFactoryRequiresPlatformConfig(t *testing.T) {
	_, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type:  "bedrock",
		Model: "amazon.titan-embed-text-v2:0",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "platform")
}

func TestBedrockIsListedAmongEmbeddingTypes(t *testing.T) {
	got := providers.RegisteredEmbeddingProviderTypes()
	assert.True(t, slices.IsSorted(got), "%v not sorted", got)
}
