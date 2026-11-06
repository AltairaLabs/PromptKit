package httputil

import (
	"errors"
	"net/http"
	"testing"

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

// SameOriginAsFirstHop compares a hop with the first request of its chain.
func TestSameOriginAsFirstHop(t *testing.T) {
	first := newReq(t, "https://api.example.com/v1")
	hop := func(target string, prev *http.Request) *http.Request {
		r := newReq(t, target)
		r.Response = &http.Response{Request: prev}
		return r
	}
	assert.True(t, SameOriginAsFirstHop(first), "a request that is not a hop")
	assert.True(t, SameOriginAsFirstHop(hop("https://API.example.com:443/v2", first)), "default port spelled out")
	assert.False(t, SameOriginAsFirstHop(hop("http://api.example.com/v1", first)), "scheme downgrade")
	assert.False(t, SameOriginAsFirstHop(hop("http://api.example.com:443/v1", first)), "another port")
	evil := hop("https://evil.example.com/a", first)
	assert.False(t, SameOriginAsFirstHop(hop("https://evil.example.com/b", evil)), "first request, not the previous hop")
	assert.False(t, SameOriginAsFirstHop(hop("ftp://api.example.com/", first)), "unknown scheme")
	ftp := newReq(t, "ftp://files.example.com/a")
	assert.True(t, SameOriginAsFirstHop(hop("ftp://files.example.com/b", ftp)), "unknown scheme, same host")
}

// WithCredentialRedirectPolicy copies the client, chains its CheckRedirect
// after the policy, and leaves the original alone.
func TestWithCredentialRedirectPolicy_CopiesAndChains(t *testing.T) {
	assert.Nil(t, WithCredentialRedirectPolicy(nil))
	calls := 0
	caller := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		calls++
		return nil
	}}
	guarded := WithCredentialRedirectPolicy(caller)
	require.NotSame(t, caller, guarded)

	off := withCredentialHeaders(newReq(t, "https://evil.example.com/"))
	require.NoError(t, guarded.CheckRedirect(off, []*http.Request{newReq(t, "https://api.example.com/")}))
	assert.Empty(t, off.Header.Get("X-Api-Key"))
	assert.Equal(t, 1, calls, "the caller's CheckRedirect runs after the policy")

	direct := withCredentialHeaders(newReq(t, "https://evil.example.com/"))
	require.NoError(t, caller.CheckRedirect(direct, []*http.Request{newReq(t, "https://api.example.com/")}))
	assert.Equal(t, "secret", direct.Header.Get("X-Api-Key"), "the caller's own client is unmodified")
}
