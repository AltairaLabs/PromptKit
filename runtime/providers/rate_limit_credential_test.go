package providers_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/claude"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// credOrderLog records, in order, when a rate-limit wait ended and when the
// credential was applied.
type credOrderLog struct {
	mu     sync.Mutex
	events []string
}

func (l *credOrderLog) add(e string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
	return len(l.events) - 1
}

func (l *credOrderLog) index(e string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, got := range l.events {
		if got == e {
			return i
		}
	}
	return -1
}

func (l *credOrderLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return fmt.Sprint(l.events)
}

// heldWaiter holds every request in its rate-limit wait until the test
// releases it. No real time passes.
type heldWaiter struct {
	entered chan struct{}
	release chan struct{}
	log     *credOrderLog
}

func newHeldWaiter(log *credOrderLog) *heldWaiter {
	return &heldWaiter{entered: make(chan struct{}, 4), release: make(chan struct{}, 4), log: log}
}

func (w *heldWaiter) Wait(ctx context.Context) error {
	w.entered <- struct{}{}
	select {
	case <-w.release:
		w.log.add("wait-end")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// recordingCredential stands in for a SigV4 signer or a token source: each
// Apply mints a new value, so the upstream shows which application it got.
type recordingCredential struct {
	typ string
	log *credOrderLog
	mu  sync.Mutex
	n   int
}

func (c *recordingCredential) Apply(_ context.Context, req *http.Request) error {
	c.mu.Lock()
	c.n++
	value := fmt.Sprintf("%s-%d", c.typ, c.n)
	c.mu.Unlock()
	c.log.add("apply " + value)
	if c.typ == "aws" {
		req.Header.Set("X-Amz-Date", value)
	}
	req.Header.Set("Authorization", value)
	return nil
}

func (c *recordingCredential) Type() string { return c.typ }

// rateLimited is the part of BaseProvider these tests drive.
type rateLimited interface {
	SetRateLimit(requestsPerSecond float64, burst int)
	GetHTTPClient() *http.Client
	GetStreamingHTTPClient() *http.Client
}

// limitedProvider is a completion provider with a BaseProvider.
type limitedProvider interface {
	providers.Provider
	rateLimited
}

// holdRateLimit gives p a rate limit whose wait is held by w.
func holdRateLimit(t *testing.T, p rateLimited, w *heldWaiter) {
	t.Helper()
	p.SetRateLimit(1, 1)
	holdWait(t, p, w)
}

// holdWait makes p's installed rate limit wait on w.
func holdWait(t *testing.T, p rateLimited, w *heldWaiter) {
	t.Helper()
	for _, c := range []*http.Client{p.GetHTTPClient(), p.GetStreamingHTTPClient()} {
		rlt, ok := c.Transport.(*base.RateLimitTransport)
		require.True(t, ok, "transport is %T", c.Transport)
		rlt.Limiter = w
	}
}

// Under a rate limit, the credential a completion provider sends is the one
// applied after the request's wait ended, not the one applied while the
// request was built. Bedrock signs with SigV4; Vertex and Azure attach a
// token.
func TestCompletionProviders_ApplyCredentialAfterRateLimitWait(t *testing.T) {
	cases := map[string]struct {
		credType string
		body     string
		build    func(baseURL string, cred providers.Credential) limitedProvider
	}{
		"claude bedrock sigv4": {
			credType: "aws",
			body:     `{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`,
			build: func(baseURL string, cred providers.Credential) limitedProvider {
				return claude.NewProviderWithCredential("c", "anthropic.claude-test", baseURL,
					providers.ProviderDefaults{MaxTokens: 10}, false, cred, "bedrock",
					&providers.PlatformConfig{Region: "us-east-1"})
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
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			sent := make(chan http.Header, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sent <- r.Header.Clone()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)

			log := &credOrderLog{}
			p := tc.build(srv.URL, &recordingCredential{typ: tc.credType, log: log})
			waiter := newHeldWaiter(log)
			holdRateLimit(t, p, waiter)

			done := make(chan error, 1)
			go func() {
				_, err := p.Predict(t.Context(), providers.PredictionRequest{
					Messages: []types.Message{{Role: "user", Content: "hi"}},
				})
				done <- err
			}()
			<-waiter.entered
			waiter.release <- struct{}{}
			require.NoError(t, <-done)

			got := <-sent
			auth := got.Get("Authorization")
			sentAt, waitEnd := log.index("apply "+auth), log.index("wait-end")
			require.NotEqual(t, -1, sentAt, "sent credential %q never applied; events %s", auth, log)
			assert.Greater(t, sentAt, waitEnd, "sent credential %q applied before the wait ended; events %s", auth, log)
			if tc.credType == "aws" {
				assert.Equal(t, auth, got.Get("X-Amz-Date"), "signature date is from the post-wait signing")
			}
		})
	}
}

// Every attempt waits for its own token, so every attempt is re-authorized
// after its own wait: a retry never resends the first attempt's signature.
// The streaming client is authorized the same way, and a credential
// registered after the limit is set still applies.
func TestBaseProvider_AuthorizesEachAttemptAfterItsWait(t *testing.T) {
	var mu sync.Mutex
	var sent []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, r.Header.Get("Authorization"))
		if len(sent) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	log := &credOrderLog{}
	cred := &recordingCredential{typ: "aws", log: log}
	bp := providers.NewBaseProvider("p", false, &http.Client{})
	bp.SetRetryPolicy(pipeline.RetryPolicy{MaxRetries: 1, Backoff: "fixed", InitialDelayMs: 1})
	bp.SetRateLimit(1, 1)
	bp.SetCredential(cred)
	waiter := newHeldWaiter(log)
	holdWait(t, &bp, waiter)
	for range 3 {
		waiter.release <- struct{}{}
	}

	_, err := bp.MakeRawRequest(t.Context(), srv.URL, []byte(`{}`),
		providers.RequestHeaders{"Content-Type": "application/json"}, "Test")
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, http.NoBody)
	require.NoError(t, err)
	resp, err := bp.GetStreamingHTTPClient().Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	assert.Equal(t, []string{"aws-1", "aws-2", "aws-3"}, sent)
	assert.Equal(t,
		"[wait-end apply aws-1 wait-end apply aws-2 wait-end apply aws-3]", log.String())
}
