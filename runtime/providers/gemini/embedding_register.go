package gemini

import "github.com/AltairaLabs/PromptKit/runtime/providers"

//nolint:gochecknoinits // Factory registration requires init
func init() {
	providers.RegisterEmbeddingProviderFactory("gemini",
		func(spec providers.EmbeddingProviderSpec) (providers.EmbeddingProvider, error) {
			opts := []EmbeddingOption{}
			if spec.Model != "" {
				opts = append(opts, WithGeminiEmbeddingModel(spec.Model))
			}
			if spec.BaseURL != "" {
				opts = append(opts, WithGeminiEmbeddingBaseURL(spec.BaseURL))
			}
			if k := providers.APIKeyFromCredential(spec.Credential); k != "" {
				opts = append(opts, WithGeminiEmbeddingAPIKey(k))
			}
			return NewEmbeddingProvider(opts...)
		},
	)
}
