package handlers

import (
	"context"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
	classifyhf "github.com/AltairaLabs/PromptKit/runtime/classify/backends/hf"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Image moderation scores the agent's visual output by default (role:
// assistant), which also covers images a tool produced during the
// assistant's turn (e.g. image__generate) — see collectImagePartsByRole.
const imageModerationDefaultRole = roleAssistant

// ImageModerationHandler is a pure eval primitive: it runs the latest image
// in the target role's turns through the configured ImageClassifier and emits
// the score for expected_label (e.g. "nsfw") as EvalResult.Score. Threshold
// judgment (min_score / max_score) lives on `type: assertion` / `type:
// guardrail` wrappers — NOT on this handler.
//
// Params:
//   - model           string  (required) — classifier model id, e.g. "Falconsai/nsfw_image_detection"
//   - expected_label  string  (required) — label whose score is emitted
//   - message_role    string  (optional, default "assistant") — whose image to score
//   - message_index   int     (optional, default -1 = latest match)
//   - classifier_id   string  (optional) — explicit registry id; empty uses the configured default
type ImageModerationHandler struct{}

// Type returns the eval type identifier.
func (h *ImageModerationHandler) Type() string { return "image_moderation" }

// Eval resolves the ImageClassifier from context, locates the target image part,
// classifies it, and emits the requested label's score. Skipped vs Error split
// mirrors audio_emotion: infrastructure absence (no registry, no image, model
// loading/unsupported) is Skipped; misconfiguration or a runtime classifier
// failure is Error.
func (h *ImageModerationHandler) Eval(
	ctx context.Context, evalCtx *evals.EvalContext, params map[string]any,
) (*evals.EvalResult, error) {
	cfg, cfgErr := parseClassifyConfig(params, imageModerationDefaultRole)
	if cfgErr != nil {
		return errorResult(h.Type(), cfgErr.Error()), nil
	}

	classifier, classifierErr := resolveImageClassifier(ctx, cfg.classifierID)
	if classifierErr != nil {
		return skippedResult(h.Type(), classifierErr.Error()), nil
	}

	imageParts := collectMediaContentByRole(evalCtx.Messages, types.ContentTypeImage, cfg.messageRole)
	if len(imageParts) == 0 {
		return skippedResult(h.Type(),
			fmt.Sprintf("no image part found with role %q", cfg.messageRole)), nil
	}
	media, partErr := pickMediaPart(imageParts, cfg.messageIndex)
	if partErr != nil {
		return errorResult(h.Type(), partErr.Error()), nil
	}

	imageBytes, readErr := readMediaBytes(media)
	if readErr != nil {
		return errorResult(h.Type(), readErr.Error()), nil
	}

	scores, classifyErr := classifier.ClassifyImage(ctx, imageBytes, classify.ImageOptions{
		Model:    cfg.model,
		MIMEType: media.MIMEType,
	})
	if classifyErr != nil {
		if errors.Is(classifyErr, classifyhf.ErrModelLoading) {
			return skippedResult(h.Type(), "model still loading after retries"), nil
		}
		if errors.Is(classifyErr, classifyhf.ErrModelNotSupported) {
			return skippedResult(h.Type(),
				"model not supported by the configured inference path "+
					"(deploy an HF Inference Endpoint or pick a supported model)"), nil
		}
		return errorResult(h.Type(), fmt.Sprintf("classify failed: %v", classifyErr)), nil
	}

	return gradeExpectedLabel(h.Type(), &cfg, scores), nil
}

// resolveImageClassifier pulls the classify registry out of context and looks up
// the requested classifier id. An empty id resolves the configured default.
func resolveImageClassifier(ctx context.Context, id string) (classify.ImageClassifier, error) {
	reg := classify.FromContext(ctx)
	if reg == nil {
		return nil, errors.New(
			"no classify registry configured; add a providers: entry with role: inference " +
				"and either defaults.inference.image_classifier or params.classifier_id")
	}
	return reg.ImageClassifier(id)
}
