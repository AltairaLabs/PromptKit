package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
	classifyhf "github.com/AltairaLabs/PromptKit/runtime/classify/backends/hf"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	audioEmotionDefaultMinScore = 0.5
	audioEmotionDefaultRole     = "user"

	// keys reused in result Value / Details maps. Extracted so the
	// linter doesn't flag duplication and so a future schema change
	// (e.g. renaming "expected_label" to "label") only edits one
	// place.
	audioEmotionKeyExpectedLabel = "expected_label"
	audioEmotionKeyMinScore      = "min_score"
	audioEmotionKeyScores        = "scores"
)

// AudioEmotionHandler scores whether the audio in a chosen message contains
// a target emotion (e.g. "angry") above a configurable confidence
// threshold. It calls the AudioClassifier resolved from the orchestrator's
// classify registry, so the model/backend are arena-config decisions, not
// handler decisions.
//
// Params:
//   - model           string  (required) — backend model id, e.g. "superb/wav2vec2-base-superb-er"
//   - expected_label  string  (required) — label that must appear with score >= min_score
//   - min_score       float   (optional, default 0.5) — confidence threshold for pass
//   - message_role    string  (optional, default "user") — which speaker's audio to score
//   - message_index   int     (optional, default -1 = latest match) — pick a specific audio message
//   - classifier_id   string  (optional) — explicit registry id; empty uses the configured default
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

// audioEmotionConfig holds the validated params after parsing. Keeping it
// separate from the YAML map lets later handlers reuse the same struct
// shape without each one re-doing the key/type dance.
type audioEmotionConfig struct {
	model         string
	expectedLabel string
	minScore      float64
	messageRole   string
	messageIndex  int
	classifierID  string
}

func parseAudioEmotionParams(params map[string]any) (audioEmotionConfig, error) {
	cfg := audioEmotionConfig{
		minScore:     audioEmotionDefaultMinScore,
		messageRole:  audioEmotionDefaultRole,
		messageIndex: -1,
	}
	model, _ := params["model"].(string)
	if model == "" {
		return cfg, errors.New("model is required (e.g. superb/wav2vec2-base-superb-er)")
	}
	cfg.model = model

	expected, _ := params[audioEmotionKeyExpectedLabel].(string)
	if expected == "" {
		return cfg, errors.New("expected_label is required")
	}
	cfg.expectedLabel = expected

	if v, ok := extractFloat64(params, "min_score"); ok {
		cfg.minScore = v
	}
	if v, ok := params["message_role"].(string); ok && v != "" {
		cfg.messageRole = v
	}
	if v, ok := params["message_index"].(int); ok {
		cfg.messageIndex = v
	} else if v, ok := extractFloat64(params, "message_index"); ok {
		cfg.messageIndex = int(v)
	}
	if v, ok := params["classifier_id"].(string); ok {
		cfg.classifierID = v
	}
	return cfg, nil
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

// readMediaBytes pulls the raw audio bytes out of a MediaContent. We
// already require the data to be available locally (base64 inline or
// file path) — URL / storage-reference parts would need a MediaLoader
// and aren't supported here yet. Surfacing a clear error is better than
// silently passing or failing the eval.
func readMediaBytes(media *types.MediaContent) ([]byte, error) {
	if media.URL != nil || media.StorageReference != nil {
		return nil, errors.New(
			"audio_emotion needs inline base64 data or a local file path; " +
				"URL and storage-reference sources are not yet supported")
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
// scored output, applies the threshold, and assembles the result. The
// Value/Details payload keeps every score the model returned so debug
// reports can show why a 0.4-confidence "angry" missed a 0.6 threshold.
func gradeAudioEmotion(
	handlerType string, cfg *audioEmotionConfig, scores []classify.LabelScore,
) *evals.EvalResult {
	var foundScore float64
	var foundLabel string
	for _, s := range scores {
		if strings.EqualFold(s.Label, cfg.expectedLabel) {
			foundScore = s.Score
			foundLabel = s.Label
			break
		}
	}
	if foundLabel == "" {
		allLabels := strings.Join(labelsFromScores(scores), ", ")
		return &evals.EvalResult{
			Type:  handlerType,
			Score: boolScore(false),
			Value: map[string]any{
				audioEmotionKeyExpectedLabel: cfg.expectedLabel,
				audioEmotionKeyMinScore:      cfg.minScore,
				keyFound:                     false,
			},
			Explanation: fmt.Sprintf("label %q not returned by model; got: %s", cfg.expectedLabel, allLabels),
			Details:     map[string]any{audioEmotionKeyScores: scores},
		}
	}
	passed := foundScore >= cfg.minScore
	scoreCopy := foundScore
	return &evals.EvalResult{
		Type:        handlerType,
		Score:       &scoreCopy,
		MetricValue: &scoreCopy,
		Value: map[string]any{
			audioEmotionKeyExpectedLabel: cfg.expectedLabel,
			audioEmotionKeyMinScore:      cfg.minScore,
			"actual_score":               foundScore,
			"passed":                     passed,
		},
		Explanation: fmt.Sprintf("%s score %.3f (threshold %.3f)", foundLabel, foundScore, cfg.minScore),
		Details:     map[string]any{audioEmotionKeyScores: scores},
	}
}

func labelsFromScores(scores []classify.LabelScore) []string {
	out := make([]string, len(scores))
	for i, s := range scores {
		out[i] = s.Label
	}
	return out
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
