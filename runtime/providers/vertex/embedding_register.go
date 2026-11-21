package vertex

import (
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

//nolint:gochecknoinits // Factory registration requires init
func init() {
	providers.RegisterPlatformEmbeddingProvider("vertex", providers.PlatformEmbeddingSpec{
		PlatformHint: "platform.type: vertex, platform.project: <project>, platform.region: <region>",
		Build: func(
			spec providers.EmbeddingProviderSpec, tr providers.EmbeddingTransport,
		) (providers.EmbeddingProvider, error) {
			opts := []EmbeddingOption{WithWiring(providers.EmbeddingWiringFrom(spec, tr))}
			if v, ok := spec.AdditionalConfig["task_type"].(string); ok && v != "" {
				opts = append(opts, WithTaskType(v))
			}
			if n, ok := providers.IntFromConfig(spec.AdditionalConfig, "batch_size"); ok {
				opts = append(opts, WithBatchSize(n))
			}
			return NewEmbeddingProvider(opts...)
		},
	})
}
