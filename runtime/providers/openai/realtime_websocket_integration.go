// Package openai provides OpenAI Realtime API streaming support.
package openai

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers/internal/streaming"
)

// WebSocket connection constants
const (
	wsMaxMessageSize   = 64 * 1024 * 1024 // 64MB for audio
	wsMaxRetries       = 3
	wsRetryBackoffBase = time.Second
	wsRetryBackoffMax  = 10 * time.Second
)

// RealtimeWebSocket manages WebSocket connections for OpenAI Realtime API.
// It delegates transport concerns to the shared streaming.Conn.
type RealtimeWebSocket struct {
	conn *streaming.Conn
}

// NewRealtimeWebSocket creates a new WebSocket manager for OpenAI Realtime API.
func NewRealtimeWebSocket(model, apiKey string) *RealtimeWebSocket {
	wsURL := fmt.Sprintf("%s?model=%s", RealtimeAPIEndpoint, model)

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	headers.Set("OpenAI-Beta", RealtimeBetaHeader)

	return &RealtimeWebSocket{
		conn: streaming.NewConn(&streaming.ConnConfig{
			URL:              wsURL,
			Headers:          headers,
			MaxMessageSize:   wsMaxMessageSize,
			MaxRetries:       wsMaxRetries,
			RetryBackoffBase: wsRetryBackoffBase,
			RetryBackoffMax:  wsRetryBackoffMax,
			Logger:           &runtimeLoggerAdapter{prefix: "OpenAI Realtime"},
		}),
	}
}

// ConnectWithRetry attempts to connect with exponential backoff.
func (ws *RealtimeWebSocket) ConnectWithRetry(ctx context.Context) error {
	return ws.conn.ConnectWithRetry(ctx)
}

// Send sends a message to the WebSocket.
func (ws *RealtimeWebSocket) Send(msg interface{}) error {
	return ws.conn.Send(msg)
}

// Receive reads a message from the WebSocket with context support.
func (ws *RealtimeWebSocket) Receive(ctx context.Context) ([]byte, error) {
	return ws.conn.Receive(ctx)
}

// ReceiveLoop continuously reads messages and sends them to the provided channel.
func (ws *RealtimeWebSocket) ReceiveLoop(ctx context.Context, msgCh chan<- []byte) error {
	return ws.conn.ReceiveLoop(ctx, msgCh)
}

// StartHeartbeat starts a goroutine that sends ping messages periodically.
func (ws *RealtimeWebSocket) StartHeartbeat(ctx context.Context, interval time.Duration) {
	ws.conn.StartHeartbeat(ctx, interval)
}

// Close closes the WebSocket connection gracefully.
func (ws *RealtimeWebSocket) Close() error {
	return ws.conn.Close()
}

// IsClosed returns whether the WebSocket is closed.
func (ws *RealtimeWebSocket) IsClosed() bool {
	return ws.conn.IsClosed()
}

// Conn returns the underlying streaming.Conn for use by the session layer.
func (ws *RealtimeWebSocket) Conn() *streaming.Conn {
	return ws.conn
}

// runtimeLoggerAdapter adapts the runtime logger to the streaming.Logger interface.
type runtimeLoggerAdapter struct {
	prefix string
}

// Debug implements streaming.Logger.
func (a *runtimeLoggerAdapter) Debug(msg string, keysAndValues ...interface{}) {
	logger.Debug(a.prefix+": "+msg, keysAndValues...)
}

// Info implements streaming.Logger.
func (a *runtimeLoggerAdapter) Info(msg string, keysAndValues ...interface{}) {
	logger.Info(a.prefix+": "+msg, keysAndValues...)
}

// Warn implements streaming.Logger.
func (a *runtimeLoggerAdapter) Warn(msg string, keysAndValues ...interface{}) {
	logger.Warn(a.prefix+": "+msg, keysAndValues...)
}

// Error implements streaming.Logger.
func (a *runtimeLoggerAdapter) Error(msg string, keysAndValues ...interface{}) {
	logger.Error(a.prefix+": "+msg, keysAndValues...)
}
