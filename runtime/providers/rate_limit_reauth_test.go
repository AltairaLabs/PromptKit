package providers_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/claude"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/imagen"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/vllm"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// credentialFamily is one completion provider on a platform whose credential
// goes stale: a SigV4 signature (Bedrock) or a short-lived token (Vertex,
// Azure).
type credentialFamily struct {
	credType string
	body     string
	build    func(baseURL string, cred providers.Credential) limitedProvider
}

func credentialFamilies() map[string]credentialFamily {
	return map[string]credentialFamily{
		"claude bedrock sigv4": {
			credType: "aws",
			body:     `{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`,
			build: func(baseURL string, cred providers.Credential) limitedProvider {
				return claude.NewProviderWithCredential("c", "anthropic.claude-test", baseURL,
					providers.ProviderDefaults{MaxTokens: 10}, false, cred, "bedrock",
					&providers.PlatformConfig{Region: "us-east-1"})
			},
		},
		"claude vertex token": {
			credType: "gcp",
			body:     `{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`,
			build: func(baseURL string, cred providers.Credential) limitedProvider {
				return claude.NewProviderWithCredential("c", "claude-test", baseURL,
					providers.ProviderDefaults{MaxTokens: 10}, false, cred, "vertex",
					&providers.PlatformConfig{Region: "us-east5", Project: "p"})
			},
		},
		"gemini vertex token": {
			credType: "gcp",
			body:     `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`,
			build: func(baseURL string, cred providers.Credential) limitedProvider {
				return gemini.NewProviderWithCredential("g", "gemini-test", baseURL,
					providers.ProviderDefaults{MaxTokens: 10}, false, cred, "vertex",
					&providers.PlatformConfig{Region: "us-central1", Project: "p"})
			},
		},
		"openai azure token": {
			credType: "azure",
			body:     `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`,
			build: func(baseURL string, cred providers.Credential) limitedProvider {
				return openai.NewProviderFromConfig(&openai.ProviderConfig{
					ID: "o", Model: "gpt-test", BaseURL: baseURL, Credential: cred, Platform: "azure",
					Defaults: providers.ProviderDefaults{MaxTokens: 10},
				})
			},
		},
		"openai bedrock sigv4": {
			credType: "aws",
			body:     `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`,
			build: func(baseURL string, cred providers.Credential) limitedProvider {
				return openai.NewProviderFromConfig(&openai.ProviderConfig{
					ID: "o", Model: "openai.gpt-oss-test", BaseURL: baseURL, Credential: cred, Platform: "bedrock",
					Defaults: providers.ProviderDefaults{MaxTokens: 10},
				})
			},
		},
	}
}

func predictHi(t *testing.T, p providers.Provider) error {
	t.Helper()
	_, err := p.Predict(t.Context(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	return err
}

// nthIndex returns the position of the n-th (1-based) occurrence of e.
func (l *credOrderLog) nthIndex(e string, n int) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	seen := 0
	for i, got := range l.events {
		if got == e {
			seen++
			if seen == n {
				return i
			}
		}
	}
	return -1
}

// count returns how many events start with prefix.
func (l *credOrderLog) count(prefix string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, got := range l.events {
		if strings.HasPrefix(got, prefix) {
			n++
		}
	}
	return n
}

// Under a rate limit the credential is applied exactly once per attempt, and
// after its wait: a SigV4 body is hashed once, a token fetched once.
func TestCredentialFamilies_ApplyOncePerAttemptUnderRateLimit(t *testing.T) {
	for name, fam := range credentialFamilies() {
		t.Run(name, func(t *testing.T) {
			sent := make(chan string, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sent <- r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(fam.body))
			}))
			t.Cleanup(srv.Close)

			log := &credOrderLog{}
			p := fam.build(srv.URL, &recordingCredential{typ: fam.credType, log: log})
			waiter := newHeldWaiter(log)
			holdRateLimit(t, p, waiter)
			waiter.release <- struct{}{}

			require.NoError(t, predictHi(t, p))
			assert.Equal(t, 1, log.count("apply "), "one attempt applies the credential once; events %s", log)
			auth := <-sent
			assert.Greater(t, log.index("apply "+auth), log.index("wait-end"),
				"the sent credential was applied after the wait; events %s", log)
		})
	}
}

// A same-host 307 is a new attempt with its own wait. http.Client builds the
// hop's headers from the original request, so unless the hop is
// re-authorized it resends the credential applied before the first wait, or
// none at all. Each hop must carry a credential applied after its own wait.
func TestCredentialFamilies_ReauthorizeSameHostRedirect(t *testing.T) {
	for name, fam := range credentialFamilies() {
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			var hops []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				hops = append(hops, r.Header.Get("Authorization"))
				first := len(hops) == 1
				mu.Unlock()
				if first {
					// Redirect to itself.
					http.Redirect(w, r, r.URL.RequestURI(), http.StatusTemporaryRedirect)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(fam.body))
			}))
			t.Cleanup(srv.Close)

			log := &credOrderLog{}
			p := fam.build(srv.URL, &recordingCredential{typ: fam.credType, log: log})
			waiter := newHeldWaiter(log)
			holdRateLimit(t, p, waiter)
			waiter.release <- struct{}{}
			waiter.release <- struct{}{}

			require.NoError(t, predictHi(t, p))
			mu.Lock()
			defer mu.Unlock()
			require.Len(t, hops, 2, "events %s", log)
			for i, auth := range hops {
				require.NotEmpty(t, auth, "hop %d carried no credential; events %s", i+1, log)
				assert.Greater(t, log.index("apply "+auth), log.nthIndex("wait-end", i+1),
					"hop %d sent %q, applied before its own wait ended; events %s", i+1, auth, log)
			}
			assert.Equal(t, 2, log.count("apply "), "one application per hop; events %s", log)
		})
	}
}

// crossHostTarget starts a server that records the headers it receives, and
// returns a URL for it that http.Client treats as another host than the
// 127.0.0.1 address httptest servers use.
func crossHostTarget(t *testing.T, body string) (targetURL string, got <-chan http.Header) {
	t.Helper()
	ch := make(chan http.Header, 4)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ch <- r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(target.Close)
	return strings.Replace(target.URL, "127.0.0.1", "localhost", 1), ch
}

// A redirect to another host never gets the credential under a rate limit:
// not the Authorization header http.Client strips itself, and not the SigV4
// headers it would copy from a request signed while it was built.
func TestCredentialFamilies_NeverAuthorizeCrossHostRedirect(t *testing.T) {
	for name, fam := range credentialFamilies() {
		t.Run(name, func(t *testing.T) {
			targetURL, got := crossHostTarget(t, fam.body)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, targetURL+r.URL.RequestURI(), http.StatusTemporaryRedirect)
			}))
			t.Cleanup(origin.Close)

			log := &credOrderLog{}
			p := fam.build(origin.URL, &recordingCredential{typ: fam.credType, log: log})
			waiter := newHeldWaiter(log)
			holdRateLimit(t, p, waiter)
			waiter.release <- struct{}{}
			waiter.release <- struct{}{}

			require.NoError(t, predictHi(t, p))
			hdr := <-got
			assert.Empty(t, hdr.Get("Authorization"), "events %s", log)
			assert.Empty(t, hdr.Get("X-Amz-Date"), "events %s", log)
			assert.Equal(t, 1, log.count("apply "), "only the request to the origin is authorized; events %s", log)
		})
	}
}

// With no rate limit nothing changes: the credential is applied once, while
// the request is built, and no limiter transport is installed.
func TestCredentialFamilies_NoRateLimitAppliesAtBuildTime(t *testing.T) {
	for name, fam := range credentialFamilies() {
		t.Run(name, func(t *testing.T) {
			sent := make(chan string, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sent <- r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(fam.body))
			}))
			t.Cleanup(srv.Close)

			log := &credOrderLog{}
			p := fam.build(srv.URL, &recordingCredential{typ: fam.credType, log: log})
			_, limited := p.GetHTTPClient().Transport.(*base.RateLimitTransport)
			require.False(t, limited)

			require.NoError(t, predictHi(t, p))
			assert.Equal(t, fam.credType+"-1", <-sent)
			assert.Equal(t, "[apply "+fam.credType+"-1]", log.String())
		})
	}
}

// namedCredential is a recordingCredential that names its headers, as every
// credential in runtime/credentials does.
type namedCredential struct{ *recordingCredential }

func (namedCredential) HeaderNames() []string { return []string{"Authorization", "X-Amz-Date"} }

// The custom-header collision check still refuses a header the credential
// sets, without applying the credential at build time. A credential that names
// its headers is refused while the request is built; one that cannot is
// refused after it is applied, before anything is sent.
func TestCredentialFamilies_CustomHeaderCollisionUnderRateLimit(t *testing.T) {
	for name, fam := range credentialFamilies() {
		for _, named := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s named=%v", name, named), func(t *testing.T) {
				var hits atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					hits.Add(1)
					_, _ = w.Write([]byte(fam.body))
				}))
				t.Cleanup(srv.Close)

				log := &credOrderLog{}
				rec := &recordingCredential{typ: fam.credType, log: log}
				var cred providers.Credential = rec
				if named {
					cred = namedCredential{rec}
				}
				p := fam.build(srv.URL, cred)
				p.SetRateLimit(1000, 10)
				p.(interface{ SetCustomHeaders(map[string]string) }).SetCustomHeaders(
					map[string]string{"authorization": "mine"})

				err := predictHi(t, p)
				require.Error(t, err)
				assert.Contains(t, err.Error(), `custom header "authorization" collides`)
				assert.Zero(t, hits.Load(), "a colliding request is never sent")
				if named {
					assert.Zero(t, log.count("apply "), "named headers are checked without applying the credential")
				}
			})
		}
	}
}

// MakeRawRequest checks custom headers against the credential's header names
// too, before the retry loop, and authorizes each attempt with no limit set.
func TestMakeRawRequest_CredentialHeadersCollideAndApply(t *testing.T) {
	sent := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent <- r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	log := &credOrderLog{}
	bp := providers.NewBaseProvider("p", false, &http.Client{})
	bp.SetCredential(namedCredential{&recordingCredential{typ: "gcp", log: log}})
	_, err := bp.MakeRawRequest(t.Context(), srv.URL, []byte(`{}`), nil, "Test")
	require.NoError(t, err)
	assert.Equal(t, "gcp-1", <-sent)

	bp.SetCustomHeaders(map[string]string{"Authorization": "mine"})
	_, err = bp.MakeRawRequest(t.Context(), srv.URL, []byte(`{}`), nil, "Test")
	require.ErrorContains(t, err, `custom header "Authorization" collides`)
	assert.Equal(t, 1, log.count("apply "), "the colliding request is not authorized")
}

// staticKeyProviders are the providers that authenticate with a fixed key.
// They go through the same BaseProvider credential as the cloud platforms.
func staticKeyProviders(t *testing.T) map[string]struct {
	header, value string
	build         func(baseURL string) limitedProvider
} {
	t.Helper()
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-key")
	t.Setenv("OPENAI_API_KEY", "openai-key")
	return map[string]struct {
		header, value string
		build         func(baseURL string) limitedProvider
	}{
		"claude direct env key": {"X-Api-Key", "anthropic-key", func(u string) limitedProvider {
			return claude.NewProvider("c", "claude-test", u, providers.ProviderDefaults{MaxTokens: 10}, false)
		}},
		"claude direct api_key credential": {"X-Api-Key", "cred-key", func(u string) limitedProvider {
			return claude.NewProviderWithCredential("c", "claude-test", u, providers.ProviderDefaults{MaxTokens: 10},
				false, credentials.NewAPIKeyCredential("cred-key",
					credentials.WithHeaderName("X-API-Key"), credentials.WithPrefix("")), "", nil)
		}},
		"openai direct env key": {"Authorization", "Bearer openai-key", func(u string) limitedProvider {
			return openai.NewProvider("o", "gpt-test", u, providers.ProviderDefaults{MaxTokens: 10}, false)
		}},
		"gemini ai studio": {"X-Goog-Api-Key", "gemini-key", func(u string) limitedProvider {
			return gemini.NewProviderWithCredential("g", "gemini-test", u, providers.ProviderDefaults{MaxTokens: 10},
				false, credentials.NewAPIKeyCredential("gemini-key"), "", nil)
		}},
		"vllm": {"Authorization", "Bearer vllm-key", func(u string) limitedProvider {
			return vllm.NewProvider("v", "m", u, providers.ProviderDefaults{MaxTokens: 10}, false,
				map[string]any{"api_key": "vllm-key"})
		}},
		"imagen": {"X-Goog-Api-Key", "imagen-key", func(u string) limitedProvider {
			return imagen.NewProvider(imagen.Config{ID: "i", BaseURL: u, ApiKey: "imagen-key"})
		}},
	}
}

// Under a rate limit a static key follows the same rule as a cloud
// credential: it is on the request to the origin and on a redirect back to the
// origin, and never on a redirect to another host, including the key headers
// http.Client would copy there itself.
func TestStaticKeyProviders_FollowTheCredentialRule(t *testing.T) {
	for name, tc := range staticKeyProviders(t) {
		t.Run(name, func(t *testing.T) {
			targetURL, got := crossHostTarget(t, `{}`)
			var mu sync.Mutex
			var hops []string
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				hops = append(hops, r.Header.Get(tc.header))
				n := len(hops)
				mu.Unlock()
				if n == 1 {
					http.Redirect(w, r, r.URL.RequestURI(), http.StatusTemporaryRedirect)
					return
				}
				http.Redirect(w, r, targetURL+r.URL.RequestURI(), http.StatusTemporaryRedirect)
			}))
			t.Cleanup(origin.Close)

			p := tc.build(origin.URL)
			p.SetRateLimit(1000, 10)
			_ = predictHi(t, p) // the target's reply need not parse

			mu.Lock()
			assert.Equal(t, []string{tc.value, tc.value}, hops, "the origin and the same-host hop carry the key")
			mu.Unlock()
			assert.Empty(t, (<-got).Get(tc.header), "the cross-host hop does not")
		})
	}
}

// The real SigV4 signer, under a rate limit and through a same-host redirect:
// each hop is signed once, after its wait, over the body it actually carries.
func TestBedrockSigV4_SignsEachHopOverItsBody(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDEXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	t.Setenv("AWS_SESSION_TOKEN", "session")
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/none")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/none")
	cred, err := credentials.NewAWSCredential(t.Context(), "us-east-1")
	require.NoError(t, err)

	type hop struct{ auth, date, token string }
	var mu sync.Mutex
	var hops []hop
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, readErr := io.ReadAll(r.Body)
		assert.NoError(t, readErr)
		sum := sha256.Sum256(body)
		assert.NotEmpty(t, body, "the hop carries the request body")
		assert.Equal(t, hex.EncodeToString(sum[:]), r.Header.Get("X-Amz-Content-Sha256"),
			"the signature covers the body this hop sent")
		mu.Lock()
		hops = append(hops, hop{r.Header.Get("Authorization"), r.Header.Get("X-Amz-Date"),
			r.Header.Get("X-Amz-Security-Token")})
		first := len(hops) == 1
		mu.Unlock()
		if first {
			http.Redirect(w, r, r.URL.RequestURI(), http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	t.Cleanup(srv.Close)

	p := claude.NewProviderWithCredential("c", "anthropic.claude-test", srv.URL,
		providers.ProviderDefaults{MaxTokens: 10}, false, cred, "bedrock",
		&providers.PlatformConfig{Region: "us-east-1"})
	p.SetRateLimit(1000, 10)
	require.NoError(t, predictHi(t, p))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, hops, 2)
	for i, h := range hops {
		assert.True(t, strings.HasPrefix(h.auth, "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/"), "hop %d: %q", i+1, h.auth)
		assert.NotEmpty(t, h.date, "hop %d", i+1)
		assert.Equal(t, "session", h.token, "hop %d", i+1)
	}
}

// A provider built on NewBaseProviderWithCredential authenticates with that
// credential without registering it again.
func TestNewBaseProviderWithCredential_StoresTheCredential(t *testing.T) {
	log := &credOrderLog{}
	cred := &recordingCredential{typ: "gcp", log: log}
	bp, _ := providers.NewBaseProviderWithCredential("p", false, 0, cred)
	assert.Same(t, cred, bp.Credential())

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com", http.NoBody)
	require.NoError(t, err)
	require.NoError(t, bp.AuthorizeRequest(t.Context(), req))
	assert.Equal(t, "gcp-1", req.Header.Get("Authorization"))
}

// failingCredential cannot be applied, like an expired AWS profile or a GCP
// token source that cannot refresh.
type failingCredential struct{ applies atomic.Int32 }

func (c *failingCredential) Apply(context.Context, *http.Request) error {
	c.applies.Add(1)
	return errors.New("token refresh failed")
}
func (*failingCredential) Type() string { return "gcp" }

// Under a rate limit the credential is applied inside the transport, so its
// failure comes back from http.Client.Do as a *url.Error, which is a
// net.Error. It must still be a hard failure: not retried, and not transient.
func TestCredentialFailureUnderRateLimit_IsNotRetriedOrTransient(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { hits.Add(1) }))
	t.Cleanup(srv.Close)

	t.Run("MakeRawRequest", func(t *testing.T) {
		cred := &failingCredential{}
		bp := providers.NewBaseProvider("p", false, &http.Client{})
		bp.SetRetryPolicy(pipeline.RetryPolicy{MaxRetries: 3, Backoff: "fixed", InitialDelayMs: 1})
		bp.SetCredential(cred)
		bp.SetRateLimit(1000, 10)
		_, err := bp.MakeRawRequest(t.Context(), srv.URL, []byte(`{}`), nil, "Test")
		require.ErrorIs(t, err, base.ErrRequestAuthorization)
		assert.Equal(t, int32(1), cred.applies.Load(), "a credential failure is not retried")
	})
	for name, fam := range credentialFamilies() {
		t.Run(name, func(t *testing.T) {
			cred := &failingCredential{}
			p := fam.build(srv.URL, cred)
			p.SetRateLimit(1000, 10)
			err := predictHi(t, p)
			require.ErrorIs(t, err, base.ErrRequestAuthorization)
			assert.False(t, providers.IsTransient(err), "a credential failure is not an infra blip: %v", err)
		})
	}
	assert.Zero(t, hits.Load(), "nothing is sent without its credential")
}

// A WithMedia view shares its provider's clients. Whether it applies the
// credential while building follows those clients, not the limit the view was
// copied with, so removing the limit later never sends a request without its
// credential.
func TestCredentialDeferral_FollowsTheSharedClients(t *testing.T) {
	sent := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent <- r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	log := &credOrderLog{}
	bp := providers.NewBaseProvider("p", false, &http.Client{})
	bp.SetCredential(&recordingCredential{typ: "gcp", log: log})
	bp.SetRateLimit(1000, 10)
	view := bp.WithMedia(providers.MediaSettings{})
	bp.SetRateLimit(0, 0)

	_, err := view.MakeRawRequest(t.Context(), srv.URL, []byte(`{}`), nil, "Test")
	require.NoError(t, err)
	assert.Equal(t, "gcp-1", <-sent)
	assert.Equal(t, 1, log.count("apply "))
}
