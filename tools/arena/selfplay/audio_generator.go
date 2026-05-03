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
//
// All audio output is streamed through io.ReadCloser — there is no
// buffered shape. Memory stays bounded by the chunk size the caller
// reads with, regardless of utterance length, which is required for any
// production-like workload where TTS output can run minutes long.
//
// Implementations should ensure the reader returned by stream methods
// arrives at TTS-source rate (faster than playback for real providers,
// instant for mocks), letting downstream consumers buffer or pace as they
// see fit.
type AudioGenerator interface {
	// NextUserTurnAudioStream generates a user message and returns the
	// synthesized audio as a streaming reader. Caller is responsible for
	// closing the reader. The text result is available before the audio
	// has finished streaming.
	NextUserTurnAudioStream(
		ctx context.Context,
		history []types.Message,
		scenarioID string,
		opts *GeneratorOptions,
	) (*AudioStreamResult, error)

	// SynthesizeTextStream synthesizes pre-known text directly to audio
	// (no LLM-driven text generation). Used by scripted-text duplex turns
	// where the text is from the scenario YAML, not a persona.
	// Caller is responsible for closing Reader.
	SynthesizeTextStream(
		ctx context.Context,
		text string,
	) (*AudioStreamResult, error)
}

// AudioStreamResult is the result of an audio generation. The audio
// hasn't been synthesized yet when this is returned — the caller drains
// Reader to consume it as it arrives from the TTS provider.
type AudioStreamResult struct {
	// TextResult is the text generation result. Nil for SynthesizeTextStream
	// where the text was supplied by the caller and no LLM ran.
	TextResult *pipeline.ExecutionResult

	// Text is the synthesized utterance. For NextUserTurnAudioStream it
	// matches TextResult.Response.Content; for SynthesizeTextStream it
	// is the input text.
	Text string

	// Reader is the streaming audio body. Caller must Close it.
	Reader io.ReadCloser

	// AudioFormat describes the audio encoding.
	AudioFormat tts.AudioFormat

	// SampleRate is the sample rate of the audio data in Hz.
	SampleRate int
}

// AudioContentGenerator wraps a ContentGenerator and adds TTS synthesis.
//
// textGenerator may be nil when constructed via GetTextSynthesisGenerator
// (scripted-text turns where no LLM is involved). In that case
// NextUserTurnAudioStream returns an error; SynthesizeTextStream still
// works.
type AudioContentGenerator struct {
	textGenerator *ContentGenerator
	ttsService    tts.Service
	ttsConfig     *config.TTSConfig
}

// NewAudioContentGenerator creates a new audio content generator.
// textGenerator may be nil for TTS-only generators.
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

// Default TTS output sample rate (most TTS services output at 24kHz).
const defaultTTSSampleRate = 24000

// openStream opens a TTS stream for text and returns the streaming
// reader along with format/rate metadata. The single place that
// translates (text, ttsConfig) → (reader, sample rate).
func (g *AudioContentGenerator) openStream(ctx context.Context, text string) (*AudioStreamResult, error) {
	synthConfig := tts.SynthesisConfig{
		Voice:  g.ttsConfig.Voice,
		Format: tts.FormatPCM16,
		Speed:  1.0,
	}
	reader, err := g.ttsService.Synthesize(ctx, text, synthConfig)
	if err != nil {
		return nil, err
	}
	sampleRate := defaultTTSSampleRate
	if g.ttsConfig.SampleRate > 0 {
		sampleRate = g.ttsConfig.SampleRate
	}
	return &AudioStreamResult{
		Text:        text,
		Reader:      reader,
		AudioFormat: synthConfig.Format,
		SampleRate:  sampleRate,
	}, nil
}

// SynthesizeTextStream implements AudioGenerator. Skips the LLM text
// generation step — used for scripted-text duplex turns where the text
// arrives pre-known from the scenario YAML.
func (g *AudioContentGenerator) SynthesizeTextStream(
	ctx context.Context,
	text string,
) (*AudioStreamResult, error) {
	if text == "" {
		return nil, fmt.Errorf("SynthesizeTextStream: text is empty")
	}
	return g.openStream(ctx, text)
}

// NextUserTurnAudioStream implements AudioGenerator. Generates user-turn
// text via the underlying ContentGenerator and returns a streaming
// reader for the synthesized audio. Memory consumption is bounded
// regardless of utterance length.
func (g *AudioContentGenerator) NextUserTurnAudioStream(
	ctx context.Context,
	history []types.Message,
	scenarioID string,
	opts *GeneratorOptions,
) (*AudioStreamResult, error) {
	if g.textGenerator == nil {
		return nil, fmt.Errorf(
			"NextUserTurnAudioStream: no text generator " +
				"(use SynthesizeTextStream for scripted-text turns)")
	}
	textResult, err := g.textGenerator.NextUserTurn(ctx, history, scenarioID, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to generate text: %w", err)
	}
	text := ""
	if textResult.Response != nil {
		text = textResult.Response.Content
	}
	if text == "" {
		return nil, fmt.Errorf("no text content generated for TTS synthesis")
	}
	stream, err := g.openStream(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("failed to open TTS stream: %w", err)
	}
	stream.TextResult = textResult
	return stream, nil
}

// GetTTSService returns the TTS service for direct access if needed.
func (g *AudioContentGenerator) GetTTSService() tts.Service {
	return g.ttsService
}

// GetTextGenerator returns the underlying text generator.
func (g *AudioContentGenerator) GetTextGenerator() *ContentGenerator {
	return g.textGenerator
}
