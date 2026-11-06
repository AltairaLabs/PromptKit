package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/internal/redirecttest"
)

// A client the caller supplies is used through a guarded copy: a key never
// follows a redirect to another host, the caller's CheckRedirect still runs,
// and the caller's client is left as it was.
func TestCallerHTTPClient_KeepsKeyOnOrigin(t *testing.T) {
	caller, hops := redirecttest.CallerClient()
	p, err := NewEmbeddingProvider(WithEmbeddingAPIKey("k"), WithEmbeddingHTTPClient(caller))
	require.NoError(t, err)
	r := redirecttest.Probe(t, p.HTTPClient, caller, hops)
	assert.NotSame(t, caller, p.HTTPClient, "the caller's own client is not used")
	assert.Equal(t, "secret", r.AtOrigin, "the origin gets the key")
	assert.Empty(t, r.AtTarget, "the key followed the redirect to another host")
	assert.Positive(t, r.CallerHops, "the caller's CheckRedirect ran")
	assert.Equal(t, "secret", r.CallerDirect, "the caller's own client is unmodified")
}
