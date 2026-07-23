package stage

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
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
	start := time.Now()
	audioData, err := s.tts.Synthesize(ctx, text)
	if err != nil {
		return err
	}
	latency := time.Since(start)

	// Add audio to element
	elem.Audio = &AudioData{
		Samples:    audioData,
		SampleRate: defaultTTSSampleRate,
		Format:     AudioFormatPCM16,
	}

	// Stamp TTS cost on the message when the element carries one.
	// The arena's cost rollup reads Message.Meta["tts_cost"] via the
	// ancillaryCostMetaKeys mechanism (same pattern as self_play_cost).
	if elem.Message != nil {
		if costMeta := tts.CostInfoToMetaMap(tts.ComputeTTSCost(s.tts, text, latency)); costMeta != nil {
			if elem.Message.Meta == nil {
				elem.Message.Meta = make(map[string]interface{})
			}
			elem.Message.Meta[ttsCostMetaKey] = costMeta
		}
	}

	logger.Debug("TTS: synthesized audio", "text_length", len(text), "audio_bytes", len(audioData))
	return nil
}

// ttsCostMetaKey is the Message.Meta key for TTS ancillary cost.
// Mirrors the constant in PromptArena's engine/cost_aggregation.go.
const ttsCostMetaKey = "tts_cost"

// extractText extracts text content from an element.
func (s *TTSStage) extractText(elem *StreamElement) string {
	// Skip incremental streaming deltas — see TTSStageWithInterruption.extractText.
	if elem.Meta.StreamingDelta {
		return ""
	}

	if elem.Text != nil && *elem.Text != "" {
		return *elem.Text
	}

	// Only the assistant is spoken — a user transcript or tool result reaching a
	// speech-out stage is data for later stages, not a line to read aloud. See
	// TTSStageWithInterruption.extractText for the topology that proved this.
	if elem.Message != nil && elem.Message.Role == roleAssistant {
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
