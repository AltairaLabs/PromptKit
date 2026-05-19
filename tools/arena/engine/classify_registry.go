package engine

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/classify"
	classifyhf "github.com/AltairaLabs/PromptKit/runtime/classify/backends/hf"
	"github.com/AltairaLabs/PromptKit/runtime/credentials"
)

// inferenceTypeHuggingFace is the type string for HuggingFace Inference API
// providers in arena's `providers:` list (role: inference). It's the only
// backend kind that ships in this slice; ONNX and others are queued behind
// separate issues.
const inferenceTypeHuggingFace = "huggingface"

// buildClassifyRegistry constructs a classify.Registry populated with the
// task-interface implementations declared in cfg.LoadedInferenceProviders
// (every `providers:` entry with `role: inference`). Each loaded provider
// instantiates one backend Client and registers against every classify
// task interface that backend implements (the HF backend covers four —
// audio/text/image classifiers + embedder). cfg.Defaults.Inference is
// then applied to pin which id wins when a handler doesn't specify one.
//
// Returns (nil, nil) when no inference providers are configured. Handlers
// that need a classifier will surface "no classifier configured" via
// classify.FromContext returning nil — arenas that don't use
// classify-backed assertions shouldn't require an HF token.
//
// Returns a non-nil error only when a configured provider is malformed
// (unsupported type, unresolvable credential, missing api key). The
// caller logs and continues — one broken entry must not block a run
// that doesn't reach a classify-dependent handler.
func buildClassifyRegistry(cfg *config.Config) (*classify.Registry, error) {
	if cfg == nil || len(cfg.LoadedInferenceProviders) == 0 {
		return nil, nil
	}
	reg := classify.NewRegistry()
	for id, provider := range cfg.LoadedInferenceProviders {
		if err := registerInferenceProvider(reg, id, provider, cfg.ConfigDir); err != nil {
			return reg, err
		}
	}
	if err := applyInferenceDefaults(reg, cfg.Defaults.Inference); err != nil {
		return reg, err
	}
	return reg, nil
}

func registerInferenceProvider(reg *classify.Registry, id string, provider *config.Provider, configDir string) error {
	switch provider.Type {
	case inferenceTypeHuggingFace:
		apiKey, err := resolveProviderAPIKey(provider, configDir)
		if err != nil {
			return fmt.Errorf("inference provider %s: %w", id, err)
		}
		client, err := classifyhf.NewClient(classifyhf.Config{
			APIKey:    apiKey,
			BaseURL:   provider.BaseURL,
			Dedicated: providerBoolFromConfig(provider, "dedicated"),
		})
		if err != nil {
			return fmt.Errorf("inference provider %s: %w", id, err)
		}
		// HF Client implements every task interface (compile-time checked in
		// runtime/classify/backends/hf/client.go). Register against each so
		// handlers asking for any task get this backend by id.
		reg.RegisterAudio(id, client)
		reg.RegisterText(id, client)
		reg.RegisterImage(id, client)
		reg.RegisterEmbedder(id, client)
		// VideoClassifier deliberately not registered: HF doesn't ship a
		// general video classification endpoint and the decomposing default
		// (audio + frame-sampled images) is queued in #1214 Phase 5.
		return nil
	default:
		return fmt.Errorf("inference provider %s: unsupported type %q (supported: %s)",
			id, provider.Type, inferenceTypeHuggingFace)
	}
}

// resolveProviderAPIKey runs the configured credential through the shared
// resolver and extracts the raw API key. Non-APIKey credentials (NoOp,
// cloud platform credentials) are rejected — the classify backends today
// need a bearer token, not a signed request.
func resolveProviderAPIKey(provider *config.Provider, configDir string) (string, error) {
	cred, err := credentials.Resolve(context.Background(), credentials.ResolverConfig{
		ProviderType:     provider.Type,
		CredentialConfig: provider.Credential,
		ConfigDir:        configDir,
	})
	if err != nil {
		return "", err
	}
	// Resolver returns NoOpCredential when no key is configured AND no
	// default env var is set. For HF we always need a bearer token —
	// surface a message that names every resolvable source.
	apiKey, ok := cred.(*credentials.APIKeyCredential)
	if !ok || apiKey.APIKey() == "" {
		return "", fmt.Errorf(
			"no api key configured (set credential.api_key, credential.credential_env, " +
				"or export HF_TOKEN / HUGGING_FACE_HUB_TOKEN)")
	}
	return apiKey.APIKey(), nil
}

// providerBoolFromConfig pulls a boolean flag out of Provider.AdditionalConfig.
// Returns false when the key is absent or the value isn't a bool — the
// backends already validate their own knobs at request time, so passing
// through "false" for a misspelled flag won't cause a silent breakage in
// practice (an unset Dedicated still hits the public HF endpoint).
func providerBoolFromConfig(provider *config.Provider, key string) bool {
	if provider == nil || provider.AdditionalConfig == nil {
		return false
	}
	v, ok := provider.AdditionalConfig[key].(bool)
	return ok && v
}

func applyInferenceDefaults(reg *classify.Registry, defaults *config.InferenceDefaults) error {
	if defaults == nil {
		return nil
	}
	for label, pair := range map[string]struct {
		id  string
		set func(string) error
	}{
		"audio_classifier": {defaults.AudioClassifier, reg.SetDefaultAudio},
		"text_classifier":  {defaults.TextClassifier, reg.SetDefaultText},
		"image_classifier": {defaults.ImageClassifier, reg.SetDefaultImage},
		"video_classifier": {defaults.VideoClassifier, reg.SetDefaultVideo},
		"embedder":         {defaults.Embedder, reg.SetDefaultEmbedder},
	} {
		if pair.id == "" {
			continue
		}
		if err := pair.set(pair.id); err != nil {
			return fmt.Errorf("defaults.inference.%s: %w", label, err)
		}
	}
	return nil
}
