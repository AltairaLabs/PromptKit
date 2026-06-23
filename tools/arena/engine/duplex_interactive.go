package engine

import (
	"context"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	arenastages "github.com/AltairaLabs/PromptKit/tools/arena/stages"
)

// RunInteractiveVoice drives a live, mic-fed voice conversation through the
// duplex pipeline until mic is closed or ctx is canceled.
//
// Mode is selected by provider capability:
//   - StreamInputSupport providers use the existing ASM pipeline
//     (buildDuplexPipeline → DuplexProviderStage → ArenaStateStoreSaveStage).
//   - Text providers use the composed VAD pipeline (Task 7, not yet implemented).
//
// Mic frames (raw PCM16 mono @ 16 kHz or provider-preferred rate) are forwarded
// to the pipeline input channel one element per frame. Response audio from the
// pipeline output channel is delivered to play concurrently; the drain goroutine
// is joined before RunInteractiveVoice returns so every response frame is
// delivered to play by the time the function exits.
//
// When mic is closed, an EndOfStream element is sent to the pipeline to signal
// end-of-user-speech to the provider (matching how processSingleDuplexTurn does
// it), then inputChan is closed and the output drain is awaited.
//
// History (transcripts + tool calls) is persisted by ArenaStateStoreSaveStage
// inside the pipeline, exactly as for a duplex scenario run.
func (de *DuplexConversationExecutor) RunInteractiveVoice(
	ctx context.Context,
	req *ConversationRequest,
	mic <-chan []byte,
	play func([]byte),
) error {
	streamProvider, ok := req.Provider.(providers.StreamInputSupport)
	if !ok {
		// Task 7 will implement a VAD-composed pipeline for text-only providers.
		return de.runInteractiveVADVoice(ctx, req, mic, play)
	}

	pipeline, err := de.buildDuplexPipeline(req, streamProvider)
	if err != nil {
		return fmt.Errorf("build duplex pipeline: %w", err)
	}

	inputChan := make(chan stage.StreamElement)
	outputChan, err := pipeline.Execute(ctx, inputChan)
	if err != nil {
		return fmt.Errorf("execute pipeline: %w", err)
	}

	// Drain the output channel in a goroutine so response audio is played back
	// concurrently with mic input streaming. The WaitGroup ensures every audio
	// frame has been delivered to play before RunInteractiveVoice returns.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		drainAudioOutput(outputChan, play)
	}()

	// Feed mic frames into the pipeline until mic closes or ctx ends, then
	// signal end-of-user-speech, close inputChan, and await drain completion.
	//
	// No user-turn placeholder is emitted here. DuplexProviderStage now
	// materializes the user turn directly from the provider's input
	// transcription when a streaming turn completes with no pre-created
	// turn_id (the continuous-streaming path), and ArenaStateStoreSaveStage
	// persists that user Message. This works across turns: each completed
	// utterance materializes its own user message.
	defer func() {
		close(inputChan)
		wg.Wait()
	}()

	return de.feedMicToPipeline(ctx, mic, inputChan)
}

// drainAudioOutput ranges over outputChan and delivers every audio frame to play.
// It exits when outputChan is closed (by the pipeline on completion or ctx cancel).
func drainAudioOutput(outputChan <-chan stage.StreamElement, play func([]byte)) {
	for elem := range outputChan {
		if elem.Audio != nil && len(elem.Audio.Samples) > 0 {
			play(elem.Audio.Samples)
		}
	}
}

// feedMicToPipeline forwards mic frames to inputChan until mic closes or ctx is
// canceled. When mic closes it sends an EndOfStream element to trigger the
// provider's end-of-user-speech response (matching the pattern used by
// streamAudioChunks), then returns nil.
func (de *DuplexConversationExecutor) feedMicToPipeline(
	ctx context.Context,
	mic <-chan []byte,
	inputChan chan<- stage.StreamElement,
) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case frame, ok := <-mic:
			if !ok {
				// Mic exhausted: signal end-of-user-speech to the pipeline.
				// Ignore send errors — the pipeline may already be shutting down
				// and the deferred close+wait in RunInteractiveVoice handles cleanup.
				endElem := stage.StreamElement{EndOfStream: true}
				select {
				case inputChan <- endElem:
				case <-ctx.Done():
				}
				return nil
			}
			audioElem := stage.StreamElement{
				Audio: &stage.AudioData{
					Samples:    frame,
					SampleRate: defaultSampleRate, // 16000 Hz
					Channels:   1,
					Format:     stage.AudioFormatPCM16,
				},
			}
			select {
			case inputChan <- audioElem:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

// runInteractiveVADVoice handles voice over a plain text provider via the
// composed STT→LLM→TTS pipeline. The STT service is resolved from req.VoiceSTT
// (set from --voice-stt) and the TTS service from the agent voice binding
// (req.VoiceOutputVoice). Mic audio is segmented into turns by the AudioTurnStage,
// transcribed, answered by the text provider's tool loop, synthesized back to
// audio, and persisted via ArenaStateStoreSaveStage — so history materializes
// identically to a text run.
func (de *DuplexConversationExecutor) runInteractiveVADVoice(
	ctx context.Context,
	req *ConversationRequest,
	mic <-chan []byte,
	play func([]byte),
) error {
	if req.VoiceSTT == nil {
		return fmt.Errorf("voice over a text agent requires an STT provider (--voice-stt)")
	}
	if de.selfPlayRegistry == nil {
		return fmt.Errorf("voice over a text agent requires a self-play registry for STT/TTS resolution")
	}

	sttSvc, err := de.selfPlayRegistry.GetSTTRegistry().GetForProvider(req.VoiceSTT)
	if err != nil {
		return fmt.Errorf("resolve STT: %w", err)
	}

	ttsSvc, err := de.resolveInteractiveTTS(req)
	if err != nil {
		return err
	}

	pipeline, err := de.buildVADComposedPipeline(req, sttSvc, ttsSvc)
	if err != nil {
		return fmt.Errorf("build VAD pipeline: %w", err)
	}

	inputChan := make(chan stage.StreamElement)
	outputChan, err := pipeline.Execute(ctx, inputChan)
	if err != nil {
		return fmt.Errorf("execute VAD pipeline: %w", err)
	}

	// Drain response audio concurrently; join before returning so every frame is
	// delivered to play by the time RunInteractiveVoice exits.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		drainAudioOutput(outputChan, play)
	}()

	defer func() {
		close(inputChan)
		wg.Wait()
	}()
	return de.feedMicToPipeline(ctx, mic, inputChan)
}

// resolveInteractiveTTS resolves the agent voice binding into a tts.Service.
// GetForProvider returns a base.TTSProvider; all concrete TTS implementations in
// the registry (the mock plus the three real vendors) also satisfy the legacy
// tts.Service interface that the TTS stage requires, so the assertion holds in
// practice. We return an error rather than panic if a future provider does not.
func (de *DuplexConversationExecutor) resolveInteractiveTTS(
	req *ConversationRequest,
) (tts.Service, error) {
	ttsProvider, err := req.Config.ResolveVoice(req.VoiceOutputVoice)
	if err != nil {
		return nil, fmt.Errorf("resolve agent voice: %w", err)
	}
	ttsBase, err := de.selfPlayRegistry.GetTTSRegistry().GetForProvider(ttsProvider)
	if err != nil {
		return nil, fmt.Errorf("resolve TTS: %w", err)
	}
	ttsSvc, ok := ttsBase.(tts.Service)
	if !ok {
		return nil, fmt.Errorf("TTS provider %s does not implement tts.Service", ttsProvider.ID)
	}
	return ttsSvc, nil
}

// buildVADComposedPipeline mirrors the SDK's buildVADPipelineStages
// (AudioTurn → STT → Provider → TTS) but inserts Arena's prompt-assembly stages
// before the provider and ArenaStateStoreSaveStage between the provider and the
// TTS stage, with the event emitter wired — so transcripts and tool calls land
// in the state store and broadcast on the event bus exactly as the text/duplex
// paths do. Save runs before TTS because TTS rewrites the assistant Message into
// a Text+Audio element, dropping the Message the save stage needs to persist.
func (de *DuplexConversationExecutor) buildVADComposedPipeline(
	req *ConversationRequest,
	sttSvc stt.Service,
	ttsSvc tts.Service,
) (*stage.StreamPipeline, error) {
	taskType := ""
	if req.Scenario != nil {
		taskType = req.Scenario.TaskType
	}
	mergedVars := de.buildMergedVariables(req)
	turnState := stage.NewTurnState()

	var emitter *events.Emitter
	if req.EventBus != nil {
		emitter = events.NewEmitter(req.EventBus, req.RunID, req.RunID, req.ConversationID)
	}

	builder := stage.NewPipelineBuilderWithConfig(
		stage.DefaultPipelineConfig().WithExecutionTimeout(0),
	)
	// 1. VAD turn segmentation: N audio chunks → 1 audio utterance.
	vadStage, err := stage.NewAudioTurnStage(de.buildInteractiveVADConfig(req))
	if err != nil {
		return nil, fmt.Errorf("audio turn stage: %w", err)
	}

	// Stages 1-4 form the fixed prefix:
	//   1. AudioTurn  — N audio chunks → 1 audio utterance.
	//   2. STT        — audio utterance → text. stt.Service embeds
	//      base.STTProvider, so it is passed directly to NewSTTStage.
	//   2a. STTUserMessage — wrap the transcript into a user Message so the
	//      provider and save stages treat the spoken turn like a typed text
	//      turn. ProviderStage only accumulates Message elements, so a bare Text
	//      element from STT would otherwise never reach the LLM or be persisted.
	//   3. Prompt assembly (same stages/order as buildDuplexPipeline) — populate
	//      turnState with the system prompt and rendered variables.
	//   4. Text provider with its tool loop, sourcing system_prompt/allowed_tools
	//      from the shared turnState (matching buildDuplexPipeline's provider stage).
	stages := []stage.Stage{
		vadStage,
		stage.NewSTTStage(sttSvc, stage.DefaultSTTStageConfig()),
		arenastages.NewSTTUserMessageStage(),
		stage.NewVariableProviderStageWithVarsAndTurnState(mergedVars, nil, turnState),
		stage.NewPromptAssemblyStageWithTurnState(de.promptRegistry, taskType, mergedVars, turnState),
		stage.NewTemplateStageWithTurnState(nil, turnState),
		stage.NewProviderStageWithTurnState(
			req.Provider, de.toolRegistry, nil, &stage.ProviderConfig{}, emitter, nil, turnState,
		),
	}

	// 5. Persist + broadcast BEFORE TTS. The save stage collects Message
	// elements (user transcript + assistant reply) and forwards them; placing it
	// before TTS is essential because TTSStageWithInterruption converts an
	// assistant Message into a Text+Audio element and drops the Message — running
	// save afterwards would persist nothing. The forwarded assistant Message
	// still carries its Content, which the TTS stage reads to synthesize audio.
	if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil {
		storeConfig := de.buildPipelineStateStoreConfig(req)
		saveStage := arenastages.NewArenaStateStoreSaveStageWithTurnState(storeConfig, turnState)
		if emitter != nil {
			saveStage = saveStage.WithEmitter(emitter)
		}
		stages = append(stages, saveStage)
	}

	// 6. AssistantTTSFilter: drop any non-assistant Message elements that slipped
	// through (e.g. the user transcript wrapped by STTUserMessageStage). Only
	// assistant Messages carry text the TTS stage should synthesize; forwarding
	// user/system/tool Messages would cause the pipeline to speak the user's own
	// words back at them.
	// 7. TTS: assistant text → audio for playback.
	stages = append(stages,
		arenastages.NewAssistantTTSFilterStage(),
		stage.NewTTSStageWithInterruption(ttsSvc, de.buildInteractiveTTSConfig(req)),
	)

	return builder.Chain(stages...).Build()
}

// buildInteractiveVADConfig returns the AudioTurnStage config for the interactive
// VAD path. It reuses buildVADConfig when the scenario declares a duplex turn-
// detection block, falling back to stage defaults for a bare interactive run
// where the scenario carries no duplex configuration.
func (de *DuplexConversationExecutor) buildInteractiveVADConfig(req *ConversationRequest) stage.AudioTurnConfig {
	if req.Scenario != nil && req.Scenario.Duplex != nil {
		return de.buildVADConfig(req)
	}
	return stage.DefaultAudioTurnConfig()
}

// buildInteractiveTTSConfig returns the TTS stage config, threading the agent
// voice id through as the synthesis voice when one is set.
func (de *DuplexConversationExecutor) buildInteractiveTTSConfig(
	req *ConversationRequest,
) stage.TTSStageWithInterruptionConfig {
	cfg := stage.DefaultTTSStageWithInterruptionConfig()
	if req.VoiceOutputVoice != "" {
		cfg.Voice = req.VoiceOutputVoice
	}
	return cfg
}
