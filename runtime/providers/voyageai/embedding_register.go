package voyageai

import "github.com/AltairaLabs/PromptKit/runtime/providers"

//nolint:gochecknoinits // Factory registration requires init
func init() {
	providers.RegisterEmbeddingProviderFactory("voyageai",
		func(spec providers.EmbeddingProviderSpec) (providers.EmbeddingProvider, error) {
			tr, err := providers.ResolveEmbeddingTransport(spec)
			if err != nil {
				return nil, err
			}
			opts := []EmbeddingOption{}
			if spec.Model != "" {
				opts = append(opts, WithModel(spec.Model))
			}
			if tr.BaseURL != "" {
				opts = append(opts, WithBaseURL(tr.BaseURL))
			}
			if tr.Client != nil {
				opts = append(opts, WithHTTPClient(tr.Client))
			}
			if tr.APIKey != "" {
				opts = append(opts, WithAPIKey(tr.APIKey))
			}
			if tr.PlatformAuth {
				opts = append(opts, WithPlatformAuth())
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
