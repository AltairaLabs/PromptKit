package cohere

import "github.com/AltairaLabs/PromptKit/runtime/v2/providers"

//nolint:gochecknoinits // Factory registration requires init
func init() {
	providers.RegisterRerankProviderFactory("cohere",
		func(spec providers.RerankProviderSpec) (providers.RerankProvider, error) {
			tr, err := providers.ResolveRerankTransport(spec)
			if err != nil {
				return nil, err
			}
			opts := []RerankOption{}
			if spec.Model != "" {
				opts = append(opts, WithModel(spec.Model))
			}
			if tr.BaseURL != "" {
				opts = append(opts, WithBaseURL(tr.BaseURL))
			}
			if tr.APIKey != "" {
				opts = append(opts, WithAPIKey(tr.APIKey))
			}
			if n, ok := providers.IntFromConfig(spec.AdditionalConfig, "max_tokens_per_doc"); ok {
				opts = append(opts, WithMaxTokensPerDoc(n))
			}
			return NewRerankProvider(opts...)
		},
	)
}
