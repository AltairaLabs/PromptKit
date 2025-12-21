package stage

import (
	"context"
	"errors"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Transcriber converts audio bytes to text.
// Follows Go naming convention for single-method interfaces.
type Transcriber interface {
	Transcribe(ctx context.Context, audio []byte) (string, error)
}

// VADConfig contains configuration for VAD accumulator stage.
type VADConfig struct {
	// Threshold for silence detection (0.0 = silence, 1.0 = speech)
	Threshold float64

	// MinSpeechDuration is the minimum duration of speech before turn can complete
	MinSpeechDuration time.Duration

	// MaxTurnDuration is the maximum duration before forcing turn completion
	MaxTurnDuration time.Duration

	// SilenceDuration is how long silence must persist to trigger turn complete
	SilenceDuration time.Duration
}

const (
	defaultVADThreshold        = 0.3
	defaultMinSpeechDurationMs = 300
	defaultMaxTurnDurationSec  = 30
	defaultSilenceDurationMs   = 700
)

// DefaultVADConfig returns sensible defaults for VAD configuration.
func DefaultVADConfig() VADConfig {
	return VADConfig{
		Threshold:         defaultVADThreshold,
		MinSpeechDuration: defaultMinSpeechDurationMs * time.Millisecond,
		MaxTurnDuration:   defaultMaxTurnDurationSec * time.Second,
		SilenceDuration:   defaultSilenceDurationMs * time.Millisecond,
	}
}

// VADAccumulatorStage reads streaming audio chunks, detects turn boundaries via VAD,
// and emits a single Message element with the transcribed text.
//
// This is an Accumulate stage: N audio chunks → 1 message element
type VADAccumulatorStage struct {
	BaseStage
	analyzer    audio.VADAnalyzer
	transcriber Transcriber
	config      VADConfig
}

// NewVADAccumulatorStage creates a new VAD accumulator stage.
func NewVADAccumulatorStage(
	analyzer audio.VADAnalyzer,
	transcriber Transcriber,
	config VADConfig,
) *VADAccumulatorStage {
	return &VADAccumulatorStage{
		BaseStage:   NewBaseStage("vad_accumulator", StageTypeAccumulate),
		analyzer:    analyzer,
		transcriber: transcriber,
		config:      config,
	}
}

// Process implements the Stage interface.
// Accumulates audio chunks until turn complete, then transcribes and emits a message.
func (s *VADAccumulatorStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	state := &vadState{
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
		if err := s.processAudioElement(ctx, &elem, state); err != nil {
			logger.Error("VAD audio processing failed", "error", err)
			output <- NewErrorElement(err)
			return err
		}

		// Check if turn is complete
		if s.shouldCompleteTurn(state) {
			return s.emitTranscribedMessage(ctx, state, output)
		}
	}

	// Stream closed - process any remaining audio
	if len(state.audioBuffer) > 0 {
		return s.emitTranscribedMessage(ctx, state, output)
	}

	return nil
}

// vadState holds the state for VAD processing.
type vadState struct {
	audioBuffer    []byte
	speechDetected bool
	silenceStart   time.Time
	turnStart      time.Time
}

// processAudioElement processes a single audio element.
func (s *VADAccumulatorStage) processAudioElement(
	ctx context.Context,
	elem *StreamElement,
	state *vadState,
) error {
	if elem.Audio == nil || len(elem.Audio.Samples) == 0 {
		return nil
	}

	// Append audio data to buffer
	state.audioBuffer = append(state.audioBuffer, elem.Audio.Samples...)

	// Run VAD analysis
	score, err := s.analyzer.Analyze(ctx, elem.Audio.Samples)
	if err != nil {
		return err
	}

	s.updateVADState(state, score)
	return nil
}

// updateVADState updates the VAD state based on the VAD score.
func (s *VADAccumulatorStage) updateVADState(state *vadState, score float64) {
	if score >= s.config.Threshold {
		// Speech detected
		state.speechDetected = true
		state.silenceStart = time.Time{} // Reset silence timer
		logger.Debug("VAD: speech detected", "score", score)
		return
	}

	// Silence detected after speech - start silence timer
	if state.speechDetected && state.silenceStart.IsZero() {
		state.silenceStart = time.Now()
		logger.Debug("VAD: silence started after speech")
	}
}

// shouldCompleteTurn checks if the turn should be completed.
func (s *VADAccumulatorStage) shouldCompleteTurn(state *vadState) bool {
	// Check silence duration
	if state.speechDetected && !state.silenceStart.IsZero() {
		if time.Since(state.silenceStart) >= s.config.SilenceDuration {
			logger.Debug("VAD: turn complete - silence duration exceeded")
			return true
		}
	}

	// Check max turn duration
	if time.Since(state.turnStart) >= s.config.MaxTurnDuration {
		logger.Debug("VAD: turn complete - max duration exceeded")
		return true
	}

	return false
}

// emitTranscribedMessage transcribes the audio buffer and emits a message element.
func (s *VADAccumulatorStage) emitTranscribedMessage(
	ctx context.Context,
	state *vadState,
	output chan<- StreamElement,
) error {
	if len(state.audioBuffer) == 0 {
		return errors.New("no audio to transcribe")
	}

	// Transcribe audio to text
	text, err := s.transcriber.Transcribe(ctx, state.audioBuffer)
	if err != nil {
		return err
	}

	if text == "" {
		return errors.New("transcription returned empty text")
	}

	logger.Debug("VAD: transcribed audio", "text_length", len(text))

	// Create message element
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeText,
				Text: &text,
			},
		},
	}

	elem := NewMessageElement(&msg)

	// Emit message
	select {
	case output <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
