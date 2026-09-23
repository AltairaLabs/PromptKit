package openai

import "github.com/AltairaLabs/PromptKit/runtime/v2/providers"

//nolint:gochecknoinits // Factory registration requires init
func init() {
	providers.RegisterEmbeddingProviderFactory("openai",
		func(spec providers.EmbeddingProviderSpec) (providers.EmbeddingProvider, error) {
			tr, err := providers.ResolveEmbeddingTransport(spec)
			if err != nil {
				return nil, err
			}
			opts := []EmbeddingOption{WithEmbeddingWiring(providers.EmbeddingWiringFrom(spec, tr))}
			if tr.APIKey != "" {
				opts = append(opts, WithEmbeddingAPIKey(tr.APIKey))
			}
			return NewEmbeddingProvider(opts...)
		},
	)
}
