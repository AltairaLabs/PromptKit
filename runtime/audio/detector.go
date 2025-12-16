package audio

import (
	"context"
)

// TurnDetector determines when a speaker has finished their turn.
// This is separate from VAD - VAD detects voice activity,
// turn detection determines conversation boundaries.
type TurnDetector interface {
	// Name returns the detector identifier.
	Name() string

	// ProcessAudio processes an incoming audio chunk.
	// Returns true if end of turn is detected.
	ProcessAudio(ctx context.Context, audio []byte) (bool, error)

	// ProcessVADState processes a VAD state update.
	// Returns true if end of turn is detected based on VAD state.
	ProcessVADState(ctx context.Context, state VADState) (bool, error)

	// IsUserSpeaking returns true if user is currently speaking.
	IsUserSpeaking() bool

	// Reset clears state for a new conversation.
	Reset()
}

// TurnCallback is called when a complete user turn is detected.
// audio contains the accumulated audio for the turn.
// transcript contains any accumulated transcript (may be empty).
type TurnCallback func(audio []byte, transcript string)

// AccumulatingTurnDetector is a TurnDetector that accumulates audio during a turn.
type AccumulatingTurnDetector interface {
	TurnDetector

	// OnTurnComplete registers a callback for when a complete turn is detected.
	OnTurnComplete(callback TurnCallback)

	// GetAccumulatedAudio returns audio accumulated so far (may be incomplete turn).
	GetAccumulatedAudio() []byte

	// SetTranscript sets the transcript for the current turn (from external STT).
	SetTranscript(transcript string)
}
