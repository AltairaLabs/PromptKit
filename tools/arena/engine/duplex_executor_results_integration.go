package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// buildResultFromStateStore loads final state and builds result.
func (de *DuplexConversationExecutor) buildResultFromStateStore(
	req *ConversationRequest,
) *ConversationResult {
	if req.StateStoreConfig == nil || req.StateStoreConfig.Store == nil {
		return &ConversationResult{
			Messages: []types.Message{},
			Cost:     types.CostInfo{},
		}
	}

	store, ok := req.StateStoreConfig.Store.(statestore.Store)
	if !ok {
		return &ConversationResult{
			Messages: []types.Message{},
			Cost:     types.CostInfo{},
			Failed:   true,
			Error:    "invalid StateStore implementation",
		}
	}

	state, err := store.Load(context.Background(), req.ConversationID)
	if err != nil && !errors.Is(err, statestore.ErrNotFound) {
		return &ConversationResult{
			Messages: []types.Message{},
			Cost:     types.CostInfo{},
			Failed:   true,
			Error:    fmt.Sprintf("failed to load state: %v", err),
		}
	}

	var messages []types.Message
	if state != nil {
		messages = state.Messages
	}

	totalCost := de.calculateTotalCost(messages)
	mediaOutputs := CollectMediaOutputs(messages)
	toolStats := de.calculateToolStats(messages)

	// Evaluate conversation-level assertions
	convAssertionResults := de.evaluateConversationAssertions(req, messages)

	return &ConversationResult{
		Messages:                     messages,
		Cost:                         totalCost,
		MediaOutputs:                 mediaOutputs,
		ToolStats:                    toolStats,
		SelfPlay:                     de.containsSelfPlay(req.Scenario),
		PersonaID:                    de.findFirstSelfPlayPersona(req.Scenario),
		ConversationAssertionResults: convAssertionResults,
	}
}

// calculateToolStats aggregates tool usage statistics from messages.
func (de *DuplexConversationExecutor) calculateToolStats(messages []types.Message) *types.ToolStats {
	toolStats := &types.ToolStats{
		TotalCalls: 0,
		ByTool:     make(map[string]int),
	}

	for i := range messages {
		for j := range messages[i].ToolCalls {
			toolStats.TotalCalls++
			toolStats.ByTool[messages[i].ToolCalls[j].Name]++
		}
	}

	if toolStats.TotalCalls == 0 {
		return nil
	}

	return toolStats
}

// getConversationHistory retrieves the conversation history from state store.
func (de *DuplexConversationExecutor) getConversationHistory(req *ConversationRequest) []types.Message {
	if req.StateStoreConfig == nil || req.StateStoreConfig.Store == nil {
		return nil
	}

	store, ok := req.StateStoreConfig.Store.(statestore.Store)
	if !ok {
		return nil
	}

	state, err := store.Load(context.Background(), req.ConversationID)
	if err != nil {
		return nil
	}

	if state == nil {
		return nil
	}

	return state.Messages
}
