package audio

import (
	"context"
	"encoding/binary"
	"math"
	"sync"
)

const (
	// stateChangeBufferSize is the buffer size for the state change channel.
	stateChangeBufferSize = 16
	// defaultSmoothingAlpha is the exponential smoothing factor (0.0-1.0).
	defaultSmoothingAlpha = 0.3
	// pcmBytesPerSample is the number of bytes per 16-bit PCM sample.
	pcmBytesPerSample = 2
	// pcmMaxAmplitude is the maximum amplitude for 16-bit signed audio.
	pcmMaxAmplitude = 32768.0
	// maxExpectedRMS is the RMS (normalized 0..1) at which speech maps to full
	// confidence. Measured 16 kHz PCM16 speech sits around 0.08–0.11 RMS (peaks
	// ~0.7), so the previous value of 0.5 mapped normal speech to only ~0.18
	// probability — below the 0.5 confidence gate — and the VAD never detected
	// speech. Calibrated to 0.1 so typical speech saturates above the gate while
	// background/silence (RMS < ~0.04) stays below it.
	maxExpectedRMS = 0.1
)

// SimpleVAD is a basic voice activity detector using RMS (Root Mean Square) analysis.
// It provides a lightweight VAD implementation without requiring external ML models.
// For more accurate detection, consider using SileroVAD.
//
// SimpleVAD embeds *vadStateMachine, which promotes State(), OnStateChange(), and
// base Reset() — together they satisfy the VADAnalyzer interface.
type SimpleVAD struct {
	*vadStateMachine

	mu          sync.Mutex
	smoothedRMS float64
	alpha       float64 // Exponential smoothing factor
}

// NewSimpleVAD creates a SimpleVAD analyzer with the given parameters.
func NewSimpleVAD(params VADParams) (*SimpleVAD, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	return &SimpleVAD{
		vadStateMachine: newVADStateMachine(params),
		alpha:           defaultSmoothingAlpha,
	}, nil
}

// Name returns the analyzer identifier.
func (v *SimpleVAD) Name() string {
	return "simple-rms"
}

// Analyze processes audio and returns voice probability based on RMS volume.
func (v *SimpleVAD) Analyze(_ context.Context, audioData []byte) (float64, error) {
	if len(audioData) == 0 {
		return 0, nil
	}

	// Calculate RMS of audio samples.
	rms := calculateRMS(audioData)

	// Apply exponential smoothing to reduce noise.
	v.mu.Lock()
	v.smoothedRMS = v.alpha*rms + (1-v.alpha)*v.smoothedRMS
	smoothed := v.smoothedRMS
	v.mu.Unlock()

	// Convert RMS to probability (0.0-1.0).
	probability := v.rmsToProbability(smoothed)

	// Advance the shared state machine by this chunk's AUDIO duration, so
	// transitions do not depend on how fast audio is delivered.
	v.update(probability, pcm16Duration(len(audioData), v.params.SampleRate))

	return probability, nil
}

// Reset clears accumulated state for a new conversation, including the smoothed RMS.
func (v *SimpleVAD) Reset() {
	v.mu.Lock()
	v.smoothedRMS = 0
	v.mu.Unlock()

	v.vadStateMachine.Reset()
}

// calculateRMS computes the Root Mean Square of 16-bit PCM audio samples.
// It is a package-level function so AdaptiveVAD can reuse it without duplication.
func calculateRMS(audio []byte) float64 {
	if len(audio) < pcmBytesPerSample {
		return 0
	}

	numSamples := len(audio) / pcmBytesPerSample
	if numSamples == 0 {
		return 0
	}

	var sumSquares float64
	for i := 0; i < numSamples; i++ {
		// #nosec G115 -- overflow is intentional for signed PCM conversion
		sample := int16(binary.LittleEndian.Uint16(audio[i*pcmBytesPerSample:]))
		normalized := float64(sample) / pcmMaxAmplitude // Normalize to -1.0 to 1.0
		sumSquares += normalized * normalized
	}

	return math.Sqrt(sumSquares / float64(numSamples))
}

// rmsToProbability converts a smoothed RMS value to a voice probability using
// the fixed SimpleVAD threshold (maxExpectedRMS).
func (v *SimpleVAD) rmsToProbability(rms float64) float64 {
	if rms <= v.params.MinVolume {
		return 0
	}

	// Scale RMS to 0-1 range using the calibrated ceiling.
	probability := (rms - v.params.MinVolume) / (maxExpectedRMS - v.params.MinVolume)

	if probability < 0 {
		return 0
	}
	if probability > 1 {
		return 1
	}
	return probability
}
