package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// authRecorder answers every request with 404 and keeps the last one's path
// and headers.
type authRecorder struct {
	mu     sync.Mutex
	path   string
	header http.Header
}

func (a *authRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	a.path, a.header = r.URL.Path, r.Header.Clone()
	a.mu.Unlock()
	http.NotFound(w, r)
}

// redirectTransport sends every request to target, keeping its path.
type redirectTransport struct{ target *url.URL }

func (r redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.URL.Scheme, out.URL.Host, out.Host = r.target.Scheme, r.target.Host, r.target.Host
	return http.DefaultTransport.RoundTrip(out)
}

// sendOnce points p at a recording server, makes one call, and returns the
// recorded path and headers.
func sendOnce(t *testing.T, p providers.Provider) (string, http.Header) {
	t.Helper()
	rec := &authRecorder{}
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	target, err := url.Parse(srv.URL)
	require.NoError(t, err)
	tp, ok := p.(interface{ SetHTTPTransport(http.RoundTripper) })
	require.True(t, ok, "%T takes no transport", p)
	tp.SetHTTPTransport(redirectTransport{target: target})
	_, _ = p.Predict(context.Background(), providers.PredictionRequest{
		Messages: nil, System: "s", MaxTokens: 1,
	}) // the 404 is not under test
	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.NotNil(t, rec.header, "the provider sent no request")
	return rec.path, rec.header
}

func clearKeys(t *testing.T) {
	t.Helper()
	for _, k := range []string{envOpenAIKey, "OPENAI_TOKEN", envAnthropicKey, envGoogleKey, envGeminiKey} {
		t.Setenv(k, "")
	}
}

// Detect authenticates with the caller's key when there is one, else with the
// environment key of the vendor it picked, in that vendor's own scheme; a
// model name picks the vendor even when only the caller's key exists.
func TestDetect_AuthenticatesWithTheRightKeyAndScheme(t *testing.T) {
	cases := []struct {
		name, apiKey, model string
		env                 map[string]string
		path, header, want  string
	}{
		{name: "openai explicit key", apiKey: "k1", path: "/v1/", header: "Authorization", want: "Bearer k1"},
		{name: "openai env key", env: map[string]string{envOpenAIKey: "env1"},
			path: "/v1/", header: "Authorization", want: "Bearer env1"},
		{name: "explicit key beats env", apiKey: "k2", env: map[string]string{envOpenAIKey: "env2"},
			path: "/v1/", header: "Authorization", want: "Bearer k2"},
		{name: "claude model, key only", apiKey: "k3", model: "claude-sonnet-5",
			path: "/v1/messages", header: "X-Api-Key", want: "k3"},
		{name: "claude model, env key", model: "claude-sonnet-5", env: map[string]string{envAnthropicKey: "env3"},
			path: "/v1/messages", header: "X-Api-Key", want: "env3"},
		{name: "claude model, explicit beats env", apiKey: "k4", model: "claude-sonnet-5",
			env: map[string]string{envAnthropicKey: "env4"}, path: "/v1/messages", header: "X-Api-Key", want: "k4"},
		{name: "gemini model, key only", apiKey: "k5", model: "gemini-3.6-flash",
			path: "/v1beta/models/gemini-3.6-flash", header: "X-Goog-Api-Key", want: "k5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearKeys(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			p, err := Detect(tc.apiKey, tc.model)
			require.NoError(t, err)
			path, header := sendOnce(t, p)
			assert.Contains(t, path, tc.path)
			assert.Equal(t, tc.want, header.Get(tc.header))
		})
	}
}

// With no key anywhere, a vendor-naming model is still not enough.
func TestDetect_ModelWithoutAnyKeyIsNotDetected(t *testing.T) {
	clearKeys(t)
	_, err := Detect("", "claude-sonnet-5")
	assert.ErrorIs(t, err, ErrNotDetected)
}
