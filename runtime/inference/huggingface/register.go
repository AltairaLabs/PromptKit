package huggingface

import (
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
		return New(Config{
			APIKey:    base.APIKeyFromCredential(spec.Credential),
			BaseURL:   spec.BaseURL,
			Model:     spec.Model,
			Dedicated: boolFromConfig(spec.AdditionalConfig, "dedicated"),
		})
	})
}

// boolFromConfig reads a boolean flag from a provider's additional_config.
// Returns false when the key is absent or not a bool.
func boolFromConfig(m map[string]any, key string) bool {
	v, ok := m[key].(bool)
	return ok && v
}
