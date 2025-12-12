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

	// Buffer for accumulating audio chunks
	var audioBuffer []byte
	var speechDetected bool
	var silenceStart time.Time
	turnStart := time.Now()

	// Block and accumulate chunks until turn complete
	for {
		select {
		case chunk, ok := <-ctx.StreamInput:
			if !ok {
				// Stream closed - if we have audio, process it
				if len(audioBuffer) > 0 {
					return m.processAudio(ctx, audioBuffer, next)
				}
				return errors.New("stream input closed without audio")
			}

			// Extract audio data from chunk
			if chunk.MediaDelta == nil || chunk.MediaDelta.MIMEType == "" {
				continue // Skip non-media chunks
			}

			// Get audio data (base64 decoded if needed)
			var audioData []byte
			if chunk.MediaDelta.Data != nil {
				// Assume raw bytes stored as string for now
				audioData = []byte(*chunk.MediaDelta.Data)
			} else {
				continue // No data in chunk
			}

			audioBuffer = append(audioBuffer, audioData...)

			// Run VAD analysis on this chunk
			score, err := m.analyzer.Analyze(ctx.Context, audioData)
			if err != nil {
				return err
			}

			// Check if this is speech or silence
			if score >= m.config.Threshold {
				// Speech detected
				speechDetected = true
				silenceStart = time.Time{} // Reset silence timer
			} else if speechDetected {
				// Silence after speech - start silence timer
				if silenceStart.IsZero() {
					silenceStart = time.Now()
				}

				// Check if silence duration exceeded
				if time.Since(silenceStart) >= m.config.SilenceDuration {
					// Turn complete - process audio
					return m.processAudio(ctx, audioBuffer, next)
				}
			}

			// Check max turn duration
			if time.Since(turnStart) >= m.config.MaxTurnDuration {
				// Force turn completion
				return m.processAudio(ctx, audioBuffer, next)
			}

		case <-ctx.Context.Done():
			return ctx.Context.Err()
		}
	}
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
