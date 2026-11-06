package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// redirectOffHost starts a target and an origin that 307s every request to
// the target under a host name http.Client treats as different. It returns
// the origin URL and the headers the target received.
func redirectOffHost(t *testing.T) (originURL string, atTarget <-chan http.Header) {
	t.Helper()
	got := make(chan http.Header, 4)
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got <- r.Header.Clone()
	}))
	t.Cleanup(target.Close)
	targetURL := strings.Replace(target.URL, "127.0.0.1", "localhost", 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetURL+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	}))
	t.Cleanup(origin.Close)
	return origin.URL, got
}

// customTokenCredential sets a header no list of well-known names would
// know, so only not applying it keeps it off another host.
type customTokenCredential struct{ applies atomic.Int32 }

func (c *customTokenCredential) Apply(_ context.Context, req *http.Request) error {
	c.applies.Add(1)
	req.Header.Set("X-Custom-Token", "secret")
	return nil
}
func (*customTokenCredential) Type() string { return "custom" }

// The platform embedding transport applies its credential to every request it
// sends, so it must not apply it to a hop that left the origin.
func TestPlatformEmbeddingTransport_DoesNotAuthorizeCrossHostRedirect(t *testing.T) {
	originURL, atTarget := redirectOffHost(t)
	cred := &customTokenCredential{}
	client, _, err := buildPlatformEmbeddingClient(cred, platformAzure, embeddingTypeOpenAI, "m",
		&PlatformConfig{Endpoint: originURL})
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, originURL+"/embeddings", strings.NewReader(`{}`))
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	hdr := <-atTarget
	assert.Empty(t, hdr.Get("X-Custom-Token"), "the credential followed the redirect")
	assert.Empty(t, hdr.Get("Authorization"))
	assert.Equal(t, int32(1), cred.applies.Load(), "only the request to the origin is authorized")
}

// The capability clients that send a static key header keep it on the
// origin: an embedding provider's default client strips the key on a
// redirect to another host.
func TestBaseEmbeddingProvider_DefaultClientStripsKeyOffHost(t *testing.T) {
	originURL, atTarget := redirectOffHost(t)
	p := NewBaseEmbeddingProvider("e", "m", originURL, 8, 8, time.Minute)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, originURL, strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Header.Set("X-Goog-Api-Key", "gemini-key")
	req.Header.Set("Authorization", "Bearer k")
	resp, err := p.HTTPClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	hdr := <-atTarget
	assert.Empty(t, hdr.Get("X-Goog-Api-Key"))
	assert.Empty(t, hdr.Get("Authorization"))
}

// A rerank provider's default client does the same.
func TestBaseRerankProvider_DefaultClientStripsKeyOffHost(t *testing.T) {
	originURL, atTarget := redirectOffHost(t)
	p := NewBaseRerankProvider("r", "m", originURL, 8, time.Minute)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, originURL, strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Header.Set("X-Api-Key", "k")
	resp, err := p.HTTPClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Empty(t, (<-atTarget).Get("X-Api-Key"))
}
