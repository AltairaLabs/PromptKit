package audio

import (
	"context"
	"encoding/binary"
	"math"
	"sync"
	"time"
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
	// maxExpectedRMS is the expected maximum RMS for voice audio.
	maxExpectedRMS = 0.5
)

// SimpleVAD is a basic voice activity detector using RMS (Root Mean Square) analysis.
// It provides a lightweight VAD implementation without requiring external ML models.
// For more accurate detection, consider using SileroVAD.
type SimpleVAD struct {
	params VADParams

	mu           sync.RWMutex
	state        VADState
	prevState    VADState
	stateChange  chan VADEvent
	stateStart   time.Time
	lastAnalysis time.Time

	// Smoothing state
	smoothedRMS float64
	alpha       float64 // Exponential smoothing factor
}

// NewSimpleVAD creates a SimpleVAD analyzer with the given parameters.
func NewSimpleVAD(params VADParams) (*SimpleVAD, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	return &SimpleVAD{
		params:      params,
		state:       VADStateQuiet,
		stateChange: make(chan VADEvent, stateChangeBufferSize),
		stateStart:  time.Now(),
		alpha:       defaultSmoothingAlpha,
	}, nil
}

// Name returns the analyzer identifier.
func (v *SimpleVAD) Name() string {
	return "simple-rms"
}

// Analyze processes audio and returns voice probability based on RMS volume.
func (v *SimpleVAD) Analyze(ctx context.Context, audio []byte) (float64, error) {
	if len(audio) == 0 {
		return 0, nil
	}

	// Calculate RMS of audio samples
	rms := v.calculateRMS(audio)

	// Apply exponential smoothing to reduce noise
	v.mu.Lock()
	v.smoothedRMS = v.alpha*rms + (1-v.alpha)*v.smoothedRMS
	smoothed := v.smoothedRMS
	v.mu.Unlock()

	// Convert RMS to probability (0.0-1.0)
	// Using a simple threshold-based approach
	probability := v.rmsToProbability(smoothed)

	// Update state machine
	v.updateState(probability)

	return probability, nil
}

// calculateRMS computes the Root Mean Square of 16-bit PCM audio samples.
func (v *SimpleVAD) calculateRMS(audio []byte) float64 {
	if len(audio) < pcmBytesPerSample {
		return 0
	}

	// Process 16-bit little-endian PCM samples
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

// rmsToProbability converts RMS to a voice probability.
func (v *SimpleVAD) rmsToProbability(rms float64) float64 {
	if rms <= v.params.MinVolume {
		return 0
	}

	// Scale RMS to 0-1 range, with some headroom
	// Typical voice RMS is 0.05-0.3 for normalized audio
	probability := (rms - v.params.MinVolume) / (maxExpectedRMS - v.params.MinVolume)

	// Clamp to 0-1
	if probability < 0 {
		return 0
	}
	if probability > 1 {
		return 1
	}
	return probability
}

// computeNextState determines the next state based on current state and probability.
// This is a pure function to reduce cognitive complexity of the state machine.
func (v *SimpleVAD) computeNextState(
	current VADState, probability float64, stateDurationSecs float64,
) VADState {
	aboveThreshold := probability >= v.params.Confidence

	switch current {
	case VADStateQuiet:
		if aboveThreshold {
			return VADStateStarting
		}
	case VADStateStarting:
		if !aboveThreshold {
			return VADStateQuiet
		}
		if stateDurationSecs >= v.params.StartSecs {
			return VADStateSpeaking
		}
	case VADStateSpeaking:
		if !aboveThreshold {
			return VADStateStopping
		}
	case VADStateStopping:
		if aboveThreshold {
			return VADStateSpeaking
		}
		if stateDurationSecs >= v.params.StopSecs {
			return VADStateQuiet
		}
	}
	return current
}

// updateState implements the VAD state machine.
func (v *SimpleVAD) updateState(probability float64) {
	v.mu.Lock()
	defer v.mu.Unlock()

	now := time.Now()
	v.lastAnalysis = now
	stateDuration := now.Sub(v.stateStart)

	newState := v.computeNextState(v.state, probability, stateDuration.Seconds())

	// Emit event on state change
	if newState != v.state {
		event := VADEvent{
			State:      newState,
			PrevState:  v.state,
			Timestamp:  now,
			Duration:   stateDuration,
			Confidence: probability,
		}

		v.prevState = v.state
		v.state = newState
		v.stateStart = now

		// Non-blocking send to event channel
		select {
		case v.stateChange <- event:
		default:
			// Channel full, drop event
		}
	}
}

// State returns the current VAD state.
func (v *SimpleVAD) State() VADState {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.state
}

// OnStateChange returns a channel that receives state transitions.
func (v *SimpleVAD) OnStateChange() <-chan VADEvent {
	return v.stateChange
}

// Reset clears accumulated state for a new conversation.
func (v *SimpleVAD) Reset() {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.state = VADStateQuiet
	v.prevState = VADStateQuiet
	v.stateStart = time.Now()
	v.smoothedRMS = 0

	// Drain the event channel
	for len(v.stateChange) > 0 {
		<-v.stateChange
	}
}
