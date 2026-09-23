package gemini

import "github.com/AltairaLabs/PromptKit/runtime/v2/providers"

//nolint:gochecknoinits // Factory registration requires init
func init() {
	providers.RegisterEmbeddingProviderFactory("gemini",
		func(spec providers.EmbeddingProviderSpec) (providers.EmbeddingProvider, error) {
			tr, err := providers.ResolveEmbeddingTransport(spec)
			if err != nil {
				return nil, err
			}
			opts := []EmbeddingOption{WithGeminiEmbeddingWiring(providers.EmbeddingWiringFrom(spec, tr))}
			if tr.APIKey != "" {
				opts = append(opts, WithGeminiEmbeddingAPIKey(tr.APIKey))
			}
			return NewEmbeddingProvider(opts...)
		},
	)
}
