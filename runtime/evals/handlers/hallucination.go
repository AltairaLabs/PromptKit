package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// HallucinationHandler scores how free the answer is of claims that
// are not supported by, or contradict, the provided context. The
// inverse framing of `faithfulness` — kept as a separate handler so
// users coming from DeepEval find the vocabulary they expect.
//
// A score of 1.0 means no hallucination (matches faithfulness=1.0); a
// score of 0.0 means entirely hallucinated.
//
// Default prompts adapted from the public DeepEval reference
// implementation (Apache 2.0). Override per-call by passing
// system_prompt or criteria in params.
//
// Params (all optional):
//   - contexts ([]string) | context (string) | context_field (string)
//   - min_score (float): pass threshold
//   - rubric, model, system_prompt: standard llm_judge knobs
type HallucinationHandler struct{}

// Type returns the eval type identifier.
func (h *HallucinationHandler) Type() string { return "hallucination" }

// Eval scores the current assistant output for hallucinations relative
// to the supplied context.
func (h *HallucinationHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	in := buildRAGInputs(evalCtx, params)
	if len(in.contexts) == 0 {
		return ragErrorResult(h.Type(), ragNoContextMessage), nil
	}

	content := fmt.Sprintf(
		"CONTEXT:\n%s\n\nANSWER:\n%s",
		formatContexts(in.contexts), in.answer,
	)
	return ragJudgeCall(
		ctx, evalCtx, h.Type(), params, content,
		hallucinationSystemPrompt, hallucinationCriteria,
	)
}

const hallucinationSystemPrompt = "You are a hallucination detector. " +
	"You will be shown a CONTEXT and an ANSWER. " +
	"Identify claims in the answer that are not supported by, or contradict, the context. " +
	"\n\n" +
	"Score on [0, 1]: " +
	"1.0 — no hallucinated claims (every claim is grounded in the context). " +
	"0.0 — every claim is hallucinated. " +
	"Partial credit for answers where some claims are grounded and others are not. " +
	"\n\n" +
	"Respond with JSON: {\"passed\": bool, \"score\": float in [0,1], " +
	"\"reasoning\": string}. " +
	"In your reasoning, list each hallucinated or contradicting claim." +
	"\n\n" +
	"(Prompt adapted from DeepEval reference implementation, Apache 2.0.)"

const hallucinationCriteria = "Score the ANSWER for absence of hallucination relative to the CONTEXT " +
	"(1.0 = no hallucination)."
