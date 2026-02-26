package streaming

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// Default session constants.
const (
	DefaultResponseChannelSize = 10
)

// MessageHandler processes a raw WebSocket message and converts it into zero or more
// StreamChunk values. Returning a non-nil error signals a fatal session error.
type MessageHandler func(data []byte) ([]providers.StreamChunk, error)

// ErrorClassifier inspects a receive-loop error and decides whether the session
// should attempt reconnection or give up.
type ErrorClassifier func(err error) (shouldReconnect bool)

// ReconnectHook is called when the session wants to reconnect. The implementation
// should re-establish provider-specific setup (e.g. resend a Gemini setup message
// or wait for an OpenAI session.created event) on the provided Conn.
// Return nil on success, or an error to abandon the reconnection attempt.
type ReconnectHook func(ctx context.Context, conn *Conn) error

// SessionConfig configures a streaming Session.
type SessionConfig struct {
	// Conn is the underlying WebSocket connection. Required.
	Conn *Conn

	// OnMessage processes raw WebSocket messages into StreamChunks. Required.
	OnMessage MessageHandler

	// OnError classifies receive errors. Optional — when nil, all errors are fatal.
	OnError ErrorClassifier

	// OnReconnect is called to re-establish provider state after a new connection.
	// Optional — when nil, reconnection is disabled.
	OnReconnect ReconnectHook

	// MaxReconnectAttempts limits how many times the session will try to reconnect.
	// Defaults to 0 (no reconnection) when OnReconnect is nil.
	MaxReconnectAttempts int

	// ResponseChannelSize sets the buffer size of the response channel.
	// Defaults to DefaultResponseChannelSize.
	ResponseChannelSize int

	// Logger for session-level messages. Optional.
	Logger Logger
}

// Session manages a bidirectional streaming session over a WebSocket connection.
// It runs a receive loop that decodes messages via the caller-provided MessageHandler
// and emits StreamChunk values on the Response channel. The session supports optional
// automatic reconnection on transient errors.
type Session struct {
	conn   *Conn
	cfg    SessionConfig
	ctx    context.Context
	cancel context.CancelFunc

	responseCh chan providers.StreamChunk
	errCh      chan error
	mu         sync.Mutex
	closed     bool
}

// NewSession creates and starts a streaming session. The receive loop is started
// automatically in a background goroutine.
func NewSession(ctx context.Context, cfg SessionConfig) (*Session, error) {
	if cfg.Conn == nil {
		return nil, fmt.Errorf("streaming.SessionConfig.Conn is required")
	}
	if cfg.OnMessage == nil {
		return nil, fmt.Errorf("streaming.SessionConfig.OnMessage is required")
	}
	if cfg.ResponseChannelSize <= 0 {
		cfg.ResponseChannelSize = DefaultResponseChannelSize
	}
	if cfg.Logger == nil {
		cfg.Logger = noopLogger{}
	}

	sessionCtx, cancel := context.WithCancel(ctx)

	s := &Session{
		conn:       cfg.Conn,
		cfg:        cfg,
		ctx:        sessionCtx,
		cancel:     cancel,
		responseCh: make(chan providers.StreamChunk, cfg.ResponseChannelSize),
		errCh:      make(chan error, 1),
	}

	go s.receiveLoop()

	return s, nil
}

// Send JSON-encodes and sends a message through the underlying connection.
// Returns an error if the session is closed.
func (s *Session) Send(msg interface{}) error {
	if err := s.checkClosed(); err != nil {
		return err
	}
	return s.conn.Send(msg)
}

// SendRaw sends pre-encoded data through the underlying connection.
func (s *Session) SendRaw(data []byte) error {
	if err := s.checkClosed(); err != nil {
		return err
	}
	return s.conn.SendRaw(data)
}

// Response returns the channel for receiving streaming response chunks.
// The channel is closed when the session ends.
func (s *Session) Response() <-chan providers.StreamChunk {
	return s.responseCh
}

// Done returns a channel that is closed when the session context is canceled.
func (s *Session) Done() <-chan struct{} {
	return s.ctx.Done()
}

// Error returns the first error that caused the session to end, or nil.
func (s *Session) Error() error {
	select {
	case err := <-s.errCh:
		return err
	default:
		return nil
	}
}

// Close terminates the session and closes the underlying connection.
// Safe to call multiple times.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	s.cancel()
	return s.conn.Close()
}

// Conn returns the underlying connection, allowing callers to perform
// provider-specific operations (e.g., direct Receive for setup handshakes).
func (s *Session) Conn() *Conn {
	return s.conn
}

// Closed reports whether the session has been closed.
func (s *Session) Closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Session) checkClosed() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session is closed")
	}
	return nil
}

func (s *Session) receiveLoop() {
	s.cfg.Logger.Debug("receive loop started")
	defer func() {
		s.cfg.Logger.Debug("receive loop exiting, closing response channel")
		close(s.responseCh)
	}()

	msgCh := make(chan []byte, s.cfg.ResponseChannelSize)
	errCh := make(chan error, 1)

	go func() {
		errCh <- s.conn.ReceiveLoop(s.ctx, msgCh)
	}()

	for {
		select {
		case <-s.ctx.Done():
			s.cfg.Logger.Debug("receive loop context done")
			return

		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				if s.tryReconnect(err) {
					// Restart the inner receive loop goroutine after successful reconnect.
					errCh = make(chan error, 1)
					msgCh = make(chan []byte, s.cfg.ResponseChannelSize)
					go func() {
						errCh <- s.conn.ReceiveLoop(s.ctx, msgCh)
					}()
					continue
				}
				s.cfg.Logger.Error("receive loop error", "error", err)
				s.emitError(err)
			}
			return

		case data := <-msgCh:
			s.handleMessage(data)
		}
	}
}

func (s *Session) handleMessage(data []byte) {
	chunks, err := s.cfg.OnMessage(data)
	if err != nil {
		s.cfg.Logger.Error("message handler error", "error", err)
		s.emitError(err)
		return
	}
	for i := range chunks {
		select {
		case s.responseCh <- chunks[i]:
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *Session) tryReconnect(err error) bool {
	if s.cfg.OnReconnect == nil || s.cfg.MaxReconnectAttempts <= 0 {
		return false
	}

	if s.cfg.OnError != nil && !s.cfg.OnError(err) {
		return false
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()

	for attempt := 1; attempt <= s.cfg.MaxReconnectAttempts; attempt++ {
		s.cfg.Logger.Info("attempting reconnection", "attempt", attempt,
			"maxAttempts", s.cfg.MaxReconnectAttempts)

		// Reset the connection for a fresh dial.
		s.conn.Reset()

		if err := s.conn.ConnectWithRetry(s.ctx); err != nil {
			s.cfg.Logger.Warn("reconnection dial failed", "attempt", attempt, "error", err)
			continue
		}

		if err := s.cfg.OnReconnect(s.ctx, s.conn); err != nil {
			s.cfg.Logger.Warn("reconnection hook failed", "attempt", attempt, "error", err)
			continue
		}

		s.cfg.Logger.Info("reconnection successful", "attempt", attempt)
		return true
	}

	s.cfg.Logger.Error("all reconnection attempts failed")
	return false
}

func (s *Session) emitError(err error) {
	select {
	case s.errCh <- err:
	default:
	}
}
