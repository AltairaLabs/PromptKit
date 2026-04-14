package openai

import "github.com/AltairaLabs/PromptKit/runtime/providers"

//nolint:gochecknoinits // Factory registration requires init
func init() {
	providers.RegisterEmbeddingProviderFactory("openai",
		func(spec providers.EmbeddingProviderSpec) (providers.EmbeddingProvider, error) {
			opts := []EmbeddingOption{}
			if spec.Model != "" {
				opts = append(opts, WithEmbeddingModel(spec.Model))
			}
			if spec.BaseURL != "" {
				opts = append(opts, WithEmbeddingBaseURL(spec.BaseURL))
			}
			if k := providers.APIKeyFromCredential(spec.Credential); k != "" {
				opts = append(opts, WithEmbeddingAPIKey(k))
			}
			return NewEmbeddingProvider(opts...)
		},
	)
}
