package handlers

import "github.com/AltairaLabs/PromptKit/runtime/v2/evals"

// Classification groups the built-in handlers declare through
// evals.GroupDeclarer. Every handler that calls a judge model, a classifier or
// another service is long-running and external, so a host that runs only
// fast-running evals inline never blocks a turn on a model call.

// modelBackedGroups returns the groups of a handler that calls a judge model,
// a classifier or a remote service.
func modelBackedGroups() []string {
	return []string{evals.GroupLongRunning, evals.GroupExternal}
}

// localLongRunningGroups returns the groups of a handler that does expensive
// work without leaving the process.
func localLongRunningGroups() []string {
	return []string{evals.GroupLongRunning}
}

// LLM judge handlers.

// EvalGroups implements evals.GroupDeclarer.
func (h *LLMJudgeHandler) EvalGroups() []string { return modelBackedGroups() }

// EvalGroups implements evals.GroupDeclarer.
func (h *LLMJudgeSessionHandler) EvalGroups() []string { return modelBackedGroups() }

// EvalGroups implements evals.GroupDeclarer.
func (h *LLMJudgeToolCallsHandler) EvalGroups() []string { return modelBackedGroups() }

// RAG-shape judge handlers.

// EvalGroups implements evals.GroupDeclarer.
func (h *FaithfulnessHandler) EvalGroups() []string { return modelBackedGroups() }

// EvalGroups implements evals.GroupDeclarer.
func (h *AnswerRelevancyHandler) EvalGroups() []string { return modelBackedGroups() }

// EvalGroups implements evals.GroupDeclarer.
func (h *ContextualPrecisionHandler) EvalGroups() []string { return modelBackedGroups() }

// EvalGroups implements evals.GroupDeclarer.
func (h *ContextualRecallHandler) EvalGroups() []string { return modelBackedGroups() }

// EvalGroups implements evals.GroupDeclarer.
func (h *ContextualRelevancyHandler) EvalGroups() []string { return modelBackedGroups() }

// EvalGroups implements evals.GroupDeclarer.
func (h *HallucinationHandler) EvalGroups() []string { return modelBackedGroups() }

// Safety judge handlers.

// EvalGroups implements evals.GroupDeclarer.
func (h *BiasHandler) EvalGroups() []string { return modelBackedGroups() }

// EvalGroups implements evals.GroupDeclarer.
func (h *ToxicityHandler) EvalGroups() []string { return modelBackedGroups() }

// EvalGroups implements evals.GroupDeclarer. pii_leakage tries a regex
// pre-pass first, but falls through to the judge on a miss.
func (h *PIILeakageHandler) EvalGroups() []string { return modelBackedGroups() }

// EvalGroups implements evals.GroupDeclarer.
func (h *RoleViolationHandler) EvalGroups() []string { return modelBackedGroups() }

// Classifier handlers.

// EvalGroups implements evals.GroupDeclarer.
func (h *AudioEmotionHandler) EvalGroups() []string { return modelBackedGroups() }

// EvalGroups implements evals.GroupDeclarer.
func (h *ImageModerationHandler) EvalGroups() []string { return modelBackedGroups() }

// EvalGroups implements evals.GroupDeclarer.
func (h *TextToxicityHandler) EvalGroups() []string { return modelBackedGroups() }

// EvalGroups implements evals.GroupDeclarer.
func (h *TextSentimentHandler) EvalGroups() []string { return modelBackedGroups() }

// EvalGroups implements evals.GroupDeclarer.
func (h *TopicPolicyHandler) EvalGroups() []string { return modelBackedGroups() }

// Remote-service handlers.

// EvalGroups implements evals.GroupDeclarer.
func (h *RestEvalHandler) EvalGroups() []string { return modelBackedGroups() }

// EvalGroups implements evals.GroupDeclarer.
func (h *RestEvalSessionHandler) EvalGroups() []string { return modelBackedGroups() }

// EvalGroups implements evals.GroupDeclarer.
func (h *A2AEvalHandler) EvalGroups() []string { return modelBackedGroups() }

// EvalGroups implements evals.GroupDeclarer.
func (h *A2AEvalSessionHandler) EvalGroups() []string { return modelBackedGroups() }

// In-process long-running handlers.

// EvalGroups implements evals.GroupDeclarer.
func (h *CosineSimilarityHandler) EvalGroups() []string { return localLongRunningGroups() }

// EvalGroups implements evals.GroupDeclarer.
func (h *OutcomeEquivalentHandler) EvalGroups() []string { return localLongRunningGroups() }
