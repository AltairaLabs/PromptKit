package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	arenastages "github.com/AltairaLabs/PromptKit/tools/arena/stages"
	arenastore "github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

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
				"max_retries", maxRetries,
				"retry_delay_ms", retryDelayMS)
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
				"max_retries", maxRetries,
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
	isExpectedErr := isExpectedDuplexError(err)
	if err != nil && !isExpectedErr {
		return &ConversationResult{
			Failed: true,
			Error:  fmt.Sprintf("duplex conversation failed: %v", err),
		}
	}

	// Build result from state store
	return de.buildResultFromStateStore(req)
}

// isExpectedDuplexError checks if an error is expected (not a failure).
func isExpectedDuplexError(err error) bool {
	if err == nil {
		return false
	}
	return err == context.DeadlineExceeded ||
		err == context.Canceled ||
		err == errPartialSuccess
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

	// Determine target sample rate from provider capabilities
	// Each provider has different audio requirements (e.g., Gemini: 16kHz, OpenAI: 24kHz)
	targetSampleRate := defaultSampleRate // fallback to 16kHz
	caps := streamProvider.GetStreamingCapabilities()
	if caps.Audio != nil && caps.Audio.PreferredSampleRate > 0 {
		targetSampleRate = caps.Audio.PreferredSampleRate
		logger.Debug("buildDuplexPipeline: using provider preferred sample rate",
			"sample_rate", targetSampleRate)
	}

	// 0. Audio resample stage - normalizes all input audio to target sample rate
	// This must be first so all downstream stages receive consistent sample rates.
	resampleConfig := stage.AudioResampleConfig{
		TargetSampleRate:      targetSampleRate,
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
	baseConfig := de.buildBaseSessionConfig(req, targetSampleRate)

	// Create emitter for audio event recording if event bus is available
	// Use RunID as SessionID to ensure events are stored (EventBus only stores events with non-empty SessionID)
	var emitter *events.Emitter
	if req.EventBus != nil {
		emitter = events.NewEmitter(req.EventBus, req.RunID, req.RunID, req.ConversationID)
	}

	if emitter != nil {
		stages = append(stages, stage.NewDuplexProviderStageWithEmitter(streamProvider, baseConfig, emitter))
	} else {
		stages = append(stages, stage.NewDuplexProviderStage(streamProvider, baseConfig))
	}

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
// sampleRate is determined from provider capabilities in buildDuplexPipeline.
func (de *DuplexConversationExecutor) buildBaseSessionConfig(
	req *ConversationRequest,
	sampleRate int,
) *providers.StreamingInputConfig {
	cfg := &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			ChunkSize:  defaultAudioChunkSize,
			SampleRate: sampleRate,
			Encoding:   "pcm_linear16",
			Channels:   1,
			BitDepth:   geminiAudioBitDepth,
		},
		Metadata: make(map[string]interface{}),
	}

	de.applyResponseModalities(cfg, req)
	de.applySelfPlayVADConfig(cfg, req)
	de.applyScenarioVADConfig(cfg, req)
	de.applyToolsConfig(cfg)

	return cfg
}

// applyResponseModalities adds response modalities from provider config.
func (de *DuplexConversationExecutor) applyResponseModalities(
	cfg *providers.StreamingInputConfig,
	req *ConversationRequest,
) {
	if req.Config == nil || req.Provider == nil {
		return
	}
	providerID := req.Provider.ID()
	providerCfg, ok := req.Config.LoadedProviders[providerID]
	if !ok || providerCfg.AdditionalConfig == nil {
		return
	}
	if modalities, exists := providerCfg.AdditionalConfig["response_modalities"]; exists {
		cfg.Metadata["response_modalities"] = modalities
	}
}

// applySelfPlayVADConfig disables VAD for selfplay scenarios.
func (de *DuplexConversationExecutor) applySelfPlayVADConfig(
	cfg *providers.StreamingInputConfig,
	req *ConversationRequest,
) {
	if req.Config == nil || req.Config.SelfPlay == nil || !req.Config.SelfPlay.Enabled {
		return
	}
	cfg.Metadata["vad_disabled"] = true
	logger.Debug("buildBaseSessionConfig: VAD disabled for selfplay scenario",
		"scenario_id", req.Scenario.ID)
}

// applyScenarioVADConfig adds VAD configuration from scenario.
func (de *DuplexConversationExecutor) applyScenarioVADConfig(
	cfg *providers.StreamingInputConfig,
	req *ConversationRequest,
) {
	if req.Scenario == nil || req.Scenario.Duplex == nil {
		return
	}
	if req.Scenario.Duplex.TurnDetection == nil || req.Scenario.Duplex.TurnDetection.VAD == nil {
		return
	}

	vad := req.Scenario.Duplex.TurnDetection.VAD
	vadConfig := make(map[string]interface{})

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

// applyToolsConfig adds tools from the tool registry to the session config.
func (de *DuplexConversationExecutor) applyToolsConfig(cfg *providers.StreamingInputConfig) {
	if de.toolRegistry == nil {
		return
	}

	toolDescs := de.toolRegistry.GetTools()
	if len(toolDescs) == 0 {
		return
	}

	cfg.Tools = make([]providers.StreamingToolDefinition, 0, len(toolDescs))
	for _, td := range toolDescs {
		cfg.Tools = append(cfg.Tools, de.convertToolDefinition(td))
	}
	logger.Debug("buildBaseSessionConfig: tools configured for streaming session",
		"tool_count", len(cfg.Tools))
}

// convertToolDefinition converts a tool descriptor to a streaming tool definition.
func (de *DuplexConversationExecutor) convertToolDefinition(
	td *tools.ToolDescriptor,
) providers.StreamingToolDefinition {
	var params map[string]interface{}
	if td.InputSchema != nil {
		if err := json.Unmarshal(td.InputSchema, &params); err != nil {
			logger.Debug("buildBaseSessionConfig: failed to parse tool schema",
				"tool", td.Name, "error", err)
			params = nil
		}
	}
	return providers.StreamingToolDefinition{
		Name:        td.Name,
		Description: td.Description,
		Parameters:  params,
	}
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
