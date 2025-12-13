package middleware

import (
	"context"
	"errors"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TranscriptionService converts audio bytes to text.
// This is used by VAD middleware to transcribe buffered audio into a Message.
type TranscriptionService interface {
	Transcribe(ctx context.Context, audio []byte) (string, error)
}

// VADConfig contains configuration for VAD middleware.
type VADConfig struct {
	// Threshold for silence detection (0.0 = silence, 1.0 = speech)
	// When VAD score drops below this threshold, turn is considered complete
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
		Threshold:         defaultVADThreshold, // Below 0.3 = silence
		MinSpeechDuration: defaultMinSpeechDurationMs * time.Millisecond,
		MaxTurnDuration:   defaultMaxTurnDurationSec * time.Second,
		SilenceDuration:   defaultSilenceDurationMs * time.Millisecond,
	}
}

// VADMiddleware reads streaming audio chunks from StreamInput, detects turn boundaries,
// and creates a Message for the pipeline to process.
//
// This middleware BLOCKS until a complete turn is detected (silence after speech),
// then transcribes the audio and adds a Message to the execution context.
//
// VAD middleware should be the FIRST middleware in the pipeline (before StateStore).
type VADMiddleware struct {
	analyzer    audio.VADAnalyzer
	transcriber TranscriptionService
	config      VADConfig
}

// NewVADMiddleware creates a new VAD middleware with the given analyzer and transcriber.
func NewVADMiddleware(analyzer audio.VADAnalyzer, transcriber TranscriptionService, config VADConfig) *VADMiddleware {
	return &VADMiddleware{
		analyzer:    analyzer,
		transcriber: transcriber,
		config:      config,
	}
}

// Process implements the Middleware interface.
// It reads from StreamInput, buffers audio, detects turn complete, transcribes, and creates a Message.
func (m *VADMiddleware) Process(ctx *pipeline.ExecutionContext, next func() error) error {
	// If not streaming or no StreamInput, skip VAD processing
	if !ctx.IsStreaming() || ctx.StreamInput == nil {
		return next()
	}

	state := &vadState{
		turnStart: time.Now(),
	}

	// Block and accumulate chunks until turn complete
	for {
		select {
		case chunk, ok := <-ctx.StreamInput:
			if !ok {
				return m.handleStreamClosed(ctx, state, next)
			}

			if err := m.processChunk(ctx, &chunk, state); err != nil {
				return err
			}

			if m.shouldCompleteTurn(state) {
				return m.processAudio(ctx, state.audioBuffer, next)
			}

		case <-ctx.Context.Done():
			return ctx.Context.Err()
		}
	}
}

// vadState holds the state for VAD processing.
type vadState struct {
	audioBuffer    []byte
	speechDetected bool
	silenceStart   time.Time
	turnStart      time.Time
}

// handleStreamClosed processes buffered audio when the stream is closed.
func (m *VADMiddleware) handleStreamClosed(ctx *pipeline.ExecutionContext, state *vadState, next func() error) error {
	if len(state.audioBuffer) > 0 {
		return m.processAudio(ctx, state.audioBuffer, next)
	}
	// If no audio was buffered but we still need to continue the pipeline
	// (e.g., only text chunks were sent), just call next
	return next()
}

// processChunk extracts audio data from a chunk and runs VAD analysis.
func (m *VADMiddleware) processChunk(
	ctx *pipeline.ExecutionContext,
	chunk *providers.StreamChunk,
	state *vadState,
) error {
	audioData, ok := m.extractAudioData(chunk)
	if !ok {
		return nil // Skip non-media chunks
	}

	state.audioBuffer = append(state.audioBuffer, audioData...)

	score, err := m.analyzer.Analyze(ctx.Context, audioData)
	if err != nil {
		return err
	}

	m.updateVADState(state, score)
	return nil
}

// extractAudioData extracts audio data from a stream chunk.
func (m *VADMiddleware) extractAudioData(chunk *providers.StreamChunk) ([]byte, bool) {
	if chunk.MediaDelta == nil || chunk.MediaDelta.MIMEType == "" {
		return nil, false
	}

	if chunk.MediaDelta.Data == nil {
		return nil, false
	}

	// Assume raw bytes stored as string for now
	return []byte(*chunk.MediaDelta.Data), true
}

// updateVADState updates the VAD state based on the VAD score.
func (m *VADMiddleware) updateVADState(state *vadState, score float64) {
	if score >= m.config.Threshold {
		// Speech detected
		state.speechDetected = true
		state.silenceStart = time.Time{} // Reset silence timer
		return
	}

	// Silence detected after speech - start silence timer
	if state.speechDetected && state.silenceStart.IsZero() {
		state.silenceStart = time.Now()
	}
}

// shouldCompleteTurn checks if the turn should be completed based on silence or max duration.
func (m *VADMiddleware) shouldCompleteTurn(state *vadState) bool {
	if m.isSilenceDurationExceeded(state) {
		return true
	}

	if m.isMaxTurnDurationExceeded(state) {
		return true
	}

	return false
}

// isSilenceDurationExceeded checks if silence duration has been exceeded.
func (m *VADMiddleware) isSilenceDurationExceeded(state *vadState) bool {
	if !state.speechDetected || state.silenceStart.IsZero() {
		return false
	}
	return time.Since(state.silenceStart) >= m.config.SilenceDuration
}

// isMaxTurnDurationExceeded checks if max turn duration has been exceeded.
func (m *VADMiddleware) isMaxTurnDurationExceeded(state *vadState) bool {
	return time.Since(state.turnStart) >= m.config.MaxTurnDuration
}

// processAudio transcribes the buffered audio and creates a Message for the pipeline.
func (m *VADMiddleware) processAudio(ctx *pipeline.ExecutionContext, audioBuffer []byte, next func() error) error {
	if len(audioBuffer) == 0 {
		return errors.New("no audio to process")
	}

	// Transcribe audio to text
	text, err := m.transcriber.Transcribe(ctx.Context, audioBuffer)
	if err != nil {
		return err
	}

	if text == "" {
		return errors.New("transcription returned empty text")
	}

	// Create Message with transcribed text
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Text: &text,
			},
		},
	}
	ctx.Messages = append(ctx.Messages, msg)

	// Continue to next middleware (StateStore, validation, prompts, provider)
	return next()
}

// StreamChunk implements the Middleware interface for processing output chunks.
// VAD middleware doesn't process output, so this is a no-op.
func (m *VADMiddleware) StreamChunk(ctx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	return nil // VAD only processes input, not output
}
