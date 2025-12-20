// Package stage provides pipeline stages for audio processing.
package stage

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

const (
	// defaultResampleTargetRate is the default target sample rate for audio resampling.
	// 16000 Hz is Gemini's expected input rate.
	defaultResampleTargetRate = 16000
)

// AudioResampleConfig contains configuration for the audio resampling stage.
type AudioResampleConfig struct {
	// TargetSampleRate is the desired output sample rate in Hz.
	// Common values: 16000 (Gemini), 24000 (OpenAI TTS), 44100 (CD quality).
	TargetSampleRate int

	// PassthroughIfSameRate skips resampling if input rate matches target rate.
	// Default: true.
	PassthroughIfSameRate bool
}

// DefaultAudioResampleConfig returns sensible defaults for audio resampling.
func DefaultAudioResampleConfig() AudioResampleConfig {
	return AudioResampleConfig{
		TargetSampleRate:      defaultResampleTargetRate,
		PassthroughIfSameRate: true,
	}
}

// AudioResampleStage resamples audio data to a target sample rate.
// This is useful for normalizing audio from different sources (TTS, files)
// to match provider requirements.
//
// This is a Transform stage: audio element → resampled audio element (1:1)
type AudioResampleStage struct {
	BaseStage
	config AudioResampleConfig

	// Log deduplication: track what we've logged to avoid flooding
	loggedPassthrough bool
	loggedResampleKey string // "from_rate->to_rate"
}

// NewAudioResampleStage creates a new audio resampling stage.
func NewAudioResampleStage(config AudioResampleConfig) *AudioResampleStage {
	return &AudioResampleStage{
		BaseStage: NewBaseStage("audio-resample", StageTypeTransform),
		config:    config,
	}
}

// Process implements the Stage interface.
// Resamples audio in each element to the target sample rate.
func (s *AudioResampleStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		// Process audio if present
		if elem.Audio != nil && len(elem.Audio.Samples) > 0 {
			if err := s.resampleElement(&elem); err != nil {
				logger.Error("Audio resampling failed", "error", err)
				elem.Error = err
			}
		}

		// Forward element (with or without resampling)
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// resampleElement resamples the audio in an element if needed.
func (s *AudioResampleStage) resampleElement(elem *StreamElement) error {
	audioData := elem.Audio
	if audioData == nil {
		return nil
	}

	// Check if resampling is needed
	currentRate := audioData.SampleRate
	targetRate := s.config.TargetSampleRate

	if currentRate == targetRate && s.config.PassthroughIfSameRate {
		if !s.loggedPassthrough {
			logger.Debug("AudioResampleStage: sample rate matches, passthrough",
				"rate", currentRate,
			)
			s.loggedPassthrough = true
		}
		return nil
	}

	if currentRate == 0 {
		// Unknown source rate, can't resample
		logger.Warn("AudioResampleStage: unknown source sample rate, skipping resample")
		return nil
	}

	// Only support PCM16 format for now
	if audioData.Format != AudioFormatPCM16 {
		return fmt.Errorf("resampling only supports PCM16 format, got %s", audioData.Format.String())
	}

	// Log resampling operation only once per unique rate combination
	resampleKey := fmt.Sprintf("%d->%d", currentRate, targetRate)
	if s.loggedResampleKey != resampleKey {
		logger.Debug("AudioResampleStage: resampling audio",
			"from_rate", currentRate,
			"to_rate", targetRate,
		)
		s.loggedResampleKey = resampleKey
	}

	// Perform resampling
	resampled, err := audio.ResamplePCM16(audioData.Samples, currentRate, targetRate)
	if err != nil {
		return fmt.Errorf("resample failed: %w", err)
	}

	// Update the element with resampled audio
	elem.Audio.Samples = resampled
	elem.Audio.SampleRate = targetRate

	return nil
}

// GetConfig returns the stage configuration.
func (s *AudioResampleStage) GetConfig() AudioResampleConfig {
	return s.config
}
