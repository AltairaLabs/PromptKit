package classify

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/classify/framesample"
)

// DecomposeProviderType is the inference-provider `type` that builds a
// decomposing VideoClassifier from sibling image/audio classifiers.
// Unlike leaf providers it has no standalone factory — a composite must
// reference already-registered backends, so BuildRegistry resolves it in
// a second pass (see buildDecomposeFromSpec).
const DecomposeProviderType = "decompose"

// buildDecomposeFromSpec constructs a decomposing VideoClassifier from a
// `type: decompose` spec, resolving its image (required) and audio
// (optional) sub-classifiers from the already-populated registry.
//
// additional_config keys:
//   - image_classifier_id  string  (required) — registry id of the ImageClassifier
//   - audio_classifier_id  string  (optional) — registry id of the AudioClassifier
//   - image_model          string  — model id passed per frame
//   - audio_model          string  — model id passed to the audio track
//   - aggregation          string  — max|mean|vote (default max)
//   - frame_sample_rate    number  — frames/sec to sample
//   - max_frames           number  — cap on sampled frames
//   - max_concurrency      number  — bound on parallel frame classification
func buildDecomposeFromSpec(reg *Registry, spec ProviderSpec) (VideoClassifier, error) {
	ac := spec.AdditionalConfig

	imageID := stringFromConfig(ac, "image_classifier_id")
	if imageID == "" {
		return nil, fmt.Errorf(
			"classify: decompose provider %q requires additional_config.image_classifier_id", spec.ID)
	}
	img, err := reg.ImageClassifier(imageID)
	if err != nil {
		return nil, fmt.Errorf("classify: decompose provider %q: image classifier: %w", spec.ID, err)
	}

	var audio AudioClassifier
	if audioID := stringFromConfig(ac, "audio_classifier_id"); audioID != "" {
		audio, err = reg.AudioClassifier(audioID)
		if err != nil {
			return nil, fmt.Errorf("classify: decompose provider %q: audio classifier: %w", spec.ID, err)
		}
	}

	imageModel := stringFromConfig(ac, "image_model")
	if imageModel == "" {
		imageModel = spec.Model
	}
	cfg := DecomposeConfig{
		ImageModel:      imageModel,
		AudioModel:      stringFromConfig(ac, "audio_model"),
		Aggregation:     stringFromConfig(ac, "aggregation"),
		FramesPerSecond: floatFromConfig(ac, "frame_sample_rate"),
		MaxFrames:       intFromConfig(ac, "max_frames"),
		MaxConcurrency:  intFromConfig(ac, "max_concurrency"),
	}
	return NewDecomposingVideoClassifier(framesample.NewGIFMJPEGSampler(), img, audio, cfg)
}

// stringFromConfig reads a string flag from additional_config, returning
// "" when absent or the wrong type. Safe on a nil map.
func stringFromConfig(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// floatFromConfig reads a numeric flag, tolerating int or float64 (JSON
// decodes numbers as float64; programmatic callers may pass int).
func floatFromConfig(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	default:
		return 0
	}
}

// intFromConfig reads an integer flag, tolerating float64 (JSON numbers).
func intFromConfig(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}
