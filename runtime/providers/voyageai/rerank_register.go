package voyageai

import "github.com/AltairaLabs/PromptKit/runtime/v2/providers"

//nolint:gochecknoinits // Factory registration requires init
func init() {
	providers.RegisterRerankProviderFactory("voyageai",
		func(spec providers.RerankProviderSpec) (providers.RerankProvider, error) {
			tr, err := providers.ResolveRerankTransport(spec)
			if err != nil {
				return nil, err
			}
			opts := []RerankOption{}
			if spec.Model != "" {
				opts = append(opts, WithRerankModel(spec.Model))
			}
			if tr.BaseURL != "" {
				opts = append(opts, WithRerankBaseURL(tr.BaseURL))
			}
			if tr.APIKey != "" {
				opts = append(opts, WithRerankAPIKey(tr.APIKey))
			}
			if v, ok := spec.AdditionalConfig["truncation"].(bool); ok {
				opts = append(opts, WithRerankTruncation(v))
			}
			return NewRerankProvider(opts...)
		},
	)
}
