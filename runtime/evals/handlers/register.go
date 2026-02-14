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

	// LLM judge handlers
	evals.RegisterDefault(&LLMJudgeHandler{})
	evals.RegisterDefault(&LLMJudgeSessionHandler{})
}
