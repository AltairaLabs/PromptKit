package audio

import (
	"context"
	"sync"
)

const (
	// adaptiveSmoothingAlpha is the weight given to the current RMS frame when
	// computing the exponential moving average (lower = more smoothing).
	adaptiveSmoothingAlpha = 0.3
	// adaptiveSpeechRatio is the multiplier applied to the noise floor to derive
	// the speech threshold. Speech must be this many times louder than background.
	adaptiveSpeechRatio = 3.0
	// adaptiveMinSpeech is the absolute minimum speech threshold — prevents the
	// threshold from collapsing to zero in true silence.
	adaptiveMinSpeech = 0.01
	// adaptiveFloorDecay is the EMA weight of the old noise floor value (IIR
	// low-pass). The complement (1-adaptiveFloorDecay) weights the current
	// smoothed RMS during floor adaptation.
	adaptiveFloorDecay = 0.95
	// adaptiveFloorUpdate is the weight given to the current smoothed RMS sample
	// during noise-floor adaptation (= 1 - adaptiveFloorDecay).
	adaptiveFloorUpdate = 0.05
	// adaptiveFloorInit is the starting noise-floor estimate (a reasonable value
	// for quiet microphone backgrounds at 16 kHz PCM16).
	adaptiveFloorInit = 0.005
	// adaptiveFloorMin clamps the noise floor so it can never decay below the
	// hardware noise level.
	adaptiveFloorMin = 0.0005
	// adaptiveFloorMax prevents the noise floor from climbing so high that
	// speech is never detectable.
	adaptiveFloorMax = 0.05
	// adaptiveProbScale is the denominator scaling factor for the adaptive
	// probability calculation: p = (smoothed - floor) / (scale × (threshold - floor)).
	// A value of 2 means the probability reaches 1.0 at twice the speech threshold.
	adaptiveProbScale = 2.0
)

// AdaptiveVAD is a voice activity detector that adapts its speech threshold to
// the ambient noise level. It is well-suited for "quiet mic" environments where
// the speaker's voice is only slightly louder than the background — a condition
// where SimpleVAD's fixed threshold often fails to trigger.
//
// Algorithm:
//
//  1. Compute the RMS of each PCM16 chunk.
//  2. Smooth with an exponential moving average (α = 0.3).
//  3. Derive speechThreshold = max(noiseFloor × 3.0, 0.01).
//  4. Adapt the noise floor upward only when smoothedRMS < speechThreshold
//     (i.e. we are probably in silence, not speech).
//  5. Map smoothedRMS → probability in [0, 1] using a linear scale between
//     noiseFloor and 2 × (speechThreshold − noiseFloor).
//
// AdaptiveVAD embeds *vadStateMachine, which promotes State(), OnStateChange(),
// and base Reset() to satisfy the VADAnalyzer interface.
type AdaptiveVAD struct {
	*vadStateMachine

	mu          sync.Mutex
	smoothedRMS float64
	noiseFloor  float64
}

// NewAdaptiveVAD creates an AdaptiveVAD analyzer with the given parameters.
func NewAdaptiveVAD(params VADParams) (*AdaptiveVAD, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	return &AdaptiveVAD{
		vadStateMachine: newVADStateMachine(params),
		noiseFloor:      adaptiveFloorInit,
	}, nil
}

// Name returns the analyzer identifier.
func (v *AdaptiveVAD) Name() string {
	return "adaptive-rms"
}

// Analyze processes audio and returns voice probability based on adaptive RMS analysis.
func (v *AdaptiveVAD) Analyze(_ context.Context, audioData []byte) (float64, error) {
	if len(audioData) == 0 {
		return 0, nil
	}

	rms := calculateRMS(audioData)

	v.mu.Lock()

	// Exponential moving average of RMS (0.3 × current + 0.7 × history).
	v.smoothedRMS = adaptiveSmoothingAlpha*rms + (1-adaptiveSmoothingAlpha)*v.smoothedRMS
	smoothed := v.smoothedRMS
	floor := v.noiseFloor

	// Derive the current speech threshold.
	speechThreshold := floor * adaptiveSpeechRatio
	if speechThreshold < adaptiveMinSpeech {
		speechThreshold = adaptiveMinSpeech
	}

	// Adapt the noise floor only when we are probably in silence.
	if smoothed < speechThreshold {
		newFloor := adaptiveFloorDecay*floor + adaptiveFloorUpdate*smoothed
		if newFloor < adaptiveFloorMin {
			newFloor = adaptiveFloorMin
		} else if newFloor > adaptiveFloorMax {
			newFloor = adaptiveFloorMax
		}
		v.noiseFloor = newFloor
		floor = newFloor
		// Recompute threshold with the updated floor.
		speechThreshold = floor * adaptiveSpeechRatio
		if speechThreshold < adaptiveMinSpeech {
			speechThreshold = adaptiveMinSpeech
		}
	}

	v.mu.Unlock()

	// Map smoothed RMS to probability.
	probability := adaptiveProbability(smoothed, floor, speechThreshold)

	// Advance the shared state machine by this chunk's AUDIO duration, so
	// transitions do not depend on how fast audio is delivered.
	v.update(probability, pcm16Duration(len(audioData), v.params.SampleRate))

	return probability, nil
}

// Reset clears accumulated state for a new conversation, resetting the smoothed
// RMS and noise floor back to their initial values.
func (v *AdaptiveVAD) Reset() {
	v.mu.Lock()
	v.smoothedRMS = 0
	v.noiseFloor = adaptiveFloorInit
	v.mu.Unlock()

	v.vadStateMachine.Reset()
}

// adaptiveProbability maps a smoothed RMS value to a voice probability in [0, 1]
// given the current noise floor and speech threshold.
func adaptiveProbability(smoothed, floor, speechThreshold float64) float64 {
	if smoothed <= floor {
		return 0
	}

	denom := adaptiveProbScale * (speechThreshold - floor)
	if denom <= 0 {
		// Guard against degenerate case (floor ≈ threshold).
		if smoothed > floor {
			return 1
		}
		return 0
	}

	p := (smoothed - floor) / denom
	if p < 0 {
		return 0
	}
	if p > 1 {
		return 1
	}
	return p
}
