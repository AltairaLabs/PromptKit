package openai

import (
	"fmt"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// providerType and nvidiaProviderType are the `type` strings a host writes
// in its *.provider.yaml alongside `role: inference`. Both share the same
// Provider — NVIDIA's NemoGuard topic-control model is served through an
// OpenAI-compatible chat-completions endpoint — so nvidia-topic-control is
// only a defaulting alias over the openai factory, not a second vendor API.
const (
	providerType       = "openai"
	nvidiaProviderType = "nvidia-topic-control"

	// timeoutConfigKey is the additional_config key that raises the
	// per-call HTTP timeout, in seconds. Named here so the producer and
	// the doc page cannot drift apart on a typo.
	timeoutConfigKey = "timeout_seconds"

	// nemoGuardHTTPTimeout is topic control's per-call default. It stays
	// under the guardrail's 30s bound so a hung attempt leaves room to retry.
	nemoGuardHTTPTimeout = 20 * time.Second
)

//nolint:gochecknoinits // Factory registration requires init.
func init() {
	inference.RegisterFactory(providerType, func(spec inference.ProviderSpec) (inference.Provider, error) {
		if strings.TrimSpace(spec.Model) == "" {
			return nil, fmt.Errorf("inference: %s: model is required", providerType)
		}
		return New(Config{
			APIKey:  base.APIKeyFromCredential(spec.Credential),
			BaseURL: spec.BaseURL,
			Model:   spec.Model,
			Timeout: timeoutFromConfig(spec.AdditionalConfig, timeoutConfigKey),
		})
	})

	inference.RegisterFactory(nvidiaProviderType, func(spec inference.ProviderSpec) (inference.Provider, error) {
		if strings.TrimSpace(spec.BaseURL) == "" {
			return nil, fmt.Errorf("inference: %s: base_url is required", nvidiaProviderType)
		}
		model := spec.Model
		if strings.TrimSpace(model) == "" {
			model = NemoGuardModel
		}
		timeout := timeoutFromConfig(spec.AdditionalConfig, timeoutConfigKey)
		if timeout <= 0 {
			timeout = nemoGuardHTTPTimeout
		}
		return New(Config{
			APIKey:  base.APIKeyFromCredential(spec.Credential),
			BaseURL: spec.BaseURL,
			Model:   model,
			Timeout: timeout,
		})
	})
}

// timeoutFromConfig reads a per-call timeout in seconds from a provider's
// additional_config. Absent, unparseable or non-positive values return
// zero, which New resolves to defaultHTTPTimeout — a malformed entry falls
// back to a working default rather than disabling the timeout entirely.
//
// YAML and JSON hand numbers over as different Go types depending on the
// decoder, so all three plausible shapes are accepted. Mirrors
// topiccontrol/register.go's timeoutFromConfig.
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
