// Package openai provides OpenAI Realtime API streaming support.
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// WebSocket connection constants
const (
	wsDialTimeout      = 10 * time.Second
	wsWriteWait        = 10 * time.Second
	wsPongWait         = 60 * time.Second
	wsMaxMessageSize   = 64 * 1024 * 1024 // 64MB for audio
	wsMaxRetries       = 3
	wsRetryBackoffBase = time.Second
	wsRetryBackoffMax  = 10 * time.Second
	wsCloseGracePeriod = 5 * time.Second
)

// RealtimeWebSocket manages WebSocket connections for OpenAI Realtime API.
type RealtimeWebSocket struct {
	conn      *websocket.Conn
	url       string
	apiKey    string
	mu        sync.Mutex
	closed    bool
	closeChan chan struct{}
}

// NewRealtimeWebSocket creates a new WebSocket manager for OpenAI Realtime API.
func NewRealtimeWebSocket(model, apiKey string) *RealtimeWebSocket {
	// Build WebSocket URL with model parameter
	wsURL := fmt.Sprintf("%s?model=%s", RealtimeAPIEndpoint, model)

	return &RealtimeWebSocket{
		url:       wsURL,
		apiKey:    apiKey,
		closeChan: make(chan struct{}),
	}
}

// Connect establishes a WebSocket connection to the OpenAI Realtime API.
func (ws *RealtimeWebSocket) Connect(ctx context.Context) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if ws.closed {
		return fmt.Errorf("websocket is closed")
	}

	// Set up headers for OpenAI Realtime API
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+ws.apiKey)
	headers.Set("OpenAI-Beta", RealtimeBetaHeader)

	// Create dialer with timeout
	dialer := websocket.Dialer{
		HandshakeTimeout: wsDialTimeout,
	}

	logger.Debug("OpenAI Realtime: connecting to WebSocket", "url", ws.url)

	conn, resp, err := dialer.DialContext(ctx, ws.url, headers)
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
			logger.Error("OpenAI Realtime: WebSocket dial failed",
				"error", err,
				"status", resp.StatusCode)
		}
		return fmt.Errorf("failed to connect: %w", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}

	// Configure connection
	conn.SetReadLimit(wsMaxMessageSize)
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return fmt.Errorf("failed to set read deadline: %w", err)
	}

	ws.conn = conn
	logger.Info("OpenAI Realtime: WebSocket connected successfully")

	return nil
}

// ConnectWithRetry attempts to connect with exponential backoff.
func (ws *RealtimeWebSocket) ConnectWithRetry(ctx context.Context) error {
	var lastErr error
	backoff := wsRetryBackoffBase

	for attempt := 1; attempt <= wsMaxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := ws.Connect(ctx)
		if err == nil {
			return nil
		}

		lastErr = err
		logger.Warn("OpenAI Realtime: connection attempt failed",
			"attempt", attempt,
			"maxAttempts", wsMaxRetries,
			"error", err)

		if attempt < wsMaxRetries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > wsRetryBackoffMax {
				backoff = wsRetryBackoffMax
			}
		}
	}

	return fmt.Errorf("failed to connect after %d attempts: %w", wsMaxRetries, lastErr)
}

// Send sends a message to the WebSocket.
func (ws *RealtimeWebSocket) Send(msg interface{}) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if ws.closed || ws.conn == nil {
		return fmt.Errorf("websocket is not connected")
	}

	if err := ws.conn.SetWriteDeadline(time.Now().Add(wsWriteWait)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	if err := ws.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	return nil
}

// Receive reads a message from the WebSocket with context support.
func (ws *RealtimeWebSocket) Receive(ctx context.Context) ([]byte, error) {
	ws.mu.Lock()
	if ws.closed || ws.conn == nil {
		ws.mu.Unlock()
		return nil, fmt.Errorf("websocket is not connected")
	}
	conn := ws.conn
	ws.mu.Unlock()

	// Create channel for result
	type readResult struct {
		data []byte
		err  error
	}
	resultCh := make(chan readResult, 1)

	go func() {
		_, data, err := conn.ReadMessage()
		resultCh <- readResult{data: data, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultCh:
		return result.data, result.err
	}
}

// ReceiveLoop continuously reads messages and sends them to the provided channel.
// It returns when the connection is closed or an error occurs.
func (ws *RealtimeWebSocket) ReceiveLoop(ctx context.Context, msgCh chan<- []byte) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ws.closeChan:
			return nil
		default:
		}

		data, err := ws.Receive(ctx)
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
		case <-ws.closeChan:
			return nil
		}
	}
}

// StartHeartbeat starts a goroutine that sends ping messages periodically.
func (ws *RealtimeWebSocket) StartHeartbeat(ctx context.Context, interval time.Duration) {
	go ws.heartbeatLoop(ctx, interval)
}

// heartbeatLoop runs the heartbeat ping loop.
func (ws *RealtimeWebSocket) heartbeatLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ws.closeChan:
			return
		case <-ticker.C:
			if !ws.sendPing() {
				return
			}
		}
	}
}

// sendPing sends a ping message. Returns false if the connection should be closed.
func (ws *RealtimeWebSocket) sendPing() bool {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if ws.closed || ws.conn == nil {
		return false
	}

	if err := ws.conn.SetWriteDeadline(time.Now().Add(wsWriteWait)); err != nil {
		logger.Warn("OpenAI Realtime: failed to set write deadline for ping", "error", err)
		return true // non-fatal error, keep heartbeat running
	}

	if err := ws.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
		logger.Warn("OpenAI Realtime: ping failed", "error", err)
		return false
	}

	return true
}

// Close closes the WebSocket connection gracefully.
func (ws *RealtimeWebSocket) Close() error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if ws.closed {
		return nil
	}

	ws.closed = true
	close(ws.closeChan)

	if ws.conn == nil {
		return nil
	}

	// Send close message
	_ = ws.conn.SetWriteDeadline(time.Now().Add(wsCloseGracePeriod))
	closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
	_ = ws.conn.WriteMessage(websocket.CloseMessage, closeMsg)

	return ws.conn.Close()
}

// IsClosed returns whether the WebSocket is closed.
func (ws *RealtimeWebSocket) IsClosed() bool {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return ws.closed
}
