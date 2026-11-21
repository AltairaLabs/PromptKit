// Package streaming provides a common WebSocket streaming session abstraction
// used by provider implementations (OpenAI Realtime, Gemini Live, etc.).
//
// The package separates transport-level concerns (connect, send, receive, heartbeat,
// reconnect) from provider-specific protocol details (message encoding/decoding).
package streaming

import (
	"cmp"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/AltairaLabs/PromptKit/runtime/v2/internal/retry"
)

// Default connection constants.
const (
	DefaultDialTimeout      = 10 * time.Second
	DefaultWriteWait        = 10 * time.Second
	DefaultMaxMessageSize   = 16 * 1024 * 1024 // 16MB
	DefaultMaxRetries       = 3
	DefaultRetryBackoffBase = 1 * time.Second
	DefaultRetryBackoffMax  = 30 * time.Second
	DefaultCloseGracePeriod = 5 * time.Second
)

// jitterFactor is the +-25% jitter applied to backoff delays.
const jitterFactor = 0.25

// defaultConnName names a Conn without a Name in retry logs and metrics.
const defaultConnName = "websocket"

// ConnConfig configures the WebSocket connection behavior.
type ConnConfig struct {
	// URL is the WebSocket endpoint URL.
	URL string

	// Headers are sent during the WebSocket handshake.
	Headers http.Header

	// DialHeaders, when set, is called before every dial attempt and its
	// headers are added over Headers. It carries credentials that must be
	// fresh at handshake time (a platform token that expires), so a retry or
	// reconnect never reuses one captured when the Conn was built.
	DialHeaders func(ctx context.Context) (http.Header, error)

	// DialTimeout is the handshake timeout. Defaults to DefaultDialTimeout.
	DialTimeout time.Duration

	// WriteWait is the write deadline for each message. Defaults to DefaultWriteWait.
	WriteWait time.Duration

	// MaxMessageSize is the read limit. Defaults to DefaultMaxMessageSize.
	MaxMessageSize int64

	// MaxRetries is the number of connection attempts for ConnectWithRetry.
	// Defaults to DefaultMaxRetries.
	MaxRetries int

	// RetryBackoffBase is the initial backoff delay. Defaults to DefaultRetryBackoffBase.
	RetryBackoffBase time.Duration

	// RetryBackoffMax caps the backoff delay. Defaults to DefaultRetryBackoffMax.
	RetryBackoffMax time.Duration

	// Name identifies the connection in retry logs and metrics. Defaults to
	// "websocket".
	Name string

	// CloseGracePeriod is the deadline for writing the close frame.
	// Defaults to DefaultCloseGracePeriod.
	CloseGracePeriod time.Duration

	// Logger receives debug/warn/error log messages. Optional.
	Logger Logger
}

// Logger is an optional interface for structured logging.
type Logger interface {
	Debug(msg string, keysAndValues ...interface{})
	Info(msg string, keysAndValues ...interface{})
	Warn(msg string, keysAndValues ...interface{})
	Error(msg string, keysAndValues ...interface{})
}

// noopLogger discards all log output.
type noopLogger struct{}

// Debug implements Logger.
func (noopLogger) Debug(_ string, _ ...interface{}) { /* discarded */ }

// Info implements Logger.
func (noopLogger) Info(_ string, _ ...interface{}) { /* discarded */ }

// Warn implements Logger.
func (noopLogger) Warn(_ string, _ ...interface{}) { /* discarded */ }

// Error implements Logger.
func (noopLogger) Error(_ string, _ ...interface{}) { /* discarded */ }

func (c *ConnConfig) defaults() {
	if c.DialTimeout == 0 {
		c.DialTimeout = DefaultDialTimeout
	}
	if c.WriteWait == 0 {
		c.WriteWait = DefaultWriteWait
	}
	if c.MaxMessageSize == 0 {
		c.MaxMessageSize = DefaultMaxMessageSize
	}
	if c.MaxRetries == 0 {
		c.MaxRetries = DefaultMaxRetries
	}
	if c.RetryBackoffBase == 0 {
		c.RetryBackoffBase = DefaultRetryBackoffBase
	}
	if c.RetryBackoffMax == 0 {
		c.RetryBackoffMax = DefaultRetryBackoffMax
	}
	if c.CloseGracePeriod == 0 {
		c.CloseGracePeriod = DefaultCloseGracePeriod
	}
	if c.Logger == nil {
		c.Logger = noopLogger{}
	}
}

// Conn manages a WebSocket connection with retry, heartbeat, and graceful shutdown.
// It handles the transport layer while leaving message encoding to the caller.
type Conn struct {
	cfg ConnConfig

	conn    *websocket.Conn
	mu      sync.Mutex
	writeMu sync.Mutex // serializes writes (gorilla/websocket requirement)
	closed  bool
	closeCh chan struct{}
}

// NewConn creates a new Conn. Call Connect or ConnectWithRetry to establish the connection.
func NewConn(cfg *ConnConfig) *Conn {
	cfg.defaults()
	return &Conn{
		cfg:     *cfg,
		closeCh: make(chan struct{}),
	}
}

// Connect establishes a WebSocket connection.
func (c *Conn) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return fmt.Errorf("connection is closed")
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: c.cfg.DialTimeout,
		TLSClientConfig:  &tls.Config{MinVersion: tls.VersionTLS12},
	}

	c.cfg.Logger.Debug("connecting to WebSocket", "url", c.cfg.URL)

	headers, err := c.handshakeHeaders(ctx)
	if err != nil {
		return err
	}

	conn, resp, err := dialer.DialContext(ctx, c.cfg.URL, headers)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
			c.cfg.Logger.Error("WebSocket dial failed", "error", err, "status", resp.StatusCode)
		}
		if resp != nil {
			err = &refusedHandshakeError{err: err, retryAfter: retry.ParseRetryAfter(resp.Header)}
		}
		return fmt.Errorf("failed to connect: %w", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}

	conn.SetReadLimit(c.cfg.MaxMessageSize)

	c.conn = conn
	c.cfg.Logger.Info("WebSocket connected successfully")

	return nil
}

// handshakeHeaders returns the static Headers with any DialHeaders added over them.
func (c *Conn) handshakeHeaders(ctx context.Context) (http.Header, error) {
	if c.cfg.DialHeaders == nil {
		return c.cfg.Headers, nil
	}
	dial, err := c.cfg.DialHeaders(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build handshake headers: %w", err)
	}
	headers := c.cfg.Headers.Clone()
	if headers == nil {
		headers = http.Header{}
	}
	for name, values := range dial {
		headers[name] = values
	}
	return headers, nil
}

// ConnectWithRetry makes up to MaxRetries connection attempts, waiting a
// jittered exponential backoff between them, or the Retry-After of a refused
// handshake.
func (c *Conn) ConnectWithRetry(ctx context.Context) error {
	attempt := 0
	_, err := retry.Do(ctx, connectPolicy(&c.cfg), func(ctx context.Context) (struct{}, error) {
		attempt++
		err := c.Connect(ctx)
		if err != nil {
			c.cfg.Logger.Warn("connection attempt failed",
				"attempt", attempt, "maxAttempts", c.cfg.MaxRetries, "error", err)
		}
		return struct{}{}, err
	})
	if err == nil || errors.Is(err, ctx.Err()) {
		return err
	}
	return fmt.Errorf("failed to connect after %d attempts: %w", attempt, err)
}

// connectPolicy retries every failed connect, a handshake timeout included;
// the caller's context ending stops the wait that follows. A refused
// handshake's Retry-After, capped at RetryBackoffMax, replaces the backoff.
func connectPolicy(cfg *ConnConfig) retry.Policy {
	return retry.Policy{
		Name:          cmp.Or(cfg.Name, defaultConnName),
		MaxAttempts:   cfg.MaxRetries,
		Backoff:       connectBackoff(cfg),
		MaxRetryAfter: cfg.RetryBackoffMax,
		Classify:      func(err error) (bool, time.Duration) { return true, retryAfterOf(err) },
	}
}

// connectBackoff starts at RetryBackoffBase and doubles per attempt up to
// RetryBackoffMax, each wait varied by up to a quarter either way and never
// more than RetryBackoffMax.
func connectBackoff(cfg *ConnConfig) retry.Backoff {
	return retry.Backoff{
		Initial:   cfg.RetryBackoffBase,
		Max:       cfg.RetryBackoffMax,
		JitterMin: 1 - jitterFactor,
		JitterMax: 1 + jitterFactor,
	}
}

// refusedHandshakeError is a handshake the server answered with a refusal; a
// positive retryAfter is the wait its Retry-After asked for.
type refusedHandshakeError struct {
	err        error
	retryAfter time.Duration
}

func (e *refusedHandshakeError) Error() string { return e.err.Error() }
func (e *refusedHandshakeError) Unwrap() error { return e.err }

// retryAfterOf returns the Retry-After of a refused handshake, or zero.
func retryAfterOf(err error) time.Duration {
	var refused *refusedHandshakeError
	if errors.As(err, &refused) {
		return refused.retryAfter
	}
	return 0
}

// Send JSON-encodes msg and writes it to the WebSocket.
func (c *Conn) Send(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	return c.SendRaw(data)
}

// SendRaw writes pre-encoded data to the WebSocket.
func (c *Conn) SendRaw(data []byte) error {
	c.mu.Lock()
	if c.closed || c.conn == nil {
		c.mu.Unlock()
		return fmt.Errorf("websocket is not connected")
	}
	conn := c.conn
	c.mu.Unlock()

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := conn.SetWriteDeadline(time.Now().Add(c.cfg.WriteWait)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}

	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	return nil
}

// Receive reads a single message from the WebSocket. The call blocks until a message
// arrives or the context is canceled.
// When the context is canceled, a read deadline is set on the connection to force
// the blocking ReadMessage to return, preventing a goroutine leak.
func (c *Conn) Receive(ctx context.Context) ([]byte, error) {
	c.mu.Lock()
	if c.closed || c.conn == nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("websocket is not connected")
	}
	conn := c.conn
	c.mu.Unlock()

	type readResult struct {
		msgType int
		data    []byte
		err     error
	}
	ch := make(chan readResult, 1)

	go func() {
		msgType, data, err := conn.ReadMessage()
		ch <- readResult{msgType: msgType, data: data, err: err}
	}()

	select {
	case <-ctx.Done():
		// Set a read deadline in the past to unblock the ReadMessage goroutine,
		// preventing a goroutine leak.
		_ = conn.SetReadDeadline(time.Now())
		// Wait for the goroutine to finish so we don't leak it.
		<-ch
		// Clear the deadline for future reads (e.g., after reconnect).
		_ = conn.SetReadDeadline(time.Time{})
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		// Accept both text and binary messages
		if r.msgType != websocket.TextMessage && r.msgType != websocket.BinaryMessage {
			return nil, fmt.Errorf("unexpected message type: %d", r.msgType)
		}
		return r.data, nil
	}
}

// ReceiveLoop continuously reads messages and sends them to msgCh.
// It returns when the connection is closed, an error occurs, or the context is canceled.
func (c *Conn) ReceiveLoop(ctx context.Context, msgCh chan<- []byte) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.closeCh:
			return nil
		default:
		}

		data, err := c.Receive(ctx)
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return nil
			}
			return err
		}

		select {
		case msgCh <- data:
		case <-ctx.Done():
			return ctx.Err()
		case <-c.closeCh:
			return nil
		}
	}
}

// StartHeartbeat starts a goroutine that sends WebSocket ping frames at the given interval.
func (c *Conn) StartHeartbeat(ctx context.Context, interval time.Duration) {
	go c.heartbeatLoop(ctx, interval)
}

func (c *Conn) heartbeatLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.closeCh:
			return
		case <-ticker.C:
			if !c.sendPing() {
				// Ping failed — the connection is dead or stalled.
				// Close the underlying WebSocket so ReceiveLoop
				// unblocks with an error and the session terminates
				// instead of silently going stale.
				c.cfg.Logger.Warn("heartbeat ping failed, closing connection")
				_ = c.Close()
				return
			}
		}
	}
}

func (c *Conn) sendPing() bool {
	c.mu.Lock()
	if c.closed || c.conn == nil {
		c.mu.Unlock()
		return false
	}
	conn := c.conn
	c.mu.Unlock()

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := conn.SetWriteDeadline(time.Now().Add(c.cfg.WriteWait)); err != nil {
		c.cfg.Logger.Warn("failed to set write deadline for ping", "error", err)
		return true // non-fatal
	}

	if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
		c.cfg.Logger.Warn("ping failed", "error", err)
		return false
	}

	return true
}

// Close gracefully closes the WebSocket connection.
// Lock ordering: c.mu is released before acquiring c.writeMu to prevent deadlock
// with SendRaw which acquires c.mu then c.writeMu.
func (c *Conn) Close() error {
	c.mu.Lock()

	if c.closed {
		c.mu.Unlock()
		return nil
	}

	c.closed = true
	close(c.closeCh)

	conn := c.conn
	if conn == nil {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	// Acquire writeMu after releasing mu to maintain consistent lock ordering.
	c.writeMu.Lock()
	closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
	_ = conn.SetWriteDeadline(time.Now().Add(c.cfg.CloseGracePeriod))
	_ = conn.WriteMessage(websocket.CloseMessage, closeMsg)
	c.writeMu.Unlock()

	return conn.Close()
}

// IsClosed returns whether the connection has been closed.
func (c *Conn) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// IsConnected returns true if the connection has been established and has not been closed.
func (c *Conn) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil && !c.closed
}

// Reset closes the current connection and prepares for a new one.
// This is useful for reconnection flows where the caller needs to re-establish
// the connection with a fresh state.
// Lock ordering: c.mu is released before acquiring c.writeMu to prevent deadlock.
func (c *Conn) Reset() {
	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()

	if conn != nil {
		c.writeMu.Lock()
		_ = conn.Close()
		c.writeMu.Unlock()
	}

	c.mu.Lock()
	// Reset closed state and channel so the Conn can be reused.
	c.closed = false
	c.closeCh = make(chan struct{})
	c.mu.Unlock()
}
