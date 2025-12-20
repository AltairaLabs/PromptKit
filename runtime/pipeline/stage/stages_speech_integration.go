package stage

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
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
}

const (
	defaultAudioTurnSilence     = 800 * time.Millisecond
	defaultAudioTurnMinSpeech   = 200 * time.Millisecond
	defaultAudioTurnMaxDuration = 30 * time.Second
	defaultAudioTurnSampleRate  = 16000
	bytesPerSample16Bit         = 2     // 16-bit audio = 2 bytes per sample
	defaultMinAudioBytes        = 1600  // 50ms at 16kHz 16-bit mono
	defaultTTSOutputSampleRate  = 24000 // OpenAI TTS output sample rate
	msPerSecond                 = 1000

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
type audioTurnState struct {
	audioBuffer    []byte
	speechDetected bool
	speechStart    time.Time
	silenceStart   time.Time
	turnStart      time.Time
}

// Process implements the Stage interface.
// Accumulates audio chunks until turn complete, then emits audio utterance.
func (s *AudioTurnStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	state := &audioTurnState{
		turnStart: time.Now(),
	}

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
		if elem.Metadata != nil {
			if passthrough, ok := elem.Metadata["passthrough"].(bool); ok && passthrough {
				if err := s.forwardElement(ctx, &elem, output); err != nil {
					return err
				}
				continue
			}
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

	// Check for interruption
	if s.checkInterruption(ctx, state) {
		return nil
	}

	// Check if turn is complete
	if s.shouldCompleteTurn(state) {
		if err := s.emitTurnAudio(ctx, state, output); err != nil {
			return err
		}
		s.resetState(state)
	}
	return nil
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

	// Run VAD analysis for turn detection
	_, err := s.vad.Analyze(ctx, elem.Audio.Samples)
	if err != nil {
		return err
	}

	vadState := s.vad.State()

	// Update speech detection state based on VAD
	switch vadState {
	case audio.VADStateSpeaking, audio.VADStateStarting:
		if !state.speechDetected {
			state.speechDetected = true
			state.speechStart = time.Now()
			logger.Debug("AudioTurnStage: speech started")
		}
		state.silenceStart = time.Time{} // Reset silence timer

	case audio.VADStateStopping, audio.VADStateQuiet:
		if state.speechDetected && state.silenceStart.IsZero() {
			state.silenceStart = time.Now()
			logger.Debug("AudioTurnStage: silence started")
		}
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

	// Check minimum speech duration
	if time.Since(state.speechStart) < s.config.MinSpeechDuration {
		return false
	}

	// Check silence duration
	if !state.silenceStart.IsZero() && time.Since(state.silenceStart) >= s.config.SilenceDuration {
		logger.Debug("AudioTurnStage: turn complete - silence duration exceeded")
		return true
	}

	// Check max turn duration
	if time.Since(state.turnStart) >= s.config.MaxTurnDuration {
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

	logger.Debug("AudioTurnStage: emitting turn audio",
		"bytes", len(state.audioBuffer),
		"duration_ms", len(state.audioBuffer)*msPerSecond/(s.config.SampleRate*bytesPerSample16Bit))

	elem := StreamElement{
		Audio: &AudioData{
			Samples:    state.audioBuffer,
			SampleRate: s.config.SampleRate,
			Channels:   1,
			Format:     AudioFormatPCM16,
		},
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"turn_complete": true,
		},
	}

	select {
	case output <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// resetState resets the turn state for a new turn.
func (s *AudioTurnStage) resetState(state *audioTurnState) {
	state.audioBuffer = nil
	state.speechDetected = false
	state.speechStart = time.Time{}
	state.silenceStart = time.Time{}
	state.turnStart = time.Now()
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
}

// DefaultSTTStageConfig returns sensible defaults.
func DefaultSTTStageConfig() STTStageConfig {
	return STTStageConfig{
		Language:      "en",
		SkipEmpty:     true,
		MinAudioBytes: defaultMinAudioBytes,
	}
}

// STTStage transcribes audio to text using a speech-to-text service.
//
// This is a Transform stage: audio element → text element (1:1)
type STTStage struct {
	BaseStage
	service stt.Service
	config  STTStageConfig
}

// NewSTTStage creates a new STT stage.
func NewSTTStage(service stt.Service, config STTStageConfig) *STTStage {
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

		// Transcribe
		text, err := s.service.Transcribe(ctx, elem.Audio.Samples, stt.TranscriptionConfig{
			Format:     stt.FormatPCM,
			SampleRate: elem.Audio.SampleRate,
			Channels:   elem.Audio.Channels,
			Language:   s.config.Language,
		})
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
		text = strings.TrimSpace(text)
		if text == "" {
			logger.Debug("STTStage: empty transcription, skipping")
			continue
		}

		logger.Debug("STTStage: transcribed", "textLength", len(text))

		// Create text element, preserving metadata
		outElem := StreamElement{
			Text:      &text,
			Timestamp: time.Now(),
			Metadata:  elem.Metadata,
		}

		select {
		case output <- outElem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
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
}

// DefaultTTSStageWithInterruptionConfig returns sensible defaults.
func DefaultTTSStageWithInterruptionConfig() TTSStageWithInterruptionConfig {
	return TTSStageWithInterruptionConfig{
		Voice:         "alloy",
		Speed:         1.0,
		SkipEmpty:     true,
		MinTextLength: 1,
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
// Synthesizes audio for text elements with interruption support.
func (s *TTSStageWithInterruption) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		if err := s.processElement(ctx, &elem, output); err != nil {
			return err
		}
	}

	return nil
}

// processElement handles a single element in the TTS pipeline.
func (s *TTSStageWithInterruption) processElement(
	ctx context.Context,
	elem *StreamElement,
	output chan<- StreamElement,
) error {
	text := s.extractText(elem)
	if text == "" {
		return s.forwardElement(ctx, *elem, output)
	}

	if s.shouldSkipText(text) {
		return nil
	}

	return s.synthesizeAndEmit(ctx, text, elem, output)
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
func (s *TTSStageWithInterruption) setBotSpeaking(speaking bool) {
	if s.interruption != nil {
		s.interruption.SetBotSpeaking(speaking)
	}
}

// checkAndResetInterruption checks if interrupted and resets if so.
func (s *TTSStageWithInterruption) checkAndResetInterruption() bool {
	if s.interruption != nil && s.interruption.WasInterrupted() {
		s.interruption.Reset()
		s.setBotSpeaking(false)
		return true
	}
	return false
}

// synthesizeAndEmit performs TTS synthesis and emits the audio element.
func (s *TTSStageWithInterruption) synthesizeAndEmit(
	ctx context.Context,
	text string,
	elem *StreamElement,
	output chan<- StreamElement,
) error {
	s.setBotSpeaking(true)

	if s.checkAndResetInterruption() {
		logger.Debug("TTSStageWithInterruption: interrupted before synthesis")
		return nil
	}

	audioData, err := s.performSynthesis(ctx, text, elem, output)
	if err != nil || audioData == nil {
		return err
	}

	if s.checkAndResetInterruption() {
		logger.Debug("TTSStageWithInterruption: interrupted after synthesis, discarding")
		return nil
	}

	return s.emitAudioElement(ctx, text, audioData, elem.Metadata, output)
}

// performSynthesis executes the TTS synthesis and returns audio data.
func (s *TTSStageWithInterruption) performSynthesis(
	ctx context.Context,
	text string,
	elem *StreamElement,
	output chan<- StreamElement,
) ([]byte, error) {
	reader, err := s.service.Synthesize(ctx, text, tts.SynthesisConfig{
		Voice:  s.config.Voice,
		Speed:  s.config.Speed,
		Format: tts.FormatPCM16,
	})
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
func (s *TTSStageWithInterruption) handleSynthesisError(
	ctx context.Context,
	err error,
	elem *StreamElement,
	output chan<- StreamElement,
) error {
	logger.Error("TTSStageWithInterruption: synthesis failed", "error", err)
	s.setBotSpeaking(false)
	elem.Error = err
	return s.forwardElement(ctx, *elem, output)
}

// emitAudioElement creates and emits the audio output element.
func (s *TTSStageWithInterruption) emitAudioElement(
	ctx context.Context,
	text string,
	audioData []byte,
	metadata map[string]interface{},
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
		Metadata:  metadata,
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
