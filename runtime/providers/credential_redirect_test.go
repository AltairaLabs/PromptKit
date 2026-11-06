package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/claude"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/imagen"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/ollama"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/vllm"
)

// gatewayHeader is a custom header a gateway might authenticate with.
const gatewayHeader = "X-Gateway-Key"

// leakProbe is one provider with one kind of credential, and the headers that
// credential sends.
type leakProbe struct {
	headers []string
	build   func(t *testing.T, baseURL string) limitedProvider
}

func awsEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDEXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	t.Setenv("AWS_SESSION_TOKEN", "session")
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/none")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/none")
}

func keyCred(key, header, prefix string) providers.Credential {
	return credentials.NewAPIKeyCredential(key, credentials.WithHeaderName(header), credentials.WithPrefix(prefix))
}

// unnamedCredential sets a header no list knows and cannot name it, like a
// caller's own Credential implementation.
type unnamedCredential struct{}

func (unnamedCredential) Apply(_ context.Context, req *http.Request) error {
	req.Header.Set("X-Unnamed-Token", "secret")
	return nil
}
func (unnamedCredential) Type() string { return "custom" }

func leakProbes() map[string]leakProbe {
	defaults := providers.ProviderDefaults{MaxTokens: 10}
	return map[string]leakProbe{
		"claude bedrock sigv4": {
			headers: []string{"Authorization", "X-Amz-Date", "X-Amz-Content-Sha256", "X-Amz-Security-Token"},
			build: func(t *testing.T, u string) limitedProvider {
				awsEnv(t)
				cred, err := credentials.NewAWSCredential(t.Context(), "us-east-1")
				require.NoError(t, err)
				return claude.NewProviderWithCredential("c", "anthropic.claude-test", u, defaults, false, cred,
					"bedrock", &providers.PlatformConfig{Region: "us-east-1"})
			},
		},
		"openai bedrock sigv4": {
			headers: []string{"Authorization", "X-Amz-Date", "X-Amz-Content-Sha256", "X-Amz-Security-Token"},
			build: func(t *testing.T, u string) limitedProvider {
				awsEnv(t)
				cred, err := credentials.NewAWSCredential(t.Context(), "us-east-1")
				require.NoError(t, err)
				return openai.NewProviderFromConfig(&openai.ProviderConfig{
					ID: "o", Model: "openai.gpt-oss-test", BaseURL: u, Credential: cred, Platform: "bedrock",
					Defaults: defaults,
				})
			},
		},
		"gemini vertex token": {
			headers: []string{"Authorization"},
			build: func(t *testing.T, u string) limitedProvider {
				return gemini.NewProviderWithCredential("g", "gemini-test", u, defaults, false,
					&recordingCredential{typ: "gcp", log: &credOrderLog{}}, "vertex",
					&providers.PlatformConfig{Region: "us-central1", Project: "p"})
			},
		},
		"openai azure api-key": {
			headers: []string{"Api-Key"},
			build: func(_ *testing.T, u string) limitedProvider {
				return openai.NewProviderFromConfig(&openai.ProviderConfig{
					ID: "o", Model: "gpt-test", BaseURL: u, Credential: keyCred("azure-key", "api-key", ""),
					Platform: "azure", Defaults: defaults,
				})
			},
		},
		"claude x-api-key": {
			headers: []string{"X-Api-Key"},
			build: func(_ *testing.T, u string) limitedProvider {
				return claude.NewProviderWithCredential("c", "claude-test", u, defaults, false,
					keyCred("anthropic-key", "X-API-Key", ""), "", nil)
			},
		},
		"gemini ai studio key": {
			headers: []string{"X-Goog-Api-Key"},
			build: func(_ *testing.T, u string) limitedProvider {
				return gemini.NewProviderWithCredential("g", "gemini-test", u, defaults, false,
					credentials.NewAPIKeyCredential("gemini-key"), "", nil)
			},
		},
		"vllm bearer": {
			headers: []string{"Authorization"},
			build: func(_ *testing.T, u string) limitedProvider {
				return vllm.NewProvider("v", "m", u, defaults, false, map[string]any{"api_key": "vllm-key"})
			},
		},
		"imagen key": {
			headers: []string{"X-Goog-Api-Key"},
			build: func(_ *testing.T, u string) limitedProvider {
				return imagen.NewProvider(imagen.Config{ID: "i", BaseURL: u, ApiKey: "imagen-key"})
			},
		},
		"vllm named non-standard header": {
			headers: []string{"X-My-Key"},
			build: func(_ *testing.T, u string) limitedProvider {
				p := vllm.NewProvider("v", "m", u, defaults, false, nil)
				p.SetCredential(keyCred("my-key", "X-My-Key", ""))
				return p
			},
		},
		"ollama unnamed credential": {
			headers: []string{"X-Unnamed-Token"},
			build: func(_ *testing.T, u string) limitedProvider {
				p := ollama.NewToolProvider("ol", "m", u, defaults, false, nil)
				p.SetCredential(unnamedCredential{})
				return p
			},
		},
		"ollama proxy key": {
			headers: []string{"X-Api-Key"},
			build: func(_ *testing.T, u string) limitedProvider {
				p := ollama.NewToolProvider("ol", "m", u, defaults, false, nil)
				p.SetCredential(keyCred("proxy-key", "X-API-Key", ""))
				return p
			},
		},
	}
}

// No credential header, and no custom header, ever follows a redirect to
// another host, with or without a rate limit. http.Client strips only
// Authorization (and keeps it for a subdomain); it copies an API-key header
// or a SigV4 security token to any host.
func TestCredentialHeaders_NeverFollowCrossHostRedirect(t *testing.T) {
	for name, probe := range leakProbes() {
		for _, limited := range []bool{false, true} {
			t.Run(name+map[bool]string{false: " no limit", true: " rate limited"}[limited], func(t *testing.T) {
				targetURL, got := crossHostTarget(t, `{}`)
				var mu sync.Mutex
				var atOrigin []http.Header
				origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					atOrigin = append(atOrigin, r.Header.Clone())
					mu.Unlock()
					http.Redirect(w, r, targetURL+r.URL.RequestURI(), http.StatusTemporaryRedirect)
				}))
				t.Cleanup(origin.Close)

				p := probe.build(t, origin.URL)
				p.(interface{ SetCustomHeaders(map[string]string) }).SetCustomHeaders(
					map[string]string{gatewayHeader: "gateway-secret"})
				if limited {
					p.SetRateLimit(1000, 10)
				}
				_ = predictHi(t, p) // the target's reply need not parse

				mu.Lock()
				require.Len(t, atOrigin, 1)
				for _, h := range append([]string{gatewayHeader}, probe.headers...) {
					assert.NotEmpty(t, atOrigin[0].Get(h), "the origin gets %s", h)
				}
				mu.Unlock()
				hdr := received(got)
				for _, h := range append([]string{gatewayHeader}, probe.headers...) {
					assert.Empty(t, hdr.Get(h), "%s followed the redirect to another host", h)
				}
			})
		}
	}
}

// A redirect back to the origin keeps the credential: only hops that leave
// it lose it.
func TestCredentialHeaders_FollowSameHostRedirect(t *testing.T) {
	for name, probe := range leakProbes() {
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			var hops []http.Header
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				hops = append(hops, r.Header.Clone())
				n := len(hops)
				mu.Unlock()
				if n == 1 {
					http.Redirect(w, r, r.URL.RequestURI(), http.StatusTemporaryRedirect)
					return
				}
				_, _ = w.Write([]byte(`{}`))
			}))
			t.Cleanup(origin.Close)

			p := probe.build(t, origin.URL)
			_ = predictHi(t, p)

			mu.Lock()
			defer mu.Unlock()
			require.Len(t, hops, 2)
			for _, h := range probe.headers {
				assert.NotEmpty(t, hops[1].Get(h), "the same-host hop keeps %s", h)
			}
		})
	}
}

// A caller's own CheckRedirect still runs after the provider strips.
func TestNewBaseProvider_KeepsCallerCheckRedirect(t *testing.T) {
	var calls int
	stop := http.ErrUseLastResponse
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		calls++
		return stop
	}}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/next", http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	bp := providers.NewBaseProvider("p", false, client)
	resp, err := bp.GetHTTPClient().Get(origin.URL)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Equal(t, 1, calls)
	assert.True(t, strings.HasPrefix(origin.URL, "http://127.0.0.1"))
}
