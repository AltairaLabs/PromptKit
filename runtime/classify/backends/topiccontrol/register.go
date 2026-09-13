package topiccontrol

import (
	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// providerType is the provider `type` string a host writes in its
// *.provider.yaml alongside `role: inference`.
const providerType = "nvidia-topic-control"

//nolint:gochecknoinits // Factory registration requires init.
func init() {
	classify.RegisterFactory(providerType, func(spec classify.ProviderSpec) (classify.Backend, error) {
		return New(Config{
			APIKey:  base.APIKeyFromCredential(spec.Credential),
			BaseURL: spec.BaseURL,
			Model:   spec.Model,
		})
	})
}
