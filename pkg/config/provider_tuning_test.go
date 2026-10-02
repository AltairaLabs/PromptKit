package config

import (
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
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
	if spec.Defaults.DisablePromptCaching {
		t.Error("DisablePromptCaching = true for an unset prompt_caching, want false")
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

func TestCapabilityHTTPTuning(t *testing.T) {
	caching := true
	cases := []struct {
		name    string
		p       *Provider
		wantErr string
		check   func(t *testing.T, got base.HTTPTuning)
	}{
		{
			name: "nil provider is zero tuning",
			p:    nil,
			check: func(t *testing.T, got base.HTTPTuning) {
				if !got.IsZero() {
					t.Errorf("got %+v, want zero", got)
				}
			},
		},
		{
			name: "empty provider is zero tuning",
			p:    &Provider{ID: "e", Role: RoleTTS},
			check: func(t *testing.T, got base.HTTPTuning) {
				if !got.IsZero() {
					t.Errorf("got %+v, want zero", got)
				}
			},
		},
		{
			name: "headers and request_timeout map",
			p: &Provider{ID: "h", Role: RoleEmbedding, Headers: map[string]string{"X-Gateway": "v"},
				RequestTimeout: "1500ms"},
			check: func(t *testing.T, got base.HTTPTuning) {
				if got.Headers["X-Gateway"] != "v" {
					t.Errorf("headers = %v", got.Headers)
				}
				if got.RequestTimeout != 1500*time.Millisecond {
					t.Errorf("request timeout = %v", got.RequestTimeout)
				}
				if got.Transport != nil {
					t.Error("no transport unless http_transport is set")
				}
			},
		},
		{
			name: "http_transport builds a transport",
			p:    &Provider{ID: "x", Role: RoleRerank, HTTPTransport: &HTTPTransportConfig{MaxConnsPerHost: 4}},
			check: func(t *testing.T, got base.HTTPTuning) {
				if got.Transport == nil {
					t.Error("want a transport")
				}
			},
		},
		{
			name:    "stream_retry rejected",
			p:       &Provider{ID: "s", Role: RoleSTT, StreamRetry: &StreamRetryConfig{Enabled: true}},
			wantErr: `provider "s": stream_retry is not supported for role "stt"`,
		},
		{
			name:    "stream_max_concurrent rejected",
			p:       &Provider{ID: "s", Role: RoleTTS, StreamMaxConcurrent: 3},
			wantErr: `stream_max_concurrent is not supported for role "tts"`,
		},
		{
			name:    "stream_idle_timeout rejected",
			p:       &Provider{ID: "s", Role: RoleInference, StreamIdleTimeout: "30s"},
			wantErr: `stream_idle_timeout is not supported for role "inference"`,
		},
		{
			name:    "defaults.prompt_caching rejected",
			p:       &Provider{ID: "s", Role: RoleEmbedding, Defaults: ProviderDefaults{PromptCaching: &caching}},
			wantErr: `defaults.prompt_caching is not supported for role "embedding"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CapabilityHTTPTuning(tc.p)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tc.check(t, got)
		})
	}
}
