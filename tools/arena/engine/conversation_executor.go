package engine

// import block above

import (
	"context"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/composition"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	asrt "github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
	"github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
)

const (
	errUnsupportedRole = "unsupported role: %s"
	roleUser           = "user"
)

// DefaultConversationExecutor implements ConversationExecutor interface
type DefaultConversationExecutor struct {
	scriptedExecutor turnexecutors.TurnExecutor
	selfPlayExecutor turnexecutors.TurnExecutor
	selfPlayRegistry *selfplay.Registry
	promptRegistry   *prompt.Registry
	evalOrchestrator *EvalOrchestrator
}

// NewDefaultConversationExecutor creates a new conversation executor
func NewDefaultConversationExecutor(
	scriptedExecutor turnexecutors.TurnExecutor,
	selfPlayExecutor turnexecutors.TurnExecutor,
	selfPlayRegistry *selfplay.Registry,
	promptRegistry *prompt.Registry,
	evalOrchestrator *EvalOrchestrator,
) *DefaultConversationExecutor {
	return &DefaultConversationExecutor{
		scriptedExecutor: scriptedExecutor,
		selfPlayExecutor: selfPlayExecutor,
		selfPlayRegistry: selfPlayRegistry,
		promptRegistry:   promptRegistry,
		evalOrchestrator: evalOrchestrator,
	}
}

// ExecuteConversation runs a complete conversation based on scenario using the new Turn model
func (ce *DefaultConversationExecutor) ExecuteConversation(ctx context.Context, req ConversationRequest) *ConversationResult {
	if req.Scenario == nil {
		return &ConversationResult{
			Error:  fmt.Sprintf("scenario is nil for conversation %s", req.ConversationID),
			Failed: true,
		}
	}

	// Enrich context with scenario and session information for structured logging
	ctx = logger.WithLoggingContext(ctx, &logger.LoggingFields{
		Scenario:  req.Scenario.ID,
		SessionID: req.ConversationID,
		Stage:     "execution",
	})

	var emitter *events.Emitter
	if req.EventBus != nil {
		emitter = events.NewEmitter(req.EventBus, req.RunID, req.RunID, req.ConversationID)
	}
	// Check if scenario uses streaming - if so, use streaming path
	if req.Scenario.Streaming {
		// Any turn uses streaming, use streaming executor
		return ce.executeWithStreaming(ctx, &req, emitter)
	}

	// Use non-streaming execution
	return ce.executeWithoutStreaming(ctx, &req, emitter)
}

// executeWithoutStreaming runs conversation without streaming (original implementation)
func (ce *DefaultConversationExecutor) executeWithoutStreaming(
	ctx context.Context,
	req *ConversationRequest,
	emitter *events.Emitter,
) *ConversationResult {
	// Execute each turn in the scenario
	for turnIdx, scenarioTurn := range req.Scenario.Turns {
		// Enrich context with turn information
		turnCtx := logger.WithTurnID(ctx, fmt.Sprintf("turn-%d", turnIdx))
		if req.ContextEnricher != nil {
			turnCtx = req.ContextEnricher(turnCtx)
		}

		ce.notifyTurnStarted(emitter, turnIdx, scenarioTurn.Role, req.Scenario.ID)
		ce.debugOnUserTurnAssertions(scenarioTurn, turnIdx)

		// Issue #1374: capture the live workflow state at TURN START (the state
		// whose prompt drives this turn) and the current assistant-message count,
		// so after the turn succeeds we can stamp current_workflow_state onto the
		// new assistant message — and only when one was actually produced.
		var turnStartState map[string]interface{}
		var preAssistantCount int
		if req.CurrentWorkflowState != nil && req.StampWorkflowState != nil {
			turnStartState = req.CurrentWorkflowState()
			preAssistantCount = ce.countAssistantMessages(ctx, req)
		}

		err := ce.executeNonStreamingTurn(turnCtx, req, scenarioTurn)

		if err != nil {
			logger.Error("Turn execution failed",
				"turn", turnIdx,
				"role", scenarioTurn.Role,
				"error", err)
			ce.notifyTurnCompleted(emitter, turnIdx, scenarioTurn.Role, req.Scenario.ID, err)

			// Load messages from StateStore (they were saved before validation failed)
			result := ce.buildResultFromStateStore(ctx, req)
			result.Failed = true
			result.Error = err.Error()
			return result
		}

		// Commit deferred workflow transitions after the pipeline completes
		if req.PostTurnHook != nil {
			if hookErr := req.PostTurnHook(); hookErr != nil {
				logger.Error("Post-turn hook failed", "turn", turnIdx, "error", hookErr)
			}
		}

		// Issue #1374: stamp the turn-start workflow state onto the assistant
		// message this turn produced. Order vs PostTurnHook is irrelevant —
		// turnStartState was captured before the turn ran. Skip plain user turns
		// that produced no new assistant message. Side-effect-only.
		if req.StampWorkflowState != nil && ce.countAssistantMessages(ctx, req) > preAssistantCount {
			req.StampWorkflowState(turnStartState)
		}

		ce.notifyTurnCompleted(emitter, turnIdx, scenarioTurn.Role, req.Scenario.ID, nil)
		logger.Debug("Turn completed",
			"turn", turnIdx,
			"role", scenarioTurn.Role)
	}

	// Load final conversation state from StateStore
	return ce.buildResultFromStateStore(ctx, req)
}

// countAssistantMessages returns the number of assistant messages currently
// persisted for the conversation. Used by the issue #1374 stamp guard to detect
// whether a turn actually produced a new assistant message. Returns 0 on any
// load error (the guard then simply skips stamping).
func (ce *DefaultConversationExecutor) countAssistantMessages(ctx context.Context, req *ConversationRequest) int {
	_, messages, err := ce.loadMessagesFromStateStore(ctx, req)
	if err != nil {
		return 0
	}
	count := 0
	for i := range messages {
		if messages[i].Role == roleAssistant {
			count++
		}
	}
	return count
}

// executeNonStreamingTurn executes a single turn without streaming
func (ce *DefaultConversationExecutor) executeNonStreamingTurn(
	ctx context.Context,
	req *ConversationRequest,
	scenarioTurn config.TurnDefinition,
) error {
	turnReq := ce.buildTurnRequest(*req, scenarioTurn)

	if ce.isSelfPlayRole(scenarioTurn.Role) {
		return ce.executeNonStreamingSelfPlayTurn(ctx, turnReq, scenarioTurn)
	}

	if scenarioTurn.Role == roleUser {
		return ce.scriptedExecutor.ExecuteTurn(ctx, turnReq)
	}

	return fmt.Errorf(errUnsupportedRole, scenarioTurn.Role)
}

// executeNonStreamingSelfPlayTurn executes self-play turns without streaming
func (ce *DefaultConversationExecutor) executeNonStreamingSelfPlayTurn(
	ctx context.Context,
	turnReq turnexecutors.TurnRequest,
	scenarioTurn config.TurnDefinition,
) error {
	minTurns, maxTurns, naturalTermination := selfPlayTurnBounds(&scenarioTurn)

	turnReq.SelfPlayRole = scenarioTurn.Role
	turnReq.SelfPlayPersona = scenarioTurn.Persona
	if naturalTermination {
		if turnReq.Metadata == nil {
			turnReq.Metadata = make(map[string]interface{})
		}
		turnReq.Metadata["natural_termination_enabled"] = true
	}

	for i := 0; i < maxTurns; i++ {
		err := ce.selfPlayExecutor.ExecuteTurn(ctx, turnReq)
		if done, loopErr := handleCompletionError(err, i, minTurns, maxTurns); done {
			return loopErr
		}
	}
	return nil
}

// executeWithStreaming runs conversation with streaming enabled, using per-turn overrides
func (ce *DefaultConversationExecutor) executeWithStreaming(
	ctx context.Context,
	req *ConversationRequest,
	emitter *events.Emitter,
) *ConversationResult {
	for turnIdx, scenarioTurn := range req.Scenario.Turns {
		// Enrich context with turn information
		turnCtx := logger.WithTurnID(ctx, fmt.Sprintf("turn-%d", turnIdx))
		turnCtx = logger.WithStage(turnCtx, "streaming")

		ce.notifyTurnStarted(emitter, turnIdx, scenarioTurn.Role, req.Scenario.ID)

		var err error
		if ce.isSelfPlayRole(scenarioTurn.Role) {
			err = ce.executeStreamingSelfPlayTurns(turnCtx, req, turnIdx, scenarioTurn)
		} else {
			err = ce.executeStreamingTurn(turnCtx, *req, turnIdx, scenarioTurn)
		}

		if err != nil {
			ce.notifyTurnCompleted(emitter, turnIdx, scenarioTurn.Role, req.Scenario.ID, err)
			return ce.handleTurnExecutionError(ctx, req, err, turnIdx, scenarioTurn)
		}

		ce.notifyTurnCompleted(emitter, turnIdx, scenarioTurn.Role, req.Scenario.ID, nil)
		ce.logTurnCompletion(turnIdx, scenarioTurn, req.Scenario.ShouldStreamTurn(turnIdx))
	}

	return ce.buildResultFromStateStore(ctx, req)
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
	if scenarioTurn.Role == roleUser && len(scenarioTurn.Assertions) > 0 {
		logger.Debug("Assertions on user turn will validate next assistant response",
			"turn", turnIdx)
	}
}

func (ce *DefaultConversationExecutor) notifyTurnStarted(
	emitter *events.Emitter, turnIdx int, role, scenarioID string,
) {
	if emitter != nil {
		emitter.EmitCustom(
			events.EventType("arena.turn.started"),
			"ArenaEngine",
			"turn_started",
			map[string]interface{}{
				"turn_index": turnIdx,
				"role":       role,
				"scenario":   scenarioID,
			},
			fmt.Sprintf("Turn %d started", turnIdx),
		)
	}
}

func (ce *DefaultConversationExecutor) notifyTurnCompleted(
	emitter *events.Emitter, turnIdx int, role, scenarioID string, err error,
) {
	if emitter != nil {
		eventType := events.EventType("arena.turn.completed")
		eventName := "turn_completed"
		if err != nil {
			eventType = events.EventType("arena.turn.failed")
			eventName = "turn_failed"
		}
		emitter.EmitCustom(
			eventType,
			"ArenaEngine",
			eventName,
			map[string]interface{}{
				"turn_index": turnIdx,
				"role":       role,
				"scenario":   scenarioID,
				"error":      err,
			},
			fmt.Sprintf("Turn %d completed", turnIdx),
		)
	}
}

// buildTurnRequest creates a TurnRequest from the conversation request and scenario turn
func (ce *DefaultConversationExecutor) buildTurnRequest(req ConversationRequest, scenarioTurn config.TurnDefinition) turnexecutors.TurnRequest {
	baseDir := ""
	metadata := make(map[string]interface{})
	if req.Config != nil {
		baseDir = req.Config.ConfigDir
		attachJudgeMetadata(metadata, req.Config)
	}
	if ce.promptRegistry != nil {
		metadata["prompt_registry"] = ce.promptRegistry
	}
	// Determine temperature: use override > config default (if set) > provider default (via 0)
	temperature := float64(req.Config.Defaults.Temperature)
	if req.Temperature != nil {
		temperature = *req.Temperature
	} else if req.Config.Defaults.Temperature == 0 {
		// Config has no temperature set, pass 0 to let provider use its default
		temperature = 0
	}

	// Determine max_tokens: use override > config default (if set) > provider default (via 0)
	maxTokens := req.Config.Defaults.MaxTokens
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	} else if req.Config.Defaults.MaxTokens == 0 {
		// Config has no max_tokens set, pass 0 to let provider use its default
		maxTokens = 0
	}

	// Build template vars: arena.yaml prompt-config vars (matched by task_type)
	// overlaid with the scenario's own Variables block (scenario wins). Issue
	// #1292: Scenario.Variables was previously ignored here. Copy into a fresh
	// map so the shared LoadedPromptConfigs vars are never mutated.
	var promptVars map[string]string
	if req.Config != nil && req.Scenario != nil {
		for _, promptConfigData := range req.Config.LoadedPromptConfigs {
			if promptConfigData.TaskType == req.Scenario.TaskType {
				promptVars = make(map[string]string, len(promptConfigData.Vars))
				for k, v := range promptConfigData.Vars {
					promptVars[k] = v
				}
				break
			}
		}
	}
	if scenarioVars := scenarioVariables(req.Scenario); len(scenarioVars) > 0 {
		if promptVars == nil {
			promptVars = make(map[string]string, len(scenarioVars))
		}
		for k, v := range scenarioVars {
			promptVars[k] = v
		}
	}

	// Resolve the active composition for the current workflow state (RFC 0010).
	// Non-workflow turns and non-composition states leave this nil.
	var activeComposition *composition.Composition
	if req.ActiveCompositionResolver != nil {
		activeComposition = req.ActiveCompositionResolver()
	}

	// RFC 0010 Task 5: reset the per-run composition recorder at the start of
	// each turn so stale outputs from the previous turn do not bleed into the
	// current turn's assertion context. Nil-safe: non-composition runs have no
	// recorder set on the ConversationRequest.
	if req.CompositionRecorder != nil {
		req.CompositionRecorder.Reset()
	}

	return turnexecutors.TurnRequest{
		Provider:            req.Provider,
		Scenario:            req.Scenario,
		PromptRegistry:      ce.promptRegistry,
		TaskType:            req.Scenario.TaskType,
		Region:              req.Region,
		PromptVars:          promptVars,
		BaseDir:             baseDir,
		Temperature:         temperature,
		MaxTokens:           maxTokens,
		Seed:                &req.Config.Defaults.Seed,
		StateStoreConfig:    convertStateStoreConfig(req.StateStoreConfig),
		ConversationID:      req.ConversationID,
		RunID:               req.RunID,
		EventBus:            req.EventBus,
		ScriptedContent:     scenarioTurn.Content, // Legacy text content (for backward compatibility)
		ScriptedParts:       scenarioTurn.Parts,   // Multimodal content parts (takes precedence over ScriptedContent)
		ConsentOverrides:    scenarioTurn.ConsentOverrides,
		ChaosConfig:         scenarioTurn.Chaos,
		Assertions:          scenarioTurn.Assertions,
		TurnEvalRunner:      ce.resolveEvalOrchestrator(&req),
		RecordingConfig:     req.RecordingConfig,
		EventStore:          req.EventStore,
		AudioRouter:         req.AudioRouter,
		Metadata:            metadata,
		ActiveComposition:   activeComposition,
		CompositionRecorder: req.CompositionRecorder,
	}
}

// resolveEvalOrchestrator returns the per-run orchestrator from the request if set,
// falling back to the shared orchestrator. This supports per-run workflow metadata
// without data races on the shared orchestrator.
func (ce *DefaultConversationExecutor) resolveEvalOrchestrator(req *ConversationRequest) *EvalOrchestrator {
	if req.EvalOrchestrator != nil {
		return req.EvalOrchestrator
	}
	return ce.evalOrchestrator
}

// executeTurnByRole executes a turn based on its role (self-play or scripted)
func (ce *DefaultConversationExecutor) executeTurnByRole(ctx context.Context, turnReq turnexecutors.TurnRequest, scenarioTurn config.TurnDefinition, shouldStream bool) error {
	if ce.isSelfPlayRole(scenarioTurn.Role) {
		return ce.executeSelfPlayTurn(ctx, turnReq, scenarioTurn, shouldStream)
	}

	if scenarioTurn.Role == roleUser {
		return ce.executeScriptedTurn(ctx, turnReq, scenarioTurn, shouldStream)
	}

	return fmt.Errorf(errUnsupportedRole, scenarioTurn.Role)
}

// executeStreamingSelfPlayTurns handles multi-turn self-play with natural termination in streaming mode.
//
//nolint:gocritic // scenarioTurn value receiver matches range variable from caller
func (ce *DefaultConversationExecutor) executeStreamingSelfPlayTurns(
	ctx context.Context,
	req *ConversationRequest,
	turnIdx int,
	scenarioTurn config.TurnDefinition,
) error {
	minTurns, maxTurns, naturalTermination := selfPlayTurnBounds(&scenarioTurn)
	shouldStream := req.Scenario.ShouldStreamTurn(turnIdx)

	for i := 0; i < maxTurns; i++ {
		turnReq := ce.buildSelfPlayTurnRequest(req, &scenarioTurn, naturalTermination)

		var err error
		if shouldStream {
			err = ce.executeTurnWithStreaming(ctx, turnReq, ce.selfPlayExecutor)
		} else {
			err = ce.selfPlayExecutor.ExecuteTurn(ctx, turnReq)
		}

		if done, loopErr := handleCompletionError(err, i, minTurns, maxTurns); done {
			return loopErr
		}
	}
	return nil
}

// selfPlayTurnBounds computes the min/max turn counts and whether natural termination is active.
func selfPlayTurnBounds(turn *config.TurnDefinition) (minTurns, maxTurns int, naturalTermination bool) {
	minTurns = turn.Turns
	if minTurns == 0 {
		minTurns = 1
	}
	maxTurns = minTurns
	if turn.MaxTurns > minTurns {
		maxTurns = turn.MaxTurns
	}
	naturalTermination = maxTurns > minTurns
	return
}

// buildSelfPlayTurnRequest creates a TurnRequest configured for self-play execution.
func (ce *DefaultConversationExecutor) buildSelfPlayTurnRequest(
	req *ConversationRequest,
	scenarioTurn *config.TurnDefinition,
	naturalTermination bool,
) turnexecutors.TurnRequest {
	turnReq := ce.buildTurnRequest(*req, *scenarioTurn)
	turnReq.SelfPlayRole = scenarioTurn.Role
	turnReq.SelfPlayPersona = scenarioTurn.Persona
	if naturalTermination {
		if turnReq.Metadata == nil {
			turnReq.Metadata = make(map[string]interface{})
		}
		turnReq.Metadata["natural_termination_enabled"] = true
	}
	return turnReq
}

// handleCompletionError processes an error from a self-play turn and determines
// whether the loop should exit. Returns (shouldExit, error).
func handleCompletionError(err error, turnIndex, minTurns, maxTurns int) (bool, error) {
	if errors.Is(err, selfplay.ErrConversationComplete) {
		if turnIndex+1 >= minTurns {
			logger.Debug("Natural conversation termination",
				"turn", turnIndex+1, "min", minTurns, "max", maxTurns)
			return true, nil
		}
		return false, nil // Below minimum — continue
	}
	if err != nil {
		return true, err
	}
	return false, nil
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
//
//nolint:gocritic // scenarioTurn value receiver matches range variable from caller
func (ce *DefaultConversationExecutor) handleTurnExecutionError(
	ctx context.Context, req *ConversationRequest, err error, turnIdx int, scenarioTurn config.TurnDefinition,
) *ConversationResult {
	result := ce.buildResultFromStateStore(ctx, req)

	if providers.IsTransient(err) {
		logger.Warn("Turn skipped (transient provider error)",
			"turn", turnIdx,
			"role", scenarioTurn.Role,
			"error", err)
		result.Failed = false
		result.Error = ""
		result.Skipped = true
		result.SkipReason = err.Error()
	} else {
		logger.Error("Turn execution failed",
			"turn", turnIdx,
			"role", scenarioTurn.Role,
			"streaming", req.Scenario.ShouldStreamTurn(turnIdx),
			"error", err)
		result.Failed = true
		result.Error = err.Error()
	}

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

	ce.sendFinalStreamResult(ctx, &req, outChan)
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

	if scenarioTurn.Role == roleUser {
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
func (ce *DefaultConversationExecutor) sendFinalStreamResult(
	ctx context.Context, req *ConversationRequest, outChan chan<- ConversationStreamChunk,
) {
	finalResult := ce.buildResultFromStateStore(ctx, req)
	outChan <- ConversationStreamChunk{
		Result: finalResult,
	}
}

// attachJudgeMetadata injects judge targets/defaults into metadata map as plain structs to avoid cycles.
func attachJudgeMetadata(metadata map[string]interface{}, cfg *config.Config) {
	if cfg == nil {
		return
	}
	if len(cfg.LoadedJudges) > 0 {
		targets := make(map[string]providers.ProviderSpec, len(cfg.LoadedJudges))
		for name, jt := range cfg.LoadedJudges {
			if jt == nil || jt.Provider == nil {
				continue
			}
			targets[name] = providerSpecFromConfig(jt.Provider)
		}
		if len(targets) > 0 {
			metadata["judge_targets"] = targets
		}
	}
	if cfg.JudgeDefaults != nil {
		metadata["judge_defaults"] = map[string]interface{}{
			"prompt":          cfg.JudgeDefaults.Prompt,
			"prompt_registry": cfg.JudgeDefaults.PromptRegistry,
		}
	}
}

// providerSpecFromConfig converts config.Provider to providers.ProviderSpec.
func providerSpecFromConfig(p *config.Provider) providers.ProviderSpec {
	return providers.ProviderSpec{
		ID:               p.ID,
		Type:             p.Type,
		Model:            p.Model,
		BaseURL:          p.BaseURL,
		Headers:          p.Headers,
		IncludeRawOutput: p.IncludeRawOutput,
		AdditionalConfig: p.AdditionalConfig,
		Defaults: providers.ProviderDefaults{
			Temperature: p.Defaults.Temperature,
			TopP:        p.Defaults.TopP,
			MaxTokens:   p.Defaults.MaxTokens,
			Pricing: providers.Pricing{
				InputCostPer1K:  p.Pricing.InputCostPer1K,
				OutputCostPer1K: p.Pricing.OutputCostPer1K,
			},
		},
	}
}

// buildResultFromStateStore loads the final conversation state from StateStore and builds the result
func (ce *DefaultConversationExecutor) buildResultFromStateStore(
	ctx context.Context, req *ConversationRequest,
) *ConversationResult {
	_, messages, err := ce.loadMessagesFromStateStore(ctx, req)
	if err != nil {
		return ce.createErrorResult(err.Error())
	}

	totalCost, toolStats := ce.calculateTotalsFromMessages(messages)
	isSelfPlay, personaID := ce.extractSelfPlayInfo(req.Scenario)

	// Collect media outputs from assistant messages
	mediaOutputs := CollectMediaOutputs(messages)

	// Evaluate conversation-level assertions, if any
	convAssertionResults := ce.evaluateConversationAssertions(ctx, req, messages)

	// Run pack-level evals at session end. These are non-gating
	// observations — same machinery that fires in production. They
	// land on EvalResults (raw runtime EvalResult), not in the
	// assertion bucket, so the report can render them without
	// pretending they have pass/fail semantics.
	var packEvalResults []evals.EvalResult
	orch := ce.resolveEvalOrchestrator(req)
	if orch != nil && orch.HasEvals() {
		packEvalResults = orch.RunSessionEvals(ctx, messages, req.ConversationID)
	}

	return &ConversationResult{
		Messages:                     messages,
		Cost:                         totalCost,
		ToolStats:                    toolStats,
		Violations:                   []types.ValidationError{},
		SelfPlay:                     isSelfPlay,
		PersonaID:                    personaID,
		MediaOutputs:                 mediaOutputs,
		ConversationAssertionResults: convAssertionResults,
		EvalResults:                  packEvalResults,
	}
}

// globalConversationAssertions returns the arena-level conversation
// assertions configured under spec.globals.conversation_assertions.
// These apply to every scenario in addition to its own assertions.
// Returns nil when no globals are set.
func globalConversationAssertions(cfg *config.Config) []asrt.AssertionConfig {
	if cfg == nil || cfg.Globals == nil {
		return nil
	}
	return cfg.Globals.ConversationAssertions
}

// mergeAssertionConfigs merges arena-level globals with source-level
// (scenario / eval) assertions into a single slice. Globals come first
// so per-scenario assertions can build on them.
func mergeAssertionConfigs(cfg *config.Config, sourceAssertions []asrt.AssertionConfig) []asrt.AssertionConfig {
	globals := globalConversationAssertions(cfg)
	if len(globals) == 0 && len(sourceAssertions) == 0 {
		return nil
	}
	merged := make([]asrt.AssertionConfig, 0, len(globals)+len(sourceAssertions))
	merged = append(merged, globals...)
	merged = append(merged, sourceAssertions...)
	return merged
}

// collectConversationAssertions merges arena-level globals with
// source-level (scenario or eval) assertions into a single slice of
// ConversationAssertion. Globals come first, followed by source.
func collectConversationAssertions(
	cfg *config.Config, sourceAssertions []asrt.AssertionConfig,
) []asrt.ConversationAssertion {
	globals := globalConversationAssertions(cfg)
	if len(globals) == 0 && len(sourceAssertions) == 0 {
		return nil
	}
	assertions := make([]asrt.ConversationAssertion, 0, len(globals)+len(sourceAssertions))
	for _, a := range globals {
		assertions = append(assertions, asrt.ConversationAssertion(a))
	}
	for _, a := range sourceAssertions {
		assertions = append(assertions, asrt.ConversationAssertion(a))
	}
	return assertions
}

// evaluateConversationAssertions evaluates scenario-level conversation assertions after all turns complete.
// Uses the eval-only path via EvalOrchestrator.
func (ce *DefaultConversationExecutor) evaluateConversationAssertions(
	ctx context.Context,
	req *ConversationRequest,
	messages []types.Message,
) []asrt.ConversationValidationResult {
	// Collect conversation assertions from pack + scenario (evaluated after conversation completes)
	var scenarioAssertions []asrt.AssertionConfig
	if req.Scenario != nil {
		scenarioAssertions = req.Scenario.ConversationAssertions
	}
	assertionConfigs := mergeAssertionConfigs(req.Config, scenarioAssertions)

	scenarioID := ""
	if req.Scenario != nil {
		scenarioID = req.Scenario.ID
	}
	logger.Debug("Evaluating conversation assertions",
		"scenario_id", scenarioID,
		"assertion_count", len(assertionConfigs))

	if len(assertionConfigs) == 0 {
		logger.Debug("No conversation assertions to evaluate")
		return nil
	}

	orch := ce.resolveEvalOrchestrator(req)
	if orch == nil {
		logger.Warn("Assertions defined but eval runner not configured — marking all as failed",
			"assertion_count", len(assertionConfigs))
		results := make([]asrt.ConversationValidationResult, len(assertionConfigs))
		for i, ac := range assertionConfigs {
			results[i] = asrt.ConversationValidationResult{
				Type:    ac.Type,
				Passed:  false,
				Message: ac.Message,
				Details: map[string]interface{}{"error": "eval runner not configured"},
			}
		}
		return results
	}

	// Run assertions through the eval pipeline and convert to ConversationValidationResult
	results := orch.RunAssertionsAsConversationResults(
		ctx, assertionConfigs, messages,
		len(messages)-1, req.ConversationID,
		evals.TriggerOnConversationComplete,
	)

	logger.Debug("Conversation assertion results",
		"result_count", len(results),
		"results", results)

	return results
}

// buildConversationContext constructs a conversation context from messages and request metadata.
func buildConversationContext(
	req *ConversationRequest,
	messages []types.Message,
	promptRegistry *prompt.Registry,
) *asrt.ConversationContext {
	meta := &asrt.ConversationMetadata{
		ScenarioID:     req.Scenario.ID,
		PromptConfigID: req.Scenario.TaskType,
		Extras:         buildMetadataExtras(req, promptRegistry),
	}
	return asrt.BuildConversationContextFromMessages(messages, meta)
}

func buildMetadataExtras(req *ConversationRequest, promptRegistry *prompt.Registry) map[string]interface{} {
	extras := make(map[string]interface{})
	if req.Config != nil {
		attachJudgeMetadata(extras, req.Config)
	}
	if promptRegistry != nil {
		extras["prompt_registry"] = promptRegistry
	}
	if len(extras) == 0 {
		return nil
	}
	return extras
}

// loadMessagesFromStateStore loads messages from the state store
func (ce *DefaultConversationExecutor) loadMessagesFromStateStore(
	ctx context.Context, req *ConversationRequest,
) (statestore.Store, []types.Message, error) {
	if req.StateStoreConfig == nil || req.StateStoreConfig.Store == nil {
		return nil, nil, errors.New("StateStore is required but not configured")
	}

	store, ok := req.StateStoreConfig.Store.(statestore.Store)
	if !ok {
		return nil, nil, errors.New("invalid StateStore implementation")
	}

	state, err := store.Load(ctx, req.ConversationID)
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

	for i := range messages {
		ce.aggregateMessageCost(&totalCost, messages[i].CostInfo)
		// Self-play user turns store their LLM spend in Meta —
		// folded into the same total so it shows up in the headline
		// cost rather than hiding in per-turn metadata.
		addSelfPlayCostFromMeta(&totalCost, messages[i].Meta)
		ce.aggregateToolStats(toolStats, messages[i].ToolCalls)
	}

	if toolStats.TotalCalls == 0 {
		toolStats = nil
	}

	return totalCost, toolStats
}

// aggregateMessageCost adds message cost information to the total. The
// run-level Breakdown is built from messages by statestore.computeTotalCost
// (which is what surfaces in the JSON report); this engine-side rollup is
// kept lean.
func (ce *DefaultConversationExecutor) aggregateMessageCost(totalCost, msgCost *types.CostInfo) {
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
