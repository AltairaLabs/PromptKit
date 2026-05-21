package handlers

import (
	"errors"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
)

// Shared infrastructure for every classify-backed eval handler
// (audio_emotion, text_toxicity, text_sentiment, … and the image /
// video handlers queued behind #1228 / #1229).
//
// Convention — classify-backed handlers are PURE EVAL PRIMITIVES:
// they call the configured TextClassifier / AudioClassifier / ...,
// pick the score for the expected label, and emit it as
// EvalResult.Score. They do NOT apply min_score / max_score
// thresholds — that is the job of the `type: assertion` wrapper
// (see runtime/evals/wrappers.go) which composes the eval with
// pass/fail judgment. Threshold logic on the eval handler itself
// is wrong: it conflates the eval and assertion roles and stops
// the same handler from being declared in a pack's `evals:` block
// (where it should emit a metric, not a verdict).
//
// See runtime/evals/handlers/CLAUDE.md for the full convention.

// Param-map keys reused across handler Value / Details maps and
// during param parsing. Hoisted so goconst stays happy and so a
// future schema rename touches one place.
const (
	classifyKeyExpectedLabel = "expected_label"
	classifyKeyActualScore   = "actual_score"
	classifyKeyScores        = "scores"
)

// classifyConfig is the validated, shared shape consumed by every
// classify-backed handler. Per-handler configs embed this and add
// their own deltas (today: none; reserved for future handler-
// specific params like sample_rate on audio).
type classifyConfig struct {
	model         string
	expectedLabel string
	messageRole   string
	messageIndex  int
	classifierID  string
}

// parseClassifyConfig validates and extracts the common param set.
// The defaultRole knob lets audio-shaped handlers default to "user"
// (scoring the caller's speech) and text-shaped handlers default to
// "assistant" (scoring the model's output) without each handler
// re-doing the YAML map dance.
//
// Threshold params (min_score / max_score) are deliberately NOT
// parsed here — see the package convention note above.
func parseClassifyConfig(params map[string]any, defaultRole string) (classifyConfig, error) {
	cfg := classifyConfig{
		messageRole:  defaultRole,
		messageIndex: -1,
	}
	model, _ := params["model"].(string)
	if model == "" {
		return cfg, errors.New("model is required")
	}
	cfg.model = model

	expected, _ := params[classifyKeyExpectedLabel].(string)
	if expected == "" {
		return cfg, errors.New("expected_label is required")
	}
	cfg.expectedLabel = expected

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

	if msg := rejectThresholdParams(params); msg != "" {
		return cfg, errors.New(msg)
	}
	return cfg, nil
}

// findExpectedLabel walks the classifier's scored output looking for
// the configured expected_label (case-insensitive). foundLabel is empty
// when the model didn't emit that label at all (a distinct outcome
// from "found but low score").
func findExpectedLabel(
	scores []classify.LabelScore, expectedLabel string,
) (foundScore float64, foundLabel string) {
	for _, s := range scores {
		if strings.EqualFold(s.Label, expectedLabel) {
			return s.Score, s.Label
		}
	}
	return 0, ""
}

// labelsFromScores returns the label names from a scored output. Used
// in error messages so handlers can tell users "you asked for X; the
// model returned [a, b, c]".
func labelsFromScores(scores []classify.LabelScore) []string {
	out := make([]string, len(scores))
	for i, s := range scores {
		out[i] = s.Label
	}
	return out
}
