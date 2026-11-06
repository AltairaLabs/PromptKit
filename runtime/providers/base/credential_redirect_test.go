package base

import (
	"errors"
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

func withCredentialHeaders(r *http.Request) *http.Request {
	for _, h := range append([]string{"X-Gateway-Key"}, WellKnownCredentialHeaders...) {
		r.Header.Set(h, "secret")
	}
	r.Header.Set("Content-Type", "application/json")
	return r
}

// Off the origin, every well-known credential header and every named header
// is dropped; other headers stay. On the origin nothing is dropped, and names
// is not even asked.
func TestCredentialRedirectPolicy_StripsOnlyOffOrigin(t *testing.T) {
	first := newReq(t, "https://api.example.com/v1")
	named := 0
	policy := CredentialRedirectPolicy(func() []string {
		named++
		return []string{"X-Gateway-Key"}
	}, nil)

	same := withCredentialHeaders(newReq(t, "https://api.example.com:443/v2"))
	require.NoError(t, policy(same, []*http.Request{first}))
	assert.Zero(t, named, "names is looked up only for a hop off the origin")
	for _, h := range append([]string{"X-Gateway-Key"}, WellKnownCredentialHeaders...) {
		assert.NotEmpty(t, same.Header.Get(h), "same-origin hop keeps %s", h)
	}

	for _, target := range []string{
		"https://evil.example.com/v1", "http://api.example.com/v1", "https://sub.api.example.com/v1",
	} {
		off := withCredentialHeaders(newReq(t, target))
		require.NoError(t, policy(off, []*http.Request{first}))
		for _, h := range append([]string{"X-Gateway-Key"}, WellKnownCredentialHeaders...) {
			assert.Empty(t, off.Header.Get(h), "%s: %s kept", target, h)
		}
		assert.Equal(t, "application/json", off.Header.Get("Content-Type"), "other headers stay")
	}
	assert.Equal(t, 3, named)
}

// The origin is the first request, not the previous hop: a foreign host that
// redirects to itself is still foreign.
func TestCredentialRedirectPolicy_ComparesWithTheFirstRequest(t *testing.T) {
	policy := CredentialRedirectPolicy(nil, nil)
	via := []*http.Request{newReq(t, "https://api.example.com/"), newReq(t, "https://evil.example.com/a")}
	hop := withCredentialHeaders(newReq(t, "https://evil.example.com/b"))
	require.NoError(t, policy(hop, via))
	assert.Empty(t, hop.Header.Get("X-Api-Key"))
}

// next runs after stripping; with no next the default 10-redirect limit holds.
func TestCredentialRedirectPolicy_ThenNextOrDefaultLimit(t *testing.T) {
	stop := errors.New("stop")
	var sawKey string
	policy := CredentialRedirectPolicy(nil, func(req *http.Request, _ []*http.Request) error {
		sawKey = req.Header.Get("X-Api-Key")
		return stop
	})
	hop := withCredentialHeaders(newReq(t, "https://evil.example.com/"))
	require.ErrorIs(t, policy(hop, []*http.Request{newReq(t, "https://api.example.com/")}), stop)
	assert.Empty(t, sawKey, "next sees the stripped hop")

	def := CredentialRedirectPolicy(nil, nil)
	via := make([]*http.Request, 10)
	for i := range via {
		via[i] = newReq(t, "https://api.example.com/")
	}
	require.EqualError(t, def(newReq(t, "https://api.example.com/"), via), "stopped after 10 redirects")
	require.NoError(t, def(newReq(t, "https://api.example.com/"), via[:9]))
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
