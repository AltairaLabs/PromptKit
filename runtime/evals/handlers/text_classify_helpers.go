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

// Text-specific infrastructure on top of classify_handler_base.go.
// The shared base owns the model + expected_label + min_score +
// message_role + message_index + classifier_id parsing and the label
// lookup; this file adds the text-specific deltas: max_score
// upper-bound mode (for toxicity-style "stay below X" framings) and
// text extraction from the message log.

// textClassifyDefaultRole defaults text scoring to the assistant's
// output. Toxicity and sentiment assertions are almost always about
// what the model said, not what the user said.
const textClassifyDefaultRole = "assistant"

// textClassifyConfig extends the shared classifyConfig with the
// max_score upper-bound mode. Only text_toxicity opts into it; sentiment
// keeps the inherited min_score-only shape.
type textClassifyConfig struct {
	classifyConfig
	maxScore    float64
	hasMaxScore bool
}

// parseTextClassifyParams runs the shared classify-config parser, then
// reads the text-specific max_score knob. allowMaxScore gates whether
// max_score is even read — sentiment-style handlers reject it to keep
// the param surface unambiguous. Combining min_score with max_score is
// also rejected.
func parseTextClassifyParams(params map[string]any, allowMaxScore bool) (textClassifyConfig, error) {
	base, err := parseClassifyConfig(params, textClassifyDefaultRole)
	if err != nil {
		return textClassifyConfig{}, err
	}
	cfg := textClassifyConfig{classifyConfig: base}

	_, minProvided := extractFloat64(params, classifyKeyMinScore)
	_, maxProvided := extractFloat64(params, classifyKeyMaxScore)
	if maxProvided && !allowMaxScore {
		return cfg, errors.New("max_score is not supported for this handler; use min_score")
	}
	if minProvided && maxProvided {
		return cfg, errors.New("min_score and max_score are mutually exclusive")
	}
	if v, ok := extractFloat64(params, classifyKeyMaxScore); ok {
		cfg.maxScore = v
		cfg.hasMaxScore = true
	}
	return cfg, nil
}

// resolveTextClassifier pulls the classify.Registry out of context and
// looks up the requested classifier. An empty id resolves the
// configured default. The error returned here surfaces as Skipped at
// the handler so keyless-CI paths stay clean.
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
// Role matches the chosen role. Each returned entry is Message.Content
// concatenated with every ContentTypeText part on that message
// (separated by single spaces) — covering both legacy single-string
// messages and modern multi-part messages.
func collectTextsByRole(messages []types.Message, role string) []string {
	var out []string
	for i := range messages {
		if messages[i].Role != role {
			continue
		}
		out = append(out, extractTextFromMessage(&messages[i]))
	}
	return out
}

// extractTextFromMessage joins Message.Content with any ContentTypeText
// parts. Multi-part messages can carry text in either field depending
// on how the provider built the message; merging both is the only way
// to be sure we don't miss content.
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
// set the assertion passes when score < maxScore (upper-bound mode);
// otherwise it passes when score >= minScore (the standard mode).
func gradeTextClassify(
	handlerType string, cfg *textClassifyConfig, scores []classify.LabelScore,
) *evals.EvalResult {
	foundScore, foundLabel := findExpectedLabel(scores, cfg.expectedLabel)
	if foundLabel == "" {
		allLabels := strings.Join(labelsFromScores(scores), ", ")
		return &evals.EvalResult{
			Type:  handlerType,
			Score: boolScore(false),
			Value: map[string]any{
				classifyKeyExpectedLabel: cfg.expectedLabel,
				keyFound:                 false,
			},
			Explanation: fmt.Sprintf("label %q not returned by model; got: %s",
				cfg.expectedLabel, allLabels),
			Details: map[string]any{classifyKeyScores: scores},
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
		thresholdKy = classifyKeyMaxScore
	} else {
		passed = foundScore >= cfg.minScore
		threshold = cfg.minScore
		comparison = ">="
		thresholdKy = classifyKeyMinScore
	}

	scoreCopy := foundScore
	return &evals.EvalResult{
		Type:        handlerType,
		Score:       &scoreCopy,
		MetricValue: &scoreCopy,
		Value: map[string]any{
			classifyKeyExpectedLabel: cfg.expectedLabel,
			thresholdKy:              threshold,
			classifyKeyActualScore:   foundScore,
			classifyKeyPassed:        passed,
		},
		Explanation: fmt.Sprintf("%s score %.3f (threshold %s %.3f)",
			foundLabel, foundScore, comparison, threshold),
		Details: map[string]any{classifyKeyScores: scores},
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
	// Drop messages that match the role but carry no extractable text
	// (e.g. audio-only parts) — text classification has nothing to
	// score for them.
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
		MultiLabel: true, // request scores for every label so we can grade any expected_label
	}
	scores, classifyErr := classifier.ClassifyText(ctx, text, opts)
	if classifyErr != nil {
		return errorResult(handlerType, fmt.Sprintf("classify failed: %v", classifyErr))
	}

	return gradeTextClassify(handlerType, &cfg, scores)
}
