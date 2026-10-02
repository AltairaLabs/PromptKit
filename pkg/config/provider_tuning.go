package config

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// ApplyProviderTuning copies a provider's request-tuning settings from its
// config onto spec: request_timeout, stream_idle_timeout, stream_retry (with
// its budget), stream_max_concurrent, http_transport and
// defaults.prompt_caching. It is the single conversion used by every loader
// that turns a config.Provider into a providers.ProviderSpec, so a field is
// honored the same way wherever the config is loaded.
//
// Malformed duration strings are logged and treated as unset, so the
// provider falls back to its built-in default for that field rather than
// failing to load.
func ApplyProviderTuning(spec *providers.ProviderSpec, p *Provider) {
	if spec == nil || p == nil {
		return
	}
	spec.RequestTimeout = parseProviderDuration(p.ID, "request_timeout", p.RequestTimeout)
	spec.StreamIdleTimeout = parseProviderDuration(p.ID, "stream_idle_timeout", p.StreamIdleTimeout)
	spec.StreamRetry = streamRetryPolicy(p.ID, p.StreamRetry)
	spec.StreamRetryBudget = streamRetryBudget(p.StreamRetry)
	spec.StreamMaxConcurrent = p.StreamMaxConcurrent
	spec.HTTPTransport = httpTransportOptions(p.ID, p.HTTPTransport)
	spec.Defaults.DisablePromptCaching = p.Defaults.PromptCaching != nil && !*p.Defaults.PromptCaching
}

// streamRetryPolicy translates stream_retry into a providers policy. A nil
// or disabled config yields the zero policy (retry off).
func streamRetryPolicy(providerID string, cfg *StreamRetryConfig) providers.StreamRetryPolicy {
	if cfg == nil || !cfg.Enabled {
		return providers.StreamRetryPolicy{}
	}
	policy := providers.StreamRetryPolicy{
		Enabled:      true,
		MaxAttempts:  cfg.MaxAttempts,
		InitialDelay: parseProviderDuration(providerID, "stream_retry.initial_delay", cfg.InitialDelay),
		MaxDelay:     parseProviderDuration(providerID, "stream_retry.max_delay", cfg.MaxDelay),
		Window:       providers.StreamRetryWindowPreFirstChunk,
	}
	switch cfg.RetryWindow {
	case "", string(providers.StreamRetryWindowPreFirstChunk):
	case string(providers.StreamRetryWindowAlways):
		policy.Window = providers.StreamRetryWindowAlways
	default:
		logger.Warn("config: unknown stream_retry.retry_window, using pre_first_chunk",
			"provider", providerID, "value", cfg.RetryWindow)
	}
	return policy
}

// streamRetryBudget builds the per-provider retry token bucket. Nil means
// unbounded retries (only max_attempts caps them).
func streamRetryBudget(cfg *StreamRetryConfig) *providers.RetryBudget {
	if cfg == nil || !cfg.Enabled || cfg.Budget == nil {
		return nil
	}
	return providers.NewRetryBudget(cfg.Budget.RatePerSec, cfg.Budget.Burst)
}

// httpTransportOptions translates http_transport. A nil config yields the
// zero value, which the runtime treats as package defaults.
func httpTransportOptions(providerID string, cfg *HTTPTransportConfig) providers.HTTPTransportOptions {
	if cfg == nil {
		return providers.HTTPTransportOptions{}
	}
	return providers.HTTPTransportOptions{
		MaxConnsPerHost:     cfg.MaxConnsPerHost,
		MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:     parseProviderDuration(providerID, "http_transport.idle_conn_timeout", cfg.IdleConnTimeout),
	}
}

// parseProviderDuration parses a Go duration string from a provider field.
// Empty yields zero (the caller's default applies). Malformed or
// non-positive values are logged and yield zero.
func parseProviderDuration(providerID, field, value string) time.Duration {
	if value == "" {
		return 0
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		logger.Warn("config: ignoring invalid provider duration, using default",
			"provider", providerID, "field", field, "value", value)
		return 0
	}
	return d
}
