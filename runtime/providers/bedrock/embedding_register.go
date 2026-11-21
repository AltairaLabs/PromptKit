package bedrock

import (
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

//nolint:gochecknoinits // Factory registration requires init
func init() {
	providers.RegisterPlatformEmbeddingProvider("bedrock", providers.PlatformEmbeddingSpec{
		PlatformHint: "platform.type: bedrock, platform.region: <region>",
		Build: func(
			spec providers.EmbeddingProviderSpec, tr providers.EmbeddingTransport,
		) (providers.EmbeddingProvider, error) {
			opts := []EmbeddingOption{WithWiring(providers.EmbeddingWiringFrom(spec, tr))}
			if v, ok := spec.AdditionalConfig["input_type"].(string); ok && v != "" {
				opts = append(opts, WithInputType(v))
			}
			if v, ok := spec.AdditionalConfig["embedding_format"].(string); ok && v != "" {
				opts = append(opts, WithEmbeddingFormat(v))
			}
			if n, ok := providers.IntFromConfig(spec.AdditionalConfig, "batch_size"); ok {
				opts = append(opts, WithBatchSize(n))
			}
			return NewEmbeddingProvider(opts...)
		},
	})
}
