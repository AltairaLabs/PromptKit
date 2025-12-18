package engine

// This file contains integration-level code that requires full pipeline
// infrastructure to test. It is excluded from per-file coverage checks.

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
	arenaassertions "github.com/AltairaLabs/PromptKit/tools/arena/assertions"
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
func (de *DuplexConversationExecutor) executeDuplexConversation(
	ctx context.Context,
	req *ConversationRequest,
	streamProvider providers.StreamInputSupport,
	emitter *events.Emitter,
) *ConversationResult {
	de.emitSessionStarted(emitter, req)

	// Build and execute the duplex pipeline
	// The session is created inside the pipeline by DuplexProviderStage,
	// using system_prompt from PromptAssemblyStage metadata.
	result := de.executeDuplexPipeline(ctx, req, streamProvider, emitter)

	de.emitSessionCompleted(emitter, req)
	return result
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
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
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
	builder := stage.NewPipelineBuilder()
	var stages []stage.Stage

	// Build merged variables for prompt assembly (consistent with non-duplex pipeline)
	mergedVars := de.buildMergedVariables(req)

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

	// 4. Arena state store save stage to capture conversation messages
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

			err := de.processSingleDuplexTurn(ctx, req, turn, logicalTurnIdx, baseDir, inputChan, outputChan)
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
func (de *DuplexConversationExecutor) processSingleDuplexTurn(
	ctx context.Context,
	req *ConversationRequest,
	turn *config.TurnDefinition,
	turnIdx int,
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
		return de.processSelfPlayDuplexTurn(ctx, req, turn, turnIdx, inputChan, outputChan)
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

	// Create user message element to capture in state store
	userMsg := &types.Message{
		Role:  "user",
		Parts: messageParts,
	}

	// Send user message to pipeline for state store capture
	userMsgElem := stage.NewMessageElement(userMsg)
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
	// It exits when it sees EndOfStream (turn complete) - pipeline draining happens in processDuplexTurns.
	responseDone := make(chan error, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				responseDone <- ctx.Err()
				return
			case elem, ok := <-outputChan:
				if !ok {
					// Channel closed - session ended
					responseDone <- nil
					return
				}
				if elem.Error != nil {
					responseDone <- elem.Error
					return
				}
				// Check for turn completion (EndOfStream flag set by provider)
				if elem.EndOfStream {
					logger.Debug("Turn response complete",
						"hasMessage", elem.Message != nil,
						"hasText", elem.Text != nil)
					responseDone <- nil
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
func (de *DuplexConversationExecutor) processSelfPlayDuplexTurn(
	ctx context.Context,
	req *ConversationRequest,
	turn *config.TurnDefinition,
	turnIdx int,
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
	audioResult, err := audioGen.NextUserTurnAudio(ctx, history, req.Scenario.ID)
	if err != nil {
		return fmt.Errorf("failed to generate audio for turn %d: %w", turnIdx, err)
	}

	// Get the generated text content
	generatedText := audioResult.TextResult.Response.Content

	logger.Debug("Self-play audio generated",
		"turn", turnIdx,
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

	// Send user message to pipeline for state store capture
	userMsgElem := stage.NewMessageElement(userMsg)
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
// If the audio sample rate differs from the target rate (16kHz for Gemini),
// the audio is resampled before streaming.
//
// IMPORTANT: Audio is streamed at real-time pace (not burst mode) because
// Gemini's ASM (Automatic Speech Model) expects audio to arrive at playback speed.
// Without pacing, TTS prosody pauses would appear as extended silence periods
// to the ASM, causing it to incorrectly detect turn boundaries mid-utterance.
func (de *DuplexConversationExecutor) streamSelfPlayAudio(
	ctx context.Context,
	audioData []byte,
	sampleRate int,
	inputChan chan<- stage.StreamElement,
	outputChan <-chan stage.StreamElement,
) error {
	// Resample audio if needed (Gemini expects 16kHz)
	targetSampleRate := defaultSampleRate // 16000
	if sampleRate != 0 && sampleRate != targetSampleRate {
		logger.Debug("Resampling selfplay audio",
			"from_rate", sampleRate,
			"to_rate", targetSampleRate,
			"input_bytes", len(audioData),
		)
		resampled, err := audio.ResamplePCM16(audioData, sampleRate, targetSampleRate)
		if err != nil {
			return fmt.Errorf("failed to resample audio: %w", err)
		}
		logger.Debug("Resampled selfplay audio",
			"output_bytes", len(resampled),
		)
		audioData = resampled
	}

	// Start a goroutine to collect responses
	// It waits for EndOfStream (turnComplete from Gemini) before returning.
	// This ensures we don't prematurely close the session while Gemini is still responding.
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
					responseDone <- errors.New("output channel closed unexpectedly")
					return
				}
				if elem.Error != nil {
					responseDone <- elem.Error
					return
				}

				// Check for turn completion (EndOfStream flag set by provider when turnComplete received)
				if elem.EndOfStream {
					logger.Debug("Self-play EndOfStream received (turn complete)",
						"hasMessage", elem.Message != nil,
						"hasText", elem.Text != nil)
					responseDone <- nil
					return
				}
				// Continue collecting response elements until we get turnComplete
			}
		}
	}()

	// Calculate chunk duration for real-time pacing
	// At 16kHz 16-bit mono: 640 bytes = 320 samples = 20ms of audio
	// We pace audio streaming to match real-time playback speed so that
	// Gemini's ASM interprets pauses correctly (natural speech cadence vs turn boundaries)
	chunkSize := defaultAudioChunkSize
	bytesPerSample := 2                                                                             // 16-bit = 2 bytes
	samplesPerChunk := chunkSize / bytesPerSample                                                   // 320 samples
	chunkDuration := time.Duration(samplesPerChunk) * time.Second / time.Duration(targetSampleRate) // 20ms

	logger.Debug("Streaming selfplay audio with real-time pacing",
		"chunk_size", chunkSize,
		"chunk_duration_ms", chunkDuration.Milliseconds(),
		"total_bytes", len(audioData),
		"total_chunks", (len(audioData)+chunkSize-1)/chunkSize,
	)

	// Stream audio in chunks with real-time pacing
	streamStart := time.Now()
	for offset := 0; offset < len(audioData); offset += chunkSize {
		end := offset + chunkSize
		if end > len(audioData) {
			end = len(audioData)
		}

		chunk := audioData[offset:end]
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

		// Real-time pacing: wait for chunk duration before sending next chunk
		// This ensures audio arrives at Gemini at playback speed
		chunkIdx := offset / chunkSize
		expectedTime := time.Duration(chunkIdx+1) * chunkDuration
		elapsed := time.Since(streamStart)
		if sleepTime := expectedTime - elapsed; sleepTime > 0 {
			time.Sleep(sleepTime)
		}
	}

	// Signal end of audio input for this turn
	// This triggers mock sessions to emit their auto-response
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
