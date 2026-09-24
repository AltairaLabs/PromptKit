package huggingface

import (
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// providerType is the `type` string a host writes in its *.provider.yaml
// alongside `role: inference`. Matches Arena's existing `role: inference`
// entries for the HF classify backend this package replaces.
const providerType = "huggingface"

//nolint:gochecknoinits // Factory registration requires init.
func init() {
	inference.RegisterFactory(providerType, func(spec inference.ProviderSpec) (inference.Provider, error) {
		p, err := New(Config{
			APIKey:    base.APIKeyFromCredential(spec.Credential),
			BaseURL:   spec.BaseURL,
			Dedicated: boolFromConfig(spec.AdditionalConfig, "dedicated"),
		})
		if err != nil {
			return nil, err
		}
		// spec.Model becomes the provider's default model, used when a
		// Request doesn't carry its own Model. The old classify factory
		// ignored spec.Model entirely; Infer's Request.Model-wins-over-
		// configured-model contract needs it stored somewhere, and
		// Config deliberately has no Model field (HF's classify backends
		// always took the model per call, never as a provider default).
		p.model = strings.TrimSpace(spec.Model)
		return p, nil
	})
}

// boolFromConfig reads a boolean flag from a provider's additional_config.
// Returns false when the key is absent or not a bool.
func boolFromConfig(m map[string]any, key string) bool {
	v, ok := m[key].(bool)
	return ok && v
}
