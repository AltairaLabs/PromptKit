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
		// Pass through non-audio elements immediately
		if elem.Audio == nil {
			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}

		// Process audio chunk
		if err := s.processAudio(ctx, &elem, state); err != nil {
			logger.Error("AudioTurnStage: processing failed", "error", err)
			output <- NewErrorElement(err)
			return err
		}

		// Check for interruption
		if s.interruption != nil {
			vadState := s.vad.State()
			if interrupted, _ := s.interruption.ProcessVADState(ctx, vadState); interrupted {
				logger.Debug("AudioTurnStage: user interrupted, resetting turn")
				s.resetState(state)
				continue
			}
		}

		// Check if turn is complete
		if s.shouldCompleteTurn(state) {
			if err := s.emitTurnAudio(ctx, state, output); err != nil {
				return err
			}
			s.resetState(state)
		}
	}

	// Stream closed - emit any remaining audio
	if len(state.audioBuffer) > 0 && state.speechDetected {
		return s.emitTurnAudio(ctx, state, output)
	}

	return nil
}

// processAudio processes a single audio element.
func (s *AudioTurnStage) processAudio(
	ctx context.Context,
	elem *StreamElement,
	state *audioTurnState,
) error {
	if elem.Audio == nil || len(elem.Audio.Samples) == 0 {
		return nil
	}

	// Run VAD analysis
	_, err := s.vad.Analyze(ctx, elem.Audio.Samples)
	if err != nil {
		return err
	}

	vadState := s.vad.State()

	// Update state based on VAD
	switch vadState {
	case audio.VADStateSpeaking, audio.VADStateStarting:
		if !state.speechDetected {
			state.speechDetected = true
			state.speechStart = time.Now()
			logger.Debug("AudioTurnStage: speech started")
		}
		state.silenceStart = time.Time{} // Reset silence timer
		// Buffer audio during speech
		state.audioBuffer = append(state.audioBuffer, elem.Audio.Samples...)

	case audio.VADStateStopping, audio.VADStateQuiet:
		if state.speechDetected {
			// Still accumulate during stopping/brief silence
			state.audioBuffer = append(state.audioBuffer, elem.Audio.Samples...)
			if state.silenceStart.IsZero() {
				state.silenceStart = time.Now()
				logger.Debug("AudioTurnStage: silence started")
			}
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

		logger.Debug("STTStage: transcribed", "text", text)

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
		// Extract text content
		text := s.extractText(&elem)
		if text == "" {
			// Pass through non-text elements
			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}

		// Check if should skip
		if s.config.SkipEmpty && len(strings.TrimSpace(text)) < s.config.MinTextLength {
			continue
		}

		// Signal bot speaking before synthesis
		if s.interruption != nil {
			s.interruption.SetBotSpeaking(true)
		}

		// Check for interruption before synthesis
		if s.interruption != nil && s.interruption.WasInterrupted() {
			logger.Debug("TTSStageWithInterruption: interrupted before synthesis")
			s.interruption.Reset()
			if s.interruption != nil {
				s.interruption.SetBotSpeaking(false)
			}
			continue
		}

		// Synthesize audio
		reader, err := s.service.Synthesize(ctx, text, tts.SynthesisConfig{
			Voice:  s.config.Voice,
			Speed:  s.config.Speed,
			Format: tts.FormatPCM16,
		})
		if err != nil {
			logger.Error("TTSStageWithInterruption: synthesis failed", "error", err)
			if s.interruption != nil {
				s.interruption.SetBotSpeaking(false)
			}
			elem.Error = err
			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}

		// Read all audio (could be streaming in future)
		audioData, err := io.ReadAll(reader)
		if closeErr := reader.Close(); closeErr != nil {
			logger.Warn("TTSStageWithInterruption: failed to close reader", "error", closeErr)
		}
		if err != nil {
			logger.Error("TTSStageWithInterruption: failed to read audio", "error", err)
			if s.interruption != nil {
				s.interruption.SetBotSpeaking(false)
			}
			continue
		}

		// Check for interruption after synthesis
		if s.interruption != nil && s.interruption.WasInterrupted() {
			logger.Debug("TTSStageWithInterruption: interrupted after synthesis, discarding")
			s.interruption.Reset()
			s.interruption.SetBotSpeaking(false)
			continue
		}

		logger.Debug("TTSStageWithInterruption: synthesized",
			"text_len", len(text), "audio_bytes", len(audioData))

		// Create output element with audio
		outElem := StreamElement{
			Text: &text,
			Audio: &AudioData{
				Samples:    audioData,
				SampleRate: defaultTTSOutputSampleRate,
				Channels:   1,
				Format:     AudioFormatPCM16,
			},
			Timestamp: time.Now(),
			Metadata:  elem.Metadata,
		}

		// Signal bot done speaking
		if s.interruption != nil {
			s.interruption.SetBotSpeaking(false)
		}

		select {
		case output <- outElem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
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
