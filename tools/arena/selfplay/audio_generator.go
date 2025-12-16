// Package selfplay provides self-play capabilities for arena testing scenarios.
// It enables LLM-driven user simulation and audio generation for duplex conversations.
package selfplay

import (
	"context"
	"fmt"
	"io"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// AudioGenerator generates user messages with audio output for duplex self-play.
// It wraps a text ContentGenerator and adds TTS synthesis.
type AudioGenerator interface {
	// NextUserTurnAudio generates a user message and converts it to audio.
	// Returns the text result and audio data.
	NextUserTurnAudio(ctx context.Context, history []types.Message, scenarioID string) (*AudioResult, error)
}

// AudioResult contains both text and audio output from audio generation.
type AudioResult struct {
	// TextResult contains the text generation result with cost info and metadata.
	TextResult *pipeline.ExecutionResult

	// Audio contains the synthesized audio data.
	Audio []byte

	// AudioFormat describes the audio encoding.
	AudioFormat tts.AudioFormat
}

// AudioContentGenerator wraps a ContentGenerator and adds TTS synthesis.
type AudioContentGenerator struct {
	textGenerator *ContentGenerator
	ttsService    tts.Service
	ttsConfig     *config.TTSConfig
}

// NewAudioContentGenerator creates a new audio content generator.
func NewAudioContentGenerator(
	textGenerator *ContentGenerator,
	ttsService tts.Service,
	ttsConfig *config.TTSConfig,
) *AudioContentGenerator {
	return &AudioContentGenerator{
		textGenerator: textGenerator,
		ttsService:    ttsService,
		ttsConfig:     ttsConfig,
	}
}

// NextUserTurnAudio generates a user message and converts it to audio.
func (g *AudioContentGenerator) NextUserTurnAudio(
	ctx context.Context,
	history []types.Message,
	scenarioID string,
) (*AudioResult, error) {
	// Generate text using the underlying generator
	textResult, err := g.textGenerator.NextUserTurn(ctx, history, scenarioID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate text: %w", err)
	}

	// Extract text content from result
	text := ""
	if textResult.Response != nil {
		text = textResult.Response.Content
	}

	if text == "" {
		return nil, fmt.Errorf("no text content generated for TTS synthesis")
	}

	// Synthesize audio
	audioData, format, err := g.synthesize(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("failed to synthesize audio: %w", err)
	}

	return &AudioResult{
		TextResult:  textResult,
		Audio:       audioData,
		AudioFormat: format,
	}, nil
}

// synthesize converts text to audio using the TTS service.
func (g *AudioContentGenerator) synthesize(ctx context.Context, text string) ([]byte, tts.AudioFormat, error) {
	// Build TTS configuration
	synthConfig := tts.SynthesisConfig{
		Voice:  g.ttsConfig.Voice,
		Format: tts.FormatPCM16, // PCM16 for streaming to duplex provider
		Speed:  1.0,
	}

	// Synthesize audio
	reader, err := g.ttsService.Synthesize(ctx, text, synthConfig)
	if err != nil {
		return nil, tts.AudioFormat{}, err
	}
	defer reader.Close()

	// Read all audio data
	audioData, err := io.ReadAll(reader)
	if err != nil {
		return nil, tts.AudioFormat{}, fmt.Errorf("failed to read audio data: %w", err)
	}

	return audioData, synthConfig.Format, nil
}

// GetTTSService returns the TTS service for direct access if needed.
func (g *AudioContentGenerator) GetTTSService() tts.Service {
	return g.ttsService
}

// GetTextGenerator returns the underlying text generator.
func (g *AudioContentGenerator) GetTextGenerator() *ContentGenerator {
	return g.textGenerator
}
