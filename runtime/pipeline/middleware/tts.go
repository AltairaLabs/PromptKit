package middleware

import (
	"context"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TTSService converts text to audio.
// This is used by TTS middleware to synthesize audio for streaming output chunks.
type TTSService interface {
	// Synthesize converts text to audio bytes.
	// Returns raw audio data (typically PCM or encoded format like MP3).
	Synthesize(ctx context.Context, text string) ([]byte, error)

	// MIMEType returns the MIME type of the synthesized audio.
	// e.g., "audio/wav", "audio/mpeg", "audio/webm"
	MIMEType() string
}

// TTSConfig contains configuration for TTS middleware.
type TTSConfig struct {
	// SkipEmpty skips synthesis for empty or whitespace-only text
	SkipEmpty bool

	// MinTextLength is the minimum text length to synthesize (0 = no minimum)
	MinTextLength int
}

// DefaultTTSConfig returns sensible defaults for TTS configuration.
func DefaultTTSConfig() TTSConfig {
	return TTSConfig{
		SkipEmpty:     true,
		MinTextLength: 1,
	}
}

// TTSMiddleware synthesizes audio for streaming text output chunks.
//
// This middleware uses the StreamChunk() hook to process each output chunk,
// adding audio to the chunk's MediaDelta field. It does NOT modify the request
// or block the pipeline - all processing happens on output chunks.
//
// TTS middleware should typically be placed near the end of the pipeline
// (after provider middleware that generates the streaming output).
type TTSMiddleware struct {
	tts    TTSService
	config TTSConfig
}

// NewTTSMiddleware creates a new TTS middleware with the given service.
func NewTTSMiddleware(tts TTSService, config TTSConfig) *TTSMiddleware {
	return &TTSMiddleware{
		tts:    tts,
		config: config,
	}
}

// Process implements the Middleware interface.
// TTS middleware doesn't modify the request, so this just calls next().
func (m *TTSMiddleware) Process(ctx *pipeline.ExecutionContext, next func() error) error {
	return next()
}

// StreamChunk implements the Middleware interface for processing output chunks.
// This is where TTS synthesis happens - it adds audio to each text chunk.
func (m *TTSMiddleware) StreamChunk(ctx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	// Only process if streaming is enabled
	if !ctx.IsStreaming() {
		return nil
	}

	// Extract text from chunk
	text := m.getTextFromChunk(chunk)

	// Trim whitespace for empty check
	trimmedText := strings.TrimSpace(text)

	if trimmedText == "" && m.config.SkipEmpty {
		return nil // Skip empty chunks
	}

	if len(trimmedText) < m.config.MinTextLength {
		return nil // Skip chunks below minimum length
	}

	// Synthesize audio for this text
	audio, err := m.tts.Synthesize(ctx.Context, text)
	if err != nil {
		// TTS error - interrupt streaming
		ctx.InterruptStream("TTS synthesis failed: " + err.Error())
		return err
	}

	// Add audio to chunk as MediaDelta
	audioData := string(audio)
	chunk.MediaDelta = &types.MediaContent{
		Data:     &audioData,
		MIMEType: m.tts.MIMEType(),
	}

	return nil
}

// getTextFromChunk extracts text content from a chunk.
// Checks Delta first (incremental), then Content (full).
func (m *TTSMiddleware) getTextFromChunk(chunk *providers.StreamChunk) string {
	// Try Delta first (most common for streaming)
	if chunk.Delta != "" {
		return chunk.Delta
	}

	// Try Content (full chunk text)
	if chunk.Content != "" {
		return chunk.Content
	}

	return ""
}
