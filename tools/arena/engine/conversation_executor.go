package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
	"github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
)

// DefaultConversationExecutor implements ConversationExecutor interface
type DefaultConversationExecutor struct {
	scriptedExecutor turnexecutors.TurnExecutor
	selfPlayExecutor turnexecutors.TurnExecutor
	selfPlayRegistry *selfplay.Registry
	promptRegistry   *prompt.Registry
}

// NewDefaultConversationExecutor creates a new conversation executor
func NewDefaultConversationExecutor(
	scriptedExecutor turnexecutors.TurnExecutor,
	selfPlayExecutor turnexecutors.TurnExecutor,
	selfPlayRegistry *selfplay.Registry,
	promptRegistry *prompt.Registry,
) ConversationExecutor {
	return &DefaultConversationExecutor{
		scriptedExecutor: scriptedExecutor,
		selfPlayExecutor: selfPlayExecutor,
		selfPlayRegistry: selfPlayRegistry,
		promptRegistry:   promptRegistry,
	}
}

// ExecuteConversation runs a complete conversation based on scenario using the new Turn model
func (ce *DefaultConversationExecutor) ExecuteConversation(ctx context.Context, req ConversationRequest) *ConversationResult {
	// Check if scenario uses streaming - if so, use streaming path
	if req.Scenario.Streaming {
		// Any turn uses streaming, use streaming executor
		return ce.executeWithStreaming(ctx, req)
	}

	// Use non-streaming execution
	return ce.executeWithoutStreaming(ctx, req)
}

// executeWithoutStreaming runs conversation without streaming (original implementation)
func (ce *DefaultConversationExecutor) executeWithoutStreaming(ctx context.Context, req ConversationRequest) *ConversationResult {
	// Execute each turn in the scenario
	for turnIdx, scenarioTurn := range req.Scenario.Turns {
		// Warn if assertions are specified on user turns (they only validate assistant responses)
		if scenarioTurn.Role == "user" && len(scenarioTurn.Assertions) > 0 {
			logger.Warn("Ignoring assertions on user turn - assertions only validate assistant responses",
				"turn", turnIdx)
		}

		// Build turn request (StateStore manages history)
		turnReq := turnexecutors.TurnRequest{
			Provider:         req.Provider,
			Scenario:         req.Scenario,
			PromptRegistry:   ce.promptRegistry,
			TaskType:         req.Scenario.TaskType,
			Region:           req.Region,
			Temperature:      float64(req.Config.Defaults.Temperature),
			MaxTokens:        req.Config.Defaults.MaxTokens,
			Seed:             &req.Config.Defaults.Seed,
			StateStoreConfig: convertStateStoreConfig(req.StateStoreConfig),
			ConversationID:   req.ConversationID,
			Assertions:       scenarioTurn.Assertions, // Pass turn-level assertions
		}

		var err error

		// Choose executor based on role
		if ce.isSelfPlayRole(scenarioTurn.Role) {
			// Self-play turn (LLM-generated user message)
			turnReq.SelfPlayRole = scenarioTurn.Role
			turnReq.SelfPlayPersona = scenarioTurn.Persona
			err = ce.selfPlayExecutor.ExecuteTurn(ctx, turnReq)
		} else if scenarioTurn.Role == "user" {
			// Scripted user turn
			turnReq.ScriptedContent = scenarioTurn.Content
			err = ce.scriptedExecutor.ExecuteTurn(ctx, turnReq)
		} else {
			err = fmt.Errorf("unsupported role: %s", scenarioTurn.Role)
		}

		if err != nil {
			logger.Error("Turn execution failed",
				"turn", turnIdx,
				"role", scenarioTurn.Role,
				"error", err)

			// Load messages from StateStore (they were saved before validation failed)
			result := ce.buildResultFromStateStore(req)
			result.Failed = true
			result.Error = err.Error()
			return result
		}

		logger.Debug("Turn completed",
			"turn", turnIdx,
			"role", scenarioTurn.Role)
	}

	// Load final conversation state from StateStore
	return ce.buildResultFromStateStore(req)
}

// executeWithStreaming runs conversation with streaming enabled, using per-turn overrides
func (ce *DefaultConversationExecutor) executeWithStreaming(ctx context.Context, req ConversationRequest) *ConversationResult {
	// Execute each turn in the scenario
	for turnIdx, scenarioTurn := range req.Scenario.Turns {
		// Warn if assertions are specified on user turns (they only validate assistant responses)
		if scenarioTurn.Role == "user" && len(scenarioTurn.Assertions) > 0 {
			logger.Warn("Ignoring assertions on user turn - assertions only validate assistant responses",
				"turn", turnIdx)
		}

		// Build turn request (StateStore manages history)
		turnReq := turnexecutors.TurnRequest{
			Provider:         req.Provider,
			Scenario:         req.Scenario,
			PromptRegistry:   ce.promptRegistry,
			TaskType:         req.Scenario.TaskType,
			Region:           req.Region,
			Temperature:      float64(req.Config.Defaults.Temperature),
			MaxTokens:        req.Config.Defaults.MaxTokens,
			Seed:             &req.Config.Defaults.Seed,
			StateStoreConfig: convertStateStoreConfig(req.StateStoreConfig),
			ConversationID:   req.ConversationID,
			Assertions:       scenarioTurn.Assertions, // Pass turn-level assertions
		}

		var err error

		// Check if this turn should use streaming
		shouldStream := req.Scenario.ShouldStreamTurn(turnIdx)

		// Choose executor based on role and streaming preference
		if ce.isSelfPlayRole(scenarioTurn.Role) {
			// Self-play turn (LLM-generated user message)
			turnReq.SelfPlayRole = scenarioTurn.Role
			turnReq.SelfPlayPersona = scenarioTurn.Persona

			if shouldStream {
				err = ce.executeTurnWithStreaming(ctx, turnReq, ce.selfPlayExecutor)
			} else {
				err = ce.selfPlayExecutor.ExecuteTurn(ctx, turnReq)
			}
		} else if scenarioTurn.Role == "user" {
			// Scripted user turn
			turnReq.ScriptedContent = scenarioTurn.Content

			if shouldStream {
				err = ce.executeTurnWithStreaming(ctx, turnReq, ce.scriptedExecutor)
			} else {
				err = ce.scriptedExecutor.ExecuteTurn(ctx, turnReq)
			}
		} else {
			err = fmt.Errorf("unsupported role: %s", scenarioTurn.Role)
		}

		if err != nil {
			logger.Error("Turn execution failed",
				"turn", turnIdx,
				"role", scenarioTurn.Role,
				"streaming", shouldStream,
				"error", err)

			// Load messages from StateStore (they were saved before validation failed)
			result := ce.buildResultFromStateStore(req)
			result.Failed = true
			result.Error = err.Error()
			return result
		}

		logger.Debug("Turn completed",
			"turn", turnIdx,
			"role", scenarioTurn.Role,
			"streaming", shouldStream)
	}

	// Load final conversation state from StateStore
	return ce.buildResultFromStateStore(req)
}

// executeTurnWithStreaming executes a turn using streaming and returns the complete messages
func (ce *DefaultConversationExecutor) executeTurnWithStreaming(
	ctx context.Context,
	req turnexecutors.TurnRequest,
	executor turnexecutors.TurnExecutor,
) error {
	stream, err := executor.ExecuteTurnStream(ctx, req)
	if err != nil {
		return err
	}

	// Consume the stream (messages are saved to StateStore during streaming)
	for chunk := range stream {
		if chunk.Error != nil {
			return chunk.Error
		}

		// Note: We're consuming the stream but not doing anything with deltas
		// This is intentional for non-streaming ExecuteConversation
		// The streaming version (ExecuteConversationStream) handles deltas
		// Messages are saved to StateStore, not collected here
	}

	return nil
}

// ExecuteConversationStream runs a complete conversation with streaming
func (ce *DefaultConversationExecutor) ExecuteConversationStream(ctx context.Context, req ConversationRequest) (<-chan ConversationStreamChunk, error) {
	outChan := make(chan ConversationStreamChunk)

	go func() {
		defer close(outChan)

		// Initialize result (will be populated from StateStore at end)
		result := &ConversationResult{
			Messages: []types.Message{},
			Cost:     types.CostInfo{},
			SelfPlay: ce.containsSelfPlay(req.Scenario),
		}

		// Execute each turn in the scenario
		for turnIdx, scenarioTurn := range req.Scenario.Turns {
			// Build turn request (StateStore manages history)
			turnReq := turnexecutors.TurnRequest{
				Provider:         req.Provider,
				Scenario:         req.Scenario,
				PromptRegistry:   ce.promptRegistry,
				TaskType:         req.Scenario.TaskType,
				Region:           req.Region,
				Temperature:      float64(req.Config.Defaults.Temperature),
				MaxTokens:        req.Config.Defaults.MaxTokens,
				Seed:             &req.Config.Defaults.Seed,
				StateStoreConfig: convertStateStoreConfig(req.StateStoreConfig),
				ConversationID:   req.ConversationID,
			}

			// Choose executor based on role
			var stream <-chan turnexecutors.MessageStreamChunk
			var err error

			if ce.isSelfPlayRole(scenarioTurn.Role) {
				// Self-play turn (LLM-generated user message)
				turnReq.SelfPlayRole = scenarioTurn.Role
				turnReq.SelfPlayPersona = scenarioTurn.Persona
				stream, err = ce.selfPlayExecutor.ExecuteTurnStream(ctx, turnReq)
			} else if scenarioTurn.Role == "user" {
				// Scripted user turn
				turnReq.ScriptedContent = scenarioTurn.Content
				stream, err = ce.scriptedExecutor.ExecuteTurnStream(ctx, turnReq)
			} else {
				outChan <- ConversationStreamChunk{
					Result: result,
					Error:  fmt.Errorf("unsupported role: %s", scenarioTurn.Role),
				}
				return
			}

			if err != nil {
				outChan <- ConversationStreamChunk{
					Result: result,
					Error:  fmt.Errorf("turn execution failed: %w", err),
				}
				return
			}

			// Stream turn chunks
			for chunk := range stream {
				if chunk.Error != nil {
					outChan <- ConversationStreamChunk{
						TurnIndex: turnIdx,
						Result:    result,
						Error:     chunk.Error,
					}
					return
				}

				// Send conversation chunk with current state
				outChan <- ConversationStreamChunk{
					TurnIndex:    turnIdx,
					Delta:        chunk.Delta,
					TokenCount:   chunk.TokenCount,
					FinishReason: chunk.FinishReason,
					Result:       result,
				}
			}
		}

		// Load final conversation state from StateStore
		finalResult := ce.buildResultFromStateStore(req)

		// Send final chunk with complete result
		outChan <- ConversationStreamChunk{
			Result: finalResult,
		}
	}()

	return outChan, nil
}

// buildResultFromStateStore loads the final conversation state from StateStore and builds the result
func (ce *DefaultConversationExecutor) buildResultFromStateStore(req ConversationRequest) *ConversationResult {
	// StateStore is required - no bypass path
	if req.StateStoreConfig == nil || req.StateStoreConfig.Store == nil {
		return &ConversationResult{
			Messages: []types.Message{},
			Cost:     types.CostInfo{},
			Failed:   true,
			Error:    "StateStore is required but not configured",
		}
	}

	// Cast Store to statestore.Store
	store, ok := req.StateStoreConfig.Store.(statestore.Store)
	if !ok {
		return &ConversationResult{
			Messages: []types.Message{},
			Cost:     types.CostInfo{},
			Failed:   true,
			Error:    "invalid StateStore implementation",
		}
	}

	// Load conversation state from StateStore
	state, err := store.Load(context.Background(), req.ConversationID)
	if err != nil && !errors.Is(err, statestore.ErrNotFound) {
		return &ConversationResult{
			Messages: []types.Message{},
			Cost:     types.CostInfo{},
			Failed:   true,
			Error:    fmt.Sprintf("failed to load conversation from StateStore: %v", err),
		}
	}

	// Extract messages from state
	var messages []types.Message
	if state != nil {
		messages = state.Messages
	}

	// Calculate totals from messages
	var totalCost types.CostInfo
	toolStats := &types.ToolStats{
		TotalCalls: 0,
		ByTool:     make(map[string]int),
	}
	var violations []types.ValidationError

	for _, msg := range messages {
		// Aggregate costs
		if msg.CostInfo != nil {
			totalCost.InputTokens += msg.CostInfo.InputTokens
			totalCost.OutputTokens += msg.CostInfo.OutputTokens
			totalCost.CachedTokens += msg.CostInfo.CachedTokens
			totalCost.InputCostUSD += msg.CostInfo.InputCostUSD
			totalCost.OutputCostUSD += msg.CostInfo.OutputCostUSD
			totalCost.CachedCostUSD += msg.CostInfo.CachedCostUSD
			totalCost.TotalCost += msg.CostInfo.TotalCost
		}

		// Count tool calls
		for _, tc := range msg.ToolCalls {
			toolStats.TotalCalls++
			toolStats.ByTool[tc.Name]++
		}
	}

	// Set to nil if no tools were used
	if toolStats.TotalCalls == 0 {
		toolStats = nil
	}

	// Detect self-play and extract persona ID
	isSelfPlay := ce.containsSelfPlay(req.Scenario)
	var personaID string
	if isSelfPlay {
		// Extract persona ID from first self-play turn
		for _, turn := range req.Scenario.Turns {
			if ce.isSelfPlayRole(turn.Role) && turn.Persona != "" {
				personaID = turn.Persona
				break
			}
		}
	}

	return &ConversationResult{
		Messages:   messages,
		Cost:       totalCost,
		ToolStats:  toolStats,
		Violations: violations,
		SelfPlay:   isSelfPlay,
		PersonaID:  personaID,
	}
}

// isSelfPlayRole checks if a role is configured for self-play
func (ce *DefaultConversationExecutor) isSelfPlayRole(role string) bool {
	if ce.selfPlayRegistry == nil {
		return false
	}
	return ce.selfPlayRegistry.IsValidRole(role)
}

// containsSelfPlay checks if scenario uses self-play
func (ce *DefaultConversationExecutor) containsSelfPlay(scenario *config.Scenario) bool {
	for _, turn := range scenario.Turns {
		if ce.isSelfPlayRole(turn.Role) {
			return true
		}
	}
	return false
}

// convertStateStoreConfig converts engine.StateStoreConfig to turnexecutors.StateStoreConfig
func convertStateStoreConfig(cfg *StateStoreConfig) *turnexecutors.StateStoreConfig {
	if cfg == nil {
		return nil
	}
	return &turnexecutors.StateStoreConfig{
		Store:    cfg.Store,
		UserID:   cfg.UserID,
		Metadata: cfg.Metadata,
	}
}
