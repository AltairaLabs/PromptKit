package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/streaming"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/arenaconfig"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
	"github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
)

// turnPartTypeAudio is the TurnContentPart.Type value for audio parts.
const turnPartTypeAudio = "audio"

// Audio chunking constants for streaming PCM s16le mono into providers that
// expect ~20 ms frames.
const (
	// bytesPerSamplePCM16 is the byte size of one 16-bit PCM mono sample.
	bytesPerSamplePCM16 = 2
	// chunksPerSecond20ms is the chunk count per second when each chunk is
	// 20 ms (1000 / 20 = 50).
	chunksPerSecond20ms = 50
	// minChunkBytes is the minimum byte size for an emitted audio chunk —
	// chunks smaller than this are dropped to avoid overwhelming downstream
	// VAD with sub-sample fragments.
	minChunkBytes = 320
)

// turnLoopConfig holds configuration for the turn processing loop.
type turnLoopConfig struct {
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
	var resilience *arenaconfig.DuplexResilienceConfig
	if req.Scenario.Duplex != nil {
		resilience = req.Scenario.Duplex.GetResilience()
	}
	return &turnLoopConfig{
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
func (de *DuplexConversationExecutor) getTurnsToExecute(turn *arenaconfig.TurnDefinition) int {
	if de.isSelfPlayRole(turn.Role) && turn.Turns > 0 {
		return turn.Turns
	}
	return 1
}

// processTurnIterations handles multiple iterations of a single turn definition.
func (de *DuplexConversationExecutor) processTurnIterations(
	ctx context.Context,
	args *turnProcessingArgs,
	turn *arenaconfig.TurnDefinition,
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
			de.waitForLocalPlaybackDrained(ctx, args.req)
		}

		args.state.logicalTurnIdx++
	}
}

// handleTurnError processes a turn error and updates state. Always results in loop break.
func (de *DuplexConversationExecutor) handleTurnError(
	err error,
	args *turnProcessingArgs,
	turn *arenaconfig.TurnDefinition,
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
	turn *arenaconfig.TurnDefinition,
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

// waitForLocalPlaybackDrained blocks until any registered audio
// drain handlers (LocalSink, etc.) have caught up with the audio
// they've been streamed. This replaces the magic-number inter-turn
// sleeps that historically guarded against turn-N's assistant audio
// still being audible when turn-N+1 began.
//
// Architectural note: the AudioRouter is an observer-style bus, not
// a backpressure surface. The drain barrier is opt-in for subscribers
// that physically play audio (LocalSink) and is invoked by the
// CONTROLLER (this turn loop), never by the data-path. The pipeline
// itself stays oblivious. When no one is listening (CI runs with no
// router attached, or scenarios with no LocalSink subscribed), this
// is a no-op.
//
// Output pacing on the duplex pipeline ensures the last assistant
// audio chunk has actually been forwarded to the sink by the time
// response.done fires; this drain barrier covers the additional ~tens
// of ms the host audio device takes to drain its own output queue.
func (de *DuplexConversationExecutor) waitForLocalPlaybackDrained(
	ctx context.Context, req *ConversationRequest,
) {
	if req == nil || req.AudioRouter == nil {
		return
	}
	logger.Debug("Inter-turn drain: waiting for local playback to finish")
	req.AudioRouter.WaitOutputDrained(ctx)
	logger.Debug("Inter-turn drain: local playback finished")
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
			Meta: stage.ElementMetadata{AllResponsesReceived: true},
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
	turn *arenaconfig.TurnDefinition,
	turnIdx int,
	selfplayTurnNum int,
	baseDir string,
	inputChan chan<- stage.StreamElement,
	outputChan <-chan stage.StreamElement,
) error {
	// User turns can take three shapes in duplex mode:
	//   1. Audio parts (file-based) → stream the file directly
	//   2. Text content with no audio parts → run through TTS (scripted-text)
	//   3. Empty → error
	if turn.Role == "user" {
		if turnHasAudioPart(turn) {
			return de.streamAudioTurn(ctx, turn, baseDir, inputChan, outputChan)
		}
		if turn.Content != "" {
			return de.processScriptedTextDuplexTurn(ctx, req, turn, turnIdx, inputChan, outputChan)
		}
		return fmt.Errorf("duplex user turn %d must have audio parts or text content", turnIdx)
	}

	// For self-play turns, generate audio via TTS
	if de.isSelfPlayRole(turn.Role) {
		return de.processSelfPlayDuplexTurn(ctx, req, turn, turnIdx, selfplayTurnNum, inputChan, outputChan)
	}

	return fmt.Errorf("unsupported turn role for duplex: %s", turn.Role)
}

// turnHasAudioPart reports whether any of the turn's parts is audio.
func turnHasAudioPart(turn *arenaconfig.TurnDefinition) bool {
	for i := range turn.Parts {
		if turn.Parts[i].Type == turnPartTypeAudio {
			return true
		}
	}
	return false
}

// resolveTTSProvider returns the loaded TTS provider for a turn by looking
// up the persona's voice id in the arena's voice catalog. Returns nil, nil if
// the turn has no associated persona or the persona declares no voice.
func resolveTTSProvider(
	arenaConfig *arenaconfig.Config,
	persona *arenaconfig.UserPersonaPack,
) (*config.Provider, error) {
	if arenaConfig == nil || persona == nil || persona.Voice == "" {
		return nil, nil
	}
	return arenaConfig.ResolveVoice(persona.Voice)
}

// processScriptedTextDuplexTurn handles user turns whose content is plain text
// (no audio parts). The text is synthesized via TTS and streamed to the duplex
// provider.
//
// The scenario must declare a voice via spec.voice, which is resolved through
// the arena voice catalog (Config.ResolveVoice).
func (de *DuplexConversationExecutor) processScriptedTextDuplexTurn(
	ctx context.Context,
	req *ConversationRequest,
	turn *arenaconfig.TurnDefinition,
	turnIdx int,
	inputChan chan<- stage.StreamElement,
	outputChan <-chan stage.StreamElement,
) error {
	if de.selfPlayRegistry == nil {
		return fmt.Errorf("self-play registry not configured for duplex turn %d (required for TTS)", turnIdx)
	}

	if req.Scenario == nil || req.Scenario.Voice == "" {
		return fmt.Errorf(
			"TTS configuration required for scripted-text duplex turn %d: set spec.voice on the scenario",
			turnIdx,
		)
	}

	ttsProvider, err := req.Config.ResolveVoice(req.Scenario.Voice)
	if err != nil {
		return fmt.Errorf("resolving voice for scripted-text turn %d: %w", turnIdx, err)
	}
	return de.streamTextAsAudio(
		ctx, turn.Content, ttsProvider, nil,
		inputChan, outputChan,
	)
}

// streamAudioTurn streams audio from a file to the pipeline.
func (de *DuplexConversationExecutor) streamAudioTurn(
	ctx context.Context,
	turn *arenaconfig.TurnDefinition,
	baseDir string,
	inputChan chan<- stage.StreamElement,
	outputChan <-chan stage.StreamElement,
) error {
	// Find audio part
	var audioPart *arenaconfig.TurnContentPart
	for i := range turn.Parts {
		if turn.Parts[i].Type == turnPartTypeAudio {
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
	// Also add turn_id to typed element metadata so DuplexProviderStage can track it
	turnIDCopy := turnID
	userMsgElem.Meta.TurnID = &turnIDCopy
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
	logger.Debug("streamAudioChunks: drain start")
	if err := de.drainStaleMessages(outputChan); err != nil {
		return err
	}
	logger.Debug("streamAudioChunks: drain done; starting response collector")

	responseDone := de.startResponseCollector(ctx, outputChan, inputChan, "Turn")

	logger.Debug("streamAudioChunks: streaming from file source")
	if err := de.streamFromFileSource(ctx, source, inputChan); err != nil {
		return err
	}
	logger.Debug("streamAudioChunks: file source drained; sending EndOfStream")

	if err := de.sendEndOfStream(ctx, inputChan, "streamAudioChunks"); err != nil {
		return err
	}
	logger.Debug("streamAudioChunks: EndOfStream sent; waiting for response")

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
	chunkIdx := 0
	totalBytes := 0
	for {
		chunk, err := source.ReadChunk(defaultAudioChunkSize)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				logger.Warn("Non-EOF error while streaming audio from file source", "error", err)
			}
			logger.Debug("streamFromFileSource: source exhausted",
				"chunks_sent", chunkIdx, "bytes_sent", totalBytes)
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
			chunkIdx++
			totalBytes += len(chunk)
			if chunkIdx == 1 {
				logger.Debug("streamFromFileSource: first chunk sent")
			}
		case <-ctx.Done():
			logger.Debug("streamFromFileSource: ctx canceled while sending chunk",
				"chunk_idx", chunkIdx)
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
//
// This is a thin wrapper that:
//  1. Resolves the TTS provider via persona.voice → arena voice catalog.
//  2. Generates persona text via the self-play text generator.
//  3. Builds selfplay-specific turn metadata.
//  4. Delegates to streamTextAsAudio to synthesize, send, and stream the audio.
func (de *DuplexConversationExecutor) processSelfPlayDuplexTurn(
	ctx context.Context,
	req *ConversationRequest,
	turn *arenaconfig.TurnDefinition,
	turnIdx int,
	selfplayTurnNum int,
	inputChan chan<- stage.StreamElement,
	outputChan <-chan stage.StreamElement,
) error {
	// Validate self-play registry is available
	if de.selfPlayRegistry == nil {
		return fmt.Errorf("self-play registry not configured for duplex turn %d", turnIdx)
	}

	// Resolve TTS via the persona → voice catalog path. The persona must declare
	// a voice id (persona.Voice != "") that maps to a loaded TTS provider yaml.
	persona := de.selfPlayRegistry.GetPersona(turn.Persona)
	ttsProvider, err := resolveTTSProvider(req.Config, persona)
	if err != nil {
		return fmt.Errorf("resolving voice for persona %s: %w", turn.Persona, err)
	}
	if ttsProvider == nil {
		return fmt.Errorf(
			"TTS configuration required for self-play duplex turn %d: "+
				"persona %q has no voice declared (set persona.voice to a voice catalog entry)",
			turnIdx, turn.Persona,
		)
	}

	// Get the text content generator (no TTS — we'll synthesize inside the helper).
	textGen, err := de.selfPlayRegistry.GetContentGenerator(turn.Role, turn.Persona)
	if err != nil {
		return fmt.Errorf("failed to get content generator for turn %d: %w", turnIdx, err)
	}

	// Wire the TTS provider's persona rubric into the text generator so
	// the selfplay LLM sees the [shouts]/[sighs] vocabulary in its system
	// prompt. The audio path through GetAudioContentGenerator does this
	// for free; the duplex executor splits text generation and synthesis
	// across two registry calls, so we have to mirror the wiring here or
	// the rubric never reaches the LLM and the persona's `expressive: true`
	// opt-in is silently a no-op.
	if cg, ok := textGen.(*selfplay.ContentGenerator); ok {
		if ttsService, terr := de.selfPlayRegistry.GetTTSRegistry().GetForProvider(ttsProvider); terr == nil {
			if rp, ok := ttsService.(tts.PersonaRubricProvider); ok {
				cg.WithProviderRubric(rp.PersonaRubric())
			}
		}
	}

	// Collect conversation history from state store and trim it to what
	// the selfplay LLM actually needs: spoken turns only. Without this
	// trim the LLM sees the agent's full system prompt, tool descriptors,
	// tool-result JSON, and every internal flag on the agent messages.
	// Two consequences: it bloats token count (we observed >1000 tokens
	// of input on turn 4 of a 4-turn scenario, ≈90% of which is noise
	// the persona was never told to ignore) and it bleeds non-persona
	// context into the LLM's output ("stuff that isn't in the prompt").
	history := selfplayHistoryView(de.getConversationHistory(ctx, req))

	// Generate text. Pass the selfplay turn number so the mock provider gets the
	// correct turn response.
	opts := &selfplay.GeneratorOptions{
		SelfplayTurnIndex: selfplayTurnNum,
	}
	textResult, err := textGen.NextUserTurn(ctx, history, req.Scenario.ID, opts)
	if err != nil {
		return fmt.Errorf("failed to generate text for turn %d: %w", turnIdx, err)
	}
	if textResult == nil || textResult.Response == nil {
		return fmt.Errorf("self-play turn %d produced no text response", turnIdx)
	}
	generatedText := textResult.Response.Content
	if generatedText == "" {
		return fmt.Errorf("self-play turn %d produced empty text", turnIdx)
	}

	// Build selfplay-specific user-message metadata. The shared helper writes
	// turn_id itself; everything else here is caller-supplied.
	turnMeta := map[string]any{
		"self_play":           true,
		"persona":             turn.Persona,
		"selfplay_turn_index": selfplayTurnNum,
	}

	// Surface the persona definition and the resolved system prompt on the
	// user message so the arena dev panel can show "what drove this turn?"
	// alongside the generated text. Two distinct keys keep concerns
	// separated: _persona_yaml is the source-of-truth spec; _selfplay_prompt
	// is the rubric-prefixed, variable-substituted string actually sent to
	// the selfplay LLM. Underscore prefix matches the existing convention
	// for dev-only metadata that downstream consumers should ignore.
	stampSelfplayDevMeta(turnMeta, de.selfPlayRegistry, turn.Persona, textResult.Metadata)

	// Copy only relevant metadata from the text generation result. Avoid copying
	// pipeline internal fields like system_prompt, base_variables, etc.
	if textResult.Metadata != nil {
		relevantFields := []string{
			"self_play_provider",
			"validation_warning",
			"warning_type",
		}
		for _, field := range relevantFields {
			if v, ok := textResult.Metadata[field]; ok {
				turnMeta[field] = v
			}
		}
	}

	// Capture the persona LLM call's cost+tokens so the report can show
	// what the synthetic user side cost. Without this the report only shows
	// the agent-side cost, hiding ~50% of the run's spend on selfplay scenarios.
	if cost := textResult.CostInfo; cost.InputTokens > 0 || cost.OutputTokens > 0 || cost.TotalCost > 0 {
		turnMeta["self_play_cost"] = map[string]any{
			"input_tokens":    cost.InputTokens,
			"output_tokens":   cost.OutputTokens,
			"cached_tokens":   cost.CachedTokens,
			"input_cost_usd":  cost.InputCostUSD,
			"output_cost_usd": cost.OutputCostUSD,
			"total_cost_usd":  cost.TotalCost,
		}
	}

	return de.streamTextAsAudio(
		ctx, generatedText, ttsProvider, turnMeta,
		inputChan, outputChan,
	)
}

// streamTextAsAudio synthesizes text → audio via a loaded TTS provider config
// (capability=tts), emits a user-role pipeline element with both text and audio
// parts, and streams the audio bytes through the duplex pipeline using burst mode.
//
// turnMeta is merged into the user message's Meta map. Callers can pass nil/empty
// when no extra metadata is needed; the helper always writes turn_id so
// transcription events can be correlated.
//
// Used by both self-play turns (after generating persona text) and scripted-text
// user turns resolved via the arena voice catalog.
func (de *DuplexConversationExecutor) streamTextAsAudio(
	ctx context.Context,
	text string,
	ttsProvider *config.Provider,
	turnMeta map[string]any,
	inputChan chan<- stage.StreamElement,
	outputChan <-chan stage.StreamElement,
) error {
	if text == "" {
		return errors.New("streamTextAsAudio: text is empty")
	}
	if ttsProvider == nil {
		return errors.New("streamTextAsAudio: ttsProvider is nil")
	}
	if de.selfPlayRegistry == nil {
		return errors.New("streamTextAsAudio: self-play registry not configured (TTS service unavailable)")
	}
	stream, err := de.openTextSynthesisStream(ctx, text, ttsProvider)
	if err != nil {
		return fmt.Errorf("failed to open TTS stream: %w", err)
	}
	costStamper := func(latency time.Duration) {
		stampTTSCostInMeta(de.selfPlayRegistry, ttsProvider, text, latency, turnMeta)
	}
	return de.doStreamTextAsAudio(ctx, text, stream, turnMeta, costStamper, inputChan, outputChan)
}

// doStreamTextAsAudio is the streaming core. It takes an already-opened TTS
// stream and a costStamper closure that writes cost metadata into turnMeta
// after the stream completes.
//
// Streams audio chunks straight from the TTS reader into the pipeline
// without ever holding the full buffer in memory. Pacing happens
// downstream in the AudioPacingStage, which emits each audio element at
// the cadence its byte count and sample rate imply. The executor itself
// does no time.After pacing — chunks are pushed onto the input channel
// as fast as they arrive from the TTS service.
//
// Flow:
//  1. Open a temp PCM file the chunks are mirrored into.
//  2. Loop: read chunk → emit elem.Audio AND append to temp file.
//  3. At EOF, close temp file and emit the user Message with a FilePath
//     pointer. The MediaExternalizerStage downstream copies the file
//     into media storage and replaces FilePath with a StorageReference.
//  4. Send EndOfStream and wait for the provider's response.
//
// Memory ceiling: one TTS chunk at a time, regardless of turn length.
//
//nolint:gocyclo // sequential streaming flow with cleanup; splitting hurts readability
func (de *DuplexConversationExecutor) doStreamTextAsAudio(
	ctx context.Context,
	text string,
	stream *selfplay.AudioStreamResult,
	turnMeta map[string]any,
	costStamper func(time.Duration),
	inputChan chan<- stage.StreamElement,
	outputChan <-chan stage.StreamElement,
) error {
	defer stream.Reader.Close()
	reader := stream.Reader
	sampleRate := stream.SampleRate

	ttsStart := time.Now()
	turnID := uuid.New().String()
	logger.Debug("Text-as-audio streaming start",
		"turn_id", turnID, "text", text, "text_length", len(text), "sample_rate", sampleRate)

	// Start the response collector before audio flows so it doesn't miss
	// an instant mock-duplex response that fires the moment EndOfStream
	// reaches the provider.
	responseDone := de.startResponseCollector(ctx, outputChan, inputChan, "doStreamTextAsAudio")

	mirror, err := newTurnAudioMirror(turnID)
	if err != nil {
		return fmt.Errorf("create turn audio mirror: %w", err)
	}
	defer mirror.cleanup()

	totalBytes, err := de.pumpTTSChunks(ctx, reader, sampleRate, turnID, inputChan, mirror)
	if err != nil {
		return fmt.Errorf("stream TTS to pipeline: %w", err)
	}
	ttsLatency := time.Since(ttsStart)

	costStamper(ttsLatency)

	mirrorPath, err := mirror.finalize()
	if err != nil {
		return fmt.Errorf("finalize turn audio mirror: %w", err)
	}

	logger.Debug("Text-as-audio streaming end",
		"turn_id", turnID, "audio_bytes", totalBytes, "mirror_path", mirrorPath)

	if err := de.sendUserMessage(ctx, text, turnID, mirrorPath, turnMeta, inputChan); err != nil {
		return err
	}

	if err := de.sendEndOfStream(ctx, inputChan, "doStreamTextAsAudio"); err != nil {
		return err
	}

	return de.waitForResponse(ctx, responseDone, "doStreamTextAsAudio")
}

// defaultSilenceTailDuration is how long a tail of silent PCM packets
// we stream after the user's TTS audio ends, before signaling
// EndOfStream. Provider-native turn detectors (Gemini ASM,
// OpenAI Realtime server-side VAD) decide turn boundaries from
// content silence in the audio stream — if we just *stop sending
// packets* they have nothing to apply VAD to and fall back to a
// wall-clock timeout (~6s observed empirically). Streaming a
// tail of zero-filled PCM gives the server's VAD content to
// detect end-of-turn promptly.
//
// 2s is empirical: 1s wasn't enough for Gemini's ASM to declare
// end-of-turn when fed TTS audio (transcript came in mid-sentence
// and the model never responded). 2s reliably triggers ASM. The
// optimum varies per provider — proper config plumbing will
// follow; for now this default is overridable via the
// PROMPTKIT_SILENCE_TAIL_MS env var so we can sweep values
// without recompiling.
const defaultSilenceTailDuration = 2 * time.Second

// silenceTailDurationEnv is the env var name used to override
// defaultSilenceTailDuration at runtime. Value in milliseconds. Used
// by the sweep harness in examples/duplex-streaming/sweep-silence-tail.sh
// and by anyone tuning a new provider against ASM behavior.
const silenceTailDurationEnv = "PROMPTKIT_SILENCE_TAIL_MS"

// silenceTailDuration returns the configured silence-tail duration,
// preferring the env var override when set and parseable.
func silenceTailDuration() time.Duration {
	if v := os.Getenv(silenceTailDurationEnv); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms >= 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return defaultSilenceTailDuration
}

// pumpTTSChunks reads the TTS stream and emits each chunk as both an
// elem.Audio pipeline element and a write to the on-disk mirror. After
// the TTS body completes, it streams silenceTailDuration of zero-filled
// PCM at the same sample rate so provider-native VAD can detect
// end-of-user-speech from the audio stream content rather than from a
// connection-level timeout. Returns the total number of audio bytes
// streamed (TTS + silence tail).
func (de *DuplexConversationExecutor) pumpTTSChunks(
	ctx context.Context,
	reader io.ReadCloser,
	sampleRate int,
	turnID string,
	inputChan chan<- stage.StreamElement,
	mirror *turnAudioMirror,
) (int, error) {
	// Read in 20 ms chunks at the source sample rate (PCM16 mono =
	// 2 bytes per sample). Bounded to a sane minimum so a misconfigured
	// sample rate doesn't degenerate to byte-at-a-time reads.
	chunkBytes := sampleRate * bytesPerSamplePCM16 / chunksPerSecond20ms
	if chunkBytes < minChunkBytes {
		chunkBytes = minChunkBytes
	}
	// Use io.ReadFull so every emitted chunk is exactly chunkBytes wide.
	// Plain reader.Read returns whatever bytes are available, which means
	// when the upstream HTTP response trickles in (or the TTS service
	// flushes a small block) we'd emit short chunks like 60 or 354 bytes.
	// OpenAI Realtime's input_audio_buffer.append rejects the resulting
	// burst of small + large appends with a cryptic
	// "Invalid 'audio'... got an invalid value" error. Buffering each
	// short read until we have a full chunk eliminates that.
	buf := make([]byte, chunkBytes)
	total := 0
	turnIDCopy := turnID
	for {
		n, readErr := io.ReadFull(reader, buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])

			elem := stage.StreamElement{
				Audio: &stage.AudioData{
					Samples:    chunk,
					SampleRate: sampleRate,
					Channels:   1,
					Format:     stage.AudioFormatPCM16,
				},
			}
			elem.Meta.TurnID = &turnIDCopy
			elem.Meta.Passthrough = true

			select {
			case inputChan <- elem:
			case <-ctx.Done():
				return total, ctx.Err()
			}

			if err := mirror.write(chunk); err != nil {
				return total, fmt.Errorf("write to audio mirror: %w", err)
			}
			total += n
		}
		// io.ReadFull returns ErrUnexpectedEOF on the final partial read
		// (when the reader ends in the middle of a chunkBytes window),
		// and io.EOF only when there were zero bytes read. Both signal
		// end-of-stream — fall through to the silence tail.
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
			tailBytes, tailErr := de.streamSilenceTail(
				ctx, sampleRate, chunkBytes, turnIDCopy, inputChan, mirror)
			return total + tailBytes, tailErr
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

// streamSilenceTail emits zero-filled PCM packets for silenceTailDuration
// at the same chunk shape pumpTTSChunks uses for the TTS body. Provider-
// native turn detectors apply VAD to the audio stream content; if we
// just stop sending packets they fall back to a wall-clock timeout.
// Streaming a tail of silence gives the server's VAD content to score
// as "user has stopped speaking" and emit turnComplete promptly.
//
// Silence is also written to the on-disk mirror so the saved user-turn
// audio reflects exactly what we sent over the wire.
func (de *DuplexConversationExecutor) streamSilenceTail(
	ctx context.Context,
	sampleRate, chunkBytes int,
	turnIDCopy string,
	inputChan chan<- stage.StreamElement,
	mirror *turnAudioMirror,
) (int, error) {
	tail := silenceTailDuration()
	totalSilenceBytes := int(int64(sampleRate) * 2 * int64(tail) / int64(time.Second))
	// Zero-filled buffer represents silence at PCM16.
	silentChunk := make([]byte, chunkBytes)
	written := 0
	for written < totalSilenceBytes {
		n := chunkBytes
		if remaining := totalSilenceBytes - written; remaining < n {
			n = remaining
		}
		// Fresh allocation per chunk so the pipeline doesn't share a
		// buffer across in-flight elements.
		chunk := make([]byte, n)
		copy(chunk, silentChunk[:n])

		elem := stage.StreamElement{
			Audio: &stage.AudioData{
				Samples:    chunk,
				SampleRate: sampleRate,
				Channels:   1,
				Format:     stage.AudioFormatPCM16,
			},
		}
		elem.Meta.TurnID = &turnIDCopy
		elem.Meta.Passthrough = true

		select {
		case inputChan <- elem:
		case <-ctx.Done():
			return written, ctx.Err()
		}

		if err := mirror.write(chunk); err != nil {
			return written, fmt.Errorf("write silence to audio mirror: %w", err)
		}
		written += n
	}
	logger.Debug("streamSilenceTail: emitted",
		"turn_id", turnIDCopy, "bytes", written,
		"duration", tail)
	return written, nil
}

// sendUserMessage emits the per-turn user Message after the TTS stream
// completes. The audio MediaContent points at the on-disk mirror file via
// FilePath; downstream MediaExternalizerStage copies it into media storage
// and replaces the FilePath with a StorageReference.
func (de *DuplexConversationExecutor) sendUserMessage(
	ctx context.Context,
	text, turnID, mirrorPath string,
	turnMeta map[string]any,
	inputChan chan<- stage.StreamElement,
) error {
	textCopy := text
	parts := []types.ContentPart{{Type: types.ContentTypeText, Text: &textCopy}}
	if mirrorPath != "" {
		mp := mirrorPath
		parts = append(parts, types.ContentPart{
			Type: types.ContentTypeAudio,
			Media: &types.MediaContent{
				FilePath: &mp,
				MIMEType: "audio/pcm",
			},
		})
	}
	userMsg := &types.Message{
		Role:    "user",
		Content: textCopy,
		Parts:   parts,
		Meta:    map[string]interface{}{"turn_id": turnID},
	}
	for k, v := range turnMeta {
		if k == "turn_id" {
			continue // protected: owned by this helper
		}
		userMsg.Meta[k] = v
	}
	turnIDCopy := turnID
	userElem := stage.NewMessageElement(userMsg)
	userElem.Meta.TurnID = &turnIDCopy
	select {
	case inputChan <- userElem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// stampTTSCostInMeta writes the TTS cost for a synthesized turn into
// turnMeta under ttsCostMetaKey. The value shape mirrors the self_play_cost
// selfplayHistoryView trims the agent-side conversation history down to
// the minimum the selfplay LLM needs to plan its next reply: the spoken
// content of prior user and assistant turns, in order, with every other
// kind of message dropped.
//
// What we strip and why:
//   - system messages — the agent's system prompt is irrelevant to the
//     selfplay persona; it's also the largest single noise source
//   - tool messages — tool results (often long JSON) aren't part of the
//     spoken conversation; the persona only reacts to what was *said*
//   - assistant messages with no text content — pure-tool-call rounds
//     that produce nothing audible
//
// We also strip media parts and tool-call metadata from the kept
// messages, leaving only Role + Content. Anything in the original
// state-store Message that the selfplay LLM might over-interpret
// (latency, cost info, audio refs, finish reasons) is dropped.
//
// Order is preserved so role swapping in the downstream
// HistoryInjectionStageSwapped keeps producing a coherent dialog
// from the selfplay LLM's perspective.
func selfplayHistoryView(history []types.Message) []types.Message {
	if len(history) == 0 {
		return nil
	}
	out := make([]types.Message, 0, len(history))
	for i := range history {
		m := history[i]
		role := strings.ToLower(m.Role)
		if role != roleUser && role != roleAssistant {
			continue
		}
		text := strings.TrimSpace(m.Content)
		if text == "" {
			continue
		}
		out = append(out, types.Message{Role: m.Role, Content: text})
	}
	return out
}

// stampSelfplayDevMeta surfaces dev-only metadata for the arena UI:
// the persona's source spec (marshaled to YAML) and the resolved system
// prompt that drove the selfplay LLM call. Both go on the user message
// under underscore-prefixed keys so downstream consumers (cost rollup,
// assertion eval, exporters) ignore them as internal-only.
//
// Errors are swallowed: a missing persona or marshal failure must never
// abort the turn. The user just won't see the corresponding dev panel
// tab — same fail-soft contract as stampTTSCostInMeta.
func stampSelfplayDevMeta(
	turnMeta map[string]any,
	registry *selfplay.Registry,
	personaID string,
	textResultMetadata map[string]interface{},
) {
	if turnMeta == nil {
		return
	}
	if textResultMetadata != nil {
		if sp, ok := textResultMetadata["system_prompt"].(string); ok && sp != "" {
			turnMeta["_selfplay_prompt"] = sp
		}
	}
	if registry == nil || personaID == "" {
		return
	}
	persona := registry.GetPersona(personaID)
	if persona == nil {
		return
	}
	yamlBytes, err := yaml.Marshal(persona)
	if err != nil {
		return
	}
	turnMeta["_persona_yaml"] = string(yamlBytes)
}

// stampTTSCostInMeta writes the TTS cost for a synthesized turn into turnMeta
// under ttsCostMetaKey so addAncillaryCostFromMeta can fold it into the total
// without special-casing.
//
// Errors are swallowed: a cost lookup failure must never abort the turn.
func stampTTSCostInMeta(
	registry *selfplay.Registry,
	ttsProvider *config.Provider,
	text string,
	latency time.Duration,
	turnMeta map[string]any,
) {
	if registry == nil || ttsProvider == nil || turnMeta == nil {
		return
	}
	svc, err := registry.GetTTSRegistry().GetForProvider(ttsProvider)
	if err != nil {
		return
	}
	costMeta := base.CostInfoToMetaMap(tts.ComputeTTSCost(svc, text, latency))
	if costMeta != nil {
		turnMeta[ttsCostMetaKey] = costMeta
	}
}
