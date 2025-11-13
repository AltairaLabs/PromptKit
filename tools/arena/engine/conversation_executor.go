package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
	"github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
)

const (
	// Error message for unsupported roles
	errUnsupportedRole = "unsupported role: %s"
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
		// Debug if assertions are specified on user turns (they only validate assistant responses)
		if scenarioTurn.Role == "user" && len(scenarioTurn.Assertions) > 0 {
			logger.Debug("Assertions on user turn will validate next assistant response",
				"turn", turnIdx)
		}

		// Build turn request using the shared builder function
		turnReq := ce.buildTurnRequest(req, scenarioTurn)

		var err error

		// Choose executor based on role
		if ce.isSelfPlayRole(scenarioTurn.Role) {
			// Self-play turn (LLM-generated user message)
			turnReq.SelfPlayRole = scenarioTurn.Role
			turnReq.SelfPlayPersona = scenarioTurn.Persona
			err = ce.selfPlayExecutor.ExecuteTurn(ctx, turnReq)
		} else if scenarioTurn.Role == "user" {
			// Scripted user turn (non-streaming path)
			err = ce.scriptedExecutor.ExecuteTurn(ctx, turnReq)
		} else {
			err = fmt.Errorf(errUnsupportedRole, scenarioTurn.Role)
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
	for turnIdx, scenarioTurn := range req.Scenario.Turns {
		err := ce.executeStreamingTurn(ctx, req, turnIdx, scenarioTurn)
		if err != nil {
			return ce.handleTurnExecutionError(req, err, turnIdx, scenarioTurn)
		}

		ce.logTurnCompletion(turnIdx, scenarioTurn, req.Scenario.ShouldStreamTurn(turnIdx))
	}

	return ce.buildResultFromStateStore(req)
}

// executeStreamingTurn executes a single turn with streaming support
func (ce *DefaultConversationExecutor) executeStreamingTurn(ctx context.Context, req ConversationRequest, turnIdx int, scenarioTurn config.TurnDefinition) error {
	ce.debugOnUserTurnAssertions(scenarioTurn, turnIdx)

	turnReq := ce.buildTurnRequest(req, scenarioTurn)
	shouldStream := req.Scenario.ShouldStreamTurn(turnIdx)

	return ce.executeTurnByRole(ctx, turnReq, scenarioTurn, shouldStream)
}

// debugOnUserTurnAssertions logs debug message if assertions are specified on user turns
func (ce *DefaultConversationExecutor) debugOnUserTurnAssertions(scenarioTurn config.TurnDefinition, turnIdx int) {
	if scenarioTurn.Role == "user" && len(scenarioTurn.Assertions) > 0 {
		logger.Debug("Assertions on user turn will validate next assistant response",
			"turn", turnIdx)
	}
}

// buildTurnRequest creates a TurnRequest from the conversation request and scenario turn
func (ce *DefaultConversationExecutor) buildTurnRequest(req ConversationRequest, scenarioTurn config.TurnDefinition) turnexecutors.TurnRequest {
	baseDir := ""
	if req.Config != nil {
		baseDir = req.Config.ConfigDir
	}
	return turnexecutors.TurnRequest{
		Provider:         req.Provider,
		Scenario:         req.Scenario,
		PromptRegistry:   ce.promptRegistry,
		TaskType:         req.Scenario.TaskType,
		Region:           req.Region,
		BaseDir:          baseDir,
		Temperature:      float64(req.Config.Defaults.Temperature),
		MaxTokens:        req.Config.Defaults.MaxTokens,
		Seed:             &req.Config.Defaults.Seed,
		StateStoreConfig: convertStateStoreConfig(req.StateStoreConfig),
		ConversationID:   req.ConversationID,
		ScriptedContent:  scenarioTurn.Content, // Legacy text content (for backward compatibility)
		ScriptedParts:    scenarioTurn.Parts,   // Multimodal content parts (takes precedence over ScriptedContent)
		Assertions:       scenarioTurn.Assertions,
	}
}

// executeTurnByRole executes a turn based on its role (self-play or scripted)
func (ce *DefaultConversationExecutor) executeTurnByRole(ctx context.Context, turnReq turnexecutors.TurnRequest, scenarioTurn config.TurnDefinition, shouldStream bool) error {
	if ce.isSelfPlayRole(scenarioTurn.Role) {
		return ce.executeSelfPlayTurn(ctx, turnReq, scenarioTurn, shouldStream)
	}

	if scenarioTurn.Role == "user" {
		return ce.executeScriptedTurn(ctx, turnReq, scenarioTurn, shouldStream)
	}

	return fmt.Errorf(errUnsupportedRole, scenarioTurn.Role)
}

// executeSelfPlayTurn executes a self-play turn
func (ce *DefaultConversationExecutor) executeSelfPlayTurn(ctx context.Context, turnReq turnexecutors.TurnRequest, scenarioTurn config.TurnDefinition, shouldStream bool) error {
	turnReq.SelfPlayRole = scenarioTurn.Role
	turnReq.SelfPlayPersona = scenarioTurn.Persona

	if shouldStream {
		return ce.executeTurnWithStreaming(ctx, turnReq, ce.selfPlayExecutor)
	}
	return ce.selfPlayExecutor.ExecuteTurn(ctx, turnReq)
}

// executeScriptedTurn executes a scripted user turn
func (ce *DefaultConversationExecutor) executeScriptedTurn(ctx context.Context, turnReq turnexecutors.TurnRequest, scenarioTurn config.TurnDefinition, shouldStream bool) error {
	turnReq.ScriptedContent = scenarioTurn.Content
	// Preserve multimodal parts for scripted turns (streaming and non-streaming)
	turnReq.ScriptedParts = scenarioTurn.Parts

	if shouldStream {
		return ce.executeTurnWithStreaming(ctx, turnReq, ce.scriptedExecutor)
	}
	return ce.scriptedExecutor.ExecuteTurn(ctx, turnReq)
}

// handleTurnExecutionError handles errors that occur during turn execution
func (ce *DefaultConversationExecutor) handleTurnExecutionError(req ConversationRequest, err error, turnIdx int, scenarioTurn config.TurnDefinition) *ConversationResult {
	logger.Error("Turn execution failed",
		"turn", turnIdx,
		"role", scenarioTurn.Role,
		"streaming", req.Scenario.ShouldStreamTurn(turnIdx),
		"error", err)

	result := ce.buildResultFromStateStore(req)
	result.Failed = true
	result.Error = err.Error()
	return result
}

// logTurnCompletion logs successful turn completion
func (ce *DefaultConversationExecutor) logTurnCompletion(turnIdx int, scenarioTurn config.TurnDefinition, shouldStream bool) {
	logger.Debug("Turn completed",
		"turn", turnIdx,
		"role", scenarioTurn.Role,
		"streaming", shouldStream)
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
		ce.executeStreamingConversation(ctx, req, outChan)
	}()

	return outChan, nil
}

// executeStreamingConversation handles the main streaming conversation logic
func (ce *DefaultConversationExecutor) executeStreamingConversation(ctx context.Context, req ConversationRequest, outChan chan<- ConversationStreamChunk) {
	result := ce.initializeStreamResult(req)

	for turnIdx, scenarioTurn := range req.Scenario.Turns {
		if !ce.executeStreamingTurnAndSendChunks(ctx, req, turnIdx, scenarioTurn, result, outChan) {
			return // Error occurred, goroutine should exit
		}
	}

	ce.sendFinalStreamResult(req, outChan)
}

// initializeStreamResult creates the initial conversation result for streaming
func (ce *DefaultConversationExecutor) initializeStreamResult(req ConversationRequest) *ConversationResult {
	return &ConversationResult{
		Messages: []types.Message{},
		Cost:     types.CostInfo{},
		SelfPlay: ce.containsSelfPlay(req.Scenario),
	}
}

// executeStreamingTurnAndSendChunks executes a turn and sends chunks to output channel
func (ce *DefaultConversationExecutor) executeStreamingTurnAndSendChunks(
	ctx context.Context,
	req ConversationRequest,
	turnIdx int,
	scenarioTurn config.TurnDefinition,
	result *ConversationResult,
	outChan chan<- ConversationStreamChunk,
) bool {
	turnReq := ce.buildTurnRequest(req, scenarioTurn)

	stream, err := ce.getStreamForRole(ctx, turnReq, scenarioTurn)
	if err != nil {
		ce.sendErrorChunk(outChan, result, err)
		return false
	}

	return ce.consumeStreamAndSendChunks(stream, turnIdx, result, outChan)
}

// getStreamForRole gets the appropriate stream based on the turn's role
func (ce *DefaultConversationExecutor) getStreamForRole(
	ctx context.Context,
	turnReq turnexecutors.TurnRequest,
	scenarioTurn config.TurnDefinition,
) (<-chan turnexecutors.MessageStreamChunk, error) {
	if ce.isSelfPlayRole(scenarioTurn.Role) {
		turnReq.SelfPlayRole = scenarioTurn.Role
		turnReq.SelfPlayPersona = scenarioTurn.Persona
		return ce.selfPlayExecutor.ExecuteTurnStream(ctx, turnReq)
	}

	if scenarioTurn.Role == "user" {
		turnReq.ScriptedContent = scenarioTurn.Content
		return ce.scriptedExecutor.ExecuteTurnStream(ctx, turnReq)
	}

	return nil, fmt.Errorf(errUnsupportedRole, scenarioTurn.Role)
}

// consumeStreamAndSendChunks consumes a turn stream and sends chunks to output
func (ce *DefaultConversationExecutor) consumeStreamAndSendChunks(
	stream <-chan turnexecutors.MessageStreamChunk,
	turnIdx int,
	result *ConversationResult,
	outChan chan<- ConversationStreamChunk,
) bool {
	for chunk := range stream {
		if chunk.Error != nil {
			ce.sendTurnErrorChunk(outChan, turnIdx, result, chunk.Error)
			return false
		}

		ce.sendTurnChunk(outChan, turnIdx, chunk, result)
	}
	return true
}

// sendErrorChunk sends an error chunk to the output channel
func (ce *DefaultConversationExecutor) sendErrorChunk(outChan chan<- ConversationStreamChunk, result *ConversationResult, err error) {
	outChan <- ConversationStreamChunk{
		Result: result,
		Error:  err,
	}
}

// sendTurnErrorChunk sends a turn-specific error chunk to the output channel
func (ce *DefaultConversationExecutor) sendTurnErrorChunk(outChan chan<- ConversationStreamChunk, turnIdx int, result *ConversationResult, err error) {
	outChan <- ConversationStreamChunk{
		TurnIndex: turnIdx,
		Result:    result,
		Error:     err,
	}
}

// sendTurnChunk sends a regular turn chunk to the output channel
func (ce *DefaultConversationExecutor) sendTurnChunk(outChan chan<- ConversationStreamChunk, turnIdx int, chunk turnexecutors.MessageStreamChunk, result *ConversationResult) {
	outChan <- ConversationStreamChunk{
		TurnIndex:    turnIdx,
		Delta:        chunk.Delta,
		TokenCount:   chunk.TokenCount,
		FinishReason: chunk.FinishReason,
		Result:       result,
	}
}

// sendFinalStreamResult sends the final result with complete conversation state
func (ce *DefaultConversationExecutor) sendFinalStreamResult(req ConversationRequest, outChan chan<- ConversationStreamChunk) {
	finalResult := ce.buildResultFromStateStore(req)
	outChan <- ConversationStreamChunk{
		Result: finalResult,
	}
}

// buildResultFromStateStore loads the final conversation state from StateStore and builds the result
func (ce *DefaultConversationExecutor) buildResultFromStateStore(req ConversationRequest) *ConversationResult {
	_, messages, err := ce.loadMessagesFromStateStore(req)
	if err != nil {
		return ce.createErrorResult(err.Error())
	}

	totalCost, toolStats := ce.calculateTotalsFromMessages(messages)
	isSelfPlay, personaID := ce.extractSelfPlayInfo(req.Scenario)

	return &ConversationResult{
		Messages:   messages,
		Cost:       totalCost,
		ToolStats:  toolStats,
		Violations: []types.ValidationError{},
		SelfPlay:   isSelfPlay,
		PersonaID:  personaID,
	}
}

// loadMessagesFromStateStore loads messages from the state store
func (ce *DefaultConversationExecutor) loadMessagesFromStateStore(req ConversationRequest) (statestore.Store, []types.Message, error) {
	if req.StateStoreConfig == nil || req.StateStoreConfig.Store == nil {
		return nil, nil, errors.New("StateStore is required but not configured")
	}

	store, ok := req.StateStoreConfig.Store.(statestore.Store)
	if !ok {
		return nil, nil, errors.New("invalid StateStore implementation")
	}

	state, err := store.Load(context.Background(), req.ConversationID)
	if err != nil && !errors.Is(err, statestore.ErrNotFound) {
		return nil, nil, fmt.Errorf("failed to load conversation from StateStore: %v", err)
	}

	var messages []types.Message
	if state != nil {
		messages = state.Messages
	}

	return store, messages, nil
}

// createErrorResult creates a ConversationResult for error cases
func (ce *DefaultConversationExecutor) createErrorResult(errorMsg string) *ConversationResult {
	return &ConversationResult{
		Messages: []types.Message{},
		Cost:     types.CostInfo{},
		Failed:   true,
		Error:    errorMsg,
	}
}

// calculateTotalsFromMessages calculates cost and tool statistics from messages
func (ce *DefaultConversationExecutor) calculateTotalsFromMessages(messages []types.Message) (types.CostInfo, *types.ToolStats) {
	var totalCost types.CostInfo
	toolStats := &types.ToolStats{
		TotalCalls: 0,
		ByTool:     make(map[string]int),
	}

	for _, msg := range messages {
		ce.aggregateMessageCost(&totalCost, msg.CostInfo)
		ce.aggregateToolStats(toolStats, msg.ToolCalls)
	}

	if toolStats.TotalCalls == 0 {
		toolStats = nil
	}

	return totalCost, toolStats
}

// aggregateMessageCost adds message cost information to the total
func (ce *DefaultConversationExecutor) aggregateMessageCost(totalCost *types.CostInfo, msgCost *types.CostInfo) {
	if msgCost == nil {
		return
	}

	totalCost.InputTokens += msgCost.InputTokens
	totalCost.OutputTokens += msgCost.OutputTokens
	totalCost.CachedTokens += msgCost.CachedTokens
	totalCost.InputCostUSD += msgCost.InputCostUSD
	totalCost.OutputCostUSD += msgCost.OutputCostUSD
	totalCost.CachedCostUSD += msgCost.CachedCostUSD
	totalCost.TotalCost += msgCost.TotalCost
}

// aggregateToolStats counts tool calls from the message
func (ce *DefaultConversationExecutor) aggregateToolStats(toolStats *types.ToolStats, toolCalls []types.MessageToolCall) {
	for _, tc := range toolCalls {
		toolStats.TotalCalls++
		toolStats.ByTool[tc.Name]++
	}
}

// extractSelfPlayInfo detects self-play and extracts persona ID
func (ce *DefaultConversationExecutor) extractSelfPlayInfo(scenario *config.Scenario) (bool, string) {
	isSelfPlay := ce.containsSelfPlay(scenario)
	var personaID string

	if isSelfPlay {
		personaID = ce.findFirstSelfPlayPersona(scenario)
	}

	return isSelfPlay, personaID
}

// findFirstSelfPlayPersona finds the persona ID from the first self-play turn
func (ce *DefaultConversationExecutor) findFirstSelfPlayPersona(scenario *config.Scenario) string {
	for _, turn := range scenario.Turns {
		if ce.isSelfPlayRole(turn.Role) && turn.Persona != "" {
			return turn.Persona
		}
	}
	return ""
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
