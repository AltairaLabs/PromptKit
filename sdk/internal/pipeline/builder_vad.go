package pipeline

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// withNamePrefix delegates to stage.WithNamePrefix.
//
// The wrapper used to be defined here, which put it behind Go's internal-package
// rule: examples and downstream consumers building multi-track graphs could not
// import it and re-derived the same ~15 lines. It now lives in
// runtime/pipeline/stage alongside the Stage interface it wraps.
func withNamePrefix(prefix string, s stage.Stage) stage.Stage {
	return stage.WithNamePrefix(prefix, s)
}

// BuildAudioTrackStages returns the ordered stages for one named audio track:
// [Resample→]AudioTurn(VAD)→STT. namePrefix uniquifies stage names (e.g. "caller")
// so multiple tracks can be wired into the same pipeline graph without name
// collisions; pass "" to keep the constructors' natural names (single-track case).
// Reused by both the VAD front (buildVADPipelineStages, includeResample=false to
// keep that topology byte-for-byte unchanged) and custom ingestion sub-graphs that
// need to feed one or more raw audio tracks — possibly at an arbitrary sample
// rate — into the standard agent chain (includeResample=true).
//
// When includeResample is true, the AudioResampleStage normalizes the track to
// vad.SampleRate (falling back to the package default when unset) before
// VAD/turn-detection runs, and is a no-op passthrough when the source is already
// at that rate — see runtime/pipeline/stage/stages_resample.go.
func BuildAudioTrackStages(
	namePrefix string,
	includeResample bool,
	vad stage.AudioTurnConfig,
	stt stage.STTStageConfig,
	sttSvc base.STTProvider,
) ([]stage.Stage, error) {
	audioTurnStage, err := stage.NewAudioTurnStage(vad)
	if err != nil {
		return nil, fmt.Errorf("failed to create AudioTurnStage: %w", err)
	}

	sttStage := stage.NewSTTStage(sttSvc, stt)

	// Resample + AudioTurn + STT is the maximum size (Resample is opt-in).
	const maxAudioTrackStages = 3
	stages := make([]stage.Stage, 0, maxAudioTrackStages)
	if includeResample {
		resampleConfig := stage.DefaultAudioResampleConfig()
		if vad.SampleRate > 0 {
			resampleConfig.TargetSampleRate = vad.SampleRate
		}
		stages = append(stages, withNamePrefix(namePrefix, stage.NewAudioResampleStage(resampleConfig)))
	}
	stages = append(stages,
		withNamePrefix(namePrefix, audioTurnStage),
		withNamePrefix(namePrefix, sttStage),
	)

	return stages, nil
}

// buildVADPipelineStages creates the VAD mode pipeline stages.
// VAD mode: Audio → VAD → STT → LLM → TTS
// This is integration-tested through the voice-interview example.
func buildVADPipelineStages(cfg *Config) ([]stage.Stage, error) {
	var stages []stage.Stage

	logger.Debug("Using VAD pipeline stages")

	// 5a-b. AudioTurnStage → STTStage - the single-track audio front. No resample:
	// this preserves the pre-existing VAD topology exactly (see BuildAudioTrackStages).
	sttConfig := stage.DefaultSTTStageConfig()
	if cfg.STTConfig != nil {
		sttConfig = *cfg.STTConfig
	}
	trackStages, err := BuildAudioTrackStages("", false, *cfg.VADConfig, sttConfig, cfg.STTService)
	if err != nil {
		return nil, fmt.Errorf("failed to create AudioTurnStage: %w", err)
	}
	stages = append(stages, trackStages...)

	// 5d. ProviderStage - LLM call
	if cfg.Provider != nil {
		providerConfig := &stage.ProviderConfig{
			MaxTokens:       cfg.MaxTokens,
			Temperature:     cfg.Temperature,
			ApprovalChecker: cfg.ApprovalChecker,
		}
		stages = append(stages, stage.NewProviderStageWithEmitter(
			cfg.Provider,
			cfg.ToolRegistry,
			cfg.ToolPolicy,
			providerConfig,
			cfg.EventEmitter,
		))
	}

	// 5e. TTSStage - synthesize text to audio
	ttsConfig := stage.DefaultTTSStageWithInterruptionConfig()
	if cfg.TTSConfig != nil {
		ttsConfig = *cfg.TTSConfig
	}
	stages = append(stages, stage.NewTTSStageWithInterruption(cfg.TTSService, ttsConfig))

	return stages, nil
}
