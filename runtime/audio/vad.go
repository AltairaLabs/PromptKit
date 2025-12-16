package audio

import (
	"context"
	"time"
)

// Default VAD parameter values.
const (
	DefaultVADConfidence = 0.5
	DefaultVADStartSecs  = 0.2
	DefaultVADStopSecs   = 0.8
	DefaultVADMinVolume  = 0.01
	DefaultVADSampleRate = 16000
)

const unknownState = "unknown"

// VADState represents the current voice activity state.
type VADState int

const (
	// VADStateQuiet indicates no voice activity detected.
	VADStateQuiet VADState = iota
	// VADStateStarting indicates voice is starting (within start threshold).
	VADStateStarting
	// VADStateSpeaking indicates active speech.
	VADStateSpeaking
	// VADStateStopping indicates voice is stopping (within stop threshold).
	VADStateStopping
)

// String returns a human-readable representation of the VAD state.
func (s VADState) String() string {
	switch s {
	case VADStateQuiet:
		return "quiet"
	case VADStateStarting:
		return "starting"
	case VADStateSpeaking:
		return "speaking"
	case VADStateStopping:
		return "stopping"
	default:
		return unknownState
	}
}

// VADParams configures voice activity detection behavior.
type VADParams struct {
	// Confidence threshold for voice detection (0.0-1.0, default: 0.5).
	// Higher values require more confidence before triggering.
	Confidence float64

	// StartSecs is seconds of speech required to trigger VADStateSpeaking (default: 0.2).
	// Prevents false starts from brief noise.
	StartSecs float64

	// StopSecs is seconds of silence required to trigger VADStateQuiet (default: 0.8).
	// Allows natural pauses without ending turn.
	StopSecs float64

	// MinVolume is the minimum RMS volume threshold (default: 0.01).
	// Audio below this is treated as silence.
	MinVolume float64

	// SampleRate is the audio sample rate in Hz (default: 16000).
	SampleRate int
}

// DefaultVADParams returns sensible defaults for voice activity detection.
func DefaultVADParams() VADParams {
	return VADParams{
		Confidence: DefaultVADConfidence,
		StartSecs:  DefaultVADStartSecs,
		StopSecs:   DefaultVADStopSecs,
		MinVolume:  DefaultVADMinVolume,
		SampleRate: DefaultVADSampleRate,
	}
}

// Validate checks that VAD parameters are within acceptable ranges.
func (p VADParams) Validate() error {
	if p.Confidence < 0 || p.Confidence > 1 {
		return &ValidationError{Field: "Confidence", Message: "must be between 0.0 and 1.0"}
	}
	if p.StartSecs < 0 {
		return &ValidationError{Field: "StartSecs", Message: "must be non-negative"}
	}
	if p.StopSecs < 0 {
		return &ValidationError{Field: "StopSecs", Message: "must be non-negative"}
	}
	if p.MinVolume < 0 || p.MinVolume > 1 {
		return &ValidationError{Field: "MinVolume", Message: "must be between 0.0 and 1.0"}
	}
	if p.SampleRate <= 0 {
		return &ValidationError{Field: "SampleRate", Message: "must be positive"}
	}
	return nil
}

// ValidationError represents a parameter validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return "invalid " + e.Field + ": " + e.Message
}

// VADEvent represents a state transition in VAD.
type VADEvent struct {
	State      VADState
	PrevState  VADState
	Timestamp  time.Time
	Duration   time.Duration // How long in the previous state
	Confidence float64       // Voice confidence at transition
}

// VADAnalyzer analyzes audio for voice activity.
type VADAnalyzer interface {
	// Name returns the analyzer identifier.
	Name() string

	// Analyze processes audio and returns voice probability (0.0-1.0).
	// audio should be raw PCM samples at the configured sample rate.
	Analyze(ctx context.Context, audio []byte) (float64, error)

	// State returns the current VAD state based on accumulated analysis.
	State() VADState

	// OnStateChange returns a channel that receives state transitions.
	// The channel is buffered and may drop events if not consumed.
	OnStateChange() <-chan VADEvent

	// Reset clears accumulated state for a new conversation.
	Reset()
}
