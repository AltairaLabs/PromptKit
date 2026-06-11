package engine

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/classify"
	_ "github.com/AltairaLabs/PromptKit/runtime/classify/backends/all" // registers classify backend factories via init()
	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// buildClassifyRegistry maps cfg.LoadedInferenceProviders (every
// `providers:` entry with role: inference) into classify.ProviderSpecs
// and delegates construction to the shared runtime factory. Returns
// (nil, nil) when no inference providers are configured.
//
// Arena keeps its own credential policy: an inference provider with no
// resolvable API key is an error (the caller logs and continues with a
// nil registry, so classify-backed handlers skip cleanly rather than
// failing — this preserves demos that run without HF_TOKEN).
func buildClassifyRegistry(cfg *config.Config) (*classify.Registry, error) {
	if cfg == nil || len(cfg.LoadedInferenceProviders) == 0 {
		return nil, nil
	}
	specs := make([]classify.ProviderSpec, 0, len(cfg.LoadedInferenceProviders))
	for id, provider := range cfg.LoadedInferenceProviders {
		cred, err := credentials.Resolve(context.Background(), credentials.ResolverConfig{
			ProviderType:     provider.Type,
			CredentialConfig: provider.Credential,
			ConfigDir:        cfg.ConfigDir,
		})
		if err != nil {
			return nil, fmt.Errorf("inference provider %s: %w", id, err)
		}
		if base.APIKeyFromCredential(cred) == "" {
			return nil, fmt.Errorf(
				"inference provider %s: no api key configured (set credential.api_key, "+
					"credential.credential_env, or export HF_TOKEN / HUGGING_FACE_HUB_TOKEN)", id)
		}
		specs = append(specs, classify.ProviderSpec{
			ID:               id,
			Type:             provider.Type,
			BaseURL:          provider.BaseURL,
			Credential:       cred,
			AdditionalConfig: provider.AdditionalConfig,
		})
	}
	return classify.BuildRegistry(specs, inferenceDefaults(cfg.Defaults.Inference))
}

func inferenceDefaults(d *config.InferenceDefaults) classify.RegistryDefaults {
	if d == nil {
		return classify.RegistryDefaults{}
	}
	return classify.RegistryDefaults{
		AudioClassifier: d.AudioClassifier,
		TextClassifier:  d.TextClassifier,
		ImageClassifier: d.ImageClassifier,
		VideoClassifier: d.VideoClassifier,
		Embedder:        d.Embedder,
	}
}
