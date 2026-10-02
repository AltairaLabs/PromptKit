package config

import (
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

func TestApplyProviderTuning_CopiesEveryTuningField(t *testing.T) {
	caching := false
	p := &Provider{
		ID:                  "p",
		RequestTimeout:      "3m",
		StreamIdleTimeout:   "45s",
		StreamMaxConcurrent: 7,
		StreamRetry: &StreamRetryConfig{
			Enabled:      true,
			MaxAttempts:  3,
			InitialDelay: "100ms",
			MaxDelay:     "1s",
			RetryWindow:  "always",
			Budget:       &StreamRetryBudgetConfig{RatePerSec: 2, Burst: 4},
		},
		HTTPTransport: &HTTPTransportConfig{MaxConnsPerHost: 50, MaxIdleConnsPerHost: 25, IdleConnTimeout: "30s"},
		RateLimit:     RateLimit{RPS: 10, Burst: 20},
		Defaults:      ProviderDefaults{PromptCaching: &caching},
	}
	var spec providers.ProviderSpec

	ApplyProviderTuning(&spec, p)

	if spec.RequestTimeout != 3*time.Minute || spec.StreamIdleTimeout != 45*time.Second {
		t.Errorf("timeouts = (%v, %v), want (3m, 45s)", spec.RequestTimeout, spec.StreamIdleTimeout)
	}
	if spec.StreamMaxConcurrent != 7 {
		t.Errorf("StreamMaxConcurrent = %d, want 7", spec.StreamMaxConcurrent)
	}
	want := providers.StreamRetryPolicy{
		Enabled: true, MaxAttempts: 3, InitialDelay: 100 * time.Millisecond, MaxDelay: time.Second,
		Window: providers.StreamRetryWindowAlways,
	}
	if spec.StreamRetry != want {
		t.Errorf("StreamRetry = %+v, want %+v", spec.StreamRetry, want)
	}
	if spec.StreamRetryBudget == nil {
		t.Error("StreamRetryBudget = nil, want a budget")
	}
	wantTransport := providers.HTTPTransportOptions{
		MaxConnsPerHost: 50, MaxIdleConnsPerHost: 25, IdleConnTimeout: 30 * time.Second,
	}
	if spec.HTTPTransport != wantTransport {
		t.Errorf("HTTPTransport = %+v, want %+v", spec.HTTPTransport, wantTransport)
	}
	if spec.RateLimit != (providers.RateLimitOptions{RequestsPerSecond: 10, Burst: 20}) {
		t.Errorf("RateLimit = %+v, want {10 20}", spec.RateLimit)
	}
	if !spec.Defaults.DisablePromptCaching {
		t.Error("DisablePromptCaching = false, want true for prompt_caching: false")
	}
}

func TestApplyProviderTuning_UnsetAndInvalidFallBackToDefaults(t *testing.T) {
	p := &Provider{
		ID:             "p",
		RequestTimeout: "not-a-duration",
		StreamRetry:    &StreamRetryConfig{Enabled: false, MaxAttempts: 5, Budget: &StreamRetryBudgetConfig{RatePerSec: 1}},
	}
	spec := providers.ProviderSpec{RequestTimeout: time.Hour}

	ApplyProviderTuning(&spec, p)

	if spec.RequestTimeout != 0 {
		t.Errorf("RequestTimeout = %v, want 0 for a malformed duration", spec.RequestTimeout)
	}
	if spec.StreamRetry.Enabled || spec.StreamRetryBudget != nil {
		t.Errorf("disabled stream_retry produced policy %+v, budget %v", spec.StreamRetry, spec.StreamRetryBudget)
	}
	if spec.RateLimit.RequestsPerSecond != 0 || spec.Defaults.DisablePromptCaching {
		t.Errorf("unset fields not zero: rate %+v, disable caching %v", spec.RateLimit, spec.Defaults.DisablePromptCaching)
	}
}

func TestApplyProviderTuning_UnknownRetryWindowUsesPreFirstChunk(t *testing.T) {
	p := &Provider{StreamRetry: &StreamRetryConfig{Enabled: true, RetryWindow: "sometimes"}}
	var spec providers.ProviderSpec

	ApplyProviderTuning(&spec, p)

	if spec.StreamRetry.Window != providers.StreamRetryWindowPreFirstChunk {
		t.Errorf("Window = %q, want pre_first_chunk", spec.StreamRetry.Window)
	}
}

func TestApplyProviderTuning_NilIsNoOp(t *testing.T) {
	spec := providers.ProviderSpec{RequestTimeout: time.Second}
	ApplyProviderTuning(&spec, nil)
	ApplyProviderTuning(nil, &Provider{})
	if spec.RequestTimeout != time.Second {
		t.Errorf("nil provider changed the spec: %v", spec.RequestTimeout)
	}
}
