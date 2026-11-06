package imagen

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

// request_timeout replaces the BaseProvider client, so imagen must send
// through GetHTTPClient for the spec's rate limit (and transport) to apply.
func TestCreateProviderFromSpec_ImagenHonorsRateLimitWithRequestTimeout(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "k")
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "img", Type: "imagen", BaseURL: srv.URL,
		RequestTimeout: 5 * time.Second,
		RateLimit:      providers.RateLimitOptions{RequestsPerSecond: 0.001, Burst: 1},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	req := providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "a cat"}}}

	_, _ = p.Predict(t.Context(), req)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	_, _ = p.Predict(ctx, req)

	if got := hits.Load(); got != 1 {
		t.Fatalf("upstream saw %d requests, want 1: the second is past the burst", got)
	}
}
