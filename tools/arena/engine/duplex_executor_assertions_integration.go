package engine

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
	arenaassertions "github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	arenastore "github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// evaluateTurnAssertions evaluates assertions configured on a turn.
// Assertions on user turns validate the subsequent assistant response.
func (de *DuplexConversationExecutor) evaluateTurnAssertions(
	ctx context.Context,
	req *ConversationRequest,
	turn *config.TurnDefinition,
	turnIdx int,
) {
	if len(turn.Assertions) == 0 {
		return
	}

	// Get messages from state store to find the latest assistant message
	messages := de.getConversationHistory(req)
	if len(messages) == 0 {
		logger.Debug("No messages to evaluate assertions against", "turn", turnIdx)
		return
	}

	// Find the latest assistant message
	var lastAssistantMsg *types.Message
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == roleAssistant {
			lastAssistantMsg = &messages[i]
			break
		}
	}

	if lastAssistantMsg == nil {
		logger.Debug("No assistant message found for assertion evaluation", "turn", turnIdx)
		return
	}

	// Convert turn assertions to assertion configs
	assertionConfigs := make([]arenaassertions.AssertionConfig, len(turn.Assertions))
	for i, a := range turn.Assertions {
		assertionConfigs[i] = arenaassertions.AssertionConfig{
			Type:    a.Type,
			Params:  a.Params,
			Message: a.Message,
		}
	}

	// Create assertion registry and evaluate
	registry := arenaassertions.NewArenaAssertionRegistry()
	results := de.runAssertions(ctx, registry, assertionConfigs, lastAssistantMsg, messages)

	// Store results in the assistant message's metadata
	de.storeAssertionResults(req, lastAssistantMsg, results)

	// Dual-write: run turn assertions through EvalRunner
	if de.packEvalHook != nil {
		evalResults := de.packEvalHook.RunAssertionsAsEvals(
			ctx, assertionConfigs, messages,
			len(messages)-1, req.ConversationID,
			evals.TriggerEveryTurn,
		)
		if len(evalResults) > 0 && lastAssistantMsg.Meta != nil {
			lastAssistantMsg.Meta["eval_results"] = evalResults
			logger.Debug("Dual-write duplex turn eval results",
				"eval_result_count", len(evalResults))
		}
	}

	logger.Debug("Turn assertions evaluated",
		"turn", turnIdx,
		"assertionCount", len(assertionConfigs),
		"passed", de.countPassedAssertions(results))
}

// runAssertions executes all assertions and returns results.
//
//nolint:unparam // ctx may be used in future assertion implementations
func (de *DuplexConversationExecutor) runAssertions(
	ctx context.Context,
	registry *validators.Registry,
	configs []arenaassertions.AssertionConfig,
	targetMsg *types.Message,
	allMessages []types.Message,
) []arenaassertions.AssertionResult {
	results := make([]arenaassertions.AssertionResult, 0, len(configs))

	for _, cfg := range configs {
		// Build validator params
		params := map[string]interface{}{
			"assistant_response": targetMsg.Content,
			"messages":           allMessages,
		}
		// Merge assertion params
		for k, v := range cfg.Params {
			params[k] = v
		}

		// Get validator factory
		factory, ok := registry.Get(cfg.Type)
		if !ok {
			results = append(results, arenaassertions.AssertionResult{
				Passed: false,
				Details: map[string]interface{}{
					"error": fmt.Sprintf("unknown validator type: %s", cfg.Type),
				},
				Message: cfg.Message,
			})
			continue
		}

		// Create validator instance and run validation
		validator := factory(params)
		validationResult := validator.Validate(targetMsg.Content, params)
		results = append(results, arenaassertions.FromValidationResult(validationResult, cfg.Message))
	}

	return results
}

// storeAssertionResults stores assertion results in the state store.
func (de *DuplexConversationExecutor) storeAssertionResults(
	req *ConversationRequest,
	msg *types.Message,
	results []arenaassertions.AssertionResult,
) {
	if req.StateStoreConfig == nil || req.StateStoreConfig.Store == nil {
		return
	}

	// Try to get ArenaStateStore to update assertion results
	arenaStore, ok := req.StateStoreConfig.Store.(*arenastore.ArenaStateStore)
	if !ok {
		return
	}

	// Convert results to map format for message metadata
	assertionResults := make(map[string]interface{})
	resultsList := make([]map[string]interface{}, 0, len(results))
	allPassed := true

	for i, r := range results {
		resultMap := map[string]interface{}{
			"type":    fmt.Sprintf("assertion_%d", i),
			"passed":  r.Passed,
			"details": r.Details,
		}
		if r.Message != "" {
			resultMap["message"] = r.Message
		}
		resultsList = append(resultsList, resultMap)
		if !r.Passed {
			allPassed = false
		}
	}

	assertionResults["results"] = resultsList
	assertionResults["all_passed"] = allPassed
	assertionResults["total"] = len(results)
	assertionResults["failed"] = de.countFailedAssertions(results)

	// Update message metadata
	if msg.Meta == nil {
		msg.Meta = make(map[string]interface{})
	}
	msg.Meta["assertions"] = assertionResults

	// Update the state store with the modified message
	arenaStore.UpdateLastAssistantMessage(msg)
}

// countPassedAssertions counts how many assertions passed.
func (de *DuplexConversationExecutor) countPassedAssertions(results []arenaassertions.AssertionResult) int {
	count := 0
	for _, r := range results {
		if r.Passed {
			count++
		}
	}
	return count
}

// countFailedAssertions counts how many assertions failed.
func (de *DuplexConversationExecutor) countFailedAssertions(results []arenaassertions.AssertionResult) int {
	count := 0
	for _, r := range results {
		if !r.Passed {
			count++
		}
	}
	return count
}

// evaluateConversationAssertions evaluates pack + scenario conversation assertions.
// Returns both old-path assertion results and new-path eval results (dual-write).
func (de *DuplexConversationExecutor) evaluateConversationAssertions(
	req *ConversationRequest,
	messages []types.Message,
) ([]arenaassertions.ConversationValidationResult, []evals.EvalResult) {
	var scenarioAssertions []arenaassertions.AssertionConfig
	if req.Scenario != nil {
		scenarioAssertions = req.Scenario.ConversationAssertions
	}
	allAssertions := collectConversationAssertions(req.Config, scenarioAssertions)

	if len(allAssertions) == 0 {
		return nil, nil
	}

	logger.Debug("Evaluating duplex conversation assertions",
		"assertion_count", len(allAssertions))

	// Build conversation context from messages
	convCtx := de.buildConversationContext(req, messages)

	// Run assertions
	reg := arenaassertions.NewConversationAssertionRegistry()
	results := reg.ValidateConversations(context.Background(), allAssertions, convCtx)

	logger.Debug("Duplex conversation assertion results",
		"result_count", len(results),
		"results", results)

	// Dual-write: also run assertions through EvalRunner
	var evalResults []evals.EvalResult
	if de.packEvalHook != nil {
		assertionConfigs := mergeAssertionConfigs(req.Config, scenarioAssertions)
		evalResults = de.packEvalHook.RunAssertionsAsEvals(
			context.Background(), assertionConfigs, messages,
			len(messages)-1, req.ConversationID,
			evals.TriggerOnConversationComplete,
		)
		logger.Debug("Dual-write duplex conversation eval results",
			"eval_result_count", len(evalResults))
	}

	return results, evalResults
}

// buildConversationContext creates the context used for conversation-level assertions.
func (de *DuplexConversationExecutor) buildConversationContext(
	req *ConversationRequest,
	messages []types.Message,
) *arenaassertions.ConversationContext {
	providerID := ""
	if req.Provider != nil {
		providerID = req.Provider.ID()
	}
	meta := &arenaassertions.ConversationMetadata{
		ScenarioID: req.Scenario.ID,
		ProviderID: providerID,
	}
	return arenaassertions.BuildConversationContextFromMessages(messages, meta)
}
