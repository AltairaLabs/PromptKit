package openai

import "github.com/AltairaLabs/PromptKit/runtime/providers"

//nolint:gochecknoinits // Factory registration requires init
func init() {
	providers.RegisterEmbeddingProviderFactory("openai",
		func(spec providers.EmbeddingProviderSpec) (providers.EmbeddingProvider, error) {
			tr, err := providers.ResolveEmbeddingTransport(spec)
			if err != nil {
				return nil, err
			}
			opts := []EmbeddingOption{}
			if spec.Model != "" {
				opts = append(opts, WithEmbeddingModel(spec.Model))
			}
			if tr.BaseURL != "" {
				opts = append(opts, WithEmbeddingBaseURL(tr.BaseURL))
			}
			if tr.Client != nil {
				opts = append(opts, WithEmbeddingHTTPClient(tr.Client))
			}
			if tr.APIKey != "" {
				opts = append(opts, WithEmbeddingAPIKey(tr.APIKey))
			}
			if tr.PlatformAuth {
				opts = append(opts, WithEmbeddingPlatformAuth())
			}
			return NewEmbeddingProvider(opts...)
		},
	)
}
