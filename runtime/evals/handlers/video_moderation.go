package handlers

import (
	"context"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
	classifyhf "github.com/AltairaLabs/PromptKit/runtime/classify/backends/hf"
	"github.com/AltairaLabs/PromptKit/runtime/classify/framesample"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Video moderation scores the agent's visual output by default (role:
// assistant), which also covers video a tool produced during the
// assistant's turn (e.g. video__generate) — see collectMediaContentByRole.
const videoModerationDefaultRole = roleAssistant

// VideoModerationHandler is a pure eval primitive: it runs the latest video
// in the target role's turns through the configured VideoClassifier and emits
// the score for expected_label (e.g. "nsfw") as EvalResult.Score. Threshold
// judgment (min_score / max_score) lives on `type: assertion` / `type:
// guardrail` wrappers — NOT on this handler.
//
// The shipped VideoClassifier is the decomposing backend: it samples frames
// (pure-Go GIF/MJPEG), classifies each via an ImageClassifier, optionally
// classifies the audio track, and aggregates the per-frame scores. Which
// sub-models and aggregation default a classifier uses is a config concern
// of the `decompose` inference provider; this handler only steers per-call
// sampling / audio / aggregation.
//
// Params:
//   - model             string  (required) — image-model id for per-frame classification
//   - expected_label    string  (required) — label whose score is emitted
//   - message_role      string  (optional, default "assistant") — whose video to score
//   - message_index     int     (optional, default -1 = latest match)
//   - classifier_id     string  (optional) — explicit registry id; empty uses the configured default
//   - frame_sample_rate float64 (optional) — frames/sec to sample; 0 uses the backend default
//   - extract_audio     bool    (optional, default false) — also classify the audio track
//   - aggregation       string  (optional) — max|mean|vote; empty uses the backend default
type VideoModerationHandler struct{}

// Type returns the eval type identifier.
func (h *VideoModerationHandler) Type() string { return "video_moderation" }

// Eval resolves the VideoClassifier from context, locates the target video
// part, classifies it, and emits the requested label's score. Skipped vs Error
// mirrors image_moderation: infrastructure absence (no registry, no video,
// undecodable container, model loading/unsupported) is Skipped; misconfiguration
// or a runtime classifier failure is Error.
func (h *VideoModerationHandler) Eval(
	ctx context.Context, evalCtx *evals.EvalContext, params map[string]any,
) (*evals.EvalResult, error) {
	cfg, cfgErr := parseClassifyConfig(params, videoModerationDefaultRole)
	if cfgErr != nil {
		return errorResult(h.Type(), cfgErr.Error()), nil
	}
	extra, extraErr := parseVideoModerationExtras(params)
	if extraErr != nil {
		return errorResult(h.Type(), extraErr.Error()), nil
	}

	classifier, classifierErr := resolveVideoClassifier(ctx, cfg.classifierID)
	if classifierErr != nil {
		return skippedResult(h.Type(), classifierErr.Error()), nil
	}

	videoParts := collectMediaContentByRole(evalCtx.Messages, types.ContentTypeVideo, cfg.messageRole)
	if len(videoParts) == 0 {
		return skippedResult(h.Type(),
			fmt.Sprintf("no video part found with role %q", cfg.messageRole)), nil
	}
	media, partErr := pickMediaPart(videoParts, cfg.messageIndex)
	if partErr != nil {
		return errorResult(h.Type(), partErr.Error()), nil
	}

	videoBytes, readErr := readMediaBytes(media)
	if readErr != nil {
		return errorResult(h.Type(), readErr.Error()), nil
	}

	scores, classifyErr := classifier.ClassifyVideo(ctx, videoBytes, classify.VideoOptions{
		Model:           cfg.model,
		MIMEType:        media.MIMEType,
		FrameSampleRate: extra.frameSampleRate,
		ExtractAudio:    extra.extractAudio,
		Aggregation:     extra.aggregation,
	})
	if classifyErr != nil {
		return h.classifyErrorResult(classifyErr), nil
	}

	return gradeExpectedLabel(h.Type(), &cfg, scores), nil
}

// classifyErrorResult maps a ClassifyVideo error to Skipped or Error. An
// undecodable container, a still-loading model, or a model unsupported on the
// configured inference path are environmental (Skipped) — keyless / free-tier
// / pure-Go-only runs shouldn't fail the scenario. Anything else is a real
// runtime failure (Error).
func (h *VideoModerationHandler) classifyErrorResult(err error) *evals.EvalResult {
	switch {
	case errors.Is(err, framesample.ErrUnsupportedContainer):
		return skippedResult(h.Type(),
			"video container not decodable by the pure-Go sampler "+
				"(mp4/webm need an ffmpeg-backed sampler; GIF/MJPEG are supported)")
	case errors.Is(err, classifyhf.ErrModelLoading):
		return skippedResult(h.Type(), "model still loading after retries")
	case errors.Is(err, classifyhf.ErrModelNotSupported):
		return skippedResult(h.Type(),
			"model not supported by the configured inference path "+
				"(deploy an HF Inference Endpoint or pick a supported model)")
	default:
		return errorResult(h.Type(), fmt.Sprintf("classify failed: %v", err))
	}
}

// videoModerationExtras holds the video-specific per-call knobs parsed on top
// of the shared classify config.
type videoModerationExtras struct {
	frameSampleRate float64
	extractAudio    bool
	aggregation     string
}

// parseVideoModerationExtras validates the video-only params. An unknown
// aggregation is a call-site misconfiguration (Error), not an environmental
// absence.
func parseVideoModerationExtras(params map[string]any) (videoModerationExtras, error) {
	var e videoModerationExtras
	if v, ok := extractFloat64(params, "frame_sample_rate"); ok {
		e.frameSampleRate = v
	}
	if v, ok := params["extract_audio"].(bool); ok {
		e.extractAudio = v
	}
	if v, ok := params["aggregation"].(string); ok && v != "" {
		switch v {
		case classify.AggregationMax, classify.AggregationMean, classify.AggregationVote:
			e.aggregation = v
		default:
			return e, fmt.Errorf("invalid aggregation %q (want %s|%s|%s)",
				v, classify.AggregationMax, classify.AggregationMean, classify.AggregationVote)
		}
	}
	return e, nil
}

// resolveVideoClassifier pulls the classify registry out of context and looks
// up the requested classifier id. An empty id resolves the configured default.
func resolveVideoClassifier(ctx context.Context, id string) (classify.VideoClassifier, error) {
	reg := classify.FromContext(ctx)
	if reg == nil {
		return nil, errors.New(
			"no classify registry configured; add a providers: entry with role: inference " +
				"and either defaults.inference.video_classifier or params.classifier_id")
	}
	return reg.VideoClassifier(id)
}
