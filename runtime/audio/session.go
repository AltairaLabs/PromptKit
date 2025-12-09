package audio

import (
	"context"
	"errors"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ErrSessionClosed is returned when operations are attempted on a closed session.
var ErrSessionClosed = errors.New("audio session closed")

// SessionConfig configures an audio Session wrapper.
type SessionConfig struct {
	// VAD is the voice activity detector to use.
	// If nil, a SimpleVAD with default params is created.
	VAD VADAnalyzer

	// TurnDetector determines when user has finished speaking.
	// If nil, turn detection is disabled.
	TurnDetector TurnDetector

	// InterruptionStrategy for handling user interruptions.
	// Default: InterruptionIgnore
	InterruptionStrategy InterruptionStrategy

	// AutoCompleteTurn automatically processes turn when detected.
	// When true, the session handles turn completion internally.
	AutoCompleteTurn bool
}

// Session wraps a StreamInputSession with VAD, turn detection, and interruption handling.
// It provides a higher-level interface for voice AI applications.
type Session struct {
	underlying   providers.StreamInputSession
	vad          VADAnalyzer
	turnDetector TurnDetector
	interruption *InterruptionHandler
	config       SessionConfig

	mu              sync.RWMutex
	turnDetected    chan struct{}
	interruptNotify chan struct{}
	closed          bool
}

// NewSession wraps an existing streaming session with audio processing.
func NewSession(
	session providers.StreamInputSession,
	config SessionConfig,
) (*Session, error) {
	// Create default VAD if not provided
	vad := config.VAD
	if vad == nil {
		var err error
		vad, err = NewSimpleVAD(DefaultVADParams())
		if err != nil {
			return nil, err
		}
	}

	// Create interruption handler if needed
	var interruption *InterruptionHandler
	if config.InterruptionStrategy != InterruptionIgnore {
		interruption = NewInterruptionHandler(config.InterruptionStrategy, vad)
	}

	s := &Session{
		underlying:      session,
		vad:             vad,
		turnDetector:    config.TurnDetector,
		interruption:    interruption,
		config:          config,
		turnDetected:    make(chan struct{}, 1),
		interruptNotify: make(chan struct{}, 1),
	}

	// Set up interruption callback
	if interruption != nil {
		interruption.OnInterrupt(func() {
			s.notifyInterrupt()
		})
	}

	return s, nil
}

// SendChunk sends an audio chunk with VAD and turn detection processing.
func (s *Session) SendChunk(ctx context.Context, chunk *types.MediaChunk) error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return ErrSessionClosed
	}
	s.mu.RUnlock()

	// Process through VAD
	_, err := s.vad.Analyze(ctx, chunk.Data)
	if err != nil {
		return err
	}

	vadState := s.vad.State()

	// Check for interruption
	if s.interruption != nil {
		if interrupted, _ := s.interruption.ProcessVADState(ctx, vadState); interrupted {
			s.notifyInterrupt()
		}
	}

	// Process turn detection
	if s.turnDetector != nil {
		// Let turn detector accumulate audio
		if _, err := s.turnDetector.ProcessAudio(ctx, chunk.Data); err != nil {
			return err
		}

		// Check for turn end based on VAD state
		if endOfTurn, err := s.turnDetector.ProcessVADState(ctx, vadState); err != nil {
			return err
		} else if endOfTurn {
			s.notifyTurnDetected()
		}
	}

	// Forward to underlying session
	return s.underlying.SendChunk(ctx, chunk)
}

// SendText sends a text message to the underlying session.
func (s *Session) SendText(ctx context.Context, text string) error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return ErrSessionClosed
	}
	s.mu.RUnlock()

	return s.underlying.SendText(ctx, text)
}

// Response returns the response channel from the underlying session.
func (s *Session) Response() <-chan providers.StreamChunk {
	return s.underlying.Response()
}

// Close ends the session and cleans up resources.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	close(s.turnDetected)
	close(s.interruptNotify)

	return s.underlying.Close()
}

// Error returns any error from the underlying session.
func (s *Session) Error() error {
	return s.underlying.Error()
}

// Done returns a channel that's closed when the session ends.
func (s *Session) Done() <-chan struct{} {
	return s.underlying.Done()
}

// OnTurnDetected returns a channel that signals when a turn is detected.
// The channel receives a signal (empty struct) each time turn detection fires.
func (s *Session) OnTurnDetected() <-chan struct{} {
	return s.turnDetected
}

// OnInterruption returns a channel that signals when user interrupts.
func (s *Session) OnInterruption() <-chan struct{} {
	return s.interruptNotify
}

// VADState returns the current voice activity state.
func (s *Session) VADState() VADState {
	return s.vad.State()
}

// IsUserSpeaking returns true if the user is currently speaking.
func (s *Session) IsUserSpeaking() bool {
	if s.turnDetector != nil {
		return s.turnDetector.IsUserSpeaking()
	}
	state := s.vad.State()
	return state == VADStateSpeaking || state == VADStateStarting
}

// SetBotSpeaking notifies the session that bot is/isn't outputting audio.
// This is used for interruption detection.
func (s *Session) SetBotSpeaking(speaking bool) {
	if s.interruption != nil {
		s.interruption.SetBotSpeaking(speaking)
	}
}

// Reset clears state for a new conversation turn.
func (s *Session) Reset() {
	s.vad.Reset()
	if s.turnDetector != nil {
		s.turnDetector.Reset()
	}
	if s.interruption != nil {
		s.interruption.Reset()
	}
}

// notifyTurnDetected sends a signal to the turn detected channel.
func (s *Session) notifyTurnDetected() {
	select {
	case s.turnDetected <- struct{}{}:
	default:
		// Channel full, signal already pending
	}
}

// notifyInterrupt sends a signal to the interruption channel.
func (s *Session) notifyInterrupt() {
	select {
	case s.interruptNotify <- struct{}{}:
	default:
		// Channel full, signal already pending
	}
}

// GetAccumulatedAudio returns audio accumulated during the current turn.
// Only available if TurnDetector implements AccumulatingTurnDetector.
func (s *Session) GetAccumulatedAudio() []byte {
	if acc, ok := s.turnDetector.(AccumulatingTurnDetector); ok {
		return acc.GetAccumulatedAudio()
	}
	return nil
}
