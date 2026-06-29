package engine

import (
	"context"
	"encoding/json"
	"errors"
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
	arenaaudio "github.com/AltairaLabs/PromptKit/tools/arena/audio"
	arenastages "github.com/AltairaLabs/PromptKit/tools/arena/stages"
	arenastore "github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// defaultPipelineIdleTimeout is the default duplex pipeline IdleTimeout when a
// scenario does not declare its own duplex.timeout. Long enough to avoid
// premature cancellation of multi-turn conversations.
const defaultPipelineIdleTimeout = 30 * time.Second

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
		// For other store types, bulk-write an empty state to reset.
		// Requires BulkWriter; stores without bulk-write support are skipped.
		if bulkWriter, ok := req.StateStoreConfig.Store.(statestore.BulkWriter); ok {
			emptyState := &statestore.ConversationState{
				ID:       req.ConversationID,
				Messages: []types.Message{},
				Metadata: make(map[string]interface{}),
			}
			return bulkWriter.Save(ctx, emptyState)
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
	return de.buildResultFromStateStore(ctx, req)
}

// isExpectedDuplexError checks if an error is expected (not a failure).
// Uses errors.Is so wrapped errors (e.g. "turn failed: %w" around
// context.DeadlineExceeded) are recognized — without that, a deadline
// reached deep in the turn loop comes back wrapped and the duplex path
// throws away an otherwise-complete result, skipping the assertion
// evaluation that's the whole point of running the scenario.
func isExpectedDuplexError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, errPartialSuccess)
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
	// Pipeline IdleTimeout defaults to 30s — far too short for duplex
	// scenarios where a single LLM-as-user / TTS / Gemini-response cycle
	// can quietly take longer than 30s. Drive it from the scenario's
	// `duplex.timeout` instead so a scenario declaring `timeout: 10m`
	// gets a 10-minute idle ceiling, not 30s.
	idleTimeout := defaultPipelineIdleTimeout
	if req.Scenario != nil && req.Scenario.Duplex != nil {
		idleTimeout = req.Scenario.Duplex.GetTimeoutDuration(idleTimeout)
	}
	pipelineConfig := stage.DefaultPipelineConfig().
		WithExecutionTimeout(0).
		WithIdleTimeout(idleTimeout)
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

	// 0a. Audio pacing stage — emits each audio element at the cadence
	// implied by its byte count and sample rate, so chunks downstream
	// (the input MonitorTap → local-sink playback, and the duplex
	// provider's VAD) see the same arrival rhythm a microphone would
	// produce. Without this, a TTS source that delivers chunks faster
	// than realtime (or instantly, like the mock) collapses an entire
	// utterance into a single instant, which causes oto playback
	// underruns at the local sink and trips false turn-end detection at
	// the provider.
	//
	// Skipped when there's no consumer that cares about cadence:
	// selfplay (VAD disabled at the provider) AND no AudioRouter (no
	// LocalSink listening). In that combination — typical of headless
	// CI runs — pacing would just spend real wall-clock time sleeping
	// for nothing, multiplying every test's runtime by the audio
	// duration of every utterance.
	//
	// Placed first, before the input MonitorTap, so both the playback
	// path and the provider path observe the paced flow when enabled.
	if needsAudioPacing(req) {
		stages = append(stages, stage.NewAudioPacingStage())
	}

	// 0b. Input audio monitor tap (opt-in) — placed BEFORE the resample
	// stage so the router (and downstream LocalSink) hear the audio at
	// its source sample rate. Going through resample first introduces
	// chunk-boundary phase discontinuities that sound like consistent
	// clicks during playback (24 kHz → 16 kHz → 24 kHz round-trips
	// each round per-chunk sample counts independently).
	//
	// MonitorTap still resamples to the router's canonical rate inside
	// the tap itself, so consumers continue to see one rate.
	if req.AudioRouter != nil {
		stages = append(stages, arenaaudio.NewMonitorTap(req.AudioRouter, arenaaudio.MonitorTapConfig{
			Position: stage.RecordingPositionInput,
		}))
	}

	// 1. Audio resample stage - normalizes all input audio to target sample rate
	// for the provider. Downstream stages receive consistent sample rates.
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
	turnState := stage.NewTurnState()
	stages = append(stages,
		stage.NewVariableProviderStageWithVarsAndTurnState(mergedVars, nil, turnState),
		stage.NewPromptAssemblyStageWithTurnState(de.promptRegistry, taskType, mergedVars, turnState),
		// NOTE: ScenarioContextExtractionStage is NOT included in the duplex pipeline.
		// It accumulates ALL elements before forwarding, which blocks the real-time
		// element flow needed for duplex streaming. Context extraction is handled
		// via mergedVars passed to PromptAssemblyStage.
		stage.NewTemplateStageWithTurnState(nil, turnState),
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

	duplexStage := stage.NewDuplexProviderStageWithTurnState(
		streamProvider, baseConfig, emitter, turnState,
	)
	// Wire the session observer (interactive ASM barge-in: session.BargeIn →
	// playback flush). Nil for non-interactive runs.
	if req.OnDuplexSession != nil {
		duplexStage.SetSessionObserver(req.OnDuplexSession)
	}
	stages = append(stages, duplexStage)

	// 2a-pre. Output audio pacing. Realtime providers (OpenAI, Gemini)
	// stream assistant audio faster than playback rate — gpt-4o-realtime
	// commonly delivers a 5-second utterance in ~1 second of wall clock.
	// Without pacing on the output side:
	//   - the LocalSink queue fills instantly and playback continues for
	//     several seconds after response.done already arrived, so the
	//     turn-end barrier (response.done from the wire) fires before the
	//     audio has actually been heard.
	//   - the next selfplay user turn's TTS starts streaming while the
	//     previous assistant audio is still audibly playing — the
	//     observed "turns are overlapping" symptom.
	// Pacing here aligns the output flow with realtime, so the EndOfStream
	// element naturally queues behind the last paced audio chunk and the
	// response collector signals done at the same instant playback ends.
	// Same gate as the input pacing — only when something downstream cares
	// about cadence. Separate instance from input pacing per the
	// AudioPacingStage "direction singularity" note. Distinct name from
	// the input pacing stage so the pipeline builder doesn't reject the
	// pair as duplicates.
	if needsAudioPacing(req) {
		stages = append(stages, stage.NewNamedAudioPacingStage("audio-pacing-output"))
	}

	// 2a. Output audio monitor tap (opt-in). Placed after the provider so
	// generated agent audio flows through the tap to the router. With the
	// pacing stage above, the tap (and downstream LocalSink) sees audio
	// arriving at realtime rate.
	if req.AudioRouter != nil {
		stages = append(stages, arenaaudio.NewMonitorTap(req.AudioRouter, arenaaudio.MonitorTapConfig{
			Position: stage.RecordingPositionOutput,
		}))
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

	// 5. Arena state store save stage to capture conversation messages.
	// This stage handles system_prompt in metadata and prepends it as a
	// system message. When an event bus is available we wire an emitter so
	// each Message element is also broadcast on the bus — that's what feeds
	// the live TUI conversation panel during a duplex run.
	if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil {
		storeConfig := de.buildPipelineStateStoreConfig(req)
		saveStage := arenastages.NewArenaStateStoreSaveStageWithTurnState(storeConfig, turnState)
		if emitter != nil {
			saveStage = saveStage.WithEmitter(emitter)
		}
		stages = append(stages, saveStage)
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

	// Explicitly enable input-audio transcription so the OpenAI Realtime provider
	// emits conversation.item.input_audio_transcription.completed events.
	// The provider defaults to enabled when the key is absent, but setting it
	// explicitly prevents accidental suppression by later metadata writes.
	cfg.Metadata["input_transcription"] = true

	de.applyResponseModalities(cfg, req)
	de.applySelfPlayVADConfig(cfg, req)
	de.applyScenarioVADConfig(cfg, req)
	de.applyToolsConfig(cfg)
	de.applyMockScenarioContext(cfg, req)

	return cfg
}

// applyMockScenarioContext threads the scenario ID through the streaming
// session metadata so a mock provider with a configured response repository
// can return per-turn scenario-mapped responses instead of a static
// auto-respond text. No-op for real providers — they ignore the key.
func (de *DuplexConversationExecutor) applyMockScenarioContext(
	cfg *providers.StreamingInputConfig,
	req *ConversationRequest,
) {
	if req.Scenario == nil || req.Scenario.ID == "" {
		return
	}
	cfg.Metadata["mock_scenario_id"] = req.Scenario.ID
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

// needsAudioPacing reports whether the data path actually needs cadence
// enforcement on this run. The pacing stage is the sole cadence
// authority (see runtime/pipeline/stage/stages_pacing.go and
// tools/arena/audio/types.go on the observer model), so the only thing
// to decide here is "is anyone on the data path going to interpret
// arrival rate?" Two cases say yes:
//
//   - Provider VAD: any non-selfplay duplex run. The provider's session
//     timestamps audio arrival to time turn-end silence, and that has
//     to be wall-clock-correct regardless of who is listening locally.
//   - Live observer attached (req.AudioRouter != nil): a LocalSink or
//     SSE relay is fanning audio out for human listening. Pacing
//     produces a smooth playback drain off the router. Observers
//     themselves cannot exert backpressure — pacing is what gives them
//     non-bursty input.
//
// The remaining case (selfplay, VAD off, no router attached — headless
// CI selfplay) is the only combination where pacing wastes wall-clock
// time on no consumer, so we skip it.
func needsAudioPacing(req *ConversationRequest) bool {
	if req.AudioRouter != nil {
		return true
	}
	if req.Config == nil || req.Config.SelfPlay == nil || !req.Config.SelfPlay.IsEnabled() {
		return true
	}
	return false
}

// applySelfPlayVADConfig disables provider-side VAD only when THIS
// scenario actually uses selfplay (i.e. has at least one persona-driven
// turn). Previously this checked whether selfplay was *configured* at
// the arena level, which incorrectly disabled VAD for scripted-text
// scenarios sharing the same arena config — see
// `duplex-scripted-text-openai`, which declares
// `turn_detection.mode: asm` but was getting `automaticActivityDetection.disabled=true`
// pushed to Gemini, defeating ASM and causing long inter-turn delays
// that looked like a playback bug.
func (de *DuplexConversationExecutor) applySelfPlayVADConfig(
	cfg *providers.StreamingInputConfig,
	req *ConversationRequest,
) {
	if req.Config == nil || req.Config.SelfPlay == nil || !req.Config.SelfPlay.IsEnabled() {
		return
	}
	if !scenarioUsesSelfPlay(req.Scenario) {
		return
	}
	cfg.Metadata["vad_disabled"] = true
	logger.Debug("buildBaseSessionConfig: VAD disabled for selfplay scenario",
		"scenario_id", req.Scenario.ID)
}

// scenarioUsesSelfPlay reports whether the given scenario has at least
// one turn driven by a persona. Scripted-text scenarios (turns with
// only `content`) return false even when the arena's self_play block
// is populated — those turns don't drive the LLM-as-user path and
// must not trigger VAD-disabled overrides intended for selfplay.
func scenarioUsesSelfPlay(scenario *config.Scenario) bool {
	if scenario == nil {
		return false
	}
	for i := range scenario.Turns {
		if scenario.Turns[i].Persona != "" {
			return true
		}
	}
	return false
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
