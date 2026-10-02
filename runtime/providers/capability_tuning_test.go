package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"

	// The openai import also registers its factories; voyageai is imported
	// for that side effect alone.
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/voyageai"
)

const tuningEmbeddingBody = `{"object":"list","data":[{"object":"embedding","index":0,` +
	`"embedding":[0.1,0.2,0.3]}],"model":"m","usage":{"prompt_tokens":1,"total_tokens":1}}`

// tuningCapture is an httptest handler that records the last request's
// headers and query and answers with a fixed JSON body.
type tuningCapture struct {
	mu     sync.Mutex
	header http.Header
	query  string
	body   string
}

func (c *tuningCapture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	c.header = r.Header.Clone()
	c.query = r.URL.RawQuery
	c.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(c.body))
}

func (c *tuningCapture) seen() (http.Header, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.header, c.query
}

// tuningPlatformCred stands in for a platform credential (Azure AD and the
// like): it sets a bearer token on every request.
type tuningPlatformCred struct{}

func (tuningPlatformCred) Apply(_ context.Context, req *http.Request) error {
	req.Header.Set("Authorization", "Bearer platform-token")
	return nil
}

func (tuningPlatformCred) Type() string { return "fake-platform" }

func TestCreateEmbeddingProviderFromSpec_TuningHeaderAndTimeout(t *testing.T) {
	capture := &tuningCapture{body: tuningEmbeddingBody}
	srv := httptest.NewServer(capture)
	defer srv.Close()

	ep, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type: "openai", BaseURL: srv.URL, AdditionalConfig: map[string]any{"dimensions": 3},
		Credential: credentials.NewAPIKeyCredential("sk-test"),
		Tuning: base.HTTPTuning{
			Headers:        map[string]string{"X-Gateway": "gw"},
			RequestTimeout: 7 * time.Second,
		},
	})
	require.NoError(t, err)

	_, err = ep.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"a"}})
	require.NoError(t, err)
	h, _ := capture.seen()
	assert.Equal(t, "gw", h.Get("X-Gateway"))
	assert.Equal(t, "Bearer sk-test", h.Get("Authorization"), "the credential is still sent")

	oep, ok := ep.(*openai.EmbeddingProvider)
	require.True(t, ok, "got %T", ep)
	assert.Equal(t, 7*time.Second, oep.HTTPClient.Timeout)
}

func TestCreateEmbeddingProviderFromSpec_TuningTimeoutCutsCallShort(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(500 * time.Millisecond):
		case <-release:
		}
		_, _ = w.Write([]byte(tuningEmbeddingBody))
	}))
	defer srv.Close()
	defer close(release)

	ep, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type: "openai", BaseURL: srv.URL, AdditionalConfig: map[string]any{"dimensions": 3},
		Credential: credentials.NewAPIKeyCredential("sk-test"),
		Tuning:     base.HTTPTuning{RequestTimeout: 50 * time.Millisecond},
	})
	require.NoError(t, err)

	start := time.Now()
	_, err = ep.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"a"}})
	require.Error(t, err)
	assert.Less(t, time.Since(start), 400*time.Millisecond)
}

func TestCreateEmbeddingProviderFromSpec_AzureTuningKeepsPlatformAuth(t *testing.T) {
	capture := &tuningCapture{body: tuningEmbeddingBody}
	srv := httptest.NewServer(capture)
	defer srv.Close()

	ep, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type: "openai", Model: "text-embedding-3-small",
		Credential:       tuningPlatformCred{},
		AdditionalConfig: map[string]any{"dimensions": 3},
		Platform:         "azure",
		PlatformConfig: &providers.PlatformConfig{
			Type: "azure", Endpoint: srv.URL,
			AdditionalConfig: map[string]any{"api_version": "2099-01-01"},
		},
		Tuning: base.HTTPTuning{
			Headers:        map[string]string{"X-Gateway": "gw"},
			RequestTimeout: 9 * time.Second,
		},
	})
	require.NoError(t, err)

	_, err = ep.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"a"}})
	require.NoError(t, err)
	h, q := capture.seen()
	assert.Equal(t, "Bearer platform-token", h.Get("Authorization"), "platform credential still applied")
	oep, ok := ep.(*openai.EmbeddingProvider)
	require.True(t, ok, "got %T", ep)
	assert.Equal(t, 9*time.Second, oep.HTTPClient.Timeout)
	assert.Equal(t, "gw", h.Get("X-Gateway"))
	assert.Contains(t, q, "api-version=2099-01-01")
}

func TestCreateEmbeddingProviderFromSpec_AzureHeaderCollidesWithCredential(t *testing.T) {
	capture := &tuningCapture{body: tuningEmbeddingBody}
	srv := httptest.NewServer(capture)
	defer srv.Close()

	ep, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
		Type: "openai", Model: "m",
		Credential:     tuningPlatformCred{},
		Platform:       "azure",
		PlatformConfig: &providers.PlatformConfig{Type: "azure", Endpoint: srv.URL},
		Tuning:         base.HTTPTuning{Headers: map[string]string{"Authorization": "Bearer mine"}},
	})
	require.NoError(t, err)

	_, err = ep.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"a"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collides with built-in header")
	h, _ := capture.seen()
	assert.Nil(t, h, "the request must not reach the server")
}

func TestCreateRerankProviderFromSpec_VoyageTuningHeaderReachesServer(t *testing.T) {
	capture := &tuningCapture{body: `{"object":"list","data":[{"index":0,"relevance_score":0.9}],` +
		`"model":"rerank-2.5","usage":{"total_tokens":1}}`}
	srv := httptest.NewServer(capture)
	defer srv.Close()

	rp, err := providers.CreateRerankProviderFromSpec(providers.RerankProviderSpec{
		Type: "voyageai", BaseURL: srv.URL,
		Credential: credentials.NewAPIKeyCredential("vk"),
		Tuning:     base.HTTPTuning{Headers: map[string]string{"X-Gateway": "gw"}},
	})
	require.NoError(t, err)

	_, err = rp.Rerank(context.Background(), providers.RerankRequest{Query: "q", Documents: []string{"d"}})
	require.NoError(t, err)
	h, _ := capture.seen()
	assert.Equal(t, "gw", h.Get("X-Gateway"))
}

func TestCreateRerankProviderFromSpec_MockRejectsTuning(t *testing.T) {
	_, err := providers.CreateRerankProviderFromSpec(providers.RerankProviderSpec{
		Type:   "mock",
		Tuning: base.HTTPTuning{Headers: map[string]string{"X-Gateway": "gw"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `provider type "mock" does not support headers`)
}
