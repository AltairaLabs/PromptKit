package engine

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	arenaassertions "github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	arenastore "github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// evaluateTurnAssertions evaluates assertions configured on a turn via PackEvalHook.
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

	// Run turn assertions through PackEvalHook
	if de.packEvalHook != nil {
		evalResults := de.packEvalHook.RunAssertionsAsEvals(
			ctx, assertionConfigs, messages,
			len(messages)-1, req.ConversationID,
			evals.TriggerEveryTurn,
		)

		// Convert eval results to assertion results for message metadata
		convResults := arenaassertions.ConvertEvalResults(evalResults)
		results := make([]arenaassertions.AssertionResult, len(convResults))
		for i, cr := range convResults {
			results[i] = arenaassertions.AssertionResult{
				Passed:  cr.Passed,
				Details: cr.Details,
				Message: cr.Message,
			}
		}

		// Store results in the assistant message's metadata
		de.storeAssertionResults(req, lastAssistantMsg, results)

		logger.Debug("Turn assertions evaluated via eval path",
			"turn", turnIdx,
			"assertionCount", len(assertionConfigs),
			"eval_result_count", len(evalResults))
	}
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

// evaluateConversationAssertions evaluates pack + scenario conversation assertions via PackEvalHook.
func (de *DuplexConversationExecutor) evaluateConversationAssertions(
	req *ConversationRequest,
	messages []types.Message,
) []arenaassertions.ConversationValidationResult {
	var scenarioAssertions []arenaassertions.AssertionConfig
	if req.Scenario != nil {
		scenarioAssertions = req.Scenario.ConversationAssertions
	}
	assertionConfigs := mergeAssertionConfigs(req.Config, scenarioAssertions)

	if len(assertionConfigs) == 0 {
		return nil
	}

	if de.packEvalHook == nil {
		logger.Debug("No packEvalHook configured, skipping duplex conversation assertions")
		return nil
	}

	logger.Debug("Evaluating duplex conversation assertions",
		"assertion_count", len(assertionConfigs))

	results := de.packEvalHook.RunAssertionsAsConversationResults(
		context.Background(), assertionConfigs, messages,
		len(messages)-1, req.ConversationID,
		evals.TriggerOnConversationComplete,
	)

	logger.Debug("Duplex conversation assertion results",
		"result_count", len(results))

	return results
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
