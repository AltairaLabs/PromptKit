package providers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/httputil"
	"golang.org/x/time/rate"
)

// TestCreateProviderFromSpec_AppliesRateLimit verifies that spec.RateLimit
// reaches any provider embedding BaseProvider, and that a missing burst
// defaults to the per-second rate (at least 1) rather than blocking.
func TestCreateProviderFromSpec_AppliesRateLimit(t *testing.T) {
	const typeName = "test-ratelimit-provider"
	originalFactory := providerFactories[typeName]
	t.Cleanup(func() {
		if originalFactory != nil {
			providerFactories[typeName] = originalFactory
		} else {
			delete(providerFactories, typeName)
		}
	})
	RegisterProviderFactory(typeName, func(spec ProviderSpec) (Provider, error) {
		b := NewBaseProvider(spec.ID, false, &http.Client{Timeout: httputil.DefaultProviderTimeout})
		return &timeoutAwareProvider{BaseProvider: &b}, nil
	})

	tests := []struct {
		name      string
		opts      RateLimitOptions
		wantNil   bool
		wantLimit rate.Limit
		wantBurst int
	}{
		{name: "rate and burst", opts: RateLimitOptions{RequestsPerSecond: 5, Burst: 2}, wantLimit: 5, wantBurst: 2},
		{name: "burst defaults to rate", opts: RateLimitOptions{RequestsPerSecond: 4}, wantLimit: 4, wantBurst: 4},
		{name: "sub-1 rate gets burst 1", opts: RateLimitOptions{RequestsPerSecond: 0.5}, wantLimit: 0.5, wantBurst: 1},
		{name: "zero rate disables", opts: RateLimitOptions{}, wantNil: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov, err := CreateProviderFromSpec(ProviderSpec{ID: "r", Type: typeName, RateLimit: tt.opts})
			if err != nil {
				t.Fatalf("CreateProviderFromSpec: %v", err)
			}
			lim := prov.(*timeoutAwareProvider).RateLimiter()
			if tt.wantNil {
				if lim != nil {
					t.Fatalf("RateLimiter() = %v, want nil", lim)
				}
				return
			}
			if lim == nil {
				t.Fatal("RateLimiter() = nil, want a limiter")
			}
			if lim.Limit() != tt.wantLimit || lim.Burst() != tt.wantBurst {
				t.Errorf("limiter = (%v, %d), want (%v, %d)", lim.Limit(), lim.Burst(), tt.wantLimit, tt.wantBurst)
			}
		})
	}
}

// TestRunStreamingRequest_WaitsForRateLimit verifies that streaming calls
// are throttled like request/response calls: with the only token spent, a
// stream whose deadline expires before the next token never sends.
func TestRunStreamingRequest_WaitsForRateLimit(t *testing.T) {
	b := NewBaseProvider("r", false, &http.Client{})
	b.SetRateLimit(0.001, 1) // one token, then one every ~17 minutes
	if err := b.WaitForRateLimit(context.Background()); err != nil {
		t.Fatalf("spending the only token: %v", err)
	}

	sent := false
	req := &StreamRetryRequest{
		ProviderName: "r",
		Client:       &http.Client{},
		RequestFn: func(ctx context.Context) (*http.Request, error) {
			sent = true
			return nil, errors.New("must not be called")
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := b.RunStreamingRequest(ctx, req, nil)
	if err == nil || !strings.Contains(err.Error(), "rate limit wait") {
		t.Fatalf("RunStreamingRequest error = %v, want a rate limit wait error", err)
	}
	if sent {
		t.Error("request was sent despite the exhausted rate limiter")
	}
}
