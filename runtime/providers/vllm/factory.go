package vllm

import (
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

//nolint:gochecknoinits // init required for automatic provider registration
func init() {
	providers.RegisterProviderFactory("vllm", func(spec providers.ProviderSpec) (providers.Provider, error) {
		return NewProvider(
			spec.ID,
			spec.Model,
			spec.BaseURL,
			spec.Defaults,
			spec.IncludeRawOutput,
			spec.AdditionalConfig,
		), nil
	})
}
