// Package streaming provides a common WebSocket streaming session abstraction
// used by provider implementations (OpenAI Realtime, Gemini Live, etc.).
//
// The package separates transport-level concerns (connect, send, receive, heartbeat,
// reconnect) from provider-specific protocol details (message encoding/decoding).
package streaming

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
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

// jitterPrecision is the granularity for crypto/rand jitter generation.
const jitterPrecision = 1000

// jitterHalfPrecision normalizes jitter output to the range [-1, 1].
const jitterHalfPrecision = jitterPrecision / 2

// ConnConfig configures the WebSocket connection behavior.
type ConnConfig struct {
	// URL is the WebSocket endpoint URL.
	URL string

	// Headers are sent during the WebSocket handshake.
	Headers http.Header

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
func (noopLogger) Debug(_ string, _ ...interface{}) {}

// Info implements Logger.
func (noopLogger) Info(_ string, _ ...interface{}) {}

// Warn implements Logger.
func (noopLogger) Warn(_ string, _ ...interface{}) {}

// Error implements Logger.
func (noopLogger) Error(_ string, _ ...interface{}) {}

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

	conn, resp, err := dialer.DialContext(ctx, c.cfg.URL, c.cfg.Headers)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
			c.cfg.Logger.Error("WebSocket dial failed", "error", err, "status", resp.StatusCode)
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

// ConnectWithRetry attempts to connect with exponential backoff and jitter.
func (c *Conn) ConnectWithRetry(ctx context.Context) error {
	var lastErr error
	backoff := c.cfg.RetryBackoffBase

	for attempt := 1; attempt <= c.cfg.MaxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := c.Connect(ctx)
		if err == nil {
			return nil
		}

		lastErr = err

		c.cfg.Logger.Warn("connection attempt failed",
			"attempt", attempt, "maxAttempts", c.cfg.MaxRetries, "error", lastErr)

		if attempt < c.cfg.MaxRetries {
			delay := calculateBackoff(backoff, c.cfg.RetryBackoffMax)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			backoff *= 2
			if backoff > c.cfg.RetryBackoffMax {
				backoff = c.cfg.RetryBackoffMax
			}
		}
	}

	return fmt.Errorf("failed to connect after %d attempts: %w", c.cfg.MaxRetries, lastErr)
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
func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true
	close(c.closeCh)

	if c.conn == nil {
		return nil
	}

	c.writeMu.Lock()
	closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
	_ = c.conn.SetWriteDeadline(time.Now().Add(c.cfg.CloseGracePeriod))
	_ = c.conn.WriteMessage(websocket.CloseMessage, closeMsg)
	c.writeMu.Unlock()

	return c.conn.Close()
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
func (c *Conn) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.writeMu.Lock()
		_ = c.conn.Close()
		c.writeMu.Unlock()
		c.conn = nil
	}

	// Reset closed state and channel so the Conn can be reused.
	c.closed = false
	c.closeCh = make(chan struct{})
}

// calculateBackoff computes a backoff duration with +-25% jitter, capped at maxDelay.
func calculateBackoff(base, maxDelay time.Duration) time.Duration {
	delay := float64(base)
	if delay > float64(maxDelay) {
		delay = float64(maxDelay)
	}
	// Jitter: +-25% using crypto/rand for SonarCloud compliance.
	n, _ := rand.Int(rand.Reader, big.NewInt(jitterPrecision))
	jitter := delay * jitterFactor * (float64(n.Int64())/jitterHalfPrecision - 1)
	result := delay + jitter
	if result < 0 {
		result = float64(base)
	}
	if result > float64(maxDelay) {
		result = float64(maxDelay)
	}
	return time.Duration(math.Max(result, 0))
}
