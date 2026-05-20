package handlers

import (
	"errors"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
)

// Shared infrastructure for every classify-backed eval handler
// (audio_emotion, text_toxicity, text_sentiment, … and the image /
// video handlers queued behind #1228 / #1229). All of them take the
// same shape of params — model + expected_label + min_score +
// message_role + message_index + classifier_id — and the same
// label-lookup logic, so factoring once keeps the per-handler files
// down to "wire up the right task interface and call the registry".

// Param-map keys reused across handler Value / Details maps and
// during param parsing. Hoisted so goconst stays happy and so a
// future schema rename touches one place.
const (
	classifyKeyExpectedLabel = "expected_label"
	classifyKeyMinScore      = "min_score"
	classifyKeyMaxScore      = "max_score"
	classifyKeyScores        = "scores"
	classifyKeyActualScore   = "actual_score"
	classifyKeyPassed        = "passed"

	classifyDefaultMinScore = 0.5
)

// classifyConfig is the validated, shared shape consumed by every
// classify-backed handler. Per-handler configs embed this and add
// their own deltas (e.g. text_toxicity's max_score upper-bound mode).
type classifyConfig struct {
	model         string
	expectedLabel string
	minScore      float64
	messageRole   string
	messageIndex  int
	classifierID  string
}

// parseClassifyConfig validates and extracts the common param set.
// The defaultRole knob lets audio-shaped handlers default to "user"
// (scoring the caller's speech) and text-shaped handlers default to
// "assistant" (scoring the model's output) without each handler
// re-doing the YAML map dance.
func parseClassifyConfig(params map[string]any, defaultRole string) (classifyConfig, error) {
	cfg := classifyConfig{
		minScore:     classifyDefaultMinScore,
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

	if v, ok := extractFloat64(params, classifyKeyMinScore); ok {
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

// findExpectedLabel walks the classifier's scored output looking for
// the configured expected_label (case-insensitive). foundLabel is empty
// when the model didn't emit that label at all (a distinct outcome
// from "found but below threshold").
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
