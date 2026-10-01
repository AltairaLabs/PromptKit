package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseWWWAuthenticate(t *testing.T) {
	got := parseWWWAuthenticate([]string{
		`Bearer realm="mcp", error="insufficient_scope", scope="files:read files:write",` +
			` resource_metadata="https://api.example.com/.well-known/oauth-protected-resource", error_description="needs \"write\""`,
		`Basic realm=simple, DPoP algs="ES256 PS256"`,
	})
	require.Len(t, got, 3)
	assert.Equal(t, "Bearer", got[0].Scheme)
	assert.Equal(t, map[string]string{
		"realm":             "mcp",
		"error":             "insufficient_scope",
		"scope":             "files:read files:write",
		"resource_metadata": "https://api.example.com/.well-known/oauth-protected-resource",
		"error_description": `needs "write"`,
	}, got[0].Params)
	assert.Equal(t, WWWAuthenticate{Scheme: "Basic", Params: map[string]string{"realm": "simple"}}, got[1])
	assert.Equal(t, WWWAuthenticate{Scheme: "DPoP", Params: map[string]string{"algs": "ES256 PS256"}}, got[2])

	assert.Equal(t, []WWWAuthenticate{{Scheme: "Bearer", Params: map[string]string{}}}, parseWWWAuthenticate([]string{"Bearer"}))
	assert.Empty(t, parseWWWAuthenticate([]string{""}))
}

// recordingAuthorizer issues token-N for the Nth credential and records the
// challenges it answered.
type recordingAuthorizer struct {
	mu         sync.Mutex
	token      int
	challenges []*AuthChallenge
	refuse     error
}

func (a *recordingAuthorizer) Authorize(_ context.Context, req *http.Request) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.token > 0 {
		req.Header.Set("Authorization", "Bearer token-"+string(rune('0'+a.token)))
	}
	return nil
}

func (a *recordingAuthorizer) Challenge(_ context.Context, c *AuthChallenge) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.challenges = append(a.challenges, c)
	if a.refuse != nil {
		return a.refuse
	}
	a.token++
	return nil
}

func TestHTTPDoer_AnswersAChallengeAndResendsTheRequest(t *testing.T) {
	var seen []string
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen, bodies = append(seen, r.Header.Get("Authorization")), append(bodies, string(body))
		switch r.Header.Get("Authorization") {
		case "":
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://rs/.well-known/oauth-protected-resource"`)
			w.WriteHeader(http.StatusUnauthorized)
		case "Bearer token-1":
			w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="admin"`)
			w.WriteHeader(http.StatusForbidden) // step-up
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	auth := &recordingAuthorizer{}
	d := &httpDoer{client: srv.Client(), auth: auth, server: "s"}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/mcp", strings.NewReader(`{"x":1}`))
	resp, err := d.do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Equal(t, []string{"", "Bearer token-1", "Bearer token-2"}, seen, "each attempt carries the newest credentials")
	assert.Equal(t, []string{`{"x":1}`, `{"x":1}`, `{"x":1}`}, bodies, "the body is resent intact")
	require.Len(t, auth.challenges, 2)
	first, second := auth.challenges[0], auth.challenges[1]
	assert.Equal(t, http.StatusUnauthorized, first.Status)
	assert.Equal(t, "https://rs/.well-known/oauth-protected-resource", first.ResourceMetadata)
	assert.Equal(t, srv.URL+"/mcp", first.ResourceURL)
	assert.Equal(t, "s", first.Server)
	assert.Equal(t, 1, first.Attempt)
	assert.Equal(t, http.StatusForbidden, second.Status)
	assert.Equal(t, "insufficient_scope", second.Error)
	assert.Equal(t, "admin", second.Scope)
	assert.Equal(t, 2, second.Attempt)
}

func TestHTTPDoer_StopsAfterMaxChallenges(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="more"`)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	auth := &recordingAuthorizer{}
	d := &httpDoer{client: srv.Client(), auth: auth, server: "s"}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, http.NoBody)
	_, err := d.do(req)
	var authErr *AuthError
	require.ErrorAs(t, err, &authErr)
	assert.ErrorIs(t, err, errAuthRetriesExhausted)
	assert.Equal(t, http.StatusForbidden, authErr.Status)
	assert.Len(t, auth.challenges, maxAuthChallenges)
	assert.Equal(t, maxAuthChallenges+1, requests, "a server that keeps asking is not retried forever")
}

func TestHTTPDoer_AuthorizerFailuresAndPlainForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/forbidden" {
			w.WriteHeader(http.StatusForbidden) // not a scope challenge
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	refusing := &recordingAuthorizer{refuse: errors.New("user declined consent")}
	d := &httpDoer{client: srv.Client(), auth: refusing, server: "s"}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, http.NoBody)
	_, err := d.do(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user declined consent")

	req, _ = http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/forbidden", http.NoBody)
	resp, err := d.do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "a 403 without insufficient_scope is not a challenge")

	none := &httpDoer{client: srv.Client(), server: "s"}
	req, _ = http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, http.NoBody)
	resp, err = none.do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "without an Authorizer the 401 is returned as-is")
}

type failingAuthorizer struct{}

func (failingAuthorizer) Authorize(context.Context, *http.Request) error {
	return errors.New("no credentials for this server")
}
func (failingAuthorizer) Challenge(context.Context, *AuthChallenge) error { return nil }

func TestStreamable_AuthorizerSecuresEveryRequest(t *testing.T) {
	// A modern server that requires a bearer token on every request: the
	// discovery probe is challenged, the host's Authorizer obtains a token,
	// and everything after carries it.
	m := &modernServer{t: t}
	inner := m.start()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token-1" {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+inner.URL+`/.well-known/oauth-protected-resource"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		proxyTo(t, inner.URL, w, r)
	}))
	defer srv.Close()

	auth := &recordingAuthorizer{}
	opts := DefaultClientOptions()
	opts.Authorizer = auth
	c := NewStreamableClientWithOptions(ServerConfig{Name: "secure", URL: srv.URL, TransportName: TransportStreamableHTTP}, opts)
	defer c.Close()
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)
	_, err = c.CallTool(context.Background(), "x", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Len(t, auth.challenges, 1, "one challenge, then the token is reused")
	assert.True(t, c.sess.isModern())

	// Without an Authorizer, a 401 is an error — not a reason to fall back
	// to the handshake era.
	plain := NewStreamableClient(ServerConfig{Name: "secure", URL: srv.URL, TransportName: TransportStreamableHTTP})
	defer plain.Close()
	_, err = plain.Initialize(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")

	failing := DefaultClientOptions()
	failing.Authorizer = failingAuthorizer{}
	broken := NewStreamableClientWithOptions(ServerConfig{Name: "secure", URL: srv.URL, TransportName: TransportStreamableHTTP}, failing)
	defer broken.Close()
	_, err = broken.Initialize(context.Background())
	var authErr *AuthError
	require.ErrorAs(t, err, &authErr)
	assert.Contains(t, err.Error(), "no credentials for this server")
}

// proxyTo forwards a request to another test server.
func proxyTo(t *testing.T, target string, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	body, _ := io.ReadAll(r.Body)
	out, _ := http.NewRequestWithContext(r.Context(), r.Method, target+r.URL.Path, strings.NewReader(string(body)))
	out.Header = r.Header.Clone()
	resp, err := http.DefaultClient.Do(out)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		w.Header()[k] = vs
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func TestRegistry_ConfigureClientSetsPerServerOptions(t *testing.T) {
	var got []string
	auth := &recordingAuthorizer{}
	reg := NewRegistryWithOptions(RegistryOptions{
		ConfigureClient: func(cfg ServerConfig, opts *ClientOptions) {
			got = append(got, cfg.Name)
			if cfg.Name == "secure" {
				opts.Authorizer = auth
			}
		},
	})
	secure := reg.newClientFunc(ServerConfig{Name: "secure", URL: "https://x", TransportName: TransportStreamableHTTP, TimeoutMs: 1234})
	open := reg.newClientFunc(ServerConfig{Name: "open", URL: "https://y", TransportName: TransportStreamableHTTP})
	assert.Equal(t, []string{"secure", "open"}, got)
	assert.Same(t, auth, secure.(*StreamableClient).options.Authorizer)
	assert.EqualValues(t, 1234_000_000, secure.(*StreamableClient).options.RequestTimeout, "the server's timeout is applied first")
	assert.Nil(t, open.(*StreamableClient).options.Authorizer)
}
