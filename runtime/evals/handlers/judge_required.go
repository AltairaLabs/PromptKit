package handlers

// JudgeRequiring marks a handler that cannot take its measurement without an
// LLM judge. It exists so a CALLER can tell, before running anything, that a
// declared check has no way to work — which is the whole of #1996.
//
// The check had two silent failure modes, in opposite directions, and both came
// from asking at eval time instead of at wiring time:
//
//   - the judge-call helpers return a 0.0 score when no judge is in the
//     metadata, and a guardrail's default floor is 1.0, so a `toxicity`
//     guardrail blocked EVERY turn and reported a content violation as the
//     reason;
//   - pii_leakage deliberately degrades open without a judge, so as a guardrail
//     its LLM layer silently never ran and the regex pre-pass was the only
//     thing enforcing.
//
// Degrading open is right for an offline eval and wrong for a guardrail, whose
// entire job is to block. Rather than encode that policy per handler, each
// handler states a fact — "I need a judge" — and each caller applies the policy
// its role calls for. `guardrails.CompileValidators` refuses to build one
// without a judge; the eval path is free to run it and report a skip.
type JudgeRequiring interface {
	// RequiresJudge reports whether this handler needs an LLM judge supplied
	// via eval-context metadata.
	RequiresJudge() bool
}

// ClassifierRequiring marks a handler that needs a classify backend — the same
// statement of fact as [JudgeRequiring], for the other family of ancillary
// provider. Callers use the two together to know WHICH kind of provider a
// check's named key has to resolve to, so binding the wrong kind is caught
// before the first turn rather than surfacing as a missing measurement.
type ClassifierRequiring interface {
	// RequiresClassifier reports whether this handler needs a classify backend.
	RequiresClassifier() bool
}

// RequiresClassifier reports whether a handler needs a classify backend.
func RequiresClassifier(h any) bool {
	cr, ok := h.(ClassifierRequiring)
	return ok && cr.RequiresClassifier()
}

// RequiresJudge reports whether a handler needs an LLM judge. A handler that
// does not implement [JudgeRequiring] is assumed not to need one, which is the
// safe default: a wrong "no" costs nothing at wiring time, while a wrong "yes"
// would refuse to build a check that works.
func RequiresJudge(h any) bool {
	jr, ok := h.(JudgeRequiring)
	return ok && jr.RequiresJudge()
}

// RequiresJudge reports true: the judge-backed family below all reach
// extractJudgeProvider, directly or through the rag/safety helpers, and take no
// measurement at all without one.
func (h *LLMJudgeHandler) RequiresJudge() bool { return true }

// RequiresJudge reports true. See [LLMJudgeHandler.RequiresJudge].
func (h *LLMJudgeSessionHandler) RequiresJudge() bool { return true }

// RequiresJudge reports true. See [LLMJudgeHandler.RequiresJudge].
func (h *LLMJudgeToolCallsHandler) RequiresJudge() bool { return true }

// RequiresJudge reports true. See [LLMJudgeHandler.RequiresJudge].
func (h *BiasHandler) RequiresJudge() bool { return true }

// RequiresJudge reports true. See [LLMJudgeHandler.RequiresJudge].
func (h *ToxicityHandler) RequiresJudge() bool { return true }

// RequiresJudge reports true. See [LLMJudgeHandler.RequiresJudge].
func (h *RoleViolationHandler) RequiresJudge() bool { return true }

// RequiresJudge reports true. See [LLMJudgeHandler.RequiresJudge].
func (h *FaithfulnessHandler) RequiresJudge() bool { return true }

// RequiresJudge reports true. See [LLMJudgeHandler.RequiresJudge].
func (h *HallucinationHandler) RequiresJudge() bool { return true }

// RequiresJudge reports true. See [LLMJudgeHandler.RequiresJudge].
func (h *AnswerRelevancyHandler) RequiresJudge() bool { return true }

// RequiresJudge reports true. See [LLMJudgeHandler.RequiresJudge].
func (h *ContextualPrecisionHandler) RequiresJudge() bool { return true }

// RequiresJudge reports true. See [LLMJudgeHandler.RequiresJudge].
func (h *ContextualRecallHandler) RequiresJudge() bool { return true }

// RequiresJudge reports true. See [LLMJudgeHandler.RequiresJudge].
func (h *ContextualRelevancyHandler) RequiresJudge() bool { return true }

// RequiresJudge reports true. PIILeakageHandler runs a regex pre-pass that
// works without a judge, but its LLM layer is the part that catches what the
// patterns miss. Reporting true keeps a pii_leakage GUARDRAIL from being built
// half-armed; as an eval it still degrades open, because the two roles want
// different answers to the same fact.
func (h *PIILeakageHandler) RequiresJudge() bool { return true }

// RequiresClassifier reports true: the classify-backed family below all resolve
// a classify backend, and measure nothing without one.
func (h *AudioEmotionHandler) RequiresClassifier() bool { return true }

// RequiresClassifier reports true. See [AudioEmotionHandler.RequiresClassifier].
func (h *ImageModerationHandler) RequiresClassifier() bool { return true }

// RequiresClassifier reports true. See [AudioEmotionHandler.RequiresClassifier].
func (h *TextSentimentHandler) RequiresClassifier() bool { return true }

// RequiresClassifier reports true. See [AudioEmotionHandler.RequiresClassifier].
func (h *TextToxicityHandler) RequiresClassifier() bool { return true }

// RequiresClassifier reports true. See [AudioEmotionHandler.RequiresClassifier].
func (h *TopicPolicyHandler) RequiresClassifier() bool { return true }
