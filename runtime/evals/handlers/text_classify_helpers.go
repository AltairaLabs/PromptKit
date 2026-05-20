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
// Pure eval primitives — see the convention note in
// classify_handler_base.go: threshold judgment lives on the
// `type: assertion` wrapper, not on these handlers.

// textClassifyDefaultRole defaults text scoring to the assistant's
// output. Toxicity and sentiment assertions are almost always about
// what the model said, not what the user said.
const textClassifyDefaultRole = "assistant"

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

// gradeTextClassify looks up the expected_label in the classifier's
// scored output and emits its score. Pure eval primitive: no
// threshold judgment — that lives on the `type: assertion` wrapper.
//
// "label not returned by model" emits Score = 0 with a louder
// Explanation; downstream wrapper-driven thresholds compute the right
// pass/fail outcome from the zero.
func gradeTextClassify(
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
			Explanation: fmt.Sprintf("label %q not returned by model; got: %s",
				cfg.expectedLabel, allLabels),
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

// runTextClassifyEval is the shared body for both text handlers. It
// resolves the classifier, picks the target text, calls the backend,
// and emits the score. Splits into a helper so per-handler files stay
// thin wrappers that just nominate the eval type.
func runTextClassifyEval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	handlerType string,
	params map[string]any,
) *evals.EvalResult {
	cfg, cfgErr := parseClassifyConfig(params, textClassifyDefaultRole)
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
		MultiLabel: true, // request scores for every label so any expected_label can be looked up
	}
	scores, classifyErr := classifier.ClassifyText(ctx, text, opts)
	if classifyErr != nil {
		return errorResult(handlerType, fmt.Sprintf("classify failed: %v", classifyErr))
	}

	return gradeTextClassify(handlerType, &cfg, scores)
}
