package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Shared infrastructure for classify-backed text handlers (text_toxicity,
// text_sentiment). Parallels audio_emotion.go's helpers; kept separate so
// the two task interfaces (AudioClassifier vs TextClassifier) don't bleed
// into each other and so adding text_emotion / text_intent later doesn't
// need to refactor the audio path.

const (
	textClassifyDefaultMinScore = 0.5
	textClassifyDefaultRole     = "assistant"

	// Keys reused across handler Value / Details maps. Hoisted so goconst
	// stays happy and so a future schema rename (e.g. "expected_label" →
	// "label") only touches one place.
	textClassifyKeyExpectedLabel = "expected_label"
	textClassifyKeyMinScore      = "min_score"
	textClassifyKeyMaxScore      = "max_score"
	textClassifyKeyScores        = "scores"
	textClassifyKeyActualScore   = "actual_score"
	textClassifyKeyPassed        = "passed"
)

// textClassifyConfig is the parsed form of the user-supplied params.
// Both text handlers share the same param schema; max_score is only
// consulted by callers that opted into the upper-bound mode.
type textClassifyConfig struct {
	model         string
	expectedLabel string
	minScore      float64
	maxScore      float64
	hasMaxScore   bool
	messageRole   string
	messageIndex  int
	classifierID  string
}

// parseTextClassifyParams validates and extracts the shared param shape.
// allowMaxScore gates whether `max_score` is even read — sentiment-style
// handlers reject it to keep param semantics legible. Toxicity-style
// handlers permit it (and forbid combining with min_score, which would
// be ambiguous).
func parseTextClassifyParams(params map[string]any, allowMaxScore bool) (textClassifyConfig, error) {
	cfg := textClassifyConfig{
		minScore:     textClassifyDefaultMinScore,
		messageRole:  textClassifyDefaultRole,
		messageIndex: -1,
	}
	model, _ := params["model"].(string)
	if model == "" {
		return cfg, errors.New("model is required (e.g. unitary/toxic-bert)")
	}
	cfg.model = model

	expected, _ := params[textClassifyKeyExpectedLabel].(string)
	if expected == "" {
		return cfg, errors.New("expected_label is required")
	}
	cfg.expectedLabel = expected

	_, minProvided := extractFloat64(params, textClassifyKeyMinScore)
	_, maxProvided := extractFloat64(params, textClassifyKeyMaxScore)
	if maxProvided && !allowMaxScore {
		return cfg, errors.New("max_score is not supported for this handler; use min_score")
	}
	if minProvided && maxProvided {
		return cfg, errors.New("min_score and max_score are mutually exclusive")
	}
	if v, ok := extractFloat64(params, textClassifyKeyMinScore); ok {
		cfg.minScore = v
	}
	if v, ok := extractFloat64(params, textClassifyKeyMaxScore); ok {
		cfg.maxScore = v
		cfg.hasMaxScore = true
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

// resolveTextClassifier pulls the classify.Registry out of context and
// looks up the requested classifier. An empty id resolves the configured
// default. Returning an error here surfaces as a Skipped result at the
// handler — the keyless-CI path passes cleanly.
func resolveTextClassifier(ctx context.Context, id string) (classify.TextClassifier, error) {
	reg := classify.FromContext(ctx)
	if reg == nil {
		return nil, errors.New(
			"no classify registry configured; add a providers: entry with role: inference " +
				"and either defaults.inference.text_classifier or params.classifier_id")
	}
	return reg.TextClassifier(id)
}

// collectTextsByRole returns the text content for every message whose
// Role matches the chosen role. Each returned entry is the concatenation
// of Message.Content and every ContentTypeText part on that message
// (joined by single spaces) — covering both legacy single-string
// messages and modern multi-part messages.
//
// Messages with no extractable text contribute an empty string at their
// position; callers filter those out (or address them by index) as
// appropriate.
func collectTextsByRole(messages []types.Message, role string) []string {
	var out []string
	for i := range messages {
		if messages[i].Role != role {
			continue
		}
		text := extractTextFromMessage(&messages[i])
		out = append(out, text)
	}
	return out
}

// extractTextFromMessage joins Message.Content with any ContentTypeText
// parts. Multi-part messages can carry the same text in either field
// depending on how the provider built the message; merging both is the
// only way to be sure we don't miss content.
func extractTextFromMessage(msg *types.Message) string {
	parts := make([]string, 0, len(msg.Parts)+1)
	if msg.Content != "" {
		parts = append(parts, msg.Content)
	}
	for j := range msg.Parts {
		p := &msg.Parts[j]
		if p.Type == types.ContentTypeText && p.Text != nil && *p.Text != "" {
			parts = append(parts, *p.Text)
		}
	}
	return strings.Join(parts, " ")
}

// pickText selects one entry from a non-empty slice. A negative index
// picks the most recent (-1 → last). Caller handles the empty-slice
// case so absence-vs-out-of-range surfaces at the right level.
func pickText(texts []string, index int) (string, error) {
	if index < 0 {
		return texts[len(texts)-1], nil
	}
	if index >= len(texts) {
		return "", fmt.Errorf("message_index %d out of range (found %d text messages)",
			index, len(texts))
	}
	return texts[index], nil
}

// gradeTextClassify looks up the expected label in the classifier's
// scored output and applies the configured threshold. With hasMaxScore
// set the assertion passes when score < maxScore (upper-bound mode for
// safety assertions); otherwise the standard score >= minScore (audio
// emotion's lower-bound mode).
func gradeTextClassify(
	handlerType string, cfg *textClassifyConfig, scores []classify.LabelScore,
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
				textClassifyKeyExpectedLabel: cfg.expectedLabel,
				keyFound:                     false,
			},
			Explanation: fmt.Sprintf("label %q not returned by model; got: %s",
				cfg.expectedLabel, allLabels),
			Details: map[string]any{textClassifyKeyScores: scores},
		}
	}

	var (
		passed      bool
		threshold   float64
		comparison  string
		thresholdKy string
	)
	if cfg.hasMaxScore {
		passed = foundScore < cfg.maxScore
		threshold = cfg.maxScore
		comparison = "<"
		thresholdKy = textClassifyKeyMaxScore
	} else {
		passed = foundScore >= cfg.minScore
		threshold = cfg.minScore
		comparison = ">="
		thresholdKy = textClassifyKeyMinScore
	}

	scoreCopy := foundScore
	return &evals.EvalResult{
		Type:        handlerType,
		Score:       &scoreCopy,
		MetricValue: &scoreCopy,
		Value: map[string]any{
			textClassifyKeyExpectedLabel: cfg.expectedLabel,
			thresholdKy:                  threshold,
			textClassifyKeyActualScore:   foundScore,
			textClassifyKeyPassed:        passed,
		},
		Explanation: fmt.Sprintf("%s score %.3f (threshold %s %.3f)",
			foundLabel, foundScore, comparison, threshold),
		Details: map[string]any{textClassifyKeyScores: scores},
	}
}

// runTextClassifyEval is the shared body for both text handlers. It
// resolves the classifier, picks the target text, calls the backend,
// and grades. Splitting into a helper keeps the per-handler files to
// thin wrappers that just nominate the eval type and param mode.
func runTextClassifyEval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	handlerType string,
	params map[string]any,
	allowMaxScore bool,
) *evals.EvalResult {
	cfg, cfgErr := parseTextClassifyParams(params, allowMaxScore)
	if cfgErr != nil {
		return errorResult(handlerType, cfgErr.Error())
	}

	classifier, classifierErr := resolveTextClassifier(ctx, cfg.classifierID)
	if classifierErr != nil {
		return skippedResult(handlerType, classifierErr.Error())
	}

	texts := collectTextsByRole(evalCtx.Messages, cfg.messageRole)
	// Filter out empty strings — a message can be in the right role but
	// carry only media (audio, image) with no text part.
	nonEmpty := texts[:0]
	for _, t := range texts {
		if strings.TrimSpace(t) != "" {
			nonEmpty = append(nonEmpty, t)
		}
	}
	if len(nonEmpty) == 0 {
		return skippedResult(handlerType,
			fmt.Sprintf("no text message found with role %q", cfg.messageRole))
	}
	text, pickErr := pickText(nonEmpty, cfg.messageIndex)
	if pickErr != nil {
		return errorResult(handlerType, pickErr.Error())
	}

	opts := classify.TextOptions{
		Model:      cfg.model,
		MultiLabel: true, // return scores for every label so we can grade any expected_label
	}
	scores, classifyErr := classifier.ClassifyText(ctx, text, opts)
	if classifyErr != nil {
		return errorResult(handlerType, fmt.Sprintf("classify failed: %v", classifyErr))
	}

	return gradeTextClassify(handlerType, &cfg, scores)
}
