package a2a

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/internal/redirecttest"
)

// A client passed with WithHTTPClient is used through a guarded copy.
func TestWithHTTPClient_KeepsKeyOnOrigin(t *testing.T) {
	caller, hops := redirecttest.CallerClient()
	c := NewClient("http://agent.local", WithHTTPClient(caller))
	r := redirecttest.Probe(t, c.httpClient, caller, hops)
	assert.NotSame(t, caller, c.httpClient, "the caller's own client is not used")
	assert.Equal(t, "secret", r.AtOrigin, "the origin gets the key")
	assert.Empty(t, r.AtTarget, "the key followed the redirect to another host")
	assert.Positive(t, r.CallerHops, "the caller's CheckRedirect ran")
	assert.Equal(t, "secret", r.CallerDirect, "the caller's own client is unmodified")
}

// The WithAuth token and the WithHeaders headers never follow a redirect to
// another host, on the request client or the SSE client.
func TestClient_AuthAndHeadersStayOnOrigin(t *testing.T) {
	got := make(chan http.Header, 2)
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got <- r.Header.Clone()
	}))
	t.Cleanup(target.Close)
	targetURL := strings.Replace(target.URL, "127.0.0.1", "localhost", 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetURL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(origin.Close)

	c := NewClient(origin.URL, WithAuth("Bearer", "tok"), WithHeaders(map[string]string{"X-Agent-Key": "secret"}))
	for name, hc := range map[string]*http.Client{"request": c.httpClient, "sse": c.sseClient} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, origin.URL, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		c.setAuth(req)
		resp, err := hc.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		_ = resp.Body.Close()
		hdr := <-got
		if hdr.Get("X-Agent-Key") != "" || hdr.Get("Authorization") != "" {
			t.Errorf("%s client: credential followed the redirect: %v", name, hdr)
		}
	}
}
