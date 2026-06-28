package engine

import (
	"context"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
	arenastages "github.com/AltairaLabs/PromptKit/tools/arena/stages"
)

// RunInteractiveVoice drives a live, mic-fed voice conversation through the
// duplex pipeline until mic is closed or ctx is canceled.
//
// Mode selection (in priority order):
//  1. req.VoiceSTT != nil → composed VAD pipeline (AudioTurn → STT → LLM → TTS).
//     The caller explicitly configured an STT provider via --voice-stt, signaling
//     the composed path even when the underlying provider also implements
//     StreamInputSupport. The OpenAI provider implements StreamInputSupport
//     unconditionally for every model (including plain text models like gpt-4o),
//     so checking VoiceSTT first prevents a plain-text agent from being wrongly
//     routed to the ASM/realtime session path (which would attempt to create a
//     gpt-4o-realtime-preview session and fail with model_not_found).
//  2. provider implements StreamInputSupport → ASM pipeline
//     (buildDuplexPipeline → DuplexProviderStage → ArenaStateStoreSaveStage).
//  3. fallback → composed VAD pipeline (runInteractiveVADVoice will return an
//     error if VoiceSTT is nil, explaining the missing --voice-stt flag).
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
	flush func(),
) error {
	// VoiceSTT is an unambiguous "use the composed STT→LLM→TTS path" signal from
	// the caller (--voice-stt flag). Check it before the StreamInputSupport
	// type assertion: the OpenAI provider implements StreamInputSupport for every
	// model — realtime and plain-text alike — so the type assertion alone cannot
	// distinguish a gpt-4o-realtime session from a plain gpt-4o text agent.
	if req.VoiceSTT != nil {
		return de.runInteractiveVADVoice(ctx, req, mic, play, flush)
	}

	streamProvider, ok := req.Provider.(providers.StreamInputSupport)
	if !ok {
		// Provider is text-only; runInteractiveVADVoice will return an error
		// explaining that --voice-stt is required.
		return de.runInteractiveVADVoice(ctx, req, mic, play, flush)
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
		drainAudioOutput(outputChan, play, flush)
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
// When an Interrupt element arrives, onInterrupt is called (if non-nil) to flush
// in-flight speaker playback, then the element is skipped. It exits when
// outputChan is closed (by the pipeline on completion or ctx cancel).
func drainAudioOutput(outputChan <-chan stage.StreamElement, play func([]byte), onInterrupt func()) {
	for elem := range outputChan {
		if elem.Interrupt && onInterrupt != nil {
			onInterrupt()
			continue
		}
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
	flush func(),
) error {
	if req.VoiceSTT == nil {
		return fmt.Errorf("voice over a text agent requires an STT provider (--voice-stt)")
	}

	sttSvc, err := de.sttRegistry().GetForProvider(req.VoiceSTT)
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
		drainAudioOutput(outputChan, play, flush)
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
	ttsBase, err := de.ttsRegistry().GetForProvider(ttsProvider)
	if err != nil {
		return nil, fmt.Errorf("resolve TTS: %w", err)
	}
	ttsSvc, ok := ttsBase.(tts.Service)
	if !ok {
		return nil, fmt.Errorf("TTS provider %s does not implement tts.Service", ttsProvider.ID)
	}
	return ttsSvc, nil
}

// sttRegistry returns the STT registry for resolving speech-to-text services.
// STT resolution is stateless and does not depend on self-play, but the
// registries are currently housed on the self-play registry. The interactive
// voice console runs configs without a `self_play` section, so selfPlayRegistry
// is nil; in that case a fresh registry is used (resolution reads provider
// config + env, no shared state needed).
func (de *DuplexConversationExecutor) sttRegistry() *selfplay.STTRegistry {
	if de.selfPlayRegistry != nil {
		return de.selfPlayRegistry.GetSTTRegistry()
	}
	return selfplay.NewSTTRegistry()
}

// ttsRegistry mirrors sttRegistry for text-to-speech service resolution.
func (de *DuplexConversationExecutor) ttsRegistry() *selfplay.TTSRegistry {
	if de.selfPlayRegistry != nil {
		return de.selfPlayRegistry.GetTTSRegistry()
	}
	return selfplay.NewTTSRegistry()
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

	// Barge-in is OPT-IN (--barge-in). When enabled, a shared interruption
	// handler coordinates it across the AudioTurn and TTS stages: TTS marks when
	// the bot is speaking, AudioTurn fires an Interrupt when the user speaks over
	// it. It is off by default because it only works cleanly on headphones / with
	// AEC, and stopping in-flight playback needs audio-sink flush support that is
	// not wired yet — so by default the console does clean turn-taking (the agent
	// finishes speaking before the next turn is captured). A nil handler makes
	// AudioTurn never emit an Interrupt and the TTS interrupt checks no-op.
	var interruptionHandler *audio.InterruptionHandler
	if req.VoiceBargeIn {
		interruptionHandler = audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)
	}

	// 1. VAD turn segmentation: N audio chunks → 1 audio utterance, emitting an
	// EndOfTurn boundary per turn (so the streaming provider fires per turn) and,
	// when barge-in is enabled, an Interrupt when the user speaks over the agent.
	vadCfg := de.buildInteractiveVADConfig(req)
	vadCfg.EmitEndOfTurn = true
	vadCfg.InterruptionHandler = interruptionHandler
	vadStage, err := stage.NewAudioTurnStage(vadCfg)
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
			req.Provider, de.toolRegistry, nil, &stage.ProviderConfig{Streaming: true}, emitter, nil, turnState,
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
	// 7. TTS: assistant text → audio for playback. Shares the interruption
	// handler with the AudioTurn stage so it knows when the bot is speaking and
	// drops in-flight audio on barge-in.
	ttsCfg := de.buildInteractiveTTSConfig(req)
	ttsCfg.InterruptionHandler = interruptionHandler
	stages = append(stages,
		arenastages.NewAssistantTTSFilterStage(),
		// Batch the provider's streaming text deltas into complete sentences so
		// TTS speaks smooth phrases (and starts after the first sentence) instead
		// of synthesizing every token chunk separately.
		arenastages.NewTTSSentenceAggregatorStage(),
		stage.NewTTSStageWithInterruption(ttsSvc, ttsCfg),
	)

	return builder.Chain(stages...).Build()
}

// buildInteractiveVADConfig returns the AudioTurnStage config for the interactive
// VAD path. It picks up scenario-declared turn-detection thresholds (silence /
// min-speech) when the scenario carries a duplex block, but ALWAYS equips the
// stage with AdaptiveVAD (unless a test injects a VAD override).
//
// This matters: the live console always builds a scenario with a Duplex block,
// and buildVADConfig leaves cfg.VAD nil — which NewAudioTurnStage fills with
// SimpleVAD. SimpleVAD's fixed threshold does not reliably detect real mic
// speech (speechDetected never flips, so no turn ever completes and the user
// gets no reply). AdaptiveVAD tracks the ambient noise floor at runtime and
// detects speech relative to it, so it works across mic gains and quiet rooms.
func (de *DuplexConversationExecutor) buildInteractiveVADConfig(req *ConversationRequest) stage.AudioTurnConfig {
	cfg := stage.DefaultAudioTurnConfig()
	if req.Scenario != nil && req.Scenario.Duplex != nil {
		// Honor any scenario-declared silence/min-speech/max-duration thresholds.
		cfg = de.buildVADConfig(req)
	}

	// A test override wins; otherwise the interactive console always uses
	// AdaptiveVAD. buildVADConfig leaves cfg.VAD nil (→ SimpleVAD), so this is
	// what actually makes live mic speech detectable.
	switch {
	case req.vadOverride != nil:
		cfg.VAD = req.vadOverride
	case cfg.VAD == nil:
		vad, err := audio.NewAdaptiveVAD(audio.DefaultVADParams())
		if err != nil {
			// NewAdaptiveVAD only errors on invalid params; DefaultVADParams is
			// always valid, so this is a safety net, not an expected path. The
			// stage then falls back to SimpleVAD.
			logger.Warn("buildInteractiveVADConfig: AdaptiveVAD init failed, falling back to SimpleVAD", "error", err)
		} else {
			cfg.VAD = vad
		}
	}
	return cfg
}

// buildInteractiveTTSConfig returns the TTS stage config, threading the agent
// voice id through as the synthesis voice when one is set.
func (de *DuplexConversationExecutor) buildInteractiveTTSConfig(
	req *ConversationRequest,
) stage.TTSStageWithInterruptionConfig {
	cfg := stage.DefaultTTSStageWithInterruptionConfig()
	// VoiceOutputVoice is a `voices:` binding id (e.g. "agent-voice"), NOT a
	// vendor voice name. Resolve it to the bound TTS provider's configured voice
	// (e.g. "alloy"). Passing the binding id straight through makes the vendor
	// reject every synthesis ("voice must be one of nova/alloy/..."), so no audio
	// ever comes back. Fall back to the default voice if resolution fails.
	if prov, err := req.Config.ResolveVoice(req.VoiceOutputVoice); err == nil && prov.Voice != "" {
		cfg.Voice = prov.Voice
	}
	return cfg
}
