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
			return NewEmbeddingProvider(opts...)
		},
	})
}
