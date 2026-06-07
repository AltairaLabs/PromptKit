package gemini

import "github.com/AltairaLabs/PromptKit/runtime/providers"

//nolint:gochecknoinits // Factory registration requires init
func init() {
	providers.RegisterEmbeddingProviderFactory("gemini",
		func(spec providers.EmbeddingProviderSpec) (providers.EmbeddingProvider, error) {
			tr, err := providers.ResolveEmbeddingTransport(spec)
			if err != nil {
				return nil, err
			}
			opts := []EmbeddingOption{}
			if spec.Model != "" {
				opts = append(opts, WithGeminiEmbeddingModel(spec.Model))
			}
			if tr.BaseURL != "" {
				opts = append(opts, WithGeminiEmbeddingBaseURL(tr.BaseURL))
			}
			if tr.Client != nil {
				opts = append(opts, WithGeminiEmbeddingHTTPClient(tr.Client))
			}
			if tr.APIKey != "" {
				opts = append(opts, WithGeminiEmbeddingAPIKey(tr.APIKey))
			}
			if tr.PlatformAuth {
				opts = append(opts, WithGeminiEmbeddingPlatformAuth())
			}
			return NewEmbeddingProvider(opts...)
		},
	)
}
