package base_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// countingToken issues a fresh bearer token on every Apply.
type countingToken struct{ n int }

func (c *countingToken) Apply(_ context.Context, req *http.Request) error {
	c.n++
	req.Header.Set("Authorization", "Bearer tok-"+string(rune('0'+c.n)))
	return nil
}

func (c *countingToken) Type() string { return "azure" }

type seenRequest struct {
	Path  string
	Query url.Values
	Auth  string
}

func recordingServer(t *testing.T) (*httptest.Server, *[]seenRequest) {
	t.Helper()
	var seen []seenRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, seenRequest{Path: r.URL.Path, Query: r.URL.Query(), Auth: r.Header.Get("Authorization")})
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

// buildAzureService builds the HTTP fields a TTS/STT service would get from
// AzureOpenAIServiceOptions for spec.
func buildAzureService(t *testing.T, spec *base.CapabilitySpec) *base.HTTPServiceFields {
	t.Helper()
	opts, err := base.AzureOpenAIServiceOptions(spec, time.Second)
	require.NoError(t, err)
	_, fields := base.NewHTTPService("", base.HTTPServiceDefaults{BaseURL: "https://api.openai.com/v1"}, opts...)
	return fields
}

func send(t *testing.T, f *base.HTTPServiceFields, path string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, f.BaseURL+path, http.NoBody)
	require.NoError(t, err)
	resp, err := f.Client.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
}

// An OpenAI-wire service on Azure calls its deployment's route with the
// platform's api-version and a credential applied to each request, so a
// token refreshed between requests is the one sent.
func TestAzureOpenAIServiceOptions_AddressTheDeploymentWithAFreshToken(t *testing.T) {
	srv, seen := recordingServer(t)
	f := buildAzureService(t, &base.CapabilitySpec{
		ID: "voice", Type: "openai", Model: "my-tts", Credential: &countingToken{},
		Platform: &credentials.PlatformConfig{
			Type: "azure", Endpoint: srv.URL, AdditionalConfig: map[string]any{"api_version": "2025-04-01-preview"},
		},
	})
	send(t, f, "/audio/speech")
	send(t, f, "/audio/speech")

	require.Len(t, *seen, 2)
	assert.Equal(t, "/openai/deployments/my-tts/audio/speech", (*seen)[0].Path)
	assert.Equal(t, "2025-04-01-preview", (*seen)[0].Query.Get("api-version"))
	assert.Equal(t, []string{"Bearer tok-1", "Bearer tok-2"}, []string{(*seen)[0].Auth, (*seen)[1].Auth})
}

// An explicit BaseURL wins over the derived deployment path, and the default
// api-version applies when the platform sets none. A request that already
// carries an api-version keeps it.
func TestAzureOpenAIServiceOptions_BaseURLOverrideAndDefaultAPIVersion(t *testing.T) {
	srv, seen := recordingServer(t)
	f := buildAzureService(t, &base.CapabilitySpec{
		ID: "voice", Type: "openai", Model: "my-tts", BaseURL: srv.URL + "/proxy", Credential: &countingToken{},
		Platform: &credentials.PlatformConfig{Type: "azure", Endpoint: "https://unused.openai.azure.com"},
	})
	send(t, f, "/audio/speech")
	send(t, f, "/audio/speech?api-version=pinned")

	require.Len(t, *seen, 2)
	assert.Equal(t, "/proxy/audio/speech", (*seen)[0].Path)
	assert.Equal(t, credentials.DefaultAzureAPIVersion, (*seen)[0].Query.Get("api-version"))
	assert.Equal(t, "pinned", (*seen)[1].Query.Get("api-version"))
}

func TestAzureOpenAIServiceOptions_RefusesAnIncompleteSpec(t *testing.T) {
	azure := &credentials.PlatformConfig{Type: "azure", Endpoint: "https://res.openai.azure.com"}
	cases := map[string]struct {
		spec base.CapabilitySpec
		want string
	}{
		"no endpoint": {
			base.CapabilitySpec{ID: "v", Type: "openai", Model: "d", Credential: &countingToken{},
				Platform: &credentials.PlatformConfig{Type: "azure"}},
			`openai "v" on azure needs platform.endpoint`,
		},
		"no deployment": {
			base.CapabilitySpec{ID: "v", Type: "openai", Credential: &countingToken{}, Platform: azure},
			`openai "v" on azure needs model: the deployment name`,
		},
		"no credential": {
			base.CapabilitySpec{ID: "v", Type: "openai", Model: "d", Platform: azure},
			`openai "v" on azure needs a platform credential`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := base.AzureOpenAIServiceOptions(&tc.spec, time.Second)
			require.Error(t, err)
			assert.Equal(t, tc.want, err.Error())
		})
	}
}

// A type that declares a platform receives it; one that does not is built
// without it and the registry warns that the platform is ignored.
func TestFactoryRegistry_PlatformReachesOnlyDeclaredTypes(t *testing.T) {
	var logs bytes.Buffer
	logger.SetLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { logger.SetLogger(nil) })

	r := base.NewFactoryRegistry[*credentials.PlatformConfig]()
	echo := func(spec base.CapabilitySpec) (*credentials.PlatformConfig, error) { return spec.Platform, nil }
	r.Register("hosted", echo)
	r.Register("local", echo)
	r.RegisterPlatforms("hosted", "azure")
	azure := &credentials.PlatformConfig{Type: "azure", Endpoint: "https://res"}

	got, err := r.Create(base.CapabilitySpec{ID: "a", Type: "hosted", Platform: azure})
	require.NoError(t, err)
	assert.Same(t, azure, got)
	assert.Empty(t, logs.String())

	got, err = r.Create(base.CapabilitySpec{ID: "b", Type: "local", Platform: azure})
	require.NoError(t, err)
	assert.Nil(t, got)
	assert.Contains(t, logs.String(), "provider=b type=local platform=azure")

	assert.True(t, r.SupportsPlatform("hosted", "azure"))
	assert.False(t, r.SupportsPlatform("hosted", "vertex"))
}

// HTTP tuning wraps the connection under the credential layer: a tuned
// client sends the provider file's headers and still authenticates.
func TestAzureHTTPClient_TuningKeepsTheCredential(t *testing.T) {
	var gotAuth, gotTenant string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotTenant = r.Header.Get("Authorization"), r.Header.Get("X-Tenant")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	client := base.HTTPTuning{Headers: map[string]string{"X-Tenant": "acme"}}.Client(
		base.AzureHTTPClient(&countingToken{}, &credentials.PlatformConfig{Type: "azure"}, time.Second))
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, http.NoBody)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, "Bearer tok-1", gotAuth)
	assert.Equal(t, "acme", gotTenant)
}

// A platform's type matches case-insensitively, as the credential resolver
// does; a platform block with no type hosts nothing, and is dropped with a
// warning rather than steering the factory onto a platform path.
func TestFactoryRegistry_PlatformTypeMatching(t *testing.T) {
	var logs bytes.Buffer
	logger.SetLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { logger.SetLogger(nil) })

	r := base.NewFactoryRegistry[*credentials.PlatformConfig]()
	r.Register("hosted", func(spec base.CapabilitySpec) (*credentials.PlatformConfig, error) { return spec.Platform, nil })
	r.RegisterPlatforms("hosted", "azure")

	capitalized := &credentials.PlatformConfig{Type: "Azure", Endpoint: "https://res"}
	got, err := r.Create(base.CapabilitySpec{ID: "a", Type: "hosted", Platform: capitalized})
	require.NoError(t, err)
	assert.Same(t, capitalized, got)

	got, err = r.Create(base.CapabilitySpec{ID: "b", Type: "hosted",
		Platform: &credentials.PlatformConfig{Endpoint: "https://res"}})
	require.NoError(t, err)
	assert.Nil(t, got)
	assert.Contains(t, logs.String(), `provider=b type=hosted platform=""`)
}

// The registry resolves a declared platform's credential chain, and the
// type's own key otherwise.
func TestFactoryRegistry_ResolveCredential(t *testing.T) {
	r := base.NewFactoryRegistry[*credentials.PlatformConfig]()
	r.RegisterPlatforms("openai", "azure")
	ctx := context.Background()
	key := &credentials.CredentialConfig{APIKey: "own-key"}

	cred, err := r.ResolveCredential(ctx, "openai", "", key,
		&credentials.PlatformConfig{Type: "azure", Endpoint: "https://res.openai.azure.com"})
	require.NoError(t, err)
	assert.Equal(t, "azure", cred.Type())

	cred, err = r.ResolveCredential(ctx, "openai", "", key, &credentials.PlatformConfig{Type: "vertex"})
	require.NoError(t, err)
	assert.Equal(t, "own-key", base.APIKeyFromCredential(cred))
}

// OpenAIWireAuth returns the static key for an unhosted spec, and for an
// Azure one the platform client and no key.
func TestOpenAIWireAuth(t *testing.T) {
	opts, key, err := base.OpenAIWireAuth(&base.CapabilitySpec{Type: "openai",
		Credential: credentials.NewAPIKeyCredential("sk")}, time.Second)
	require.NoError(t, err)
	assert.Equal(t, "sk", key)
	assert.Empty(t, opts)

	srv, seen := recordingServer(t)
	opts, key, err = base.OpenAIWireAuth(&base.CapabilitySpec{Type: "openai", Model: "dep",
		Credential: &countingToken{}, Platform: &credentials.PlatformConfig{Type: "azure", Endpoint: srv.URL}}, time.Second)
	require.NoError(t, err)
	assert.Empty(t, key)
	_, fields := base.NewHTTPService(key, base.HTTPServiceDefaults{BaseURL: "https://api.openai.com/v1"}, opts...)
	send(t, fields, "/audio/transcriptions")
	require.Len(t, *seen, 1)
	assert.Equal(t, "/openai/deployments/dep/audio/transcriptions", (*seen)[0].Path)
}
