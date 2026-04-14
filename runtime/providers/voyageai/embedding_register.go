package voyageai

import "github.com/AltairaLabs/PromptKit/runtime/providers"

//nolint:gochecknoinits // Factory registration requires init
func init() {
	providers.RegisterEmbeddingProviderFactory("voyageai",
		func(spec providers.EmbeddingProviderSpec) (providers.EmbeddingProvider, error) {
			opts := []EmbeddingOption{}
			if spec.Model != "" {
				opts = append(opts, WithModel(spec.Model))
			}
			if spec.BaseURL != "" {
				opts = append(opts, WithBaseURL(spec.BaseURL))
			}
			if k := providers.APIKeyFromCredential(spec.Credential); k != "" {
				opts = append(opts, WithAPIKey(k))
			}
			if dims, ok := providers.IntFromConfig(spec.AdditionalConfig, "dimensions"); ok {
				opts = append(opts, WithDimensions(dims))
			}
			if v, ok := spec.AdditionalConfig["input_type"].(string); ok && v != "" {
				opts = append(opts, WithInputType(v))
			}
			return NewEmbeddingProvider(opts...)
		},
	)
}
