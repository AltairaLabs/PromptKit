package engine

// This file contains integration-level code that requires full pipeline
// infrastructure to test. It is excluded from per-file coverage checks.

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
	arenaassertions "github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
	arenastages "github.com/AltairaLabs/PromptKit/tools/arena/stages"
	arenastore "github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
)

const (
	// Audio configuration constants
	geminiAudioBitDepth = 16 // Required for Gemini Live API

	// Default timing constants - can be overridden via scenario.duplex.resilience config
	defaultInterTurnDelayMS         = 500
	defaultSelfplayInterTurnDelayMS = 1000
	defaultRetryDelayMS             = 1000
	defaultMaxRetries               = 0
	defaultPartialSuccessMinTurns   = 1
	defaultIgnoreLastTurnSessionEnd = true
	drainTimeoutSec                 = 30

	// Role constants
	roleAssistant = "assistant"
)

// errPartialSuccess is returned when a duplex conversation ends early but enough
// turns have completed to consider it a partial success. This is not a failure.
var errPartialSuccess = errors.New("partial success")

// responseAction indicates how a response element should be handled.
type responseAction int

const (
	// responseActionContinue means the element was informational (interruption signal),
	// and we should continue waiting for the final response.
	responseActionContinue responseAction = iota
	// responseActionComplete means we received a complete response.
	responseActionComplete
	// responseActionError means an error occurred or the response was empty.
	responseActionError
)

// processResponseElement handles a response element from the pipeline, determining
// the appropriate action based on interruption signals, turn completion, and errors.
//
// This consolidates the response handling logic that was duplicated in
// streamAudioChunks and streamSelfPlayAudio.
//
// Returns:
//   - responseAction: what action to take
//   - error: any error to return (only set when action is responseActionError)
func processResponseElement(elem *stage.StreamElement, logPrefix string) (responseAction, error) {
	// Check for errors
	if elem.Error != nil {
		return responseActionError, elem.Error
	}

	// Check for interruption signals - these are informational, keep waiting
	if elem.Metadata != nil {
		// Interruption signal: Gemini detected user started speaking during response.
		// The partial response has been captured, now waiting for the new response.
		if interrupted, ok := elem.Metadata["interrupted"].(bool); ok && interrupted {
			logger.Debug(logPrefix + ": response interrupted, waiting for new response")
			return responseActionContinue, nil
		}

		// Interrupted turn complete: Empty turnComplete after interruption.
		// This is just Gemini closing the interrupted turn, not the final response.
		if itc, ok := elem.Metadata["interrupted_turn_complete"].(bool); ok && itc {
			logger.Debug(logPrefix + ": interrupted turn complete, waiting for real response")
			return responseActionContinue, nil
		}
	}

	// Check for turn completion (EndOfStream from DuplexProviderStage)
	if elem.EndOfStream {
		logger.Debug(logPrefix+": EndOfStream received",
			"hasMessage", elem.Message != nil,
			"hasText", elem.Text != nil)

		// Check for empty response - shouldn't happen with proper interruption handling,
		// but serves as a safety net. Treat as retriable error.
		if elem.Message == nil || (elem.Message.Content == "" && len(elem.Message.Parts) == 0) {
			logger.Debug(logPrefix + ": empty response, treating as retriable error")
			return responseActionError, errors.New("empty response from Gemini, likely interrupted")
		}

		return responseActionComplete, nil
	}

	// Element doesn't require action (e.g., streaming text/audio chunk)
	return responseActionContinue, nil
}

// ExecuteConversationStream runs a duplex conversation with streaming output.
// For duplex mode, this returns chunks as they arrive from the provider.
func (de *DuplexConversationExecutor) ExecuteConversationStream(
	ctx context.Context,
	req ConversationRequest, //nolint:gocritic // Interface compliance requires value receiver
) (<-chan ConversationStreamChunk, error) {
	outChan := make(chan ConversationStreamChunk)

	go func() {
		defer close(outChan)

		// For now, execute non-streaming and send final result
		// TODO: Implement true streaming with intermediate chunks
		result := de.ExecuteConversation(ctx, req)
		outChan <- ConversationStreamChunk{
			Result: result,
		}
	}()

	return outChan, nil
}

// executeDuplexConversation handles the main duplex conversation logic.
// The pipeline is the single source of truth: PromptAssemblyStage loads the prompt,
// then DuplexProviderStage creates the session using system_prompt from metadata.
//
// This method implements retry logic for recoverable errors (session drops, network issues).
// On failure, it waits for retry_delay_ms and creates a fresh pipeline/session.
func (de *DuplexConversationExecutor) executeDuplexConversation(
	ctx context.Context,
	req *ConversationRequest,
	streamProvider providers.StreamInputSupport,
	emitter *events.Emitter,
) *ConversationResult {
	de.emitSessionStarted(emitter, req)

	// Get retry configuration from scenario
	var resilience *config.DuplexResilienceConfig
	if req.Scenario != nil && req.Scenario.Duplex != nil {
		resilience = req.Scenario.Duplex.GetResilience()
	}
	maxRetries := resilience.GetMaxRetries(defaultMaxRetries)
	retryDelayMS := resilience.GetRetryDelayMs(defaultRetryDelayMS)

	var result *ConversationResult
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			logger.Info("Retrying duplex conversation",
				"attempt", attempt,
				"maxRetries", maxRetries,
				"retryDelayMs", retryDelayMS)
			time.Sleep(time.Duration(retryDelayMS) * time.Millisecond)

			// Clear state store for fresh retry
			if err := de.clearStateStoreForRetry(ctx, req); err != nil {
				logger.Warn("Failed to clear state store for retry", "error", err)
			}
		}

		// Build and execute the duplex pipeline
		// The session is created inside the pipeline by DuplexProviderStage,
		// using system_prompt from PromptAssemblyStage metadata.
		result = de.executeDuplexPipeline(ctx, req, streamProvider, emitter)

		// Check if we should retry
		if !result.Failed {
			// Success - no need to retry
			break
		}

		if !de.isRecoverableError(result.Error) {
			// Non-recoverable error - don't retry
			logger.Debug("Non-recoverable error, not retrying", "error", result.Error)
			break
		}

		if attempt < maxRetries {
			logger.Warn("Duplex conversation failed with recoverable error, will retry",
				"attempt", attempt+1,
				"maxRetries", maxRetries,
				"error", result.Error)
		}
	}

	de.emitSessionCompleted(emitter, req)
	return result
}

// isRecoverableError checks if an error is recoverable and should trigger a retry.
// Recoverable errors include session drops, network issues, and provider transient failures.
func (de *DuplexConversationExecutor) isRecoverableError(errMsg string) bool {
	recoverablePatterns := []string{
		"output channel closed unexpectedly",
		"session ended",
		"websocket",
		"connection reset",
		"connection refused",
		"timeout",
		"EOF",
		"broken pipe",
		"interrupted",    // Gemini interrupted the response
		"empty response", // Empty response, likely from interruption
	}

	errLower := strings.ToLower(errMsg)
	for _, pattern := range recoverablePatterns {
		if strings.Contains(errLower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// clearStateStoreForRetry clears the state store before a retry attempt.
// This ensures we don't accumulate duplicate messages across retries.
func (de *DuplexConversationExecutor) clearStateStoreForRetry(ctx context.Context, req *ConversationRequest) error {
	if req.StateStoreConfig == nil || req.StateStoreConfig.Store == nil {
		return nil
	}

	// ArenaStateStore has a Delete method, but the generic Store interface doesn't
	arenaStore, ok := req.StateStoreConfig.Store.(*arenastore.ArenaStateStore)
	if !ok {
		// For other store types, save an empty state to reset
		store, ok := req.StateStoreConfig.Store.(statestore.Store)
		if ok {
			emptyState := &statestore.ConversationState{
				ID:       req.ConversationID,
				Messages: []types.Message{},
				Metadata: make(map[string]interface{}),
			}
			return store.Save(ctx, emptyState)
		}
		return nil
	}

	// Delete existing state for this conversation
	return arenaStore.Delete(ctx, req.ConversationID)
}

// executeDuplexPipeline builds and runs the duplex streaming pipeline.
func (de *DuplexConversationExecutor) executeDuplexPipeline(
	ctx context.Context,
	req *ConversationRequest,
	streamProvider providers.StreamInputSupport,
	emitter *events.Emitter,
) *ConversationResult {
	// Create pipeline for duplex streaming
	//nolint:gocritic // Variable shadowing unavoidable in this context
	pipeline, err := de.buildDuplexPipeline(req, streamProvider)
	if err != nil {
		return &ConversationResult{
			Failed: true,
			Error:  fmt.Sprintf("failed to build duplex pipeline: %v", err),
		}
	}

	// Create input channel for audio chunks
	inputChan := make(chan stage.StreamElement)

	// Start pipeline execution
	outputChan, err := pipeline.Execute(ctx, inputChan)
	if err != nil {
		return &ConversationResult{
			Failed: true,
			Error:  fmt.Sprintf("failed to execute duplex pipeline: %v", err),
		}
	}

	// Get base directory for resolving file paths
	baseDir := ""
	if req.Config != nil {
		baseDir = req.Config.ConfigDir
	}

	// Process turns from scenario
	err = de.processDuplexTurns(ctx, req, baseDir, inputChan, outputChan, emitter)
	isExpectedErr := errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, errPartialSuccess)
	if err != nil && !isExpectedErr {
		return &ConversationResult{
			Failed: true,
			Error:  fmt.Sprintf("duplex conversation failed: %v", err),
		}
	}

	// Build result from state store
	return de.buildResultFromStateStore(req)
}

// buildDuplexPipeline creates the streaming pipeline for duplex mode.
// The pipeline follows the same pattern as non-duplex: PromptAssemblyStage runs first
// to add system_prompt to metadata, then DuplexProviderStage creates the session
// using that system_prompt.
func (de *DuplexConversationExecutor) buildDuplexPipeline(
	req *ConversationRequest,
	streamProvider providers.StreamInputSupport,
) (*stage.StreamPipeline, error) {
	// Create pipeline with no ExecutionTimeout - duplex conversations use the parent context's
	// timeout (configured via scenario.duplex.timeout, default 10 minutes) for overall timing.
	// The default 30-second ExecutionTimeout would prematurely cancel multi-turn conversations.
	pipelineConfig := stage.DefaultPipelineConfig().WithExecutionTimeout(0)
	builder := stage.NewPipelineBuilderWithConfig(pipelineConfig)
	var stages []stage.Stage

	// Build merged variables for prompt assembly (consistent with non-duplex pipeline)
	mergedVars := de.buildMergedVariables(req)

	// 0. Audio resample stage - normalizes all input audio to target sample rate
	// This must be first so all downstream stages receive consistent sample rates.
	// Gemini expects 16kHz audio input.
	resampleConfig := stage.AudioResampleConfig{
		TargetSampleRate:      defaultSampleRate, // 16000 Hz for Gemini
		PassthroughIfSameRate: true,
	}
	stages = append(stages, stage.NewAudioResampleStage(resampleConfig))

	// Add VAD stage if using client-side turn detection
	if de.shouldUseClientVAD(req) {
		vadConfig := de.buildVADConfig(req)
		vadStage, err := stage.NewAudioTurnStage(vadConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create VAD stage: %w", err)
		}
		stages = append(stages, vadStage)
	}

	// 1. Prompt assembly stage (runs BEFORE provider, like non-duplex)
	// This enriches elements with:
	// - system_prompt for DuplexProviderStage to use at session creation
	// - base_variables for template processing
	taskType := ""
	if req.Scenario != nil {
		taskType = req.Scenario.TaskType
	}
	stages = append(stages,
		stage.NewPromptAssemblyStage(de.promptRegistry, taskType, mergedVars),
		// NOTE: ScenarioContextExtractionStage is NOT included in the duplex pipeline.
		// It accumulates ALL elements before forwarding, which blocks the real-time
		// element flow needed for duplex streaming. Context extraction is handled
		// via mergedVars passed to PromptAssemblyStage.
		stage.NewTemplateStage(),
	)

	// 2. Duplex provider stage - creates session using system_prompt from metadata
	// The session is created lazily when the first element arrives, reading
	// system_prompt from the element's metadata (set by PromptAssemblyStage).
	baseConfig := de.buildBaseSessionConfig(req)
	stages = append(stages, stage.NewDuplexProviderStage(streamProvider, baseConfig))

	// NOTE: ResponseVADStage was removed. It was intended to delay EndOfStream until
	// VAD confirmed response audio stopped, but it caused timing issues with selfplay:
	// 1. The 3-second max wait overlapped with TTS synthesis time
	// 2. This caused turn overlaps leading to Gemini interruptions
	// Gemini's turnComplete signal is now used directly for turn completion.

	// 3. Media externalizer stage to save audio files
	if de.mediaStorage != nil {
		mediaConfig := &stage.MediaExternalizerConfig{
			Enabled:         true,
			StorageService:  de.mediaStorage,
			SizeThresholdKB: 0, // Externalize all media (audio can be large)
			DefaultPolicy:   "retain",
			RunID:           req.RunID,
			ConversationID:  req.ConversationID,
		}
		stages = append(stages, stage.NewMediaExternalizerStage(mediaConfig))
	}

	// NOTE: ValidationStage is NOT included in the duplex pipeline.
	// ValidationStage accumulates ALL elements before forwarding, which blocks
	// the real-time element flow needed for duplex streaming. Turn assertions
	// are handled separately by evaluateTurnAssertions() in the executor.

	// 5. Arena state store save stage to capture conversation messages
	// This stage handles system_prompt in metadata and prepends it as a system message
	if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil {
		storeConfig := de.buildPipelineStateStoreConfig(req)
		stages = append(stages, arenastages.NewArenaStateStoreSaveStage(storeConfig))
	}

	return builder.Chain(stages...).Build()
}

// buildBaseSessionConfig creates the base streaming configuration without system instruction.
// The system instruction will be added by DuplexProviderStage from element metadata.
func (de *DuplexConversationExecutor) buildBaseSessionConfig(req *ConversationRequest) *providers.StreamingInputConfig {
	cfg := &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			ChunkSize:  defaultAudioChunkSize,
			SampleRate: defaultSampleRate,
			Encoding:   "pcm_linear16",
			Channels:   1,
			BitDepth:   geminiAudioBitDepth, // Required for Gemini Live API
		},
		Metadata: make(map[string]interface{}),
	}

	// Pass through response_modalities from provider config if available
	if req.Config != nil && req.Provider != nil {
		providerID := req.Provider.ID()
		if providerCfg, ok := req.Config.LoadedProviders[providerID]; ok && providerCfg.AdditionalConfig != nil {
			if modalities, exists := providerCfg.AdditionalConfig["response_modalities"]; exists {
				cfg.Metadata["response_modalities"] = modalities
			}
		}
	}

	// Enable VAD disabled mode for selfplay scenarios
	// Selfplay uses pre-recorded TTS audio which has natural speech pauses.
	// Gemini's automatic VAD detects these pauses as turn boundaries, causing
	// interruptions mid-sentence. Manual turn control (activityStart/activityEnd)
	// prevents this by explicitly signaling when the audio starts and ends.
	if req.Config != nil && req.Config.SelfPlay != nil && req.Config.SelfPlay.Enabled {
		cfg.Metadata["vad_disabled"] = true
		logger.Debug("buildBaseSessionConfig: VAD disabled for selfplay scenario",
			"scenario_id", req.Scenario.ID)
	}

	// Pass VAD config from scenario's duplex config
	// This allows the scenario to control Gemini's turn detection sensitivity
	// Note: When vad_disabled=true (selfplay), VAD config thresholds are ignored
	if req.Scenario != nil && req.Scenario.Duplex != nil &&
		req.Scenario.Duplex.TurnDetection != nil && req.Scenario.Duplex.TurnDetection.VAD != nil {
		vad := req.Scenario.Duplex.TurnDetection.VAD
		vadConfig := map[string]interface{}{}

		if vad.SilenceThresholdMs > 0 {
			vadConfig["silence_threshold_ms"] = vad.SilenceThresholdMs
		}
		if vad.MinSpeechMs > 0 {
			vadConfig["min_speech_ms"] = vad.MinSpeechMs
		}
		if vad.MaxTurnDurationS > 0 {
			vadConfig["max_turn_duration_s"] = vad.MaxTurnDurationS
		}

		if len(vadConfig) > 0 {
			cfg.Metadata["vad_config"] = vadConfig
			logger.Debug("buildBaseSessionConfig: VAD config from scenario",
				"silence_threshold_ms", vad.SilenceThresholdMs,
				"min_speech_ms", vad.MinSpeechMs)
		}
	}

	return cfg
}

// buildMergedVariables builds the merged variables map for prompt assembly.
// This is consistent with how non-duplex pipelines build variables.
func (de *DuplexConversationExecutor) buildMergedVariables(req *ConversationRequest) map[string]string {
	mergedVars := make(map[string]string)

	// Add region if available
	if req.Region != "" {
		mergedVars["region"] = req.Region
	}

	// Add any metadata from the request as variables
	for k, v := range req.Metadata {
		mergedVars[k] = v
	}

	return mergedVars
}

// buildPipelineStateStoreConfig converts engine StateStoreConfig to pipeline StateStoreConfig.
// It also injects the system prompt from the prompt registry into metadata so that
// ArenaStateStoreSaveStage can capture it in the state store output.
func (de *DuplexConversationExecutor) buildPipelineStateStoreConfig(
	req *ConversationRequest,
) *pipeline.StateStoreConfig {
	if req.StateStoreConfig == nil {
		return nil
	}

	// Start with existing metadata or create new map
	metadata := make(map[string]interface{})
	for k, v := range req.StateStoreConfig.Metadata {
		metadata[k] = v
	}

	// Inject system prompt from prompt registry if available
	// This ensures the system prompt is captured in the state store output
	if de.promptRegistry != nil && req.Scenario != nil && req.Scenario.TaskType != "" {
		if assembled := de.promptRegistry.Load(req.Scenario.TaskType); assembled != nil {
			if assembled.SystemPrompt != "" {
				metadata["system_prompt"] = assembled.SystemPrompt
			}
		}
	}

	return &pipeline.StateStoreConfig{
		Store:          req.StateStoreConfig.Store,
		ConversationID: req.ConversationID,
		UserID:         req.StateStoreConfig.UserID,
		Metadata:       metadata,
	}
}

// processDuplexTurns processes each turn in the scenario through the duplex pipeline.
func (de *DuplexConversationExecutor) processDuplexTurns(
	ctx context.Context,
	req *ConversationRequest,
	baseDir string,
	inputChan chan<- stage.StreamElement,
	outputChan <-chan stage.StreamElement,
	emitter *events.Emitter,
) error {
	// Note: We don't use defer here because we need to close inputChan
	// BEFORE draining outputChan to allow the pipeline to finish.
	var turnErr error

	logger.Debug("processDuplexTurns: starting", "numTurns", len(req.Scenario.Turns))

	// Get resilience configuration from scenario (or use defaults)
	var resilience *config.DuplexResilienceConfig
	if req.Scenario.Duplex != nil {
		resilience = req.Scenario.Duplex.GetResilience()
	}
	interTurnDelayMS := resilience.GetInterTurnDelayMs(defaultInterTurnDelayMS)
	selfplayInterTurnDelayMS := resilience.GetSelfplayInterTurnDelayMs(defaultSelfplayInterTurnDelayMS)
	partialSuccessMinTurns := resilience.GetPartialSuccessMinTurns(defaultPartialSuccessMinTurns)
	ignoreLastTurnSessionEnd := resilience.ShouldIgnoreLastTurnSessionEnd(defaultIgnoreLastTurnSessionEnd)

	// Track logical turn index (accounts for expanded selfplay turns)
	logicalTurnIdx := 0

	for scenarioTurnIdx := range req.Scenario.Turns {
		turn := &req.Scenario.Turns[scenarioTurnIdx]

		// Determine how many iterations to run for this turn
		// Selfplay turns can specify turns: N to generate multiple interactions
		turnsToExecute := 1
		if de.isSelfPlayRole(turn.Role) && turn.Turns > 0 {
			turnsToExecute = turn.Turns
		}

		logger.Debug("processDuplexTurns: processing turn",
			"scenarioTurnIdx", scenarioTurnIdx,
			"role", turn.Role,
			"turnsToExecute", turnsToExecute)

		for iteration := 0; iteration < turnsToExecute; iteration++ {
			de.emitTurnStarted(emitter, logicalTurnIdx, turn.Role, req.Scenario.ID)

			// For selfplay turns, pass the iteration number (1-indexed) so the mock provider gets the correct turn
			selfplayTurnNum := iteration + 1
			err := de.processSingleDuplexTurn(ctx, req, turn, logicalTurnIdx, selfplayTurnNum, baseDir, inputChan, outputChan)
			if err != nil {
				// Check if this is a session ended error
				// Handle based on resilience configuration
				isLastIteration := iteration == turnsToExecute-1
				isLastScenarioTurn := scenarioTurnIdx == len(req.Scenario.Turns)-1
				isLastTurn := isLastIteration && isLastScenarioTurn

				if errors.Is(err, errSessionEnded) {
					// Check if we should ignore session end on the last turn
					if ignoreLastTurnSessionEnd && isLastTurn && logicalTurnIdx > 0 {
						logger.Info("Session ended on final turn, treating as complete",
							"logicalTurnIdx", logicalTurnIdx,
							"completedTurns", logicalTurnIdx)
						de.emitTurnCompleted(emitter, logicalTurnIdx, turn.Role, req.Scenario.ID, nil)
						break
					}

					// Check if we have enough turns for partial success
					if logicalTurnIdx >= partialSuccessMinTurns {
						logger.Info("Session ended early, accepting partial success",
							"logicalTurnIdx", logicalTurnIdx,
							"completedTurns", logicalTurnIdx,
							"minTurnsForSuccess", partialSuccessMinTurns)
						de.emitTurnCompleted(emitter, logicalTurnIdx, turn.Role, req.Scenario.ID, nil)
						// Use sentinel error to exit both loops without marking run as failed
						turnErr = errPartialSuccess
						break
					}
				}

				logger.Error("processDuplexTurns: turn failed",
					"logicalTurnIdx", logicalTurnIdx,
					"iteration", iteration,
					"error", err)
				de.emitTurnCompleted(emitter, logicalTurnIdx, turn.Role, req.Scenario.ID, err)
				turnErr = err
				break
			}
			logger.Debug("processDuplexTurns: turn completed successfully",
				"logicalTurnIdx", logicalTurnIdx,
				"iteration", iteration)

			// Evaluate turn assertions if configured
			// Assertions on user turns validate the subsequent assistant response
			if len(turn.Assertions) > 0 {
				de.evaluateTurnAssertions(ctx, req, turn, logicalTurnIdx)
			}

			de.emitTurnCompleted(emitter, logicalTurnIdx, turn.Role, req.Scenario.ID, nil)
			logger.Debug("Duplex turn completed", "turn", logicalTurnIdx, "role", turn.Role)

			// Add a delay between turns to allow the provider to fully process
			// the previous response before we start sending the next turn's audio.
			// Without this delay, providers may interpret new audio as an "interruption".
			isLastIteration := iteration == turnsToExecute-1
			isLastScenarioTurn := scenarioTurnIdx == len(req.Scenario.Turns)-1
			//nolint:staticcheck // De Morgan form is less readable here
			if !(isLastIteration && isLastScenarioTurn) {
				// Use longer delay after selfplay turns because TTS audio
				// can be lengthy and providers' ASM may detect turn boundaries mid-utterance.
				delayMS := interTurnDelayMS
				if de.isSelfPlayRole(turn.Role) {
					delayMS = selfplayInterTurnDelayMS
				}
				logger.Debug("Inter-turn delay before next turn", "delayMs", delayMS, "wasSelfplay", de.isSelfPlayRole(turn.Role))
				time.Sleep(time.Duration(delayMS) * time.Millisecond)
			}

			logicalTurnIdx++
		}

		if turnErr != nil {
			break
		}
	}

	// Signal that all responses have been received (we wait synchronously for each turn's response)
	// This tells DuplexProviderStage to skip the finalResponseTimeout since we're done
	if turnErr == nil || errors.Is(turnErr, errPartialSuccess) {
		allDoneElem := stage.StreamElement{
			Metadata: map[string]interface{}{
				"all_responses_received": true,
			},
		}
		select {
		case inputChan <- allDoneElem:
			logger.Debug("processDuplexTurns: sent all_responses_received signal")
		case <-ctx.Done():
			// Context canceled, just close
		}
	}

	// Close input channel to signal pipeline to finish
	close(inputChan)

	// Drain the output channel to ensure all pipeline stages
	// (including state store save) have finished processing before we try to read results.
	// Use a separate context for draining - we want to wait for pipeline completion
	// even if the original context has timed out (e.g., turn timeout shouldn't prevent save).
	drainCtx, drainCancel := context.WithTimeout(context.Background(), drainTimeoutSec*time.Second)
	defer drainCancel()
	de.drainOutputChannel(drainCtx, outputChan)

	return turnErr
}

// drainOutputChannel consumes remaining elements from the output channel until closed.
// This ensures all pipeline stages have finished processing.
func (de *DuplexConversationExecutor) drainOutputChannel(
	ctx context.Context,
	outputChan <-chan stage.StreamElement,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-outputChan:
			if !ok {
				// Channel closed - all stages have finished
				return
			}
			// Continue draining
		}
	}
}

// processSingleDuplexTurn processes a single turn in duplex mode.
// selfplayTurnNum is the 1-indexed selfplay turn number (only relevant for selfplay turns).
func (de *DuplexConversationExecutor) processSingleDuplexTurn(
	ctx context.Context,
	req *ConversationRequest,
	turn *config.TurnDefinition,
	turnIdx int,
	selfplayTurnNum int,
	baseDir string,
	inputChan chan<- stage.StreamElement,
	outputChan <-chan stage.StreamElement,
) error {
	// For user turns with audio, stream the audio file
	if turn.Role == "user" && len(turn.Parts) > 0 {
		return de.streamAudioTurn(ctx, turn, baseDir, inputChan, outputChan)
	}

	// For self-play turns, generate audio via TTS
	if de.isSelfPlayRole(turn.Role) {
		return de.processSelfPlayDuplexTurn(ctx, req, turn, turnIdx, selfplayTurnNum, inputChan, outputChan)
	}

	return fmt.Errorf("unsupported turn role for duplex: %s", turn.Role)
}

// streamAudioTurn streams audio from a file to the pipeline.
func (de *DuplexConversationExecutor) streamAudioTurn(
	ctx context.Context,
	turn *config.TurnDefinition,
	baseDir string,
	inputChan chan<- stage.StreamElement,
	outputChan <-chan stage.StreamElement,
) error {
	// Find audio part
	var audioPart *config.TurnContentPart
	for i := range turn.Parts {
		if turn.Parts[i].Type == "audio" {
			audioPart = &turn.Parts[i]
			break
		}
	}

	if audioPart == nil || audioPart.Media == nil {
		return errors.New("no audio content found in turn")
	}

	// Create audio source and stream chunks
	source, err := turnexecutors.NewAudioFileSource(audioPart.Media.FilePath, baseDir)
	if err != nil {
		return fmt.Errorf("failed to create audio source: %w", err)
	}
	defer source.Close()

	// Load audio data for state store capture (converted to WAV for playability)
	// Use the media loader's ConvertTurnPartsToMessageParts for proper conversion
	messageParts, err := turnexecutors.ConvertTurnPartsToMessageParts(ctx, turn.Parts, baseDir, nil, nil)
	if err != nil {
		// Fallback to file path reference if conversion fails
		logger.Debug("streamAudioTurn: failed to load audio data, using file path", "error", err)
		audioPath := audioPart.Media.FilePath
		mimeType := audioPart.Media.MIMEType
		messageParts = []types.ContentPart{
			{
				Type: types.ContentTypeAudio,
				Media: &types.MediaContent{
					FilePath: &audioPath,
					MIMEType: mimeType,
				},
			},
		}
	}

	// Generate unique turn ID for this user message
	// This is used to correlate transcription events with the correct user message
	turnID := uuid.New().String()

	// Create user message element to capture in state store
	userMsg := &types.Message{
		Role:  "user",
		Parts: messageParts,
		Meta: map[string]interface{}{
			"turn_id": turnID,
		},
	}

	// Send user message to pipeline for state store capture
	userMsgElem := stage.NewMessageElement(userMsg)
	// Also add turn_id to element metadata so DuplexProviderStage can track it
	if userMsgElem.Metadata == nil {
		userMsgElem.Metadata = make(map[string]interface{})
	}
	userMsgElem.Metadata["turn_id"] = turnID
	select {
	case inputChan <- userMsgElem:
	case <-ctx.Done():
		return ctx.Err()
	}

	// Stream audio chunks to pipeline
	return de.streamAudioChunks(ctx, source, inputChan, outputChan)
}

// errSessionEnded is returned when the session has ended (not an error, just complete)
var errSessionEnded = errors.New("session ended")

// streamAudioChunks streams audio from source to the pipeline and collects responses.
func (de *DuplexConversationExecutor) streamAudioChunks(
	ctx context.Context,
	source *turnexecutors.AudioFileSource,
	inputChan chan<- stage.StreamElement,
	outputChan <-chan stage.StreamElement,
) error {
	// First, drain any stale messages from the output channel.
	// Messages can arrive between turns (e.g., "interrupted" status from the previous turn)
	// and we don't want them to be picked up by this turn's response collector.
	drainCount := 0
drainLoop:
	for {
		select {
		case elem, ok := <-outputChan:
			if !ok {
				// Channel closed - session has ended
				// This can happen if Gemini's session timed out or disconnected
				logger.Debug("streamAudioChunks: session ended during drain (channel closed)")
				return errSessionEnded
			}
			drainCount++
			//nolint:lll // Debug logging - acceptable long line in interactive code
			logger.Debug("streamAudioChunks: drained stale element", "hasText", elem.Text != nil, "endOfStream", elem.EndOfStream)
			// If we hit an EndOfStream, that was a stale turn completion - continue draining
		default:
			// No more messages to drain
			break drainLoop
		}
	}
	if drainCount > 0 {
		logger.Debug("streamAudioChunks: drained stale messages", "count", drainCount)
	}

	// Start a goroutine to collect responses.
	// It waits for EndOfStream (turn complete) from the pipeline.
	// EndOfStream is set by DuplexProviderStage when Gemini sends turnComplete.
	responseDone := make(chan error, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				responseDone <- ctx.Err()
				return

			case elem, ok := <-outputChan:
				if !ok {
					// Channel closed before receiving response - session ended prematurely
					// This can happen if the input channel closes before Gemini responds
					logger.Debug("Turn response channel closed before receiving complete response")
					responseDone <- fmt.Errorf("session ended before receiving response: %w", errSessionEnded)
					return
				}

				action, err := processResponseElement(&elem, "Turn")
				switch action {
				case responseActionContinue:
					continue
				case responseActionComplete:
					responseDone <- nil
					return
				case responseActionError:
					responseDone <- err
					return
				}
			}
		}
	}()

	// Stream audio chunks
	for {
		chunk, err := source.ReadChunk(defaultAudioChunkSize)
		if err != nil {
			break // EOF or error
		}

		elem := stage.StreamElement{
			Audio: &stage.AudioData{
				Samples:    chunk,
				SampleRate: defaultSampleRate,
				Channels:   1,
				Format:     stage.AudioFormatPCM16,
			},
		}

		select {
		case inputChan <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Signal end of audio input for this turn
	// This triggers mock sessions to emit their auto-response
	// For Gemini, this calls EndInput() which sends turn_complete=true
	logger.Debug("streamAudioChunks: sending EndOfStream signal to trigger response")
	endOfTurn := stage.StreamElement{EndOfStream: true}
	select {
	case inputChan <- endOfTurn:
		logger.Debug("streamAudioChunks: EndOfStream signal sent, waiting for response")
	case <-ctx.Done():
		return ctx.Err()
	}

	// Wait for turn response (EndOfStream or error)
	select {
	case err := <-responseDone:
		logger.Debug("streamAudioChunks: response received", "error", err)
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// processSelfPlayDuplexTurn handles self-play turns in duplex mode.
// selfplayTurnNum is the 1-indexed selfplay turn number (first selfplay = 1).
func (de *DuplexConversationExecutor) processSelfPlayDuplexTurn(
	ctx context.Context,
	req *ConversationRequest,
	turn *config.TurnDefinition,
	turnIdx int,
	selfplayTurnNum int,
	inputChan chan<- stage.StreamElement,
	outputChan <-chan stage.StreamElement,
) error {
	// For self-play in duplex mode:
	// 1. Wait for assistant response (if not first turn)
	// 2. Generate user message using self-play LLM
	// 3. Convert to audio using TTS (if configured)
	// 4. Stream audio to pipeline

	// Validate self-play registry is available
	if de.selfPlayRegistry == nil {
		return fmt.Errorf("self-play registry not configured for duplex turn %d", turnIdx)
	}

	// Check if TTS is configured
	if turn.TTS == nil {
		return fmt.Errorf("TTS configuration required for self-play duplex turn %d", turnIdx)
	}

	// Get audio generator from registry
	audioGen, err := de.selfPlayRegistry.GetAudioContentGenerator(
		turn.Role,
		turn.Persona,
		turn.TTS,
	)
	if err != nil {
		return fmt.Errorf("failed to get audio generator for turn %d: %w", turnIdx, err)
	}

	// Collect conversation history from state store
	history := de.getConversationHistory(req)

	// Generate text and convert to audio
	// Pass the selfplay turn number so the mock provider gets the correct turn response
	opts := &selfplay.GeneratorOptions{
		SelfplayTurnIndex: selfplayTurnNum,
	}
	audioResult, err := audioGen.NextUserTurnAudio(ctx, history, req.Scenario.ID, opts)
	if err != nil {
		return fmt.Errorf("failed to generate audio for turn %d: %w", turnIdx, err)
	}

	// Get the generated text content
	generatedText := audioResult.TextResult.Response.Content

	// Generate unique turn ID for this user message
	// This is used to correlate transcription events with the correct user message
	turnID := uuid.New().String()

	logger.Debug("Self-play audio generated",
		"turn", turnIdx,
		"turn_id", turnID,
		"generated_text", generatedText,
		"text_length", len(generatedText),
		"audio_bytes", len(audioResult.Audio),
		"sample_rate", audioResult.SampleRate,
	)

	// Create user message element to capture in state store
	// Include both the generated text and the TTS audio data (base64 encoded)
	audioDataBase64 := base64.StdEncoding.EncodeToString(audioResult.Audio)
	userMsg := &types.Message{
		Role:    "user",
		Content: generatedText, // Include the selfplay-generated text
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeText,
				Text: &generatedText,
			},
			{
				Type: types.ContentTypeAudio,
				Media: &types.MediaContent{
					Data:     &audioDataBase64,
					MIMEType: "audio/pcm",
				},
			},
		},
	}

	// Add selfplay metadata to the user message for reporting
	// Include turn_id for correlating transcription events
	// Only include essential selfplay fields, not pipeline internal metadata
	userMsg.Meta = map[string]interface{}{
		"turn_id":             turnID,
		"self_play":           true,
		"persona":             turn.Persona,
		"selfplay_turn_index": selfplayTurnNum,
	}

	// Copy only relevant metadata from text generation result
	// Avoid copying pipeline internal fields like system_prompt, base_variables, etc.
	if audioResult.TextResult != nil && audioResult.TextResult.Metadata != nil {
		// Only copy specific fields that are relevant to selfplay output
		relevantFields := []string{
			"self_play_provider",
			"validation_warning",
			"warning_type",
		}
		for _, field := range relevantFields {
			if v, ok := audioResult.TextResult.Metadata[field]; ok {
				userMsg.Meta[field] = v
			}
		}
	}

	// Send user message to pipeline for state store capture
	userMsgElem := stage.NewMessageElement(userMsg)
	// Also add turn_id to element metadata so DuplexProviderStage can track it
	if userMsgElem.Metadata == nil {
		userMsgElem.Metadata = make(map[string]interface{})
	}
	userMsgElem.Metadata["turn_id"] = turnID
	select {
	case inputChan <- userMsgElem:
	case <-ctx.Done():
		return ctx.Err()
	}

	// Stream audio chunks to the pipeline
	// Pass sample rate so streaming can resample if needed
	return de.streamSelfPlayAudio(ctx, audioResult.Audio, audioResult.SampleRate, inputChan, outputChan)
}

// streamSelfPlayAudio streams synthesized audio to the duplex pipeline.
// The audio is passed with its original sample rate - the AudioResampleStage
// in the pipeline will normalize it to the provider's expected rate (16kHz for Gemini).
//
// Audio is sent in burst mode (as fast as possible) to avoid interruption issues.
// Real-time pacing was previously used but caused problems: Gemini would start
// responding before all audio was sent (detecting speech pauses mid-utterance),
// and when more audio arrived, Gemini treated it as "user interrupted" and
// discarded its response. Burst mode avoids this by sending all audio before
// Gemini can detect any turn boundaries.
func (de *DuplexConversationExecutor) streamSelfPlayAudio(
	ctx context.Context,
	audioData []byte,
	sampleRate int,
	inputChan chan<- stage.StreamElement,
	outputChan <-chan stage.StreamElement,
) error {
	// Note: Resampling is now handled by AudioResampleStage in the pipeline.
	// The audio element carries the source sample rate so the stage can
	// resample as needed.

	// Use source sample rate for chunking (default to 16kHz if not specified)
	sourceSampleRate := sampleRate
	if sourceSampleRate == 0 {
		sourceSampleRate = defaultSampleRate
	}

	// Start a goroutine to collect responses.
	// It waits for EndOfStream (turn complete) from the pipeline.
	// EndOfStream is set by DuplexProviderStage when Gemini sends turnComplete.
	responseDone := make(chan error, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				responseDone <- ctx.Err()
				return

			case elem, ok := <-outputChan:
				if !ok {
					// Channel closed - session ended unexpectedly
					logger.Debug("Self-play output channel closed unexpectedly")
					responseDone <- fmt.Errorf("output channel closed unexpectedly: %w", errSessionEnded)
					return
				}

				action, err := processResponseElement(&elem, "Self-play")
				switch action {
				case responseActionContinue:
					continue
				case responseActionComplete:
					responseDone <- nil
					return
				case responseActionError:
					responseDone <- err
					return
				}
			}
		}
	}()

	// Stream audio in BURST MODE (as fast as possible) to avoid interruption issues.
	// Real-time pacing was removed because it caused problems: Gemini's VAD would detect
	// pauses in the TTS audio (commas, periods) mid-utterance and start responding before
	// all audio was sent. When more audio arrived, Gemini treated it as an interruption.
	// Burst mode sends all audio before Gemini can detect any turn boundaries.
	chunkSize := defaultAudioChunkSize

	totalChunks := (len(audioData) + chunkSize - 1) / chunkSize
	logger.Debug("Streaming selfplay audio in BURST MODE",
		"chunk_size", chunkSize,
		"source_sample_rate", sourceSampleRate,
		"total_bytes", len(audioData),
		"total_chunks", totalChunks,
	)

	streamStart := time.Now()
	for offset := 0; offset < len(audioData); offset += chunkSize {
		end := offset + chunkSize
		if end > len(audioData) {
			end = len(audioData)
		}

		chunk := audioData[offset:end]
		chunkIdx := offset / chunkSize

		elem := stage.StreamElement{
			Audio: &stage.AudioData{
				Samples:    chunk,
				SampleRate: sourceSampleRate, // Pass source rate so AudioResampleStage can convert
				Channels:   1,
				Format:     stage.AudioFormatPCM16,
			},
			// Mark as passthrough so AudioTurnStage forwards immediately
			// instead of accumulating (which breaks real-time streaming)
			Metadata: map[string]interface{}{
				"passthrough": true,
			},
		}

		select {
		case inputChan <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}

		// Log first, middle, and last chunk for debugging
		if chunkIdx == 0 || chunkIdx == totalChunks/2 || chunkIdx == totalChunks-1 {
			logger.Debug("Selfplay audio chunk sent (burst mode)",
				"chunk_idx", chunkIdx,
				"total_chunks", totalChunks,
				"elapsed_ms", time.Since(streamStart).Milliseconds(),
				"chunk_bytes", len(chunk),
			)
		}
		// NO SLEEP - burst mode sends as fast as possible
	}

	// Signal end of audio input for this turn
	// This triggers mock sessions to emit their auto-response
	// For Gemini, this calls EndInput() which sends turn_complete=true
	logger.Debug("streamSelfPlayAudio: sending EndOfStream signal to trigger response")
	endOfTurn := stage.StreamElement{EndOfStream: true}
	select {
	case inputChan <- endOfTurn:
	case <-ctx.Done():
		return ctx.Err()
	}

	// Wait for response collection to complete
	select {
	case err := <-responseDone:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
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
		logger.Debug("Failed to load conversation history", "error", err)
		return nil
	}

	if state == nil {
		return nil
	}

	return state.Messages
}

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

	return &ConversationResult{
		Messages:     messages,
		Cost:         totalCost,
		MediaOutputs: mediaOutputs,
		SelfPlay:     de.containsSelfPlay(req.Scenario),
	}
}

// Event emission helpers

func (de *DuplexConversationExecutor) emitSessionStarted(
	emitter *events.Emitter,
	req *ConversationRequest,
) {
	if emitter == nil {
		return
	}
	emitter.EmitCustom(
		events.EventType("arena.duplex.session.started"),
		"DuplexExecutor",
		"session_started",
		map[string]interface{}{
			"scenario":        req.Scenario.ID,
			"conversation_id": req.ConversationID,
		},
		"Duplex session started",
	)
}

func (de *DuplexConversationExecutor) emitSessionCompleted(
	emitter *events.Emitter,
	req *ConversationRequest,
) {
	if emitter == nil {
		return
	}
	emitter.EmitCustom(
		events.EventType("arena.duplex.session.completed"),
		"DuplexExecutor",
		"session_completed",
		map[string]interface{}{
			"scenario":        req.Scenario.ID,
			"conversation_id": req.ConversationID,
		},
		"Duplex session completed",
	)
}

func (de *DuplexConversationExecutor) emitSessionError(
	emitter *events.Emitter,
	req *ConversationRequest,
	err error,
) {
	if emitter == nil {
		return
	}
	emitter.EmitCustom(
		events.EventType("arena.duplex.session.error"),
		"DuplexExecutor",
		"session_error",
		map[string]interface{}{
			"scenario":        req.Scenario.ID,
			"conversation_id": req.ConversationID,
			"error":           err.Error(),
		},
		"Duplex session error",
	)
}

func (de *DuplexConversationExecutor) emitTurnStarted(
	emitter *events.Emitter,
	turnIdx int,
	role string,
	scenarioID string,
) {
	if emitter == nil {
		return
	}
	emitter.EmitCustom(
		events.EventType("arena.duplex.turn.started"),
		"DuplexExecutor",
		"turn_started",
		map[string]interface{}{
			"turn_index": turnIdx,
			"role":       role,
			"scenario":   scenarioID,
		},
		fmt.Sprintf("Duplex turn %d started", turnIdx),
	)
}

func (de *DuplexConversationExecutor) emitTurnCompleted(
	emitter *events.Emitter,
	turnIdx int,
	role string,
	scenarioID string,
	err error,
) {
	if emitter == nil {
		return
	}
	eventType := events.EventType("arena.duplex.turn.completed")
	eventName := "turn_completed"
	if err != nil {
		eventType = events.EventType("arena.duplex.turn.failed")
		eventName = "turn_failed"
	}
	emitter.EmitCustom(
		eventType,
		"DuplexExecutor",
		eventName,
		map[string]interface{}{
			"turn_index": turnIdx,
			"role":       role,
			"scenario":   scenarioID,
			"error":      err,
		},
		fmt.Sprintf("Duplex turn %d completed", turnIdx),
	)
}

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
