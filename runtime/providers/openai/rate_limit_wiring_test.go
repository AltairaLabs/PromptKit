package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A provider built from a spec with a rate limit holds both Predict and
// PredictStream to one bucket: the burst goes out, and the next call is not
// sent. The upstream answers 401 so no call is retried; only the request count
// matters here.
func TestCreateProviderFromSpec_RateLimitCoversPredictAndStream(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"no"}}`))
	}))
	defer srv.Close()

	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "o", Type: "openai", Model: "gpt-4o-mini", BaseURL: srv.URL,
		Credential: staticKey("k"),
		// One token per 1000s: the call past the burst is refused at once
		// against its one-minute deadline, never slept on.
		RateLimit: providers.RateLimitOptions{RequestsPerSecond: 0.001, Burst: 2},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer p.Close()

	req := providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}
	callStream := func(ctx context.Context) {
		ch, err := p.PredictStream(ctx, req)
		if err == nil {
			for range ch { //nolint:revive // drain
			}
		}
	}

	_, _ = p.Predict(t.Context(), req)
	callStream(t.Context())
	if got := hits.Load(); got != 2 {
		t.Fatalf("burst of 2: upstream saw %d requests, want 2", got)
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	_, _ = p.Predict(ctx, req)
	callStream(ctx)
	if got := hits.Load(); got != 2 {
		t.Fatalf("past the burst: upstream saw %d requests, want still 2", got)
	}
}

type staticKey string

func (k staticKey) Apply(_ context.Context, r *http.Request) error {
	r.Header.Set("Authorization", "Bearer "+string(k))
	return nil
}
func (staticKey) Type() string     { return "api_key" }
func (k staticKey) APIKey() string { return string(k) }
