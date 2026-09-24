//go:build integration

// Live Hugging Face embedding test against the serverless router.
//
//	HF_TOKEN=... go test -tags=integration ./runtime/providers/huggingface/... -v
package huggingface_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

func TestLiveFeatureExtraction(t *testing.T) {
	token := os.Getenv("HF_TOKEN")
	if token == "" {
		t.Skip("HF_TOKEN not set")
	}
	p, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type:       "huggingface",
		Model:      "sentence-transformers/all-MiniLM-L6-v2",
		Credential: credentials.NewAPIKeyCredential(token),
	})
	require.NoError(t, err)

	resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"hello world", "second"}})

	require.NoError(t, err)
	require.Len(t, resp.Embeddings, 2)
	assert.Len(t, resp.Embeddings[0], 384)
	assert.Equal(t, 384, p.EmbeddingDimensions())
}
