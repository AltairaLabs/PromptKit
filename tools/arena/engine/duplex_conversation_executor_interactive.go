package engine

// This file contains integration-level code that requires full pipeline
// infrastructure to test. It is excluded from per-file coverage checks.

import (
	"context"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
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
func (de *DuplexConversationExecutor) executeDuplexConversation(
	ctx context.Context,
	req *ConversationRequest,
	streamProvider providers.StreamInputSupport,
	emitter *events.Emitter,
) *ConversationResult {
	de.emitSessionStarted(emitter, req)

	// Start duplex session with provider
	sessionConfig := de.buildSessionConfig(req)
	session, err := streamProvider.CreateStreamSession(ctx, sessionConfig)
	if err != nil {
		de.emitSessionError(emitter, req, err)
		return &ConversationResult{
			Failed: true,
			Error:  fmt.Sprintf("failed to start duplex session: %v", err),
		}
	}
	defer session.Close()

	// Build and execute the duplex pipeline
	result := de.executeDuplexPipeline(ctx, req, session, emitter)

	de.emitSessionCompleted(emitter, req)
	return result
}

// executeDuplexPipeline builds and runs the duplex streaming pipeline.
func (de *DuplexConversationExecutor) executeDuplexPipeline(
	ctx context.Context,
	req *ConversationRequest,
	session providers.StreamInputSession,
	emitter *events.Emitter,
) *ConversationResult {
	// Create pipeline for duplex streaming
	pipeline, err := de.buildDuplexPipeline(req, session)
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
func (de *DuplexConversationExecutor) buildDuplexPipeline(
	req *ConversationRequest,
	session providers.StreamInputSession,
) (*stage.StreamPipeline, error) {
	builder := stage.NewPipelineBuilder()
	var stages []stage.Stage

	// Add VAD stage if using client-side turn detection
	if de.shouldUseClientVAD(req) {
		vadConfig := de.buildVADConfig(req)
		vadStage, err := stage.NewAudioTurnStage(vadConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create VAD stage: %w", err)
		}
		stages = append(stages, vadStage)
	}

	// Add duplex provider stage
	stages = append(stages, stage.NewDuplexProviderStage(session))

	return builder.Chain(stages...).Build()
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
	defer close(inputChan)

	for turnIdx := range req.Scenario.Turns {
		turn := &req.Scenario.Turns[turnIdx]
		de.emitTurnStarted(emitter, turnIdx, turn.Role, req.Scenario.ID)

		err := de.processSingleDuplexTurn(ctx, req, turn, turnIdx, baseDir, inputChan, outputChan)
		if err != nil {
			de.emitTurnCompleted(emitter, turnIdx, turn.Role, req.Scenario.ID, err)
			return err
		}

		de.emitTurnCompleted(emitter, turnIdx, turn.Role, req.Scenario.ID, nil)
		logger.Debug("Duplex turn completed", "turn", turnIdx, "role", turn.Role)
	}

	return nil
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
	// Start a goroutine to collect responses
	responseDone := make(chan error, 1)
	go func() {
		for elem := range outputChan {
			if elem.Error != nil {
				responseDone <- elem.Error
				return
			}
			// Process response elements (text, audio)
			// These are handled by the state store stage
		}
		responseDone <- nil
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

	// Wait for response collection to complete
	select {
	case err := <-responseDone:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// processSelfPlayDuplexTurn handles self-play turns in duplex mode.
func (de *DuplexConversationExecutor) processSelfPlayDuplexTurn(
	_ context.Context,
	_ *ConversationRequest,
	_ *config.TurnDefinition,
	turnIdx int,
	_ chan<- stage.StreamElement,
	_ <-chan stage.StreamElement,
) error {
	// For self-play in duplex mode:
	// 1. Wait for assistant response (if not first turn)
	// 2. Generate user message using self-play LLM
	// 3. Convert to audio using TTS (if configured)
	// 4. Stream audio to pipeline

	// TODO: Implement self-play with TTS integration
	// This requires:
	// - Getting the content generator from selfPlayRegistry
	// - Generating text response
	// - Converting to audio via TTS service
	// - Streaming audio chunks

	return fmt.Errorf("self-play duplex turns not yet implemented (turn %d)", turnIdx)
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
