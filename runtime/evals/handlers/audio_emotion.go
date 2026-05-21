package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
	classifyhf "github.com/AltairaLabs/PromptKit/runtime/classify/backends/hf"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Audio-specific defaults override the classify-base ones. Audio
// scoring usually targets the caller's speech (role: user), unlike
// text scoring which targets the assistant.
const audioEmotionDefaultRole = "user"

// AudioEmotionHandler is a pure eval primitive: it calls the
// AudioClassifier resolved from the orchestrator's classify registry,
// picks the score for the chosen expected_label, and emits it as
// EvalResult.Score. Threshold judgment (min_score / max_score) lives
// on `type: assertion` wrappers — NOT on this handler.
//
// Wrap with `type: assertion` to assert against a threshold:
//
//   - type: assertion
//     params:
//     eval_type: audio_emotion
//     eval_params: { model: "...", expected_label: "ang", message_role: user }
//     min_score: 0.5
//
// Use directly in pack `evals:` to emit the raw signal at runtime
// (for metrics / observability).
//
// Params:
//   - model           string  (required) — backend model id, e.g. "superb/wav2vec2-base-superb-er"
//   - expected_label  string  (required) — label whose score is emitted
//   - message_role    string  (optional, default "user") — which speaker's audio to score
//   - message_index   int     (optional, default -1 = latest match) — pick a specific audio message
//   - classifier_id   string  (optional) — explicit registry id; empty uses the configured default
//
// Putting min_score / max_score on this handler is rejected — the
// assertion wrapper is the canonical home for thresholds.
type AudioEmotionHandler struct{}

// Type returns the eval type identifier.
func (h *AudioEmotionHandler) Type() string { return "audio_emotion" }

// Eval pulls the AudioClassifier out of context, locates the target audio
// part in the conversation, runs classification, and grades the requested
// label against the configured threshold.
//
// Skipped vs Error distinction:
//   - Skipped — the assertion couldn't run because preconditions weren't met.
//     Used for infrastructure absence: no classify registry configured (e.g.
//     CI runs without HF_TOKEN), no audio parts in the message log (e.g. a
//     mock provider that doesn't emit audio), or HF returned ErrModelLoading.
//     These are configuration / environment shapes, not assertion failures.
//   - Error — the assertion is misconfigured at the call site (missing
//     required param) or the classifier call failed at runtime. The user
//     should fix something.
//
// The split lets a single arena config sit happily under both real-provider
// runs (assertion exercises a real model) and keyless CI runs (assertion
// skips cleanly) without needing per-environment scenario forks.
func (h *AudioEmotionHandler) Eval(
	ctx context.Context, evalCtx *evals.EvalContext, params map[string]any,
) (*evals.EvalResult, error) {
	cfg, cfgErr := parseAudioEmotionParams(params)
	if cfgErr != nil {
		return errorResult(h.Type(), cfgErr.Error()), nil
	}

	classifier, classifierErr := resolveAudioClassifier(ctx, cfg.classifierID)
	if classifierErr != nil {
		return skippedResult(h.Type(), classifierErr.Error()), nil
	}

	audioParts := collectAudioPartsByRole(evalCtx.Messages, cfg.messageRole)
	if len(audioParts) == 0 {
		// No audio at all in the chosen role's turns is an infrastructure
		// shape (mock provider, text-only scenario), not a config error.
		return skippedResult(h.Type(),
			fmt.Sprintf("no audio part found with role %q", cfg.messageRole)), nil
	}
	media, partErr := pickAudioPart(audioParts, cfg.messageIndex)
	if partErr != nil {
		// Audio exists but the user picked a specific index that's out of
		// range — that's a misconfiguration at the assertion call site,
		// not an environmental absence. Fail with a clear message.
		return errorResult(h.Type(), partErr.Error()), nil
	}

	audioBytes, readErr := readMediaBytes(media)
	if readErr != nil {
		return errorResult(h.Type(), readErr.Error()), nil
	}

	opts := classify.AudioOptions{
		Model:    cfg.model,
		MIMEType: media.MIMEType,
	}
	scores, classifyErr := classifier.ClassifyAudio(ctx, audioBytes, opts)
	if classifyErr != nil {
		if errors.Is(classifyErr, classifyhf.ErrModelLoading) {
			return skippedResult(h.Type(), "model still loading after retries"), nil
		}
		if errors.Is(classifyErr, classifyhf.ErrModelNotSupported) {
			// The configured model can't be served on the configured
			// inference path — typically a retired free-tier SER model
			// on `router.huggingface.co/hf-inference`. Skip cleanly so
			// keyless / free-tier demo runs don't fail the scenario;
			// the user fixes the config by deploying an Inference
			// Endpoint or picking a model the path supports.
			return skippedResult(h.Type(),
				"model not supported by the configured inference path "+
					"(deploy an HF Inference Endpoint or pick a supported model)"), nil
		}
		return errorResult(h.Type(), fmt.Sprintf("classify failed: %v", classifyErr)), nil
	}

	return gradeAudioEmotion(h.Type(), &cfg, scores), nil
}

// skippedResult builds an EvalResult that records "didn't run, didn't fail"
// — the SkipReason carries the actual cause so reports can show it. Score is
// set to a pass-shaped boolean so any downstream consumer that pre-filters
// on Score-passes treats skipped as non-failing.
func skippedResult(handlerType, reason string) *evals.EvalResult {
	return &evals.EvalResult{
		Type:       handlerType,
		Score:      boolScore(true),
		Skipped:    true,
		SkipReason: reason,
	}
}

// parseAudioEmotionParams returns the audio handler's view of the
// validated config. Audio adds nothing on top of the shared classify
// base today; the dedicated entry point is kept so future audio-only
// params (e.g. sample-rate-resampling toggles) have a place to land.
func parseAudioEmotionParams(params map[string]any) (classifyConfig, error) {
	return parseClassifyConfig(params, audioEmotionDefaultRole)
}

// resolveAudioClassifier pulls the registry out of context and looks up the
// requested classifier id. An empty id resolves the configured default, so
// arenas with `defaults.inference.audio_classifier` set don't need to repeat
// the id on every handler.
func resolveAudioClassifier(ctx context.Context, id string) (classify.AudioClassifier, error) {
	reg := classify.FromContext(ctx)
	if reg == nil {
		return nil, errors.New(
			"no classify registry configured; add a providers: entry with role: inference " +
				"and either defaults.inference.audio_classifier or params.classifier_id")
	}
	return reg.AudioClassifier(id)
}

// pickAudioPart selects one part from a non-empty slice of audio parts. A
// negative index picks the most recent (-1 → last). Caller is responsible
// for the empty-slice case so the absence-vs-out-of-range distinction can
// be surfaced at the right semantic level (Skipped vs Error).
func pickAudioPart(audioParts []*types.MediaContent, index int) (*types.MediaContent, error) {
	if index < 0 {
		return audioParts[len(audioParts)-1], nil
	}
	if index >= len(audioParts) {
		return nil, fmt.Errorf("message_index %d out of range (found %d audio parts)",
			index, len(audioParts))
	}
	return audioParts[index], nil
}

// storageReferenceAsPath returns the storage reference iff it looks like
// a local file path the handler can open directly. Cloud storage backends
// would produce references like `s3://bucket/key` or `gs://...`; those
// can't be read with os.ReadFile and need a real MediaLoader instead.
// The duplex pipeline's local-storage backend writes references that ARE
// filesystem paths, which is the case this handler shortcuts.
func storageReferenceAsPath(media *types.MediaContent) string {
	if media.StorageReference == nil || *media.StorageReference == "" {
		return ""
	}
	ref := *media.StorageReference
	if strings.Contains(ref, "://") {
		return ""
	}
	return ref
}

func collectAudioPartsByRole(messages []types.Message, role string) []*types.MediaContent {
	var out []*types.MediaContent
	for i := range messages {
		if messages[i].Role != role {
			continue
		}
		for j := range messages[i].Parts {
			part := &messages[i].Parts[j]
			if part.Type == types.ContentTypeAudio && part.Media != nil {
				out = append(out, part.Media)
			}
		}
	}
	return out
}

// readMediaBytes pulls the raw audio bytes out of a MediaContent.
// Supports inline base64 (Data), local file path, and storage_reference
// when the reference resolves to a readable local file. Production setups
// using cloud storage backends would need a full MediaLoader plumbed
// through context — that's queued separately. URL sources are deferred:
// they need an HTTP client and bound size limits the handler doesn't own.
//
// The duplex pipeline writes audio to disk via the media-storage backend
// and persists the storage_reference into the message log. For local
// storage (the demo's mode) that reference IS a path the handler can
// open directly; treating it as such avoids round-tripping through a
// storage service the eval handler doesn't have visibility into.
func readMediaBytes(media *types.MediaContent) ([]byte, error) {
	if media.URL != nil {
		return nil, errors.New(
			"audio_emotion can't yet fetch audio from a URL source; " +
				"set up media-storage that resolves to a local path or use inline data")
	}
	// storage_reference is treated as a local path fall-through. Production
	// setups using cloud storage would route through a MediaLoader; for
	// the local-storage demo path the reference IS a readable file.
	if storageRef := storageReferenceAsPath(media); storageRef != "" {
		body, err := os.ReadFile(storageRef) //nolint:gosec // path comes from runtime media-storage backend, not user input
		if err != nil {
			return nil, fmt.Errorf("read audio from storage reference %q: %w", storageRef, err)
		}
		if len(body) == 0 {
			return nil, errors.New("audio payload is empty")
		}
		return body, nil
	}
	reader, err := media.ReadData()
	if err != nil {
		return nil, fmt.Errorf("read audio: %w", err)
	}
	defer func() { _ = reader.Close() }()
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read audio body: %w", err)
	}
	if len(body) == 0 {
		return nil, errors.New("audio payload is empty")
	}
	return body, nil
}

// gradeAudioEmotion looks up the requested label in the classifier's
// scored output and emits its score. Pure eval primitive: no
// threshold judgment — that lives on the `type: assertion` wrapper.
//
// When the expected_label is absent from the classifier's output we
// emit Score = 0 (the label's effective confidence is zero) plus a
// clear Explanation; consumers comparing the resulting Score against
// a wrapper-supplied threshold get the right outcome (any positive
// min_score fails; "label not returned" is louder in the report).
func gradeAudioEmotion(
	handlerType string, cfg *classifyConfig, scores []classify.LabelScore,
) *evals.EvalResult {
	foundScore, foundLabel := findExpectedLabel(scores, cfg.expectedLabel)
	if foundLabel == "" {
		zero := 0.0
		allLabels := strings.Join(labelsFromScores(scores), ", ")
		return &evals.EvalResult{
			Type:        handlerType,
			Score:       &zero,
			MetricValue: &zero,
			Explanation: fmt.Sprintf("label %q not returned by model; got: %s", cfg.expectedLabel, allLabels),
			Details: map[string]any{
				classifyKeyExpectedLabel: cfg.expectedLabel,
				classifyKeyActualScore:   0.0,
				classifyKeyScores:        scores,
				keyFound:                 false,
			},
		}
	}
	scoreCopy := foundScore
	return &evals.EvalResult{
		Type:        handlerType,
		Score:       &scoreCopy,
		MetricValue: &scoreCopy,
		Explanation: fmt.Sprintf("%s score %.3f", foundLabel, foundScore),
		Details: map[string]any{
			classifyKeyExpectedLabel: cfg.expectedLabel,
			classifyKeyActualScore:   foundScore,
			classifyKeyScores:        scores,
		},
	}
}

// errorResult builds an EvalResult that records the failure reason
// without ever returning a Go error to the runner. Eval handlers that
// return errors short-circuit the batch; we'd rather let other evals
// still run and surface this one as failed with a clear explanation.
func errorResult(handlerType, msg string) *evals.EvalResult {
	return &evals.EvalResult{
		Type:        handlerType,
		Score:       boolScore(false),
		Error:       msg,
		Explanation: msg,
	}
}
