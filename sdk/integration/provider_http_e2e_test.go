package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/v2/config"
	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	geminiprov "github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	// vllm is not registered by the SDK; a caller declaring it imports it.
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/vllm"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// These tests drive a real HTTP provider adapter, built from a provider config
// the way a caller declares it, against an httptest server. What they assert
// on is what the server received.

const testAPIKey = "sk-e2e-secret"

// providerPath builds the SDK option that declares a provider, one per way a
// caller can declare it. Each path has its own conversion into
// providers.ProviderSpec, so each is run.
type providerPath struct {
	name string
	opt  func(t *testing.T, vc vendorCase, baseURL, apiKey, extraSpec string, spec sdk.ProviderSpec) sdk.Option
}

var providerPaths = []providerPath{
	{
		name: "provider_file",
		opt: func(t *testing.T, vc vendorCase, baseURL, apiKey, extraSpec string, _ sdk.ProviderSpec) sdk.Option {
			return sdk.WithProviderFile(writeProviderFile(t, vc, baseURL, apiKey, extraSpec))
		},
	},
	{
		name: "runtime_config",
		opt: func(t *testing.T, vc vendorCase, baseURL, apiKey, extraSpec string, _ sdk.ProviderSpec) sdk.Option {
			return sdk.WithRuntimeConfig(writeRuntimeConfigWithProvider(t, vc, baseURL, apiKey, extraSpec))
		},
	},
	{
		name: "llm_provider_spec",
		opt: func(_ *testing.T, vc vendorCase, baseURL, apiKey, _ string, spec sdk.ProviderSpec) sdk.Option {
			spec.ID = vc.name + "-under-test"
			spec.Type = vc.typ
			spec.Model = vc.model
			spec.BaseURL = baseURL + vc.baseSuffix
			spec.AdditionalConfig = vc.extra
			if apiKey != "" {
				spec.Credential = &credentials.CredentialConfig{APIKey: apiKey}
			}
			return sdk.WithLLMProvider(spec)
		},
	},
}

// toolPromptPackJSON offers the model a tool, so a turn goes through the
// provider's tool-calling request path rather than the plain one.
const toolPromptPackJSON = `{
	"id": "integration-test-tool-prompt",
	"version": "1.0.0",
	"description": "Pack whose prompt offers a tool",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant with tools.",
			"tools": ["get_weather"]
		}
	},
	"tools": {
		"get_weather": {
			"name": "get_weather",
			"description": "Get weather for a city",
			"parameters": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}
		}
	}
}`

// turnShapes are the two request paths every HTTP provider has: a plain turn
// and a turn that offers tools.
var turnShapes = []struct{ name, pack string }{
	{"plain", minimalPackJSON},
	{"tools", toolPromptPackJSON},
}

func openHTTPConv(t *testing.T, opt sdk.Option, extra ...sdk.Option) *sdk.Conversation {
	t.Helper()
	return openHTTPConvWithPack(t, minimalPackJSON, opt, extra...)
}

func openHTTPConvWithPack(t *testing.T, packJSON string, opt sdk.Option, extra ...sdk.Option) *sdk.Conversation {
	t.Helper()
	packPath := writePackFile(t, packJSON)
	opts := append([]sdk.Option{opt, sdk.WithSkipSchemaValidation()}, extra...)
	conv, err := sdk.Open(packPath, "chat", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv
}

// TestProviderRateLimit_SpacesEveryRequest declares rate_limit on a provider
// and checks the server sees the requests spaced at the configured rate, for
// unary and streaming turns alike (one bucket per provider instance), on
// every way of declaring the provider.
func TestProviderRateLimit_SpacesEveryRequest(t *testing.T) {
	const rps = 20 // one request every 50ms with burst 1
	vc := vendorCases[0]
	for _, path := range providerPaths {
		t.Run(path.name, func(t *testing.T) {
			srv := newFakeVendorServer(t)
			conv := openHTTPConv(t, path.opt(t, vc, srv.URL, testAPIKey,
				"  rate_limit:\n    rps: 20\n    burst: 1\n",
				sdk.ProviderSpec{RateLimit: pkgconfig.RateLimit{RPS: rps, Burst: 1}}))

			ctx := context.Background()
			_, err := conv.Send(ctx, "one")
			require.NoError(t, err)
			for chunk := range conv.Stream(ctx, "two") {
				require.NoError(t, chunk.Error)
			}
			_, err = conv.Send(ctx, "three")
			require.NoError(t, err)

			got := srv.got()
			require.Len(t, got, 3, "every turn reaches the server once")
			span := got[2].At.Sub(got[0].At)
			// Three requests at 20 rps with burst 1 take at least two
			// intervals (100ms). Allow scheduling slack below that.
			assert.GreaterOrEqual(t, span, 85*time.Millisecond,
				"rate_limit was not applied: 3 requests arrived within %s", span)
		})
	}
}

// TestProviderCredential_ReachesEveryRequestUnderRateLimit checks the
// credential is on every request, including those that waited for a rate
// limit token, for every HTTP provider type, on every declaration path.
func TestProviderCredential_ReachesEveryRequestUnderRateLimit(t *testing.T) {
	for _, vc := range vendorCases {
		for _, path := range providerPaths {
			for _, shape := range turnShapes {
				t.Run(vc.name+"/"+path.name+"/"+shape.name, func(t *testing.T) {
					srv := newFakeVendorServer(t)
					conv := openHTTPConvWithPack(t, shape.pack, path.opt(t, vc, srv.URL, testAPIKey,
						"  rate_limit:\n    rps: 50\n    burst: 1\n",
						sdk.ProviderSpec{RateLimit: pkgconfig.RateLimit{RPS: 50, Burst: 1}}))

					ctx := context.Background()
					for _, msg := range []string{"one", "two"} {
						resp, err := conv.Send(ctx, msg)
						require.NoError(t, err)
						assert.Equal(t, "fake reply", resp.Text())
					}
					got := srv.got()
					require.Len(t, got, 2)
					for i, r := range got {
						assert.Equal(t, vc.authValue(testAPIKey), r.Header.Get(vc.authHeader),
							"request %d to %s lacks the credential", i, r.Path)
					}
				})
			}
		}
	}
}

// TestProviderRedirect_CredentialAndHeadersStayOnOrigin sends a turn to a
// provider whose server redirects the request. A same-origin 307 keeps the
// credential and the custom headers; a 307 to another host drops both.
func TestProviderRedirect_CredentialAndHeadersStayOnOrigin(t *testing.T) {
	const customHeader = "X-Gateway-Token"
	const customValue = "gw-secret"
	headers := map[string]string{customHeader: customValue}
	headerYAML := "  headers:\n    " + customHeader + ": " + customValue + "\n"

	for _, vc := range vendorCases {
		for _, path := range providerPaths {
			for _, shape := range turnShapes {
				name := vc.name + "/" + path.name + "/" + shape.name
				t.Run(name+"/cross_host", func(t *testing.T) {
					origin := newFakeVendorServer(t)
					other := newFakeVendorServer(t)
					origin.setRedirect(func(r *http.Request) (string, int) {
						return other.URL + r.URL.RequestURI(), http.StatusTemporaryRedirect
					})
					conv := openHTTPConvWithPack(t, shape.pack, path.opt(t, vc, origin.URL, testAPIKey, headerYAML,
						sdk.ProviderSpec{Headers: headers}))

					resp, err := conv.Send(context.Background(), "hello")
					require.NoError(t, err)
					assert.Equal(t, "fake reply", resp.Text())

					first := origin.got()
					require.Len(t, first, 1)
					assert.Equal(t, vc.authValue(testAPIKey), first[0].Header.Get(vc.authHeader),
						"the origin request carries the credential")
					assert.Equal(t, customValue, first[0].Header.Get(customHeader))

					hop := other.got()
					require.Len(t, hop, 1, "the redirect is followed")
					for _, h := range []string{"Authorization", "X-Api-Key", "X-Goog-Api-Key", "Api-Key", customHeader} {
						assert.Empty(t, hop[0].Header.Get(h), "%s followed the redirect to another host", h)
					}
				})

				t.Run(name+"/same_origin", func(t *testing.T) {
					srv := newFakeVendorServer(t)
					srv.setRedirect(func(r *http.Request) (string, int) {
						if r.URL.Query().Get("hop") != "" {
							return "", 0
						}
						q := r.URL.Query()
						q.Set("hop", "1")
						return r.URL.Path + "?" + q.Encode(), http.StatusTemporaryRedirect
					})
					conv := openHTTPConvWithPack(t, shape.pack, path.opt(t, vc, srv.URL, testAPIKey, headerYAML,
						sdk.ProviderSpec{Headers: headers}))

					resp, err := conv.Send(context.Background(), "hello")
					require.NoError(t, err)
					assert.Equal(t, "fake reply", resp.Text())

					got := srv.got()
					require.Len(t, got, 2, "the request and its same-origin hop")
					for i, r := range got {
						assert.Equal(t, vc.authValue(testAPIKey), r.Header.Get(vc.authHeader),
							"request %d (%s) lacks the credential", i, r.Path)
						assert.Equal(t, customValue, r.Header.Get(customHeader),
							"request %d (%s) lacks the custom header", i, r.Path)
					}
				})
			}
		}
	}
}

// TestEmbeddingRedirect_CredentialStaysOnOrigin runs retrieval against an
// embedding endpoint that redirects to another host, with the embedding
// provider built two ways: by the SDK from a spec (with custom headers), and
// by the caller around its own *http.Client. Neither the key nor a custom
// header may reach the other host.
func TestEmbeddingRedirect_CredentialStaysOnOrigin(t *testing.T) {
	const customHeader = "X-Gateway-Token"
	cases := []struct {
		name string
		opt  func(t *testing.T, baseURL string) sdk.Option
	}{
		{
			name: "sdk_spec",
			opt: func(_ *testing.T, baseURL string) sdk.Option {
				return sdk.WithEmbeddingProvider(sdk.ProviderSpec{
					ID: "emb", Type: "openai", Model: "text-embedding-3-small", BaseURL: baseURL,
					Credential: &credentials.CredentialConfig{APIKey: testAPIKey},
					Headers:    map[string]string{customHeader: "gw-secret"},
				})
			},
		},
		{
			// http.Client strips Authorization on a redirect to another port
			// itself; x-goog-api-key it copies, so only the policy keeps it.
			name: "caller_client",
			opt: func(t *testing.T, baseURL string) sdk.Option {
				ep, err := geminiprov.NewEmbeddingProvider(
					geminiprov.WithGeminiEmbeddingBaseURL(baseURL),
					geminiprov.WithGeminiEmbeddingAPIKey(testAPIKey),
					geminiprov.WithGeminiEmbeddingHTTPClient(&http.Client{Timeout: 5 * time.Second}),
				)
				require.NoError(t, err)
				return sdk.WithContextRetrieval(ep, 2)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origin := newFakeVendorServer(t)
			other := newFakeVendorServer(t)
			origin.setRedirect(func(r *http.Request) (string, int) {
				if isEmbeddingPath(r.URL.Path) {
					return other.URL + r.URL.RequestURI(), http.StatusTemporaryRedirect
				}
				return "", 0
			})
			conv := openHTTPConv(t, sdk.WithProvider(mock.NewProvider("agent", "m", false)),
				tc.opt(t, origin.URL+"/v1"),
				sdk.WithStateStore(statestore.NewMemoryStore()),
				sdk.WithContextWindow(1),
			)
			ctx := context.Background()
			for _, msg := range []string{"first message", "second message", "third message"} {
				_, err := conv.Send(ctx, msg)
				require.NoError(t, err)
			}

			var embedHops int
			for _, r := range other.got() {
				if !isEmbeddingPath(r.Path) {
					continue
				}
				embedHops++
				for _, h := range []string{"Authorization", "X-Goog-Api-Key", customHeader} {
					assert.Empty(t, r.Header.Get(h), "%s followed the embedding redirect to another host", h)
				}
			}
			require.Positive(t, embedHops, "retrieval never called the embedding endpoint")
			for _, r := range origin.got() {
				if isEmbeddingPath(r.Path) {
					assert.True(t, r.Header.Get("Authorization") == "Bearer "+testAPIKey ||
						r.Header.Get("X-Goog-Api-Key") == testAPIKey, "the origin embedding request lacks the key")
				}
			}
		})
	}
}

func isEmbeddingPath(p string) bool {
	return strings.HasSuffix(p, "/embeddings") || strings.Contains(p, "mbedContent")
}
