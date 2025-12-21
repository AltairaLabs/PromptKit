package stage

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// TTSService converts text to audio.
type TTSService interface {
	// Synthesize converts text to audio bytes.
	Synthesize(ctx context.Context, text string) ([]byte, error)

	// MIMEType returns the MIME type of the synthesized audio.
	MIMEType() string
}

// TTSConfig contains configuration for TTS stage.
type TTSConfig struct {
	// SkipEmpty skips synthesis for empty or whitespace-only text
	SkipEmpty bool

	// MinTextLength is the minimum text length to synthesize (0 = no minimum)
	MinTextLength int
}

// defaultTTSSampleRate is the default audio sample rate for TTS output.
const defaultTTSSampleRate = 24000

// DefaultTTSConfig returns sensible defaults for TTS configuration.
func DefaultTTSConfig() TTSConfig {
	return TTSConfig{
		SkipEmpty:     true,
		MinTextLength: 1,
	}
}

// TTSStage synthesizes audio for streaming text elements.
// It reads text elements from input and adds audio data to them.
//
// This is a Transform stage: text element → text+audio element (1:1)
type TTSStage struct {
	BaseStage
	tts    TTSService
	config TTSConfig
}

// NewTTSStage creates a new TTS stage.
func NewTTSStage(tts TTSService, config TTSConfig) *TTSStage {
	return &TTSStage{
		BaseStage: NewBaseStage("tts", StageTypeTransform),
		tts:       tts,
		config:    config,
	}
}

// Process implements the Stage interface.
// Synthesizes audio for each text element and adds it to the element.
func (s *TTSStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		// Process element with TTS if it has text content
		if err := s.processElement(ctx, &elem); err != nil {
			logger.Error("TTS synthesis failed", "error", err)
			// Continue processing other elements rather than failing the entire pipeline
			elem.Error = err
		}

		// Forward element (with or without audio)
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// processElement synthesizes audio for an element if it contains text.
func (s *TTSStage) processElement(ctx context.Context, elem *StreamElement) error {
	text := s.extractText(elem)
	if text == "" {
		return nil // No text to synthesize
	}

	// Check configuration filters
	if !s.shouldSynthesize(text) {
		return nil
	}

	// Synthesize audio
	audioData, err := s.tts.Synthesize(ctx, text)
	if err != nil {
		return err
	}

	// Add audio to element
	elem.Audio = &AudioData{
		Samples:    audioData,
		SampleRate: defaultTTSSampleRate,
		Format:     AudioFormatPCM16,
	}

	logger.Debug("TTS: synthesized audio", "text_length", len(text), "audio_bytes", len(audioData))
	return nil
}

// extractText extracts text content from an element.
func (s *TTSStage) extractText(elem *StreamElement) string {
	if elem.Text != nil && *elem.Text != "" {
		return *elem.Text
	}

	if elem.Message != nil {
		// Extract text from message content
		if elem.Message.Content != "" {
			return elem.Message.Content
		}

		// Extract from parts
		for _, part := range elem.Message.Parts {
			if part.Text != nil && *part.Text != "" {
				return *part.Text
			}
		}
	}

	return ""
}

// shouldSynthesize checks if text should be synthesized based on config.
func (s *TTSStage) shouldSynthesize(text string) bool {
	if s.config.SkipEmpty && text == "" {
		return false
	}

	if len(text) < s.config.MinTextLength {
		return false
	}

	return true
}
