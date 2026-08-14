package vertex

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

//nolint:gochecknoinits // Factory registration requires init
func init() {
	providers.RegisterEmbeddingProviderFactory("vertex",
		func(spec providers.EmbeddingProviderSpec) (providers.EmbeddingProvider, error) {
			// Vertex has no API-key mode: without a platform block there is no
			// token source and every request would go unauthenticated. Fail
			// here rather than once per request.
			if spec.Platform == "" {
				return nil, fmt.Errorf(
					"vertex embedding provider requires a platform block " +
						"(platform.type: vertex, platform.project: <project>, platform.region: <region>)")
			}
			tr, err := providers.ResolveEmbeddingTransport(spec)
			if err != nil {
				return nil, err
			}
			return NewEmbeddingProvider(optionsFromSpec(spec, tr)...)
		},
	)
}

// optionsFromSpec translates a resolved spec and transport into constructor
// options.
func optionsFromSpec(spec providers.EmbeddingProviderSpec, tr providers.EmbeddingTransport) []EmbeddingOption {
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
	if tr.PlatformAuth {
		opts = append(opts, WithPlatformAuth())
	}
	if dims, ok := providers.IntFromConfig(spec.AdditionalConfig, "dimensions"); ok {
		opts = append(opts, WithDimensions(dims))
	}
	if v, ok := spec.AdditionalConfig["task_type"].(string); ok && v != "" {
		opts = append(opts, WithTaskType(v))
	}
	return opts
}
