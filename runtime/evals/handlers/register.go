package handlers

import "github.com/AltairaLabs/PromptKit/runtime/evals"

//nolint:gochecknoinits // init registers handlers to avoid circular imports
func init() {
	// Turn-level deterministic handlers
	evals.RegisterDefault(&ContainsHandler{})
	evals.RegisterDefault(&RegexHandler{})
	evals.RegisterDefault(&JSONValidHandler{})
	evals.RegisterDefault(&JSONSchemaHandler{})
	evals.RegisterDefault(&ToolsCalledHandler{})
	evals.RegisterDefault(&ToolsNotCalledHandler{})
	evals.RegisterDefault(&ToolArgsHandler{})
	evals.RegisterDefault(&LatencyBudgetHandler{})
	evals.RegisterDefault(&CosineSimilarityHandler{})

	// Session-level deterministic handlers
	evals.RegisterDefault(&ContainsAnyHandler{})
	evals.RegisterDefault(&ContentExcludesHandler{})
	evals.RegisterDefault(&ToolsCalledSessionHandler{})
	evals.RegisterDefault(&ToolsNotCalledSessionHandler{})
	evals.RegisterDefault(&ToolArgsSessionHandler{})
	evals.RegisterDefault(&ToolArgsExcludedSessionHandler{})

	// Tool call handlers (Batch 1)
	evals.RegisterDefault(&NoToolErrorsHandler{})
	evals.RegisterDefault(&ToolCallCountHandler{})
	evals.RegisterDefault(&ToolResultIncludesHandler{})
	evals.RegisterDefault(&ToolResultMatchesHandler{})
	evals.RegisterDefault(&ToolResultHasMediaHandler{})
	evals.RegisterDefault(&ToolResultMediaTypeHandler{})
	evals.RegisterDefault(&ToolCallSequenceHandler{})
	evals.RegisterDefault(&ToolCallChainHandler{})
	evals.RegisterDefault(&ToolCallsWithArgsHandler{})

	// Tool pattern and efficiency handlers (Batch 1b)
	evals.RegisterDefault(&ToolAntiPatternHandler{})
	evals.RegisterDefault(&ToolNoRepeatHandler{})
	evals.RegisterDefault(&ToolEfficiencyHandler{})
	evals.RegisterDefault(&CostBudgetHandler{})

	// JSON path, agent, and guardrail handlers (Batch 2)
	evals.RegisterDefault(&JSONPathHandler{})
	evals.RegisterDefault(&AgentInvokedHandler{})
	evals.RegisterDefault(&AgentNotInvokedHandler{})
	evals.RegisterDefault(&AgentResponseContainsHandler{})
	evals.RegisterDefault(&GuardrailTriggeredHandler{})

	// Property invariant validators (Phase 3)
	evals.RegisterDefault(&InvariantFieldsPreservedHandler{})

	// Composition handlers (RFC 0010 — Arena testability)
	evals.RegisterDefault(&CompositionStepOutputHandler{})
	evals.RegisterDefault(&CompositionBranchTakenHandler{})
	evals.RegisterDefault(&CompositionParallelCompleteHandler{})
	evals.RegisterDefault(&CompositionOutputHandler{})

	// Workflow and skill handlers (Batch 3)
	evals.RegisterDefault(&WorkflowCompleteHandler{})
	evals.RegisterDefault(&WorkflowStateIsHandler{})
	evals.RegisterDefault(&WorkflowTransitionedToHandler{})
	evals.RegisterDefault(&WorkflowTransitionOrderHandler{})
	evals.RegisterDefault(&WorkflowToolAccessHandler{})
	evals.RegisterDefault(&SkillActivatedHandler{})
	evals.RegisterDefault(&SkillNotActivatedHandler{})
	evals.RegisterDefault(&SkillActivationOrderHandler{})

	// Media handlers (Batch 4)
	evals.RegisterDefault(&ImageFormatHandler{})
	evals.RegisterDefault(&ImageDimensionsHandler{})
	evals.RegisterDefault(&AudioFormatHandler{})
	evals.RegisterDefault(&AudioDurationHandler{})
	evals.RegisterDefault(&VideoDurationHandler{})
	evals.RegisterDefault(&VideoResolutionHandler{})

	// Classify-backed media handlers — score model output (audio emotion,
	// text toxicity, ...) using the classify.Registry from context.
	evals.RegisterDefault(&AudioEmotionHandler{})
	evals.RegisterDefault(&TextToxicityHandler{})
	evals.RegisterDefault(&TextSentimentHandler{})

	// LLM judge handlers
	evals.RegisterDefault(&LLMJudgeHandler{})
	evals.RegisterDefault(&LLMJudgeSessionHandler{})
	evals.RegisterDefault(&LLMJudgeToolCallsHandler{})

	// RAG-shape eval handlers — thin wrappers over llm_judge with
	// hardened default prompts. See rag_helpers.go.
	evals.RegisterDefault(&FaithfulnessHandler{})
	evals.RegisterDefault(&AnswerRelevancyHandler{})
	evals.RegisterDefault(&ContextualPrecisionHandler{})
	evals.RegisterDefault(&ContextualRecallHandler{})
	evals.RegisterDefault(&ContextualRelevancyHandler{})
	evals.RegisterDefault(&HallucinationHandler{})

	// Safety eval handlers — demo-default wiring is as guardrails via
	// pack `validators:` block; scenarios observe firings via
	// `guardrail_triggered`. Direct scenario invocation also works.
	// See safety_helpers.go.
	evals.RegisterDefault(&BiasHandler{})
	evals.RegisterDefault(&ToxicityHandler{})
	evals.RegisterDefault(&PIILeakageHandler{})
	evals.RegisterDefault(&RoleViolationHandler{})

	// Length validation handlers
	evals.RegisterDefault(&MinLengthHandler{})
	evals.RegisterDefault(&MaxLengthHandler{})

	// External eval handlers
	evals.RegisterDefault(&RestEvalHandler{})
	evals.RegisterDefault(&RestEvalSessionHandler{})
	evals.RegisterDefault(&A2AEvalHandler{})
	evals.RegisterDefault(&A2AEvalSessionHandler{})

	// Tool-invocation gate. Calls a registered tool by name and asserts
	// the call succeeded — generic primitive for "did this side-effect
	// pass" assertions (sandbox test runs, validation tools, …).
	evals.RegisterDefault(&ToolExecHandler{})

	// Behavioral testing handlers (Phase 6)
	evals.RegisterDefault(&OutcomeEquivalentHandler{})
	evals.RegisterDefault(&DirectionalHandler{})

	// Sentence count and field presence handlers (ported from guardrails)
	evals.RegisterDefault(&SentenceCountHandler{})
	evals.RegisterDefault(&FieldPresenceHandler{})

	// Arena assertion type aliases — map legacy/alternative names to canonical handlers
	evals.RegisterDefaultAlias("content_includes", "contains")
	evals.RegisterDefaultAlias("content_includes_any", "contains_any")
	evals.RegisterDefaultAlias("content_matches", "regex")
	evals.RegisterDefaultAlias("content_not_includes", "content_excludes")
	evals.RegisterDefaultAlias("is_valid_json", "json_valid")
	evals.RegisterDefaultAlias("valid_json", "json_valid")
	evals.RegisterDefaultAlias("tool_called", "tools_called")
	evals.RegisterDefaultAlias("tools_not_called_with_args", "tool_args_excluded_session")
	evals.RegisterDefaultAlias("llm_judge_conversation", "llm_judge_session")

	// New guardrail/validator aliases
	evals.RegisterDefaultAlias("banned_words", "content_excludes")
	evals.RegisterDefaultAlias("length", "max_length")
	evals.RegisterDefaultAlias("max_sentences", "sentence_count")
	evals.RegisterDefaultAlias("required_fields", "field_presence")
}
