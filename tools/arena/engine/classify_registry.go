package engine

import (
	"fmt"
	"os"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/classify"
	classifyhf "github.com/AltairaLabs/PromptKit/runtime/classify/backends/hf"
)

// inferenceTypeHuggingFace is the type string for HuggingFace Inference API
// entries in arena's `inference:` block. It's the only backend kind that
// ships in this slice; ONNX and others are queued behind separate issues.
const inferenceTypeHuggingFace = "huggingface"

// buildClassifyRegistry constructs a classify.Registry populated with the
// task-interface implementations declared in cfg.Inference. Each loaded
// entry instantiates one backend Client and registers it against every
// task interface that backend implements (HF's Client satisfies all five —
// audio/text/image classifiers + embedder). cfg.Defaults.Inference is then
// applied to pin which id wins when a handler doesn't specify one.
//
// Returns (nil, nil) when no inference entries are configured — handlers
// that need a classifier will surface their own "no classifier configured"
// error via classify.FromContext returning nil, which is the desired
// behavior: arenas that don't use classify-backed assertions shouldn't
// require an HF_TOKEN.
//
// Returns a non-nil error only when a configured entry is malformed
// (unsupported type, missing api key). The caller logs and continues —
// one broken entry must not block a run that doesn't reach a
// classify-dependent handler.
func buildClassifyRegistry(cfg *config.Config) (*classify.Registry, error) {
	if cfg == nil || len(cfg.LoadedInference) == 0 {
		return nil, nil
	}
	reg := classify.NewRegistry()
	for id, entry := range cfg.LoadedInference {
		if err := registerInferenceEntry(reg, id, entry); err != nil {
			return reg, err
		}
	}
	if err := applyInferenceDefaults(reg, cfg.Defaults.Inference); err != nil {
		return reg, err
	}
	return reg, nil
}

func registerInferenceEntry(reg *classify.Registry, id string, entry *config.InferenceConfig) error {
	switch entry.Type {
	case inferenceTypeHuggingFace:
		apiKey, err := resolveInferenceAPIKey(entry)
		if err != nil {
			return fmt.Errorf("inference[%s]: %w", id, err)
		}
		client, err := classifyhf.NewClient(classifyhf.Config{
			APIKey:    apiKey,
			BaseURL:   entry.BaseURL,
			Dedicated: entry.Dedicated,
		})
		if err != nil {
			return fmt.Errorf("inference[%s]: %w", id, err)
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
		return fmt.Errorf("inference[%s]: unsupported type %q (supported: %s)", id, entry.Type, inferenceTypeHuggingFace)
	}
}

func resolveInferenceAPIKey(entry *config.InferenceConfig) (string, error) {
	if entry.APIKey != "" {
		return entry.APIKey, nil
	}
	if entry.APIKeyEnv != "" {
		if v := os.Getenv(entry.APIKeyEnv); v != "" {
			return v, nil
		}
		return "", fmt.Errorf("env var %s is empty or unset", entry.APIKeyEnv)
	}
	// HF canonical env var fallback so arenas with a single inference entry
	// don't need an explicit api_key_env. HF_TOKEN is the documented name;
	// HUGGING_FACE_HUB_TOKEN is the legacy alias that some CI runners use.
	for _, name := range []string{"HF_TOKEN", "HUGGING_FACE_HUB_TOKEN"} {
		if v := os.Getenv(name); v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("api_key or api_key_env required (HF_TOKEN / HUGGING_FACE_HUB_TOKEN also accepted as fallback)")
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
