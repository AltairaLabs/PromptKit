package engine

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/streaming"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
	"github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
)

// turnLoopConfig holds configuration for the turn processing loop.
type turnLoopConfig struct {
	interTurnDelayMS         int
	selfplayInterTurnDelayMS int
	partialSuccessMinTurns   int
	ignoreLastTurnSessionEnd bool
}

// turnLoopState tracks state during turn processing.
type turnLoopState struct {
	logicalTurnIdx int
	turnErr        error
}

// turnProcessingArgs groups common arguments for turn processing functions.
// This reduces parameter counts and improves readability.
type turnProcessingArgs struct {
	req        *ConversationRequest
	baseDir    string
	inputChan  chan<- stage.StreamElement
	outputChan <-chan stage.StreamElement
	emitter    *events.Emitter
	cfg        *turnLoopConfig
	state      *turnLoopState
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
	logger.Debug("processDuplexTurns: starting", "num_turns", len(req.Scenario.Turns))

	args := &turnProcessingArgs{
		req:        req,
		baseDir:    baseDir,
		inputChan:  inputChan,
		outputChan: outputChan,
		emitter:    emitter,
		cfg:        de.getTurnLoopConfig(req),
		state:      &turnLoopState{},
	}

	de.processAllTurns(ctx, args)
	de.finalizeTurnProcessing(ctx, inputChan, outputChan, args.state.turnErr)

	return args.state.turnErr
}

// getTurnLoopConfig extracts resilience configuration for turn processing.
func (de *DuplexConversationExecutor) getTurnLoopConfig(req *ConversationRequest) *turnLoopConfig {
	var resilience *config.DuplexResilienceConfig
	if req.Scenario.Duplex != nil {
		resilience = req.Scenario.Duplex.GetResilience()
	}
	return &turnLoopConfig{
		interTurnDelayMS:         resilience.GetInterTurnDelayMs(defaultInterTurnDelayMS),
		selfplayInterTurnDelayMS: resilience.GetSelfplayInterTurnDelayMs(defaultSelfplayInterTurnDelayMS),
		partialSuccessMinTurns:   resilience.GetPartialSuccessMinTurns(defaultPartialSuccessMinTurns),
		ignoreLastTurnSessionEnd: resilience.ShouldIgnoreLastTurnSessionEnd(defaultIgnoreLastTurnSessionEnd),
	}
}

// processAllTurns iterates through all scenario turns.
func (de *DuplexConversationExecutor) processAllTurns(ctx context.Context, args *turnProcessingArgs) {
	for scenarioTurnIdx := range args.req.Scenario.Turns {
		turn := &args.req.Scenario.Turns[scenarioTurnIdx]
		turnsToExecute := de.getTurnsToExecute(turn)

		logger.Debug("processDuplexTurns: processing turn",
			"scenario_turn_idx", scenarioTurnIdx,
			"role", turn.Role,
			"turns_to_execute", turnsToExecute)

		de.processTurnIterations(ctx, args, turn, scenarioTurnIdx, turnsToExecute)

		if args.state.turnErr != nil {
			break
		}
	}
}

// getTurnsToExecute returns the number of iterations to run for a turn.
func (de *DuplexConversationExecutor) getTurnsToExecute(turn *config.TurnDefinition) int {
	if de.isSelfPlayRole(turn.Role) && turn.Turns > 0 {
		return turn.Turns
	}
	return 1
}

// processTurnIterations handles multiple iterations of a single turn definition.
func (de *DuplexConversationExecutor) processTurnIterations(
	ctx context.Context,
	args *turnProcessingArgs,
	turn *config.TurnDefinition,
	scenarioTurnIdx, turnsToExecute int,
) {
	for iteration := 0; iteration < turnsToExecute; iteration++ {
		de.emitTurnStarted(args.emitter, args.state.logicalTurnIdx, turn.Role, args.req.Scenario.ID)

		selfplayTurnNum := iteration + 1
		err := de.processSingleDuplexTurn(
			ctx, args.req, turn, args.state.logicalTurnIdx, selfplayTurnNum,
			args.baseDir, args.inputChan, args.outputChan,
		)

		if err != nil {
			de.handleTurnError(err, args, turn, scenarioTurnIdx, iteration, turnsToExecute)
			break
		}

		de.handleTurnSuccess(ctx, args.req, turn, args.state.logicalTurnIdx, args.emitter)

		isLastTurn := (iteration == turnsToExecute-1) && (scenarioTurnIdx == len(args.req.Scenario.Turns)-1)
		if !isLastTurn {
			de.applyInterTurnDelay(turn, args.cfg)
		}

		args.state.logicalTurnIdx++
	}
}

// handleTurnError processes a turn error and updates state. Always results in loop break.
func (de *DuplexConversationExecutor) handleTurnError(
	err error,
	args *turnProcessingArgs,
	turn *config.TurnDefinition,
	scenarioTurnIdx, iteration, turnsToExecute int,
) {
	totalTurns := len(args.req.Scenario.Turns)
	scenarioID := args.req.Scenario.ID
	isLastTurn := (iteration == turnsToExecute-1) && (scenarioTurnIdx == totalTurns-1)

	if errors.Is(err, errSessionEnded) {
		if args.cfg.ignoreLastTurnSessionEnd && isLastTurn && args.state.logicalTurnIdx > 0 {
			logger.Info("Session ended on final turn, treating as complete",
				"logical_turn_idx", args.state.logicalTurnIdx)
			de.emitTurnCompleted(args.emitter, args.state.logicalTurnIdx, turn.Role, scenarioID, nil)
			return
		}

		if args.state.logicalTurnIdx >= args.cfg.partialSuccessMinTurns {
			logger.Info("Session ended early, accepting partial success",
				"logical_turn_idx", args.state.logicalTurnIdx,
				"min_turns_for_success", args.cfg.partialSuccessMinTurns)
			de.emitTurnCompleted(args.emitter, args.state.logicalTurnIdx, turn.Role, scenarioID, nil)
			args.state.turnErr = errPartialSuccess
			return
		}
	}

	logger.Error("processDuplexTurns: turn failed",
		"logical_turn_idx", args.state.logicalTurnIdx,
		"iteration", iteration,
		"error", err)
	de.emitTurnCompleted(args.emitter, args.state.logicalTurnIdx, turn.Role, scenarioID, err)
	args.state.turnErr = err
}

// handleTurnSuccess processes successful turn completion.
func (de *DuplexConversationExecutor) handleTurnSuccess(
	ctx context.Context,
	req *ConversationRequest,
	turn *config.TurnDefinition,
	logicalTurnIdx int,
	emitter *events.Emitter,
) {
	logger.Debug("processDuplexTurns: turn completed successfully", "logical_turn_idx", logicalTurnIdx)

	if len(turn.Assertions) > 0 {
		de.evaluateTurnAssertions(ctx, req, turn, logicalTurnIdx)
	}

	de.emitTurnCompleted(emitter, logicalTurnIdx, turn.Role, req.Scenario.ID, nil)
	logger.Debug("Duplex turn completed", "turn", logicalTurnIdx, "role", turn.Role)
}

// applyInterTurnDelay adds a delay between turns to avoid interruption issues.
func (de *DuplexConversationExecutor) applyInterTurnDelay(turn *config.TurnDefinition, cfg *turnLoopConfig) {
	delayMS := cfg.interTurnDelayMS
	if de.isSelfPlayRole(turn.Role) {
		delayMS = cfg.selfplayInterTurnDelayMS
	}
	logger.Debug("Inter-turn delay before next turn", "delay_ms", delayMS, "was_selfplay", de.isSelfPlayRole(turn.Role))
	time.Sleep(time.Duration(delayMS) * time.Millisecond)
}

// finalizeTurnProcessing sends completion signal and drains the output channel.
func (de *DuplexConversationExecutor) finalizeTurnProcessing(
	ctx context.Context,
	inputChan chan<- stage.StreamElement,
	outputChan <-chan stage.StreamElement,
	turnErr error,
) {
	if turnErr == nil || errors.Is(turnErr, errPartialSuccess) {
		allDoneElem := stage.StreamElement{
			Metadata: map[string]interface{}{"all_responses_received": true},
		}
		select {
		case inputChan <- allDoneElem:
			logger.Debug("processDuplexTurns: sent all_responses_received signal")
		case <-ctx.Done():
		}
	}

	close(inputChan)

	drainCtx, drainCancel := context.WithTimeout(context.Background(), drainTimeoutSec*time.Second) //nolint:all // NOSONAR: Intentional - ctx may be canceled, need fresh context for drain
	defer drainCancel()
	de.drainOutputChannel(drainCtx, outputChan)
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

// streamAudioChunks streams audio from source to the pipeline and collects responses.
func (de *DuplexConversationExecutor) streamAudioChunks(
	ctx context.Context,
	source *turnexecutors.AudioFileSource,
	inputChan chan<- stage.StreamElement,
	outputChan <-chan stage.StreamElement,
) error {
	if err := de.drainStaleMessages(outputChan); err != nil {
		return err
	}

	responseDone := de.startResponseCollector(ctx, outputChan, inputChan, "Turn")

	if err := de.streamFromFileSource(ctx, source, inputChan); err != nil {
		return err
	}

	if err := de.sendEndOfStream(ctx, inputChan, "streamAudioChunks"); err != nil {
		return err
	}

	return de.waitForResponse(ctx, responseDone, "streamAudioChunks")
}

// drainStaleMessages removes stale messages from the output channel.
func (de *DuplexConversationExecutor) drainStaleMessages(outputChan <-chan stage.StreamElement) error {
	_, err := streaming.DrainStaleMessages(outputChan)
	return err
}

// startResponseCollector starts a goroutine to collect responses from the output channel.
// Uses the runtime/streaming.ResponseCollector for response handling and tool execution.
func (de *DuplexConversationExecutor) startResponseCollector(
	ctx context.Context,
	outputChan <-chan stage.StreamElement,
	inputChan chan<- stage.StreamElement,
	logPrefix string,
) <-chan error {
	collector := streaming.NewResponseCollector(streaming.ResponseCollectorConfig{
		ToolExecutor: newArenaToolExecutor(de.toolRegistry),
		LogPrefix:    logPrefix,
	})
	return collector.Start(ctx, outputChan, inputChan)
}

// streamFromFileSource streams audio chunks from a file source.
func (de *DuplexConversationExecutor) streamFromFileSource(
	ctx context.Context,
	source *turnexecutors.AudioFileSource,
	inputChan chan<- stage.StreamElement,
) error {
	for {
		chunk, err := source.ReadChunk(defaultAudioChunkSize)
		if err != nil {
			return nil // EOF or error - stop streaming
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
}

// sendEndOfStream signals end of audio input for a turn.
func (de *DuplexConversationExecutor) sendEndOfStream(
	ctx context.Context,
	inputChan chan<- stage.StreamElement,
	logPrefix string,
) error {
	_ = logPrefix // Used in streaming.SendEndOfStream's internal logging
	return streaming.SendEndOfStream(ctx, inputChan)
}

// waitForResponse waits for the response collection to complete.
func (de *DuplexConversationExecutor) waitForResponse(
	ctx context.Context,
	responseDone <-chan error,
	logPrefix string,
) error {
	err := streaming.WaitForResponse(ctx, responseDone)
	logger.Debug("response received", "component", logPrefix, "error", err)
	return err
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
	sourceSampleRate := de.getSourceSampleRate(sampleRate)

	responseDone := de.startResponseCollector(ctx, outputChan, inputChan, "Self-play")

	if err := de.streamAudioBurstMode(ctx, audioData, sourceSampleRate, inputChan); err != nil {
		return err
	}

	if err := de.sendEndOfStream(ctx, inputChan, "streamSelfPlayAudio"); err != nil {
		return err
	}

	return de.waitForResponse(ctx, responseDone, "streamSelfPlayAudio")
}

// getSourceSampleRate returns the sample rate to use, defaulting if not specified.
func (de *DuplexConversationExecutor) getSourceSampleRate(sampleRate int) int {
	if sampleRate == 0 {
		return defaultSampleRate
	}
	return sampleRate
}

// streamAudioBurstMode streams audio data as fast as possible without pacing.
// Uses the runtime/streaming.AudioStreamer for efficient audio streaming.
func (de *DuplexConversationExecutor) streamAudioBurstMode(
	ctx context.Context,
	audioData []byte,
	sampleRate int,
	inputChan chan<- stage.StreamElement,
) error {
	streamer := streaming.NewAudioStreamer()
	return streamer.StreamBurst(ctx, audioData, sampleRate, inputChan)
}
