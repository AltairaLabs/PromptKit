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
	evals.RegisterDefault(&ToolCallSequenceHandler{})
	evals.RegisterDefault(&ToolCallChainHandler{})
	evals.RegisterDefault(&ToolCallsWithArgsHandler{})

	// JSON path, agent, and guardrail handlers (Batch 2)
	evals.RegisterDefault(&JSONPathHandler{})
	evals.RegisterDefault(&AgentInvokedHandler{})
	evals.RegisterDefault(&AgentNotInvokedHandler{})
	evals.RegisterDefault(&AgentResponseContainsHandler{})
	evals.RegisterDefault(&GuardrailTriggeredHandler{})

	// Workflow and skill handlers (Batch 3)
	evals.RegisterDefault(&WorkflowCompleteHandler{})
	evals.RegisterDefault(&WorkflowStateIsHandler{})
	evals.RegisterDefault(&WorkflowTransitionedToHandler{})
	evals.RegisterDefault(&SkillActivatedHandler{})
	evals.RegisterDefault(&SkillNotActivatedHandler{})

	// Media handlers (Batch 4)
	evals.RegisterDefault(&ImageFormatHandler{})
	evals.RegisterDefault(&ImageDimensionsHandler{})
	evals.RegisterDefault(&AudioFormatHandler{})
	evals.RegisterDefault(&AudioDurationHandler{})
	evals.RegisterDefault(&VideoDurationHandler{})
	evals.RegisterDefault(&VideoResolutionHandler{})

	// LLM judge handlers
	evals.RegisterDefault(&LLMJudgeHandler{})
	evals.RegisterDefault(&LLMJudgeSessionHandler{})
	evals.RegisterDefault(&LLMJudgeToolCallsHandler{})
}
