package base

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newReq(t *testing.T, target string) *http.Request {
	t.Helper()
	r, err := http.NewRequest(http.MethodGet, target, http.NoBody)
	require.NoError(t, err)
	return r
}

// The HTTP service client (tts, stt) keeps its key header on the origin.
func TestNewHTTPService_ClientStripsKeyOffHost(t *testing.T) {
	got := make(chan http.Header, 1)
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got <- r.Header.Clone()
	}))
	t.Cleanup(target.Close)
	targetURL := strings.Replace(target.URL, "127.0.0.1", "localhost", 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetURL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(origin.Close)

	_, fields := NewHTTPService("k", HTTPServiceDefaults{Name: "svc", Timeout: time.Minute})
	req := newReq(t, origin.URL)
	req.Header.Set("Xi-Api-Key", "elevenlabs-key")
	req.Header.Set("X-API-Key", "cartesia-key")
	resp, err := fields.Client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	hdr := <-got
	assert.Empty(t, hdr.Get("Xi-Api-Key"))
	assert.Empty(t, hdr.Get("X-Api-Key"))
}

// Provider-file headers (HTTPTuning.Headers) are added below CheckRedirect,
// so the header transport itself leaves them off a hop to another host.
func TestHeaderTransport_SkipsHeadersOffOrigin(t *testing.T) {
	got := make(chan http.Header, 2)
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got <- r.Header.Clone()
	}))
	t.Cleanup(target.Close)
	targetURL := strings.Replace(target.URL, "127.0.0.1", "localhost", 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Clone()
		http.Redirect(w, r, targetURL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(origin.Close)

	client := HTTPTuning{Headers: map[string]string{"X-Gateway-Key": "secret"}}.Client(&http.Client{})
	resp, err := client.Do(newReq(t, origin.URL))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, "secret", (<-got).Get("X-Gateway-Key"), "the origin gets the header")
	assert.Empty(t, (<-got).Get("X-Gateway-Key"), "another host does not")
}

// The base names are the httputil policy: a copy made here strips off the
// origin and leaves the caller's client alone.
func TestWithCredentialRedirectPolicy_IsTheHTTPUtilPolicy(t *testing.T) {
	caller := &http.Client{}
	guarded := WithCredentialRedirectPolicy(caller)
	require.NotSame(t, caller, guarded)
	assert.Nil(t, caller.CheckRedirect)
	assert.Nil(t, WithCredentialRedirectPolicy(nil))

	hop := newReq(t, "https://evil.example.com/")
	hop.Header.Set("X-Api-Key", "k")
	require.NoError(t, guarded.CheckRedirect(hop, []*http.Request{newReq(t, "https://api.example.com/")}))
	assert.Empty(t, hop.Header.Get("X-Api-Key"))
	assert.Contains(t, WellKnownCredentialHeaders, "X-Api-Key")
}
