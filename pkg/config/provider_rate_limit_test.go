package config

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// rate_limit in a provider file reaches the runtime spec through the loader
// and ApplyProviderTuning, the path the SDK and Arena both build providers by.
func TestApplyProviderTuning_CarriesRateLimitFromProviderFile(t *testing.T) {
	p, err := LoadProvider(writeManifest(t, providerHeader+
		"spec:\n  type: openai\n  rate_limit:\n    rps: 4\n    burst: 8\n"))
	if err != nil {
		t.Fatalf("LoadProvider: %v", err)
	}
	var spec providers.ProviderSpec
	ApplyProviderTuning(&spec, p)

	want := providers.RateLimitOptions{RequestsPerSecond: 4, Burst: 8}
	if spec.RateLimit != want {
		t.Errorf("spec.RateLimit = %+v, want %+v", spec.RateLimit, want)
	}
}

func TestApplyProviderTuning_NoRateLimitIsUnlimited(t *testing.T) {
	var spec providers.ProviderSpec
	ApplyProviderTuning(&spec, &Provider{ID: "p"})
	if spec.RateLimit != (providers.RateLimitOptions{}) {
		t.Errorf("spec.RateLimit = %+v, want zero (unlimited)", spec.RateLimit)
	}
}

func TestCapabilityHTTPTuning_RateLimitGivesEachProviderItsOwnLimiter(t *testing.T) {
	p := &Provider{ID: "e", Role: RoleEmbedding, RateLimit: RateLimit{RPS: 3, Burst: 2}}

	first, err := CapabilityHTTPTuning(p)
	if err != nil {
		t.Fatalf("CapabilityHTTPTuning: %v", err)
	}
	if first.RateLimiter == nil {
		t.Fatal("rate_limit was dropped for a capability provider")
	}
	if first.RateLimiter.Limit() != 3 || first.RateLimiter.Burst() != 2 {
		t.Errorf("limiter = (%v, %d), want (3, 2)", first.RateLimiter.Limit(), first.RateLimiter.Burst())
	}
	if first.IsZero() {
		t.Error("tuning with a rate limit must not be zero, or the factory skips applying it")
	}

	second, err := CapabilityHTTPTuning(p)
	if err != nil {
		t.Fatalf("CapabilityHTTPTuning: %v", err)
	}
	if second.RateLimiter == first.RateLimiter {
		t.Error("two providers built from one config must not share a bucket")
	}

	none, err := CapabilityHTTPTuning(&Provider{ID: "e", Role: RoleEmbedding})
	if err != nil {
		t.Fatalf("CapabilityHTTPTuning: %v", err)
	}
	if none.RateLimiter != nil {
		t.Error("no rate_limit must mean no limiter")
	}
}
