package handlers

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ContextualRelevancyHandler scores the average per-chunk relevance of
// the retrieved chunks to the question. Equivalent in name to DeepEval
// `contextual_relevancy`.
//
// Distinct from `contextual_precision`: precision is a binary
// relevant/not-relevant ratio; relevancy is the mean of graded scores.
//
// Default prompts adapted from the public DeepEval reference
// implementation (Apache 2.0). Override per-call by passing
// system_prompt or criteria in params.
//
// Params (all optional):
//   - contexts ([]string) | context (string) | context_field (string)
//   - question (string)
//   - rubric, model, system_prompt
//
// Putting min_score / max_score on this handler is rejected — wrap
// with `type: assertion` and set the threshold on the wrapper.
type ContextualRelevancyHandler struct{}

// Type returns the eval type identifier.
func (h *ContextualRelevancyHandler) Type() string { return "contextual_relevancy" }

// Eval scores the mean relevance of retrieved chunks to the question.
func (h *ContextualRelevancyHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	return evalChunkContext(ctx, evalCtx, h.Type(), params, chunkContextSpec{
		otherLabel:   chunkLabelQuestion,
		otherValueFn: func(in ragInputs) string { return in.question },
		otherMissing: ragNoQuestionMessage,
		systemPrompt: contextualRelevancySystemPrompt,
		criteria:     contextualRelevancyCriteria,
	})
}

const contextualRelevancySystemPrompt = "You are a retrieval-relevancy evaluator. " +
	"You will be shown a QUESTION and a numbered list of RETRIEVED CHUNKS. " +
	"For each chunk, score its relevance to the question on [0, 1] " +
	"(1.0 = directly relevant, 0.0 = irrelevant, partial credit allowed). " +
	"Return the mean of the per-chunk scores." +
	"\n\n" +
	"Respond with JSON: {\"passed\": bool, \"score\": float in [0,1], " +
	"\"reasoning\": string}. " +
	"In your reasoning, include the per-chunk score for each chunk." +
	"\n\n" +
	"(Prompt adapted from DeepEval reference implementation, Apache 2.0.)"

const contextualRelevancyCriteria = "Score the mean per-chunk relevance of the RETRIEVED CHUNKS to the QUESTION."
