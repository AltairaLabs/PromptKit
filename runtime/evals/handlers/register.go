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

	// LLM judge handlers
	evals.RegisterDefault(&LLMJudgeHandler{})
	evals.RegisterDefault(&LLMJudgeSessionHandler{})
	evals.RegisterDefault(&LLMJudgeToolCallsHandler{})

	// External eval handlers
	evals.RegisterDefault(&RestEvalHandler{})
	evals.RegisterDefault(&RestEvalSessionHandler{})
	evals.RegisterDefault(&A2AEvalHandler{})
	evals.RegisterDefault(&A2AEvalSessionHandler{})
}
