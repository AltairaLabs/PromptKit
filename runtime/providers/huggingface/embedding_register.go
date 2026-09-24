package huggingface

import "github.com/AltairaLabs/PromptKit/runtime/v2/providers"

// dedicatedConfigKey marks base_url as a dedicated Inference Endpoint, the
// same additional_config key the huggingface inference backend uses.
const dedicatedConfigKey = "dedicated"

//nolint:gochecknoinits // Factory registration requires init
func init() {
	providers.RegisterEmbeddingProviderFactory("huggingface",
		func(spec providers.EmbeddingProviderSpec) (providers.EmbeddingProvider, error) {
			tr, err := providers.ResolveEmbeddingTransport(spec)
			if err != nil {
				return nil, err
			}
			opts := []EmbeddingOption{WithWiring(providers.EmbeddingWiringFrom(spec, tr))}
			if tr.APIKey != "" {
				opts = append(opts, WithAPIKey(tr.APIKey))
			}
			if dedicated, _ := spec.AdditionalConfig[dedicatedConfigKey].(bool); dedicated {
				opts = append(opts, WithDedicatedEndpoint())
			}
			return NewEmbeddingProvider(opts...)
		},
	)
}
