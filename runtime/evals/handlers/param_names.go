package handlers

import "github.com/AltairaLabs/PromptKit/runtime/v2/evals"

// ParamNames lists, for every built-in handler, the params it reads after
// evals.ApplyDefaults and evals.NormalizeParams (evals.ParamNamer). Load-time
// validation rejects any other key, so each list must name every param the
// handler reads and nothing more: a missing name turns a working config into a
// load error, an extra one hides a typo.
//
// Not listed, by design:
//   - min_score and max_score: wrapper thresholds, always accepted at load;
//     the handlers that must not see them reject them on Eval.
//   - keys a handler only rejects (model on a judge, classifier_id,
//     min_similarity, threshold): those are not params it reads, so load
//     reports them as unknown; the handler's own message applies only on a
//     path that skips load validation. The classify handlers reject
//     classifier_id in ValidateParams, which runs first, so load reports
//     their own message for it.
//
// ExecEvalHandler forwards its params to a subprocess whose keys it cannot
// know, so it declares none and is not checked.

// Param names shared by several handlers.
const (
	pnPatterns       = "patterns"
	pnPattern        = "pattern"
	pnToolNames      = "tool_names"
	pnTools          = "tools"
	pnTool           = "tool"
	pnToolName       = "tool_name"
	pnExpectedArgs   = "expected_args"
	pnMinCalls       = "min_calls"
	pnMaxCalls       = "max_calls"
	pnMin            = "min"
	pnMax            = "max"
	pnState          = "state"
	pnSequence       = "sequence"
	pnSkillNames     = "skill_names"
	pnAgents         = "agents"
	pnFields         = "fields"
	pnFormats        = "formats"
	pnOccurrence     = "occurrence"
	pnAllowWrapped   = "allow_wrapped"
	pnExtractJSON    = "extract_json"
	pnContains       = "contains"
	pnExpected       = "expected"
	pnCriteria       = "criteria"
	pnExtra          = "extra"
	pnTimeout        = "timeout"
	pnMinWidth       = "min_width"
	pnMaxWidth       = "max_width"
	pnMinHeight      = "min_height"
	pnMaxHeight      = "max_height"
	pnMinSeconds     = "min_seconds"
	pnMaxSeconds     = "max_seconds"
	pnContexts       = "contexts"
	pnContext        = "context"
	pnContextField   = "context_field"
	pnQuestion       = "question"
	pnMatchMode      = "match_mode"
	pnResultInclude  = "result_includes"
	pnResultMatches  = "result_matches"
	pnArgsMatch      = "args_match"
	pnNoError        = "no_error"
	pnAgentRole      = "agent_role"
	pnBranch         = "branch"
	pnExpectedTools  = "expected_tools"
	pnExpression     = "expression"
	pnFailurePattern = "failure_pattern"
	pnRubric         = "rubric"
	pnSuccessPattern = "success_pattern"
	pnParallel       = "parallel"
	pnReference      = "reference"
	pnValidator      = "validator"
	pnValidatorType  = "validator_type"
)

// names joins param-name groups into one list.
func names(groups ...[]string) []string {
	var out []string
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

var (
	// judgeParamNames are read by every LLM-judge handler (buildJudgeOpts and
	// resolveJudgeProvider): the llm_judge family, the RAG checks and the
	// safety checks.
	judgeParamNames = []string{pnCriteria, pnRubric, "system_prompt", pnExtra, ProviderParam}

	// ragContextParamNames are the three forms a RAG check takes its chunks in.
	ragContextParamNames = []string{pnContexts, pnContext, pnContextField}

	// classifyParamNames are read by parseClassifyConfig.
	classifyParamNames = []string{"model", classifyKeyExpectedLabel, "message_role", "message_index", ProviderParam}

	// externalParamNames are read by buildExternalRequest (rest_eval, a2a_eval).
	externalParamNames = []string{pnCriteria, pnExtra, "include_messages", "include_tool_calls"}

	jsonParseParamNames = []string{pnAllowWrapped, pnExtractJSON}
	toolListParamNames  = []string{pnToolNames, pnTools}
)

// Every registered built-in handler declares its params; see
// TestBuiltinHandlersDeclareParamNames.
var _ evals.ParamNamer = (*ContainsHandler)(nil)

// ParamNames implements evals.ParamNamer.
func (h *ContainsHandler) ParamNames() []string { return []string{pnPatterns} }

// ParamNames implements evals.ParamNamer.
func (h *ContainsAnyHandler) ParamNames() []string { return []string{pnPatterns} }

// ParamNames implements evals.ParamNamer.
func (h *ContentExcludesHandler) ParamNames() []string {
	return []string{pnPatterns, pnMatchMode, paramCaseSensitive}
}

// ParamNames implements evals.ParamNamer.
func (h *RegexHandler) ParamNames() []string { return []string{pnPattern, "expect_match"} }

// ParamNames implements evals.ParamNamer.
func (h *JSONValidHandler) ParamNames() []string { return jsonParseParamNames }

// ParamNames implements evals.ParamNamer.
func (h *JSONSchemaHandler) ParamNames() []string {
	return names([]string{"schema"}, jsonParseParamNames)
}

// ParamNames implements evals.ParamNamer.
func (h *JSONPathHandler) ParamNames() []string {
	return names([]string{
		pnExpression, "jmespath_expression", pnExpected, pnContains, pnMin, pnMax, "min_results", "max_results",
	}, jsonParseParamNames)
}

// ParamNames implements evals.ParamNamer.
func (h *ToolsCalledHandler) ParamNames() []string {
	return names(toolListParamNames, []string{pnMinCalls, pnMaxCalls, "ignore_validation", "require_args"})
}

// ParamNames implements evals.ParamNamer.
func (h *ToolsOfferedHandler) ParamNames() []string {
	return names(toolListParamNames, []string{"exact", "absent"})
}

// ParamNames implements evals.ParamNamer.
func (h *ToolsNotCalledHandler) ParamNames() []string { return toolListParamNames }

// ParamNames implements evals.ParamNamer.
func (h *ToolArgsHandler) ParamNames() []string { return []string{pnToolName, pnExpectedArgs} }

// ParamNames implements evals.ParamNamer.
func (h *LatencyBudgetHandler) ParamNames() []string { return []string{"max_ms"} }

// ParamNames implements evals.ParamNamer.
func (h *CosineSimilarityHandler) ParamNames() []string { return []string{pnReference} }

// ParamNames implements evals.ParamNamer.
func (h *ToolsCalledSessionHandler) ParamNames() []string {
	return names(toolListParamNames, []string{pnMinCalls})
}

// ParamNames implements evals.ParamNamer.
func (h *ToolsNotCalledSessionHandler) ParamNames() []string { return toolListParamNames }

// ParamNames implements evals.ParamNamer.
func (h *ToolArgsSessionHandler) ParamNames() []string { return []string{pnToolName, pnExpectedArgs} }

// ParamNames implements evals.ParamNamer.
func (h *ToolArgsExcludedSessionHandler) ParamNames() []string {
	return []string{pnToolName, "excluded_args", "forbidden_args"}
}

// ParamNames implements evals.ParamNamer.
func (h *NoToolErrorsHandler) ParamNames() []string { return []string{pnTools} }

// ParamNames implements evals.ParamNamer.
func (h *ToolCallCountHandler) ParamNames() []string { return []string{pnTool, pnMin, pnMax} }

// ParamNames implements evals.ParamNamer.
func (h *ToolResultIncludesHandler) ParamNames() []string {
	return []string{pnTool, pnPatterns, pnOccurrence}
}

// ParamNames implements evals.ParamNamer.
func (h *ToolResultMatchesHandler) ParamNames() []string {
	return []string{pnTool, pnPattern, pnOccurrence}
}

// ParamNames implements evals.ParamNamer.
func (h *ToolResultHasMediaHandler) ParamNames() []string { return []string{pnTool, "media_type"} }

// ParamNames implements evals.ParamNamer.
func (h *ToolResultMediaTypeHandler) ParamNames() []string { return []string{pnTool, "mime_type"} }

// ParamNames implements evals.ParamNamer.
func (h *ToolCallSequenceHandler) ParamNames() []string { return []string{pnSequence} }

// ParamNames implements evals.ParamNamer. Each step's own keys are inside steps.
func (h *ToolCallChainHandler) ParamNames() []string { return []string{"steps"} }

// ParamNames implements evals.ParamNamer.
func (h *ToolCallsWithArgsHandler) ParamNames() []string {
	return []string{pnToolName, pnExpectedArgs, pnArgsMatch, pnResultInclude, pnResultMatches, pnNoError}
}

// ParamNames implements evals.ParamNamer.
func (h *ToolAntiPatternHandler) ParamNames() []string { return []string{pnPatterns} }

// ParamNames implements evals.ParamNamer.
func (h *ToolNoRepeatHandler) ParamNames() []string { return []string{pnTools, "max_repeats"} }

// ParamNames implements evals.ParamNamer.
func (h *ToolEfficiencyHandler) ParamNames() []string {
	return []string{pnMaxCalls, "max_errors", "max_error_rate"}
}

// ParamNames implements evals.ParamNamer.
func (h *CostBudgetHandler) ParamNames() []string {
	return []string{"max_cost_usd", "max_input_tokens", "max_output_tokens", "max_total_tokens"}
}

// ParamNames implements evals.ParamNamer.
func (h *AgentInvokedHandler) ParamNames() []string { return []string{pnAgents} }

// ParamNames implements evals.ParamNamer.
func (h *AgentNotInvokedHandler) ParamNames() []string { return []string{pnAgents} }

// ParamNames implements evals.ParamNamer.
func (h *AgentResponseContainsHandler) ParamNames() []string { return []string{"agent", pnContains} }

// ParamNames implements evals.ParamNamer.
func (h *GuardrailTriggeredHandler) ParamNames() []string {
	return []string{pnValidatorType, pnValidator, "should_trigger", keyDirection}
}

// ParamNames implements evals.ParamNamer.
func (h *InvariantFieldsPreservedHandler) ParamNames() []string { return []string{pnTool, pnFields} }

// ParamNames implements evals.ParamNamer.
func (h *CompositionStepOutputHandler) ParamNames() []string {
	return []string{paramStep, paramContains, paramEquals}
}

// ParamNames implements evals.ParamNamer.
func (h *CompositionBranchTakenHandler) ParamNames() []string { return []string{pnBranch, keyExpected} }

// ParamNames implements evals.ParamNamer.
func (h *CompositionParallelCompleteHandler) ParamNames() []string { return []string{pnParallel} }

// ParamNames implements evals.ParamNamer.
func (h *CompositionOutputHandler) ParamNames() []string { return []string{paramContains, paramEquals} }

// ParamNames implements evals.ParamNamer.
func (h *WorkflowCompleteHandler) ParamNames() []string { return nil }

// ParamNames implements evals.ParamNamer.
func (h *WorkflowStateIsHandler) ParamNames() []string { return []string{pnState} }

// ParamNames implements evals.ParamNamer.
func (h *WorkflowTransitionedToHandler) ParamNames() []string { return []string{pnState} }

// ParamNames implements evals.ParamNamer.
func (h *WorkflowSpokeInStateHandler) ParamNames() []string { return []string{keyState} }

// ParamNames implements evals.ParamNamer.
func (h *WorkflowTransitionOrderHandler) ParamNames() []string { return []string{pnSequence} }

// ParamNames implements evals.ParamNamer.
func (h *WorkflowToolAccessHandler) ParamNames() []string { return []string{"rules"} }

// ParamNames implements evals.ParamNamer.
func (h *SkillActivatedHandler) ParamNames() []string { return []string{pnSkillNames, pnMinCalls} }

// ParamNames implements evals.ParamNamer.
func (h *SkillNotActivatedHandler) ParamNames() []string { return []string{pnSkillNames} }

// ParamNames implements evals.ParamNamer.
func (h *SkillActivationOrderHandler) ParamNames() []string { return []string{pnSequence} }

// ParamNames implements evals.ParamNamer.
func (h *ImageFormatHandler) ParamNames() []string { return []string{pnFormats} }

// ParamNames implements evals.ParamNamer.
func (h *ImageDimensionsHandler) ParamNames() []string {
	return []string{pnMinWidth, pnMaxWidth, pnMinHeight, pnMaxHeight, "width", "height"}
}

// ParamNames implements evals.ParamNamer.
func (h *AudioFormatHandler) ParamNames() []string { return []string{pnFormats} }

// ParamNames implements evals.ParamNamer.
func (h *AudioDurationHandler) ParamNames() []string { return []string{pnMinSeconds, pnMaxSeconds} }

// ParamNames implements evals.ParamNamer.
func (h *VideoDurationHandler) ParamNames() []string { return []string{pnMinSeconds, pnMaxSeconds} }

// ParamNames implements evals.ParamNamer.
func (h *VideoResolutionHandler) ParamNames() []string {
	return []string{pnMinWidth, pnMaxWidth, pnMinHeight, pnMaxHeight, "presets"}
}

// ParamNames implements evals.ParamNamer.
func (h *AudioEmotionHandler) ParamNames() []string { return classifyParamNames }

// ParamNames implements evals.ParamNamer.
func (h *ImageModerationHandler) ParamNames() []string { return classifyParamNames }

// ParamNames implements evals.ParamNamer.
func (h *TextToxicityHandler) ParamNames() []string { return classifyParamNames }

// ParamNames implements evals.ParamNamer.
func (h *TextSentimentHandler) ParamNames() []string { return classifyParamNames }

// ParamNames implements evals.ParamNamer. It is topic_policy's closed key set,
// which its own ValidateParams also enforces.
func (h *TopicPolicyHandler) ParamNames() []string {
	out := make([]string, 0, len(topicPolicyKeys))
	for k := range topicPolicyKeys {
		out = append(out, k)
	}
	return out
}

// ParamNames implements evals.ParamNamer.
func (h *LLMJudgeHandler) ParamNames() []string { return judgeParamNames }

// ParamNames implements evals.ParamNamer.
func (h *LLMJudgeSessionHandler) ParamNames() []string { return judgeParamNames }

// ParamNames implements evals.ParamNamer.
func (h *LLMJudgeToolCallsHandler) ParamNames() []string {
	return names(judgeParamNames, []string{pnTools})
}

// ParamNames implements evals.ParamNamer.
func (h *FaithfulnessHandler) ParamNames() []string {
	return names(judgeParamNames, ragContextParamNames)
}

// ParamNames implements evals.ParamNamer.
func (h *HallucinationHandler) ParamNames() []string {
	return names(judgeParamNames, ragContextParamNames)
}

// ParamNames implements evals.ParamNamer.
func (h *AnswerRelevancyHandler) ParamNames() []string {
	return names(judgeParamNames, []string{pnQuestion})
}

// ParamNames implements evals.ParamNamer.
func (h *ContextualPrecisionHandler) ParamNames() []string {
	return names(judgeParamNames, ragContextParamNames, []string{pnQuestion})
}

// ParamNames implements evals.ParamNamer.
func (h *ContextualRecallHandler) ParamNames() []string {
	return names(judgeParamNames, ragContextParamNames, []string{pnReference, "expected_output"})
}

// ParamNames implements evals.ParamNamer.
func (h *ContextualRelevancyHandler) ParamNames() []string {
	return names(judgeParamNames, ragContextParamNames, []string{pnQuestion})
}

// ParamNames implements evals.ParamNamer.
func (h *BiasHandler) ParamNames() []string { return judgeParamNames }

// ParamNames implements evals.ParamNamer.
func (h *ToxicityHandler) ParamNames() []string { return judgeParamNames }

// ParamNames implements evals.ParamNamer.
func (h *PIILeakageHandler) ParamNames() []string { return judgeParamNames }

// ParamNames implements evals.ParamNamer.
func (h *RoleViolationHandler) ParamNames() []string {
	return names(judgeParamNames, []string{pnAgentRole})
}

// ParamNames implements evals.ParamNamer. The legacy names are read directly
// as well as through NormalizeParams.
func (h *MinLengthHandler) ParamNames() []string {
	return []string{pnMin, "min_characters", "min_chars"}
}

// ParamNames implements evals.ParamNamer.
func (h *MaxLengthHandler) ParamNames() []string {
	return []string{pnMax, "max_characters", "max_chars", "max_tokens"}
}

// ParamNames implements evals.ParamNamer.
func (h *RestEvalHandler) ParamNames() []string {
	return names([]string{"url", "method", "headers", pnTimeout}, externalParamNames)
}

// ParamNames implements evals.ParamNamer.
func (h *RestEvalSessionHandler) ParamNames() []string { return (&RestEvalHandler{}).ParamNames() }

// ParamNames implements evals.ParamNamer.
func (h *A2AEvalHandler) ParamNames() []string {
	return names([]string{"agent_url", "auth_token", pnTimeout}, externalParamNames)
}

// ParamNames implements evals.ParamNamer.
func (h *A2AEvalSessionHandler) ParamNames() []string { return (&A2AEvalHandler{}).ParamNames() }

// ParamNames implements evals.ParamNamer.
func (h *ToolExecHandler) ParamNames() []string {
	return []string{
		pnTool, "args", "timeout_seconds", pnSuccessPattern, pnFailurePattern,
		"score_path", "score_pattern", "score_scale",
	}
}

// ParamNames implements evals.ParamNamer.
func (h *OutcomeEquivalentHandler) ParamNames() []string {
	return []string{"metric", pnExpectedTools, "expected_state", "expected_content"}
}

// ParamNames implements evals.ParamNamer.
func (h *DirectionalHandler) ParamNames() []string {
	return []string{"check", "baseline_tools", "baseline_state", "baseline_content"}
}

// ParamNames implements evals.ParamNamer.
func (h *SentenceCountHandler) ParamNames() []string { return []string{keyMax} }

// ParamNames implements evals.ParamNamer.
func (h *FieldPresenceHandler) ParamNames() []string { return []string{pnFields} }
