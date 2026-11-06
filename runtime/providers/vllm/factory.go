package vllm

import (
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

//nolint:gochecknoinits // init required for automatic provider registration
func init() {
	// Key precedence: a resolved credential (spec.Credential, from the
	// provider's credential block) wins over additional_config.api_key, which
	// is the fallback. With neither, vLLM is called without a credential.
	providers.RegisterProviderFactory("vllm", func(spec providers.ProviderSpec) (providers.Provider, error) {
		p := NewProvider(
			spec.ID,
			spec.Model,
			spec.BaseURL,
			spec.Defaults,
			spec.IncludeRawOutput,
			spec.AdditionalConfig,
		)
		if spec.HasCredential() {
			p.SetCredential(spec.Credential)
		}
		return p, nil
	})
}
