package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/imagen"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/ollama"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/vllm"
)

// headerServer records the headers of each request and answers with body.
func headerServer(t *testing.T, body string) (string, <-chan http.Header) {
	t.Helper()
	got := make(chan http.Header, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, got
}

// resolved is what credentials.Resolve returns for an explicit api_key on a
// provider type with no header of its own: a Bearer token.
func resolved(t *testing.T, providerType, key string) providers.Credential {
	t.Helper()
	cred, err := credentials.Resolve(context.Background(), credentials.ResolverConfig{
		ProviderType:     providerType,
		CredentialConfig: &credentials.CredentialConfig{APIKey: key},
	})
	require.NoError(t, err)
	return cred
}

// received returns the headers of the request the server saw, or none if it
// saw no request. The handler records them before it answers, so once the
// call returns they are there or were never sent.
func received(got <-chan http.Header) http.Header {
	select {
	case h := <-got:
		return h
	default:
		return http.Header{}
	}
}

const openAIChatOK = `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`

// vLLM, Ollama and Imagen send the credential the spec resolved. Each keeps
// its own key source as a fallback, below the credential.
func TestSpecCredential_ReachesVLLMOllamaImagen(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "env-google-key")
	t.Setenv("GEMINI_API_KEY", "env-gemini-key")
	cases := map[string]struct {
		spec          providers.ProviderSpec
		body          string
		header, value string
	}{
		"vllm credential beats additional_config.api_key": {
			spec: providers.ProviderSpec{Type: "vllm", Model: "m",
				AdditionalConfig: map[string]any{"api_key": "config-key"}},
			body: openAIChatOK, header: "Authorization", value: "Bearer cred-key",
		},
		"ollama credential for a proxy": {
			spec: providers.ProviderSpec{Type: "ollama", Model: "m"},
			body: openAIChatOK, header: "Authorization", value: "Bearer cred-key",
		},
		"imagen api_key credential beats the env keys": {
			spec: providers.ProviderSpec{Type: "imagen"},
			body: `{}`, header: "X-Goog-Api-Key", value: "cred-key",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			u, got := headerServer(t, tc.body)
			spec := tc.spec
			spec.ID, spec.BaseURL = "p", u
			spec.Credential = resolved(t, spec.Type, "cred-key")
			p, err := providers.CreateProviderFromSpec(spec)
			require.NoError(t, err)
			_ = predictHi(t, p)
			assert.Equal(t, tc.value, received(got).Get(tc.header))
		})
	}
}

// Without a credential the fallbacks still work, in their documented order.
func TestSpecCredential_FallbacksWithoutOne(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "env-google-key")
	t.Setenv("GEMINI_API_KEY", "env-gemini-key")
	none := &credentials.NoOpCredential{}
	cases := map[string]struct {
		spec          providers.ProviderSpec
		body          string
		header, value string
	}{
		"vllm additional_config.api_key": {
			spec: providers.ProviderSpec{Type: "vllm", Model: "m", Credential: none,
				AdditionalConfig: map[string]any{"api_key": "config-key"}},
			body: openAIChatOK, header: "Authorization", value: "Bearer config-key",
		},
		"ollama sends nothing": {
			spec: providers.ProviderSpec{Type: "ollama", Model: "m", Credential: none},
			body: openAIChatOK, header: "Authorization", value: "",
		},
		"imagen GOOGLE_API_KEY first": {
			spec: providers.ProviderSpec{Type: "imagen", Credential: none},
			body: `{}`, header: "X-Goog-Api-Key", value: "env-google-key",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			u, got := headerServer(t, tc.body)
			spec := tc.spec
			spec.ID, spec.BaseURL = "p", u
			p, err := providers.CreateProviderFromSpec(spec)
			require.NoError(t, err)
			_ = predictHi(t, p)
			assert.Equal(t, tc.value, received(got).Get(tc.header))
		})
	}
}

// An Imagen credential that is not an API key (a platform token) is applied
// as it is, and Predict does not refuse for want of an env key.
func TestSpecCredential_ImagenAppliesNonKeyCredential(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	u, got := headerServer(t, `{}`)
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "i", Type: "imagen", BaseURL: u,
		Credential: &recordingCredential{typ: "gcp", log: &credOrderLog{}},
	})
	require.NoError(t, err)
	_ = predictHi(t, p)
	hdr := received(got)
	assert.Equal(t, "gcp-1", hdr.Get("Authorization"))
	assert.Empty(t, hdr.Get("X-Goog-Api-Key"))
}

// With no credential and no key, Imagen still refuses before sending.
func TestSpecCredential_ImagenWithoutAnyKeyRefuses(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{ID: "i", Type: "imagen", BaseURL: "http://127.0.0.1:1"})
	require.NoError(t, err)
	require.ErrorContains(t, predictHi(t, p), "no credential")
}

// The SDK and Arena always resolve a credential. For Imagen with no
// credential block that is the default env key, and it must be the same one
// Imagen always read first: GOOGLE_API_KEY.
func TestSpecCredential_ImagenResolvedEnvKeyKeepsGoogleFirst(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "env-google-key")
	t.Setenv("GEMINI_API_KEY", "env-gemini-key")
	cred, err := credentials.Resolve(context.Background(), credentials.ResolverConfig{ProviderType: "imagen"})
	require.NoError(t, err)
	u, got := headerServer(t, `{}`)
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{ID: "i", Type: "imagen", BaseURL: u, Credential: cred})
	require.NoError(t, err)
	_ = predictHi(t, p)
	assert.Equal(t, "env-google-key", received(got).Get("X-Goog-Api-Key"))
}
