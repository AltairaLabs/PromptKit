package hf

import (
	"github.com/AltairaLabs/PromptKit/runtime/classify"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// huggingFaceType is the provider `type` string for HF Inference API
// classify backends. Matches Arena's existing `role: inference` entries.
const huggingFaceType = "huggingface"

//nolint:gochecknoinits // Factory registration requires init.
func init() {
	classify.RegisterFactory(huggingFaceType, func(spec classify.ProviderSpec) (classify.Backend, error) {
		return NewClient(Config{
			APIKey:    base.APIKeyFromCredential(spec.Credential),
			BaseURL:   spec.BaseURL,
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
