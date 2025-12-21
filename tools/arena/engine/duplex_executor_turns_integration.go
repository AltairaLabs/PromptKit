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

// processDuplexTurns processes each turn in the scenario through the duplex pipeline.
func (de *DuplexConversationExecutor) processDuplexTurns(
	ctx context.Context,
	req *ConversationRequest,
	baseDir string,
	inputChan chan<- stage.StreamElement,
	outputChan <-chan stage.StreamElement,
	emitter *events.Emitter,
) error {
	logger.Debug("processDuplexTurns: starting", "numTurns", len(req.Scenario.Turns))

	cfg := de.getTurnLoopConfig(req)
	state := &turnLoopState{}

	de.processAllTurns(ctx, req, baseDir, inputChan, outputChan, emitter, cfg, state)
	de.finalizeTurnProcessing(ctx, inputChan, outputChan, state.turnErr)

	return state.turnErr
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
func (de *DuplexConversationExecutor) processAllTurns(
	ctx context.Context,
	req *ConversationRequest,
	baseDir string,
	inputChan chan<- stage.StreamElement,
	outputChan <-chan stage.StreamElement,
	emitter *events.Emitter,
	cfg *turnLoopConfig,
	state *turnLoopState,
) {
	for scenarioTurnIdx := range req.Scenario.Turns {
		turn := &req.Scenario.Turns[scenarioTurnIdx]
		turnsToExecute := de.getTurnsToExecute(turn)

		logger.Debug("processDuplexTurns: processing turn",
			"scenarioTurnIdx", scenarioTurnIdx,
			"role", turn.Role,
			"turnsToExecute", turnsToExecute)

		de.processTurnIterations(ctx, req, turn, scenarioTurnIdx, turnsToExecute, baseDir,
			inputChan, outputChan, emitter, cfg, state)

		if state.turnErr != nil {
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
	req *ConversationRequest,
	turn *config.TurnDefinition,
	scenarioTurnIdx int,
	turnsToExecute int,
	baseDir string,
	inputChan chan<- stage.StreamElement,
	outputChan <-chan stage.StreamElement,
	emitter *events.Emitter,
	cfg *turnLoopConfig,
	state *turnLoopState,
) {
	for iteration := 0; iteration < turnsToExecute; iteration++ {
		de.emitTurnStarted(emitter, state.logicalTurnIdx, turn.Role, req.Scenario.ID)

		selfplayTurnNum := iteration + 1
		err := de.processSingleDuplexTurn(
			ctx, req, turn, state.logicalTurnIdx, selfplayTurnNum, baseDir, inputChan, outputChan,
		)

		if err != nil {
			de.handleTurnError(err, turn, scenarioTurnIdx, iteration, turnsToExecute,
				len(req.Scenario.Turns), emitter, cfg, state, req.Scenario.ID)
			break
		}

		de.handleTurnSuccess(ctx, req, turn, state.logicalTurnIdx, emitter)

		isLastTurn := (iteration == turnsToExecute-1) && (scenarioTurnIdx == len(req.Scenario.Turns)-1)
		if !isLastTurn {
			de.applyInterTurnDelay(turn, cfg)
		}

		state.logicalTurnIdx++
	}
}

// handleTurnError processes a turn error and updates state. Always results in loop break.
func (de *DuplexConversationExecutor) handleTurnError(
	err error,
	turn *config.TurnDefinition,
	scenarioTurnIdx, iteration, turnsToExecute, totalTurns int,
	emitter *events.Emitter,
	cfg *turnLoopConfig,
	state *turnLoopState,
	scenarioID string,
) {
	isLastTurn := (iteration == turnsToExecute-1) && (scenarioTurnIdx == totalTurns-1)

	if errors.Is(err, errSessionEnded) {
		if cfg.ignoreLastTurnSessionEnd && isLastTurn && state.logicalTurnIdx > 0 {
			logger.Info("Session ended on final turn, treating as complete",
				"logicalTurnIdx", state.logicalTurnIdx)
			de.emitTurnCompleted(emitter, state.logicalTurnIdx, turn.Role, scenarioID, nil)
			return
		}

		if state.logicalTurnIdx >= cfg.partialSuccessMinTurns {
			logger.Info("Session ended early, accepting partial success",
				"logicalTurnIdx", state.logicalTurnIdx,
				"minTurnsForSuccess", cfg.partialSuccessMinTurns)
			de.emitTurnCompleted(emitter, state.logicalTurnIdx, turn.Role, scenarioID, nil)
			state.turnErr = errPartialSuccess
			return
		}
	}

	logger.Error("processDuplexTurns: turn failed",
		"logicalTurnIdx", state.logicalTurnIdx,
		"iteration", iteration,
		"error", err)
	de.emitTurnCompleted(emitter, state.logicalTurnIdx, turn.Role, scenarioID, err)
	state.turnErr = err
}

// handleTurnSuccess processes successful turn completion.
func (de *DuplexConversationExecutor) handleTurnSuccess(
	ctx context.Context,
	req *ConversationRequest,
	turn *config.TurnDefinition,
	logicalTurnIdx int,
	emitter *events.Emitter,
) {
	logger.Debug("processDuplexTurns: turn completed successfully", "logicalTurnIdx", logicalTurnIdx)

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
	logger.Debug("Inter-turn delay before next turn", "delayMs", delayMS, "wasSelfplay", de.isSelfPlayRole(turn.Role))
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

	drainCtx, drainCancel := context.WithTimeout(context.Background(), drainTimeoutSec*time.Second)
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
	drainCount := 0
	for {
		select {
		case elem, ok := <-outputChan:
			if !ok {
				logger.Debug("drainStaleMessages: session ended during drain (channel closed)")
				return errSessionEnded
			}
			drainCount++
			logger.Debug("drainStaleMessages: drained stale element",
				"hasText", elem.Text != nil, "endOfStream", elem.EndOfStream)
		default:
			if drainCount > 0 {
				logger.Debug("drainStaleMessages: drained stale messages", "count", drainCount)
			}
			return nil
		}
	}
}

// startResponseCollector starts a goroutine to collect responses from the output channel.
func (de *DuplexConversationExecutor) startResponseCollector(
	ctx context.Context,
	outputChan <-chan stage.StreamElement,
	inputChan chan<- stage.StreamElement,
	logPrefix string,
) <-chan error {
	responseDone := make(chan error, 1)
	go de.collectResponses(ctx, outputChan, inputChan, responseDone, logPrefix)
	return responseDone
}

// collectResponses processes elements from the output channel until complete.
func (de *DuplexConversationExecutor) collectResponses(
	ctx context.Context,
	outputChan <-chan stage.StreamElement,
	inputChan chan<- stage.StreamElement,
	responseDone chan<- error,
	logPrefix string,
) {
	for {
		select {
		case <-ctx.Done():
			responseDone <- ctx.Err()
			return

		case elem, ok := <-outputChan:
			if !ok {
				logger.Debug(logPrefix + " response channel closed before receiving complete response")
				responseDone <- fmt.Errorf("session ended before receiving response: %w", errSessionEnded)
				return
			}

			action, err := processResponseElement(&elem, logPrefix)
			done := de.handleResponseAction(ctx, action, err, &elem, inputChan, responseDone, logPrefix)
			if done {
				return
			}
		}
	}
}

// handleResponseAction processes the response action and returns true if collection is done.
func (de *DuplexConversationExecutor) handleResponseAction(
	ctx context.Context,
	action responseAction,
	err error,
	elem *stage.StreamElement,
	inputChan chan<- stage.StreamElement,
	responseDone chan<- error,
	logPrefix string,
) bool {
	switch action {
	case responseActionContinue:
		return false
	case responseActionComplete:
		responseDone <- nil
		return true
	case responseActionError:
		responseDone <- err
		return true
	case responseActionToolCalls:
		if elem.Message != nil && len(elem.Message.ToolCalls) > 0 {
			toolResult := de.executeToolCalls(ctx, elem.Message.ToolCalls)
			if toolResult != nil && len(toolResult.providerResponses) > 0 {
				if err := de.sendToolResults(ctx, toolResult, inputChan); err != nil {
					logger.Error(logPrefix+": failed to send tool results", "error", err)
					responseDone <- err
					return true
				}
			}
		}
		return false
	}
	return false
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
	logger.Debug(logPrefix + ": sending EndOfStream signal to trigger response")
	endOfTurn := stage.StreamElement{EndOfStream: true}
	select {
	case inputChan <- endOfTurn:
		logger.Debug(logPrefix + ": EndOfStream signal sent, waiting for response")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// waitForResponse waits for the response collection to complete.
func (de *DuplexConversationExecutor) waitForResponse(
	ctx context.Context,
	responseDone <-chan error,
	logPrefix string,
) error {
	select {
	case err := <-responseDone:
		logger.Debug(logPrefix+": response received", "error", err)
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
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
func (de *DuplexConversationExecutor) streamAudioBurstMode(
	ctx context.Context,
	audioData []byte,
	sampleRate int,
	inputChan chan<- stage.StreamElement,
) error {
	chunkSize := defaultAudioChunkSize
	totalChunks := (len(audioData) + chunkSize - 1) / chunkSize

	logger.Debug("Streaming selfplay audio in BURST MODE",
		"chunk_size", chunkSize,
		"source_sample_rate", sampleRate,
		"total_bytes", len(audioData),
		"total_chunks", totalChunks,
	)

	streamStart := time.Now()
	for offset := 0; offset < len(audioData); offset += chunkSize {
		chunk, chunkIdx := de.getAudioChunk(audioData, offset, chunkSize)

		if err := de.sendAudioChunk(ctx, chunk, sampleRate, inputChan); err != nil {
			return err
		}

		de.logBurstProgress(chunkIdx, totalChunks, streamStart, len(chunk))
	}

	return nil
}

// getAudioChunk extracts a chunk from audio data at the given offset.
func (de *DuplexConversationExecutor) getAudioChunk(
	audioData []byte, offset, chunkSize int,
) (chunk []byte, chunkIdx int) {
	end := offset + chunkSize
	if end > len(audioData) {
		end = len(audioData)
	}
	return audioData[offset:end], offset / chunkSize
}

// sendAudioChunk sends an audio chunk to the input channel.
func (de *DuplexConversationExecutor) sendAudioChunk(
	ctx context.Context,
	chunk []byte,
	sampleRate int,
	inputChan chan<- stage.StreamElement,
) error {
	elem := stage.StreamElement{
		Audio: &stage.AudioData{
			Samples:    chunk,
			SampleRate: sampleRate,
			Channels:   1,
			Format:     stage.AudioFormatPCM16,
		},
		Metadata: map[string]interface{}{
			"passthrough": true,
		},
	}

	select {
	case inputChan <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// logBurstProgress logs progress for first, middle, and last chunks.
func (de *DuplexConversationExecutor) logBurstProgress(
	chunkIdx, totalChunks int, streamStart time.Time, chunkBytes int,
) {
	if chunkIdx == 0 || chunkIdx == totalChunks/2 || chunkIdx == totalChunks-1 {
		logger.Debug("Selfplay audio chunk sent (burst mode)",
			"chunk_idx", chunkIdx,
			"total_chunks", totalChunks,
			"elapsed_ms", time.Since(streamStart).Milliseconds(),
			"chunk_bytes", chunkBytes,
		)
	}
}
