package topiccontrol

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// providerType is the provider `type` string a host writes in its
// *.provider.yaml alongside `role: inference`.
const providerType = "nvidia-topic-control"

// timeoutConfigKey is the additional_config key that raises the per-call HTTP
// timeout, in seconds. Named here so the producer and the doc page cannot drift
// apart on a typo.
const timeoutConfigKey = "timeout_seconds"

//nolint:gochecknoinits // Factory registration requires init.
func init() {
	classify.RegisterFactory(providerType, func(spec classify.ProviderSpec) (classify.Backend, error) {
		return New(Config{
			APIKey:  base.APIKeyFromCredential(spec.Credential),
			BaseURL: spec.BaseURL,
			Model:   spec.Model,
			Timeout: timeoutFromConfig(spec.AdditionalConfig, timeoutConfigKey),
		})
	})
}

// timeoutFromConfig reads a per-call timeout in seconds from a provider's
// additional_config. Absent, unparseable or non-positive values return zero,
// which New resolves to defaultHTTPTimeout — a malformed entry falls back to a
// working default rather than disabling the timeout entirely.
//
// YAML and JSON hand numbers over as different Go types depending on the
// decoder, so all three plausible shapes are accepted.
func timeoutFromConfig(m map[string]any, key string) time.Duration {
	raw, ok := m[key]
	if !ok {
		return 0
	}
	var seconds float64
	switch v := raw.(type) {
	case int:
		seconds = float64(v)
	case int64:
		seconds = float64(v)
	case float64:
		seconds = v
	default:
		return 0
	}
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}
