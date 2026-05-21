package handlers

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ContextualPrecisionHandler scores the fraction of retrieved chunks
// that are actually relevant to the question. Equivalent in name to
// DeepEval `contextual_precision`.
//
// Default prompts adapted from the public DeepEval reference
// implementation (Apache 2.0). Override per-call by passing
// system_prompt or criteria in params.
//
// Params (all optional):
//   - contexts ([]string) | context (string) | context_field (string):
//     retrieved chunks
//   - question (string): override the auto-extracted last user turn
//   - rubric, model, system_prompt: standard llm_judge knobs
type ContextualPrecisionHandler struct{}

// Type returns the eval type identifier.
func (h *ContextualPrecisionHandler) Type() string { return "contextual_precision" }

// Eval scores the precision of retrieved chunks relative to the question.
func (h *ContextualPrecisionHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	return evalChunkContext(ctx, evalCtx, h.Type(), params, chunkContextSpec{
		otherLabel:   chunkLabelQuestion,
		otherValueFn: func(in ragInputs) string { return in.question },
		otherMissing: ragNoQuestionMessage,
		systemPrompt: contextualPrecisionSystemPrompt,
		criteria:     contextualPrecisionCriteria,
	})
}

const contextualPrecisionSystemPrompt = "You are a retrieval-precision evaluator. " +
	"You will be shown a QUESTION and a numbered list of RETRIEVED CHUNKS. " +
	"For each chunk, decide whether it is relevant to answering the question. " +
	"Compute precision = (relevant chunks) / (total chunks)." +
	"\n\n" +
	"Score on [0, 1]: " +
	"1.0 — every retrieved chunk is relevant. " +
	"0.0 — none of the retrieved chunks are relevant. " +
	"\n\n" +
	"Respond with JSON: {\"passed\": bool, \"score\": float in [0,1], " +
	"\"reasoning\": string}. " +
	"In your reasoning, mark each chunk as relevant or irrelevant and explain why." +
	"\n\n" +
	"(Prompt adapted from DeepEval reference implementation, Apache 2.0.)"

const contextualPrecisionCriteria = "Compute the precision of the RETRIEVED CHUNKS relative to the QUESTION."
