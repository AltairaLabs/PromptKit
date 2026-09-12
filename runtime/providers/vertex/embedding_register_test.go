package vertex_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Side-effect import registers the factory under test.
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/vertex"
)

func vertexSpec(model string, extra map[string]any) providers.EmbeddingProviderSpec {
	return providers.EmbeddingProviderSpec{
		ID:       "embed",
		Type:     "vertex",
		Model:    model,
		Platform: "vertex",
		PlatformConfig: &credentials.PlatformConfig{
			Type: "vertex", Project: "my-proj", Region: "us-central1",
		},
		Credential:       credentials.NewAPIKeyCredential("stub"),
		AdditionalConfig: extra,
	}
}

func TestFactoryIsRegistered(t *testing.T) {
	assert.Contains(t, providers.RegisteredEmbeddingProviderTypes(), "vertex")
}

// The path #1301 reported as gated: type "vertex" with a Vertex platform block
// must now construct instead of erroring with "not yet supported".
func TestFactoryBuildsProviderFromSpec(t *testing.T) {
	p, err := providers.CreateEmbeddingProviderFromSpec(vertexSpec("text-embedding-005", nil))

	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "vertex-embedding", p.ID())
}

func TestFactoryPassesDimensionsThrough(t *testing.T) {
	p, err := providers.CreateEmbeddingProviderFromSpec(
		vertexSpec("text-embedding-005", map[string]any{"dimensions": 256}))

	require.NoError(t, err)
	assert.Equal(t, 256, p.EmbeddingDimensions())
}

func TestFactoryPassesTaskTypeThrough(t *testing.T) {
	// gemini-embedding batches one at a time, so the batch size proves the
	// model family reached the constructor rather than being defaulted.
	p, err := providers.CreateEmbeddingProviderFromSpec(
		vertexSpec("gemini-embedding-001", map[string]any{"task_type": "RETRIEVAL_QUERY"}))

	require.NoError(t, err)
	assert.Equal(t, 1, p.MaxBatchSize())
}

// Without a platform block there is no token source, so every request would go
// unauthenticated. Failing at construction beats failing per request.
func TestFactoryRequiresPlatformConfig(t *testing.T) {
	_, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type:  "vertex",
		Model: "text-embedding-005",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "platform")
}

// project and region both appear twice in the Vertex URL and cannot be
// defaulted, so a spec missing either must fail at construction.
func TestFactoryRequiresProject(t *testing.T) {
	spec := vertexSpec("text-embedding-005", nil)
	spec.PlatformConfig = &credentials.PlatformConfig{Type: "vertex", Region: "us-central1"}

	_, err := providers.CreateEmbeddingProviderFromSpec(spec)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "platform.project")
}
