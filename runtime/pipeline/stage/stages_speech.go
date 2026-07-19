package stage

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
)

// AudioTurnConfig configures the AudioTurnStage.
type AudioTurnConfig struct {
	// VAD is the voice activity detector.
	// If nil, a SimpleVAD with default params is created.
	VAD audio.VADAnalyzer

	// TurnDetector determines when user has finished speaking.
	// If nil, VAD state transitions are used for turn detection.
	TurnDetector audio.TurnDetector

	// InterruptionHandler detects when user interrupts TTS output.
	// This should be shared with TTSStageWithInterruption.
	// If nil, interruption detection is disabled.
	InterruptionHandler *audio.InterruptionHandler

	// SilenceDuration is how long silence must persist to trigger turn complete.
	// Default: 800ms
	SilenceDuration time.Duration

	// MinSpeechDuration is minimum speech before turn can complete.
	// Default: 200ms
	MinSpeechDuration time.Duration

	// MaxTurnDuration is maximum turn length before forcing completion.
	// Default: 30s
	MaxTurnDuration time.Duration

	// SampleRate is the audio sample rate for output AudioData.
	// Default: 16000
	SampleRate int

	// EmitEndOfTurn, when true, emits an EndOfTurn control element after each
	// completed turn's audio. The streaming (continuous multi-turn) composed-VAD
	// pipeline sets this so the streaming provider stage fires once per turn.
	// Default false preserves single-shot behavior for every other consumer.
	EmitEndOfTurn bool
}

const (
	defaultAudioTurnSilence     = 800 * time.Millisecond
	defaultAudioTurnMinSpeech   = 200 * time.Millisecond
	defaultAudioTurnMaxDuration = 30 * time.Second
	defaultAudioTurnSampleRate  = 16000
	// defaultAudioTurnPreRoll is the audio kept ahead of VAD's speech-onset marker.
	// VAD reports speech only once it has accumulated enough confident frames
	// (DefaultVADStartSecs is 0.2s, and chunk granularity pushes the marker later
	// still), so trimming flush at the marker cuts into the utterance.
	//
	// Deliberately generous. The costs are asymmetric: an extra second of retained
	// silence is a rounding error against the many seconds of dead air removed,
	// whereas too small a pre-roll corrupts the transcript by clipping the
	// caller's first word. Bias toward keeping audio.
	defaultAudioTurnPreRoll    = time.Second
	bytesPerSample16Bit        = 2     // 16-bit audio = 2 bytes per sample
	defaultMinAudioBytes       = 1600  // 50ms at 16kHz 16-bit mono
	defaultTTSOutputSampleRate = 24000 // OpenAI TTS output sample rate
	msPerSecond                = 1000

	// ResponseVAD default params - tuned for TTS audio detection
	defaultResponseVADConfidence = 0.3   // Lower threshold for TTS audio
	defaultResponseVADStartSecs  = 0.05  // Quick start detection (50ms)
	defaultResponseVADStopSecs   = 0.3   // 300ms silence to confirm stop
	defaultResponseVADMinVolume  = 0.005 // Lower min volume for TTS
)

// DefaultAudioTurnConfig returns sensible defaults for AudioTurnStage.
func DefaultAudioTurnConfig() AudioTurnConfig {
	return AudioTurnConfig{
		SilenceDuration:   defaultAudioTurnSilence,
		MinSpeechDuration: defaultAudioTurnMinSpeech,
		MaxTurnDuration:   defaultAudioTurnMaxDuration,
		SampleRate:        defaultAudioTurnSampleRate,
	}
}

// AudioTurnStage detects voice activity and accumulates audio into complete turns.
// It outputs complete audio utterances when the user stops speaking.
//
// This stage consolidates:
// - Voice Activity Detection (VAD)
// - Turn boundary detection
// - Audio accumulation
// - Interruption detection (shared with TTSStageWithInterruption)
//
// This is an Accumulate stage: N audio chunks → 1 audio utterance
type AudioTurnStage struct {
	BaseStage
	config       AudioTurnConfig
	vad          audio.VADAnalyzer
	turnDetector audio.TurnDetector
	interruption *audio.InterruptionHandler
}

// NewAudioTurnStage creates a new audio turn stage.
func NewAudioTurnStage(config AudioTurnConfig) (*AudioTurnStage, error) {
	// Turn boundaries are measured in audio time (sample counts), so a zero
	// sample rate would collapse every duration to 0 and no turn would ever
	// complete. Fall back to the default rather than stalling silently.
	if config.SampleRate <= 0 {
		config.SampleRate = defaultAudioTurnSampleRate
	}

	// Create default VAD if not provided
	vad := config.VAD
	if vad == nil {
		var err error
		vad, err = audio.NewSimpleVAD(audio.DefaultVADParams())
		if err != nil {
			return nil, err
		}
	}

	return &AudioTurnStage{
		BaseStage:    NewBaseStage("audio_turn", StageTypeAccumulate),
		config:       config,
		vad:          vad,
		turnDetector: config.TurnDetector,
		interruption: config.InterruptionHandler,
	}, nil
}

// audioTurnState holds the state for turn processing.
//
// Turn boundaries are timed in AUDIO time (accumulated PCM sample counts), not
// wall-clock: a live pipeline stalls (a blocking STT call on the previous turn,
// a GC pause) or delivers a burst after a stall, so wall-clock diverges from the
// audio's own duration. Timing silence by wall-clock mis-cut turns and dropped
// the tail of an utterance. Sample counts make segmentation independent of how
// fast audio arrives.
type audioTurnState struct {
	audioBuffer    []byte
	speechDetected bool
	speechSamples  int // samples since speech first detected (~ time since speech start)
	silenceSamples int // consecutive silence samples since the last speech (0 while speaking)
	turnSamples    int // total samples accumulated this turn
	// speechStartOffset is the byte offset in audioBuffer of the chunk where VAD
	// first reported speech, or -1 while no speech has been detected. Leading
	// silence before this point (less a pre-roll) is trimmed at emit time.
	speechStartOffset int
}

// newAudioTurnState returns turn state with no speech onset recorded yet.
func newAudioTurnState() *audioTurnState {
	return &audioTurnState{speechStartOffset: -1}
}

// samplesToDuration converts a PCM16-mono sample count to a wall-clock-equivalent
// duration at the given sample rate. Returns 0 for a non-positive rate.
func samplesToDuration(samples, sampleRate int) time.Duration {
	if sampleRate <= 0 {
		return 0
	}
	return time.Duration(samples) * time.Second / time.Duration(sampleRate)
}

// Process implements the Stage interface.
// Accumulates audio chunks until turn complete, then emits audio utterance.
func (s *AudioTurnStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	state := newAudioTurnState()

	for elem := range input {
		// Handle EndOfStream specially - emit any accumulated audio first
		// This prevents EndOfStream from racing ahead of buffered audio
		if elem.EndOfStream {
			logger.Debug("AudioTurnStage: EndOfStream received, emitting accumulated audio")
			if err := s.emitRemainingAudio(ctx, state, output); err != nil {
				return err
			}
			// Reset state for next turn
			s.resetState(state)
			// Forward the EndOfStream after the audio
			if err := s.forwardElement(ctx, &elem, output); err != nil {
				return err
			}
			continue
		}

		// Pass through other non-audio elements immediately
		if elem.Audio == nil {
			if err := s.forwardElement(ctx, &elem, output); err != nil {
				return err
			}
			continue
		}

		// Check for passthrough mode - forward audio immediately without accumulation
		// This is used for selfplay/TTS audio that needs real-time streaming to the provider
		if elem.Meta.Passthrough {
			if err := s.forwardElement(ctx, &elem, output); err != nil {
				return err
			}
			continue
		}

		// Process audio chunk and handle turn completion
		if err := s.processAudioElement(ctx, &elem, state, output); err != nil {
			return err
		}
	}

	// Stream closed - emit any remaining audio
	return s.emitRemainingAudio(ctx, state, output)
}

// forwardElement forwards a non-audio element to output.
func (s *AudioTurnStage) forwardElement(
	ctx context.Context,
	elem *StreamElement,
	output chan<- StreamElement,
) error {
	select {
	case output <- *elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// processAudioElement processes an audio element and handles turn completion.
func (s *AudioTurnStage) processAudioElement(
	ctx context.Context,
	elem *StreamElement,
	state *audioTurnState,
	output chan<- StreamElement,
) error {
	if err := s.processAudio(ctx, elem, state); err != nil {
		logger.Error("AudioTurnStage: processing failed", "error", err)
		output <- NewErrorElement(err)
		return err
	}

	// Check for interruption (barge-in): user spoke while the bot was talking.
	// Emit an Interrupt control element so the provider and TTS stages cancel
	// in-flight generation/playback, then drop the partial turn.
	if s.checkInterruption(ctx, state) {
		return s.emitInterrupt(ctx, output)
	}

	// Check if turn is complete
	if s.shouldCompleteTurn(state) {
		if err := s.emitTurnAudio(ctx, state, output); err != nil {
			return err
		}
		s.resetState(state)
		// Mark the conversational turn boundary so the streaming provider stage
		// fires this turn. The trailing turn at stream close is fired by the
		// EndOfStream that follows, so it is not marked here.
		if s.config.EmitEndOfTurn {
			if err := s.emitEndOfTurn(ctx, output); err != nil {
				return err
			}
		}
	}
	return nil
}

// emitEndOfTurn sends an EndOfTurn control element downstream, marking the end
// of one conversational turn's input within the still-open session.
func (s *AudioTurnStage) emitEndOfTurn(ctx context.Context, output chan<- StreamElement) error {
	select {
	case output <- NewEndOfTurnElement():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// emitInterrupt sends an Interrupt control element downstream so the provider
// and TTS stages can cancel in-flight generation and playback on barge-in.
func (s *AudioTurnStage) emitInterrupt(ctx context.Context, output chan<- StreamElement) error {
	logger.Debug("AudioTurnStage: barge-in detected, emitting Interrupt")
	select {
	case output <- NewInterruptElement():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// checkInterruption checks for user interruption and resets state if detected.
func (s *AudioTurnStage) checkInterruption(ctx context.Context, state *audioTurnState) bool {
	if s.interruption == nil {
		return false
	}
	vadState := s.vad.State()
	if interrupted, _ := s.interruption.ProcessVADState(ctx, vadState); interrupted {
		logger.Debug("AudioTurnStage: user interrupted, resetting turn")
		s.resetState(state)
		return true
	}
	return false
}

// emitRemainingAudio emits any buffered audio when the stream closes or EndOfStream is received.
// When force=false (default), only emits if speech was detected.
// When the stream is ending, we always emit buffered audio to ensure nothing is lost.
func (s *AudioTurnStage) emitRemainingAudio(
	ctx context.Context,
	state *audioTurnState,
	output chan<- StreamElement,
) error {
	if len(state.audioBuffer) == 0 {
		return nil
	}
	// Emit buffered audio if speech was detected OR if we have significant audio data
	// For pre-recorded files, VAD may not detect speech, but we still want to forward the audio
	if state.speechDetected || len(state.audioBuffer) > defaultMinAudioBytes {
		logger.Debug("AudioTurnStage: emitting remaining audio",
			"speechDetected", state.speechDetected,
			"bufferSize", len(state.audioBuffer))
		return s.emitTurnAudio(ctx, state, output)
	}
	return nil
}

// processAudio processes a single audio element.
// Always buffers audio for forwarding - VAD is only used for turn detection, not filtering.
func (s *AudioTurnStage) processAudio(
	ctx context.Context,
	elem *StreamElement,
	state *audioTurnState,
) error {
	if elem.Audio == nil || len(elem.Audio.Samples) == 0 {
		return nil
	}

	// Always buffer audio - VAD only determines turn boundaries, not what to forward
	// This is important for pre-recorded audio files where VAD may not detect speech
	state.audioBuffer = append(state.audioBuffer, elem.Audio.Samples...)

	// Account for this chunk in AUDIO time (sample count), never wall-clock.
	n := len(elem.Audio.Samples) / bytesPerSample16Bit
	state.turnSamples += n

	// Run VAD analysis for turn detection
	_, err := s.vad.Analyze(ctx, elem.Audio.Samples)
	if err != nil {
		return err
	}

	vadState := s.vad.State()

	// Update speech/silence sample tallies based on VAD.
	switch vadState {
	case audio.VADStateSpeaking, audio.VADStateStarting:
		if !state.speechDetected {
			state.speechDetected = true
			// Record where this chunk starts so the dead air before it can be trimmed.
			state.speechStartOffset = len(state.audioBuffer) - len(elem.Audio.Samples)
			logger.Debug("AudioTurnStage: speech started")
		}
		state.silenceSamples = 0 // speech resumed; reset the silence run

	case audio.VADStateStopping, audio.VADStateQuiet:
		if state.speechDetected {
			if state.silenceSamples == 0 {
				logger.Debug("AudioTurnStage: silence started")
			}
			state.silenceSamples += n
		}
	}

	// Time-since-speech-start (incl. intra-utterance pauses), for MinSpeechDuration.
	if state.speechDetected {
		state.speechSamples += n
	}

	// Also use TurnDetector if available
	if s.turnDetector != nil {
		if _, err := s.turnDetector.ProcessAudio(ctx, elem.Audio.Samples); err != nil {
			return err
		}
	}

	return nil
}

// shouldCompleteTurn checks if the current turn should be completed.
func (s *AudioTurnStage) shouldCompleteTurn(state *audioTurnState) bool {
	// Need speech to have been detected
	if !state.speechDetected {
		return false
	}

	sr := s.config.SampleRate

	// Check minimum speech duration
	if samplesToDuration(state.speechSamples, sr) < s.config.MinSpeechDuration {
		return false
	}

	// Check silence duration (audio time, not wall-clock)
	if state.silenceSamples > 0 && samplesToDuration(state.silenceSamples, sr) >= s.config.SilenceDuration {
		logger.Debug("AudioTurnStage: turn complete - silence duration exceeded")
		return true
	}

	// Check max turn duration
	if samplesToDuration(state.turnSamples, sr) >= s.config.MaxTurnDuration {
		logger.Debug("AudioTurnStage: turn complete - max duration exceeded")
		return true
	}

	// Check TurnDetector if available
	if s.turnDetector != nil && !s.turnDetector.IsUserSpeaking() {
		logger.Debug("AudioTurnStage: turn complete - turn detector")
		return true
	}

	return false
}

// emitTurnAudio emits the accumulated audio as a complete turn.
func (s *AudioTurnStage) emitTurnAudio(
	ctx context.Context,
	state *audioTurnState,
	output chan<- StreamElement,
) error {
	if len(state.audioBuffer) == 0 {
		return nil
	}

	samples := s.trimLeadingSilence(state)

	logger.Debug("AudioTurnStage: emitting turn audio",
		"bytes", len(samples),
		"trimmed_bytes", len(state.audioBuffer)-len(samples),
		"duration_ms", len(samples)*msPerSecond/(s.config.SampleRate*bytesPerSample16Bit))

	elem := StreamElement{
		Audio: &AudioData{
			Samples:    samples,
			SampleRate: s.config.SampleRate,
			Channels:   1,
			Format:     AudioFormatPCM16,
		},
		Timestamp: time.Now(),
	}

	select {
	case output <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// trimLeadingSilence drops the dead air preceding speech onset, keeping a
// pre-roll margin. STT is billed per second and Whisper is prone to emitting
// hallucinated text over long near-silent input, so shipping a caller's several
// seconds of pause with their utterance costs money and invites phantom
// transcripts.
//
// When VAD never reported speech the buffer is returned whole. That is
// deliberate: the stage buffers unconditionally so a VAD misread mis-cuts a turn
// instead of deleting audio, and trimming must not turn a misread into silent
// data loss.
func (s *AudioTurnStage) trimLeadingSilence(state *audioTurnState) []byte {
	if !state.speechDetected || state.speechStartOffset <= 0 {
		return state.audioBuffer
	}

	preRoll := int(defaultAudioTurnPreRoll.Seconds() * float64(s.config.SampleRate) * bytesPerSample16Bit)
	start := state.speechStartOffset - preRoll
	if start <= 0 {
		return state.audioBuffer
	}
	return state.audioBuffer[start:]
}

// resetState resets the turn state for a new turn.
func (s *AudioTurnStage) resetState(state *audioTurnState) {
	state.audioBuffer = nil
	state.speechDetected = false
	state.speechSamples = 0
	state.silenceSamples = 0
	state.turnSamples = 0
	state.speechStartOffset = -1
	s.vad.Reset()
	if s.turnDetector != nil {
		s.turnDetector.Reset()
	}
}

// STTStageConfig configures the STTStage.
type STTStageConfig struct {
	// Language hint for transcription (e.g., "en")
	Language string

	// SkipEmpty skips transcription for empty audio
	SkipEmpty bool

	// MinAudioBytes is minimum audio size to transcribe
	MinAudioBytes int

	// Retry configures bounded retry on transient transcription errors.
	// Zero value uses stt.DefaultRetryConfig (3 attempts, 250ms initial).
	Retry stt.RetryConfig
}

// DefaultSTTStageConfig returns sensible defaults.
func DefaultSTTStageConfig() STTStageConfig {
	return STTStageConfig{
		Language:      "en",
		SkipEmpty:     true,
		MinAudioBytes: defaultMinAudioBytes,
		Retry:         stt.DefaultRetryConfig(),
	}
}

// STTStage transcribes audio to text using a speech-to-text service.
//
// This is a Transform stage: audio element → text element (1:1)
type STTStage struct {
	BaseStage
	service base.STTProvider
	config  STTStageConfig
}

// NewSTTStage creates a new STT stage.
// The service parameter accepts any base.STTProvider implementation.
// The legacy stt.Service interface embeds base.STTProvider, so existing callers
// that pass an stt.Service remain compatible without changes.
func NewSTTStage(service base.STTProvider, config STTStageConfig) *STTStage {
	return &STTStage{
		BaseStage: NewBaseStage("stt", StageTypeTransform),
		service:   service,
		config:    config,
	}
}

// Process implements the Stage interface.
// Transcribes audio elements to text.
func (s *STTStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		// Pass through non-audio elements
		if elem.Audio == nil || len(elem.Audio.Samples) == 0 {
			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}

		// Skip if too small
		if s.config.SkipEmpty && len(elem.Audio.Samples) < s.config.MinAudioBytes {
			logger.Debug("STTStage: skipping small audio", "bytes", len(elem.Audio.Samples))
			continue
		}

		// Build the base.STTRequest from the audio element.
		req := base.STTRequest{
			Audio:    elem.Audio.Samples,
			MIMEType: "audio/pcm",
			Hints: map[string]string{
				"sample_rate": itoa(elem.Audio.SampleRate),
				"channels":    itoa(elem.Audio.Channels),
				"language":    s.config.Language,
			},
		}

		// Transcribe (with retry on transient errors)
		sttResp, err := stt.TranscribeWithRetry(ctx, s.service, req, s.config.Retry)
		if err != nil {
			logger.Error("STTStage: transcription failed", "error", err)
			elem.Error = err
			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}

		// Skip empty transcriptions
		text := strings.TrimSpace(sttResp.Text)
		if text == "" {
			logger.Debug("STTStage: empty transcription, skipping")
			continue
		}

		logger.Debug("STTStage: transcribed", "textLength", len(text))

		// Create text element, preserving metadata
		outElem := StreamElement{
			Text:      &text,
			Timestamp: time.Now(),
			Meta:      elem.Meta,
		}

		// Stamp STT cost onto a message element when present.
		// The arena's cost rollup reads Message.Meta["stt_cost"] via ancillaryMetaCostKeys.
		if outElem.Message != nil && sttResp.Cost != nil {
			if outElem.Message.Meta == nil {
				outElem.Message.Meta = make(map[string]interface{})
			}
			outElem.Message.Meta[sttCostMetaKey] = base.CostInfoToMetaMap(sttResp.Cost)
		}

		select {
		case output <- outElem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// sttCostMetaKey is the Message.Meta key for STT ancillary cost.
// Mirrors the constant in PromptArena's engine/cost_aggregation.go.
const sttCostMetaKey = "stt_cost"

// itoa converts an int to its decimal string representation.
// Inlined here to avoid importing strconv in a hot path.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	return strconv.Itoa(n)
}

// TTSStageWithInterruptionConfig configures TTSStageWithInterruption.
type TTSStageWithInterruptionConfig struct {
	// Voice is the voice ID to use
	Voice string

	// Speed is the speech rate (0.5-2.0)
	Speed float64

	// InterruptionHandler for detecting user interrupts during TTS output.
	// Should be shared with AudioTurnStage.
	InterruptionHandler *audio.InterruptionHandler

	// SkipEmpty skips synthesis for empty text
	SkipEmpty bool

	// MinTextLength is minimum text length to synthesize
	MinTextLength int

	// Retry configures bounded retry on transient synthesis errors.
	// Zero value uses tts.DefaultRetryConfig (3 attempts, 250ms initial).
	Retry tts.RetryConfig
}

// DefaultTTSStageWithInterruptionConfig returns sensible defaults.
func DefaultTTSStageWithInterruptionConfig() TTSStageWithInterruptionConfig {
	return TTSStageWithInterruptionConfig{
		Voice:         "alloy",
		Speed:         1.0,
		SkipEmpty:     true,
		MinTextLength: 1,
		Retry:         tts.DefaultRetryConfig(),
	}
}

// TTSStageWithInterruption synthesizes text to audio with interruption support.
// When the user starts speaking (detected via shared InterruptionHandler),
// synthesis is stopped and pending output is discarded.
//
// This is a Transform stage: text element → audio element (1:1)
type TTSStageWithInterruption struct {
	BaseStage
	service      tts.Service
	config       TTSStageWithInterruptionConfig
	interruption *audio.InterruptionHandler
}

// NewTTSStageWithInterruption creates a new TTS stage with interruption support.
func NewTTSStageWithInterruption(
	service tts.Service,
	config TTSStageWithInterruptionConfig,
) *TTSStageWithInterruption {
	return &TTSStageWithInterruption{
		BaseStage:    NewBaseStage("tts_interruptible", StageTypeTransform),
		service:      service,
		config:       config,
		interruption: config.InterruptionHandler,
	}
}

// Process implements the Stage interface.
// Synthesizes audio for text elements with interruption support. A concurrent
// reader cancels in-flight synthesis the instant a barge-in Interrupt arrives,
// so a long synthesis call cannot swallow the interrupt; queued text behind the
// interrupt is dropped too, and synthesis resumes on the next turn.
func (s *TTSStageWithInterruption) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	canceller := newTurnCanceller(ctx)
	defer canceller.stop()

	work := make(chan StreamElement, interruptWorkBuffer)
	go drainCancelingOnInterrupt(input, work, canceller)

	for elem := range work {
		if elem.Interrupt {
			// In-flight synthesis was already canceled out of band by the
			// reader. Roll a fresh context for the next turn, reset the shared
			// handler so its interrupted flag does not carry over and block the
			// next turn's synthesis, then forward the Interrupt downstream so
			// playback can flush too.
			canceller.refresh()
			if s.interruption != nil {
				s.interruption.Reset()
			}
			if err := s.forwardElement(ctx, elem, output); err != nil {
				return err
			}
			continue
		}
		if err := s.processElement(ctx, canceller.context(), &elem, output); err != nil {
			return err
		}
	}

	return nil
}

// processElement handles a single element in the TTS pipeline. forwardCtx (the
// pipeline context) carries pass-through of non-synthesizable elements; synthCtx
// (cancelable per turn) governs synthesis so a barge-in can abort it. They
// differ only in the window after an interrupt cancels synthCtx but before the
// loop refreshes it — forwarding a queued control element must not fail then.
func (s *TTSStageWithInterruption) processElement(
	forwardCtx, synthCtx context.Context,
	elem *StreamElement,
	output chan<- StreamElement,
) error {
	text := s.extractText(elem)
	if text == "" {
		return s.forwardElement(forwardCtx, *elem, output)
	}

	if s.shouldSkipText(text) {
		return nil
	}

	return s.synthesizeAndEmit(synthCtx, text, elem, output)
}

// shouldSkipText checks if text should be skipped based on config.
func (s *TTSStageWithInterruption) shouldSkipText(text string) bool {
	return s.config.SkipEmpty && len(strings.TrimSpace(text)) < s.config.MinTextLength
}

// forwardElement forwards an element to output.
//
//nolint:gocritic // hugeParam: StreamElement intentionally passed by value to avoid modification
func (s *TTSStageWithInterruption) forwardElement(
	ctx context.Context,
	elem StreamElement,
	output chan<- StreamElement,
) error {
	select {
	case output <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// setBotSpeaking safely sets bot speaking state if handler exists.
// The bot-speaking flag is still needed: AudioTurnStage's ProcessVADState only
// fires a barge-in while the bot is speaking, so this gate must stay in sync.
func (s *TTSStageWithInterruption) setBotSpeaking(speaking bool) {
	if s.interruption != nil {
		s.interruption.SetBotSpeaking(speaking)
	}
}

// synthesizeAndEmit performs TTS synthesis and emits the audio element.
// Interruption is handled exclusively by the in-channel Interrupt element and
// the per-turn synthCtx (canceled by turnCanceller on barge-in), so there is
// no need to poll the shared handler here.
func (s *TTSStageWithInterruption) synthesizeAndEmit(
	ctx context.Context,
	text string,
	elem *StreamElement,
	output chan<- StreamElement,
) error {
	s.setBotSpeaking(true)

	audioData, err := s.performSynthesis(ctx, text, elem, output)
	if err != nil || audioData == nil {
		return err
	}

	return s.emitAudioElement(ctx, text, audioData, &elem.Meta, output)
}

// performSynthesis executes the TTS synthesis and returns audio data.
func (s *TTSStageWithInterruption) performSynthesis(
	ctx context.Context,
	text string,
	elem *StreamElement,
	output chan<- StreamElement,
) ([]byte, error) {
	reader, err := tts.SynthesizeWithRetry(ctx, s.service, text, tts.SynthesisConfig{
		Voice:  s.config.Voice,
		Speed:  s.config.Speed,
		Format: tts.FormatPCM16,
	}, s.config.Retry)
	if err != nil {
		return nil, s.handleSynthesisError(ctx, err, elem, output)
	}

	audioData, err := io.ReadAll(reader)
	if closeErr := reader.Close(); closeErr != nil {
		logger.Warn("TTSStageWithInterruption: failed to close reader", "error", closeErr)
	}
	if err != nil {
		logger.Error("TTSStageWithInterruption: failed to read audio", "error", err)
		s.setBotSpeaking(false)
		return nil, nil // Not a fatal error, just skip this element
	}

	return audioData, nil
}

// handleSynthesisError handles synthesis errors and emits error element.
// A context cancellation means a barge-in interrupt (or pipeline shutdown)
// aborted the synthesis — that audio is intentionally dropped, not an error to
// surface downstream.
func (s *TTSStageWithInterruption) handleSynthesisError(
	ctx context.Context,
	err error,
	elem *StreamElement,
	output chan<- StreamElement,
) error {
	s.setBotSpeaking(false)
	if errors.Is(err, context.Canceled) {
		logger.Debug("TTSStageWithInterruption: synthesis canceled (barge-in/shutdown), dropping output")
		return nil
	}
	logger.Error("TTSStageWithInterruption: synthesis failed", "error", err)
	elem.Error = err
	return s.forwardElement(ctx, *elem, output)
}

// emitAudioElement creates and emits the audio output element.
func (s *TTSStageWithInterruption) emitAudioElement(
	ctx context.Context,
	text string,
	audioData []byte,
	meta *ElementMetadata,
	output chan<- StreamElement,
) error {
	logger.Debug("TTSStageWithInterruption: synthesized",
		"text_len", len(text), "audio_bytes", len(audioData))

	outElem := StreamElement{
		Text: &text,
		Audio: &AudioData{
			Samples:    audioData,
			SampleRate: defaultTTSOutputSampleRate,
			Channels:   1,
			Format:     AudioFormatPCM16,
		},
		Timestamp: time.Now(),
	}
	if meta != nil {
		outElem.Meta = *meta
	}

	s.setBotSpeaking(false)
	return s.forwardElement(ctx, outElem, output)
}

// extractText extracts text content from an element.
func (s *TTSStageWithInterruption) extractText(elem *StreamElement) string {
	if elem.Text != nil && *elem.Text != "" {
		return *elem.Text
	}

	if elem.Message != nil {
		if elem.Message.Content != "" {
			return elem.Message.Content
		}
		for _, part := range elem.Message.Parts {
			if part.Text != nil && *part.Text != "" {
				return *part.Text
			}
		}
	}

	return ""
}

// ResponseVADConfig configures the ResponseVADStage.
type ResponseVADConfig struct {
	// VAD is the voice activity detector.
	// If nil, a SimpleVAD with default params is created.
	VAD audio.VADAnalyzer

	// SilenceDuration is how long silence must persist after EndOfStream
	// to confirm turn completion.
	// Default: 500ms
	SilenceDuration time.Duration

	// MaxWaitDuration is the maximum time to wait for silence after EndOfStream.
	// If silence is not detected within this time, EndOfStream is emitted anyway.
	// Default: 3s
	MaxWaitDuration time.Duration

	// SampleRate is the expected audio sample rate.
	// Default: 24000 (Gemini output)
	SampleRate int
}

const (
	defaultResponseVADSilence    = 500 * time.Millisecond
	defaultResponseVADMaxWait    = 3 * time.Second
	defaultResponseVADSampleRate = 24000
	responseVADCheckIntervalMs   = 50
)

// DefaultResponseVADConfig returns sensible defaults for ResponseVADStage.
func DefaultResponseVADConfig() ResponseVADConfig {
	return ResponseVADConfig{
		SilenceDuration: defaultResponseVADSilence,
		MaxWaitDuration: defaultResponseVADMaxWait,
		SampleRate:      defaultResponseVADSampleRate,
	}
}

// ResponseVADStage monitors response audio for silence and delays EndOfStream
// until actual silence is detected. This decouples turn completion from provider
// signaling (e.g., Gemini's turnComplete) which may arrive before all audio
// chunks have been received.
//
// This stage:
// 1. Passes through all elements immediately (audio, text, messages)
// 2. When EndOfStream is received from upstream, starts monitoring for silence
// 3. Only emits EndOfStream downstream when VAD confirms sustained silence
// 4. Has a max wait timeout to prevent indefinite blocking
//
// This is a Transform stage with buffering: it may hold EndOfStream temporarily.
type ResponseVADStage struct {
	BaseStage
	config ResponseVADConfig
	vad    audio.VADAnalyzer
}

// NewResponseVADStage creates a new response VAD stage.
func NewResponseVADStage(config ResponseVADConfig) (*ResponseVADStage, error) {
	// Create default VAD if not provided
	vad := config.VAD
	if vad == nil {
		// Use params tuned for response audio detection
		params := audio.VADParams{
			Confidence: defaultResponseVADConfidence,
			StartSecs:  defaultResponseVADStartSecs,
			StopSecs:   defaultResponseVADStopSecs,
			MinVolume:  defaultResponseVADMinVolume,
			SampleRate: config.SampleRate,
		}
		if config.SampleRate == 0 {
			params.SampleRate = defaultResponseVADSampleRate
		}

		var err error
		vad, err = audio.NewSimpleVAD(params)
		if err != nil {
			return nil, err
		}
	}

	return &ResponseVADStage{
		BaseStage: NewBaseStage("response_vad", StageTypeTransform),
		config:    config,
		vad:       vad,
	}, nil
}

// responseVADState holds the state for response VAD processing.
type responseVADState struct {
	// endOfStreamReceived indicates upstream signaled turn complete
	endOfStreamReceived bool
	// endOfStreamElem holds the EndOfStream element to emit after silence confirmed
	endOfStreamElem *StreamElement
	// silenceStartTime is when silence was first detected after EndOfStream
	silenceStartTime time.Time
	// endOfStreamTime is when EndOfStream was received
	endOfStreamTime time.Time
	// lastAudioTime is when the last audio chunk was received
	lastAudioTime time.Time
}

// Process implements the Stage interface.
// Monitors response audio for silence and delays EndOfStream until confirmed.
//
//nolint:gocognit // Complex state machine for audio stream processing - refactoring would hurt readability
func (s *ResponseVADStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	state := &responseVADState{}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case elem, ok := <-input:
			if !ok {
				// Input channel closed - emit any held EndOfStream
				if state.endOfStreamElem != nil {
					logger.Debug("ResponseVADStage: input closed, emitting held EndOfStream")
					if err := s.emitElement(ctx, state.endOfStreamElem, output); err != nil {
						return err
					}
				}
				return nil
			}

			// Handle EndOfStream - hold it and start monitoring for silence
			if elem.EndOfStream {
				logger.Debug("ResponseVADStage: EndOfStream received, starting silence monitoring")
				state.endOfStreamReceived = true
				state.endOfStreamElem = &elem
				state.endOfStreamTime = time.Now()
				// Initialize silence tracking based on current VAD state
				if s.vad.State() == audio.VADStateQuiet || s.vad.State() == audio.VADStateStopping {
					state.silenceStartTime = time.Now()
				}
				continue
			}

			// Process audio through VAD
			if elem.Audio != nil && len(elem.Audio.Samples) > 0 {
				state.lastAudioTime = time.Now()
				_, err := s.vad.Analyze(ctx, elem.Audio.Samples)
				if err != nil {
					logger.Error("ResponseVADStage: VAD analysis failed", "error", err)
				}

				// Update silence tracking
				vadState := s.vad.State()
				if vadState == audio.VADStateQuiet || vadState == audio.VADStateStopping {
					if state.silenceStartTime.IsZero() {
						state.silenceStartTime = time.Now()
					}
				} else {
					// Audio detected - reset silence timer
					state.silenceStartTime = time.Time{}
				}
			}

			// Forward all elements immediately
			if err := s.emitElement(ctx, &elem, output); err != nil {
				return err
			}

			// Check if we should emit held EndOfStream
			if state.endOfStreamReceived && s.shouldEmitEndOfStream(state) {
				logger.Debug("ResponseVADStage: silence confirmed, emitting EndOfStream",
					"silenceDuration", time.Since(state.silenceStartTime))
				if err := s.emitElement(ctx, state.endOfStreamElem, output); err != nil {
					return err
				}
				// Reset state for next turn
				s.vad.Reset()
				state.endOfStreamReceived = false
				state.endOfStreamElem = nil
				state.silenceStartTime = time.Time{}
			}

		default:
			// No input available - check if we should emit held EndOfStream
			if state.endOfStreamReceived && s.shouldEmitEndOfStream(state) {
				logger.Debug("ResponseVADStage: silence confirmed (no more audio), emitting EndOfStream")
				if err := s.emitElement(ctx, state.endOfStreamElem, output); err != nil {
					return err
				}
				// Reset state for next turn
				s.vad.Reset()
				state.endOfStreamReceived = false
				state.endOfStreamElem = nil
				state.silenceStartTime = time.Time{}
			}

			// Small sleep to prevent busy-waiting
			time.Sleep(responseVADCheckIntervalMs * time.Millisecond)
		}
	}
}

// shouldEmitEndOfStream determines if the held EndOfStream should be emitted.
func (s *ResponseVADStage) shouldEmitEndOfStream(state *responseVADState) bool {
	if !state.endOfStreamReceived {
		return false
	}

	silenceDuration := s.config.SilenceDuration
	if silenceDuration == 0 {
		silenceDuration = defaultResponseVADSilence
	}

	maxWait := s.config.MaxWaitDuration
	if maxWait == 0 {
		maxWait = defaultResponseVADMaxWait
	}

	// Check max wait timeout
	if time.Since(state.endOfStreamTime) >= maxWait {
		logger.Debug("ResponseVADStage: max wait exceeded, emitting EndOfStream",
			"maxWait", maxWait)
		return true
	}

	// Check if silence duration is met
	if !state.silenceStartTime.IsZero() && time.Since(state.silenceStartTime) >= silenceDuration {
		return true
	}

	return false
}

// emitElement sends an element to the output channel.
func (s *ResponseVADStage) emitElement(
	ctx context.Context,
	elem *StreamElement,
	output chan<- StreamElement,
) error {
	select {
	case output <- *elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
