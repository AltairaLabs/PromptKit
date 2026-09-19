//go:build integration

package gemini

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// TestGeminiEmbedding_AuthenticatesByHeader_Live guards the embedding half of
// #1873.
//
// The key used to ride in the URL as ?key=, so this path could not fail to
// authenticate as long as the URL was built. Now the key is sent as
// x-goog-api-key and the URL carries no credential, which means auth is a
// separate thing that can break on its own — and unit tests cannot tell a
// header the API accepts from one it ignores.
//
// Uses the package DEFAULT deliberately. That default was text-embedding-004,
// a model the endpoint no longer serves, so this test would have 404d — which
// is how #1881 was found. Pinning the default here means a future retirement
// fails a test rather than every caller.
func TestGeminiEmbedding_AuthenticatesByHeader_Live(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("set GEMINI_API_KEY to run the live embedding auth test")
	}

	p, err := NewEmbeddingProvider()
	require.NoError(t, err)

	resp, err := p.Embed(context.Background(), providers.EmbeddingRequest{
		Texts: []string{"hello world"},
	})
	require.NoError(t, err,
		"the embedding path must authenticate with x-goog-api-key now that the "+
			"key is no longer in the URL")
	require.NotEmpty(t, resp.Embeddings)
	require.NotEmpty(t, resp.Embeddings[0], "a real embedding vector must come back")
}
