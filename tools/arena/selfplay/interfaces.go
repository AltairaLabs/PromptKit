package selfplay

import (
	"context"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Provider provides access to content generators for self-play scenarios.
// This is the main interface that the engine and turn executors use to
// obtain content generators based on role and persona.
type Provider interface {
	GetContentGenerator(role, personaID string) (Generator, error)
}

// AudioProvider extends Provider with audio generation capabilities for duplex mode.
type AudioProvider interface {
	Provider

	// GetAudioContentGenerator returns an audio generator for the given role and persona.
	// The TTS config specifies the voice and provider for text-to-speech synthesis.
	GetAudioContentGenerator(role, personaID string, ttsConfig *config.TTSConfig) (AudioGenerator, error)
}

// Generator generates user messages for self-play scenarios.
// Each generator is configured with a specific persona and LLM provider,
// and produces user turns based on conversation history.
// Returns the full pipeline ExecutionResult which includes trace data, costs, and metadata.
type Generator interface {
	NextUserTurn(ctx context.Context, history []types.Message, scenarioID string) (*pipeline.ExecutionResult, error)
}
