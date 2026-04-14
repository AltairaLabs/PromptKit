package ollama

import "github.com/AltairaLabs/PromptKit/runtime/providers"

//nolint:gochecknoinits // Factory registration requires init
func init() {
	providers.RegisterEmbeddingProviderFactory("ollama",
		func(spec providers.EmbeddingProviderSpec) (providers.EmbeddingProvider, error) {
			opts := []EmbeddingOption{}
			if spec.Model != "" {
				opts = append(opts, WithEmbeddingModel(spec.Model))
			}
			if spec.BaseURL != "" {
				opts = append(opts, WithEmbeddingBaseURL(spec.BaseURL))
			}
			if dims, ok := providers.IntFromConfig(spec.AdditionalConfig, "dimensions"); ok {
				opts = append(opts, WithEmbeddingDimensions(dims))
			}
			return NewEmbeddingProvider(opts...), nil
		},
	)
}
