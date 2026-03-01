package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers/internal/streaming"
)

// Common error messages
const (
	ErrNotConnected  = "not connected"
	ErrManagerClosed = "manager is closed"
)

// WebSocket limits and connection defaults.
const (
	// MaxMessageSize is the maximum allowed WebSocket message size (16MB).
	MaxMessageSize = 16 * 1024 * 1024

	geminiDialTimeout      = 45 * time.Second
	geminiMaxRetries       = 5
	geminiRetryBackoffMax  = 60 * time.Second
	geminiRetryBackoffBase = 1 * time.Second
)

// WebSocketManager manages a WebSocket connection with reconnection logic.
// It delegates transport concerns to the shared streaming.Conn.
type WebSocketManager struct {
	conn *streaming.Conn

	// url and apiKey are stored for reconnection support.
	url    string
	apiKey string
}

// NewWebSocketManager creates a new WebSocket manager
func NewWebSocketManager(url, apiKey string) *WebSocketManager {
	headers := http.Header{}
	headers.Set("x-goog-api-key", apiKey)

	return &WebSocketManager{
		url:    url,
		apiKey: apiKey,
		conn: streaming.NewConn(&streaming.ConnConfig{
			URL:              url,
			Headers:          headers,
			DialTimeout:      geminiDialTimeout,
			MaxMessageSize:   MaxMessageSize,
			MaxRetries:       geminiMaxRetries,
			RetryBackoffBase: geminiRetryBackoffBase,
			RetryBackoffMax:  geminiRetryBackoffMax,
			Logger:           &geminiLoggerAdapter{},
		}),
	}
}

// Connect establishes a WebSocket connection to the Gemini Live API
func (wm *WebSocketManager) Connect(ctx context.Context) error {
	return wm.conn.Connect(ctx)
}

// IsConnected returns true if the WebSocket is connected
func (wm *WebSocketManager) IsConnected() bool {
	return wm.conn.IsConnected()
}

// Send sends a message through the WebSocket
func (wm *WebSocketManager) Send(msg interface{}) error {
	return wm.conn.Send(msg)
}

// Receive reads a message from the WebSocket and unmarshals it into v.
func (wm *WebSocketManager) Receive(ctx context.Context, v interface{}) error {
	data, err := wm.conn.Receive(ctx)
	if err != nil {
		return fmt.Errorf("failed to read message: %w", err)
	}

	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("failed to unmarshal message: %w", err)
	}

	return nil
}

// SendPing sends a WebSocket ping to keep the connection alive
func (wm *WebSocketManager) SendPing() error {
	if wm.conn.IsClosed() {
		return errors.New(ErrNotConnected)
	}
	return nil
}

// StartHeartbeat starts a goroutine that sends periodic pings
func (wm *WebSocketManager) StartHeartbeat(ctx context.Context, interval time.Duration) {
	wm.conn.StartHeartbeat(ctx, interval)
}

// ConnectWithRetry connects with exponential backoff retry logic
func (wm *WebSocketManager) ConnectWithRetry(ctx context.Context) error {
	return wm.conn.ConnectWithRetry(ctx)
}

// Close gracefully closes the WebSocket connection
func (wm *WebSocketManager) Close() error {
	return wm.conn.Close()
}

// Conn returns the underlying streaming.Conn for use by the session layer.
func (wm *WebSocketManager) Conn() *streaming.Conn {
	return wm.conn
}

// Reset closes the current connection and prepares for reuse (reconnection).
func (wm *WebSocketManager) Reset() {
	wm.conn.Reset()
}

// geminiLoggerAdapter adapts the runtime logger to the streaming.Logger interface.
type geminiLoggerAdapter struct{}

// Debug implements streaming.Logger.
func (a *geminiLoggerAdapter) Debug(msg string, keysAndValues ...interface{}) {
	logger.Debug(msg, append([]interface{}{"component", "Gemini"}, keysAndValues...)...)
}

// Info implements streaming.Logger.
func (a *geminiLoggerAdapter) Info(msg string, keysAndValues ...interface{}) {
	logger.Info(msg, append([]interface{}{"component", "Gemini"}, keysAndValues...)...)
}

// Warn implements streaming.Logger.
func (a *geminiLoggerAdapter) Warn(msg string, keysAndValues ...interface{}) {
	logger.Warn(msg, append([]interface{}{"component", "Gemini"}, keysAndValues...)...)
}

// Error implements streaming.Logger.
func (a *geminiLoggerAdapter) Error(msg string, keysAndValues ...interface{}) {
	logger.Error(msg, append([]interface{}{"component", "Gemini"}, keysAndValues...)...)
}
