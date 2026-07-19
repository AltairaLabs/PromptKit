package audio

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// DefaultMaxAudioBufferSize is the maximum size of the audio buffer in bytes.
// At 16kHz/16-bit mono (32KB/s), 10MB holds approximately 5 minutes of audio.
const DefaultMaxAudioBufferSize = 10 * 1024 * 1024

// SilenceDetector detects turn boundaries based on silence duration.
// It triggers end-of-turn when silence exceeds a configurable threshold.
type SilenceDetector struct {
	// Threshold is the silence duration required to trigger turn end.
	Threshold time.Duration

	// MaxBufferSize is the maximum audio buffer size in bytes.
	// When exceeded, the oldest audio data is discarded to stay within the limit.
	// Default: DefaultMaxAudioBufferSize (10MB).
	MaxBufferSize int

	mu           sync.RWMutex
	silenceStart time.Time
	inSilence    bool
	userSpeaking bool
	audioBuffer  []byte
	// bufStart is the offset in audioBuffer at which live audio begins.
	// Trimming advances this instead of moving bytes; the dead prefix is
	// reclaimed by an occasional compaction. See trimToCapLocked.
	bufStart     int
	transcript   string
	turnCallback TurnCallback
	lastVADState VADState
	hadSpeech    bool // Track if we've had any speech this turn
}

// liveAudioLocked returns the audio currently retained, excluding any dead
// prefix left behind by trimming. Must be called with mu held.
func (d *SilenceDetector) liveAudioLocked() []byte {
	return d.audioBuffer[d.bufStart:]
}

// trimToCapLocked enforces MaxBufferSize, keeping the most recent audio.
// Must be called with mu held.
//
// Trimming advances bufStart rather than shifting the buffer down. Shifting per
// chunk costs a full memmove of the retained audio on every call once the cap is
// reached — with the 10MB default at ~100 chunks/sec that measured 121x slower
// than the uncapped path (2561 MB/s down to 21 MB/s), scaling linearly with
// MaxBufferSize.
//
// The dead prefix is reclaimed only once it grows past the cap, so a compaction
// copies at most MaxBufferSize bytes per MaxBufferSize bytes appended: O(1)
// amortized per byte instead of O(buffer) per chunk. The cost is up to 2x
// MaxBufferSize of allocated slice while a dead prefix is outstanding.
func (d *SilenceDetector) trimToCapLocked() {
	if d.MaxBufferSize <= 0 {
		return
	}

	live := len(d.audioBuffer) - d.bufStart
	if live <= d.MaxBufferSize {
		return
	}

	// Drop the oldest audio by advancing the window, not by moving bytes.
	d.bufStart += live - d.MaxBufferSize

	// Reclaim once the dead prefix is worth a copy. Logging here rather than on
	// every trim keeps this off the per-chunk path: at 100 chunks/sec a
	// per-chunk warning is a sustained 100 lines/sec of log spam.
	if d.bufStart >= d.MaxBufferSize {
		copy(d.audioBuffer, d.liveAudioLocked())
		d.audioBuffer = d.audioBuffer[:d.MaxBufferSize]
		d.bufStart = 0
		slog.Warn("audio buffer at max size, dropped oldest data",
			"max_buffer_size", d.MaxBufferSize,
			"dropped_bytes", d.MaxBufferSize,
		)
	}
}

// SilenceDetectorOption configures a SilenceDetector.
type SilenceDetectorOption func(*SilenceDetector)

// WithMaxAudioBufferSize sets the maximum audio buffer size in bytes.
// When the buffer exceeds this limit, the oldest data is trimmed.
func WithMaxAudioBufferSize(size int) SilenceDetectorOption {
	return func(d *SilenceDetector) {
		d.MaxBufferSize = size
	}
}

// NewSilenceDetector creates a SilenceDetector with the given threshold.
// threshold is the duration of silence required to trigger end-of-turn.
func NewSilenceDetector(threshold time.Duration, opts ...SilenceDetectorOption) *SilenceDetector {
	d := &SilenceDetector{
		Threshold:     threshold,
		MaxBufferSize: DefaultMaxAudioBufferSize,
		silenceStart:  time.Now(),
		inSilence:     true,
		lastVADState:  VADStateQuiet,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
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
		d.trimToCapLocked()
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
	if live := d.liveAudioLocked(); d.turnCallback != nil && len(live) > 0 {
		// Copy buffer before callback
		audio := make([]byte, len(live))
		copy(audio, live)
		transcript := d.transcript

		// Fire callback (without lock to prevent deadlock)
		go d.turnCallback(audio, transcript)
	}

	// Reset for next turn
	d.audioBuffer = nil
	d.bufStart = 0
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
	d.bufStart = 0
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

	live := d.liveAudioLocked()
	if len(live) == 0 {
		return nil
	}
	result := make([]byte, len(live))
	copy(result, live)
	return result
}

// SetTranscript sets the transcript for the current turn.
func (d *SilenceDetector) SetTranscript(transcript string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.transcript = transcript
}
