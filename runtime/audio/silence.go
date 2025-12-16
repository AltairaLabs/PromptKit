package audio

import (
	"context"
	"sync"
	"time"
)

// SilenceDetector detects turn boundaries based on silence duration.
// It triggers end-of-turn when silence exceeds a configurable threshold.
type SilenceDetector struct {
	// Threshold is the silence duration required to trigger turn end.
	Threshold time.Duration

	mu           sync.RWMutex
	silenceStart time.Time
	inSilence    bool
	userSpeaking bool
	audioBuffer  []byte
	transcript   string
	turnCallback TurnCallback
	lastVADState VADState
	hadSpeech    bool // Track if we've had any speech this turn
}

// NewSilenceDetector creates a SilenceDetector with the given threshold.
// threshold is the duration of silence required to trigger end-of-turn.
func NewSilenceDetector(threshold time.Duration) *SilenceDetector {
	return &SilenceDetector{
		Threshold:    threshold,
		silenceStart: time.Now(),
		inSilence:    true,
		lastVADState: VADStateQuiet,
	}
}

// Name returns the detector identifier.
func (d *SilenceDetector) Name() string {
	return "silence"
}

// ProcessAudio processes an incoming audio chunk.
// This implementation delegates to ProcessVADState and expects VAD to be run separately.
// Returns true if end of turn is detected.
func (d *SilenceDetector) ProcessAudio(ctx context.Context, audio []byte) (bool, error) {
	d.mu.Lock()
	// Accumulate audio if we're tracking a turn
	if d.userSpeaking || d.hadSpeech {
		d.audioBuffer = append(d.audioBuffer, audio...)
	}
	d.mu.Unlock()

	// Actual turn detection happens in ProcessVADState
	return false, nil
}

// ProcessVADState processes a VAD state update and detects turn boundaries.
// Returns true if end of turn is detected.
func (d *SilenceDetector) ProcessVADState(ctx context.Context, state VADState) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	prevState := d.lastVADState
	d.lastVADState = state

	switch state {
	case VADStateSpeaking:
		d.userSpeaking = true
		d.hadSpeech = true
		d.inSilence = false

	case VADStateStopping:
		// Start silence timer if transitioning from speaking
		if prevState == VADStateSpeaking {
			d.silenceStart = now
			d.inSilence = true
		}

	case VADStateQuiet:
		// Check if we should trigger turn end
		if d.hadSpeech && d.inSilence {
			silenceDuration := now.Sub(d.silenceStart)
			if silenceDuration >= d.Threshold {
				d.triggerTurnComplete()
				return true, nil
			}
		} else if !d.inSilence {
			// Just became quiet
			d.silenceStart = now
			d.inSilence = true
		}
		d.userSpeaking = false

	case VADStateStarting:
		// User might be starting to speak again
		d.inSilence = false
	}

	return false, nil
}

// triggerTurnComplete fires the callback and resets state.
// Must be called with mu held.
func (d *SilenceDetector) triggerTurnComplete() {
	if d.turnCallback != nil && len(d.audioBuffer) > 0 {
		// Copy buffer before callback
		audio := make([]byte, len(d.audioBuffer))
		copy(audio, d.audioBuffer)
		transcript := d.transcript

		// Fire callback (without lock to prevent deadlock)
		go d.turnCallback(audio, transcript)
	}

	// Reset for next turn
	d.audioBuffer = nil
	d.transcript = ""
	d.hadSpeech = false
	d.userSpeaking = false
}

// IsUserSpeaking returns true if user is currently speaking.
func (d *SilenceDetector) IsUserSpeaking() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.userSpeaking
}

// Reset clears state for a new conversation.
func (d *SilenceDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.silenceStart = time.Now()
	d.inSilence = true
	d.userSpeaking = false
	d.audioBuffer = nil
	d.transcript = ""
	d.lastVADState = VADStateQuiet
	d.hadSpeech = false
}

// OnTurnComplete registers a callback for when a complete turn is detected.
func (d *SilenceDetector) OnTurnComplete(callback TurnCallback) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.turnCallback = callback
}

// GetAccumulatedAudio returns audio accumulated so far.
func (d *SilenceDetector) GetAccumulatedAudio() []byte {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.audioBuffer) == 0 {
		return nil
	}
	result := make([]byte, len(d.audioBuffer))
	copy(result, d.audioBuffer)
	return result
}

// SetTranscript sets the transcript for the current turn.
func (d *SilenceDetector) SetTranscript(transcript string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.transcript = transcript
}
