package pipeline

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// buildVADPipelineStages creates the VAD mode pipeline stages.
// VAD mode: Audio → VAD → STT → LLM → TTS
// This is integration-tested through the voice-interview example.
func buildVADPipelineStages(cfg *Config) ([]stage.Stage, error) {
	var stages []stage.Stage

	logger.Debug("Using VAD pipeline stages")

	// 5a. AudioTurnStage - VAD + turn detection + audio accumulation
	audioTurnStage, err := stage.NewAudioTurnStage(*cfg.VADConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create AudioTurnStage: %w", err)
	}
	stages = append(stages, audioTurnStage)

	// 5b. STTStage - transcribe audio to text
	sttConfig := stage.DefaultSTTStageConfig()
	if cfg.STTConfig != nil {
		sttConfig = *cfg.STTConfig
	}
	stages = append(stages, stage.NewSTTStage(cfg.STTService, sttConfig))

	// 5c. ProviderStage - LLM call
	if cfg.Provider != nil {
		providerConfig := &stage.ProviderConfig{
			MaxTokens:   cfg.MaxTokens,
			Temperature: cfg.Temperature,
		}
		stages = append(stages, stage.NewProviderStageWithEmitter(
			cfg.Provider,
			cfg.ToolRegistry,
			cfg.ToolPolicy,
			providerConfig,
			cfg.EventEmitter,
		))
	}

	// 5d. TTSStage - synthesize text to audio
	ttsConfig := stage.DefaultTTSStageWithInterruptionConfig()
	if cfg.TTSConfig != nil {
		ttsConfig = *cfg.TTSConfig
	}
	stages = append(stages, stage.NewTTSStageWithInterruption(cfg.TTSService, ttsConfig))

	return stages, nil
}
