package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Common error messages
const (
	ErrNotConnected  = "not connected"
	ErrManagerClosed = "manager is closed"
)

// WebSocket limits
const (
	// MaxMessageSize is the maximum allowed WebSocket message size (16MB).
	// This protects against memory exhaustion from malformed or malicious responses.
	// The limit is generous to accommodate base64-encoded audio/video content.
	MaxMessageSize = 16 * 1024 * 1024
)

// WebSocketManager manages a WebSocket connection with reconnection logic.
type WebSocketManager struct {
	url    string
	apiKey string

	conn      *websocket.Conn
	mu        sync.RWMutex
	writeMu   sync.Mutex // Separate mutex for serializing writes (gorilla/websocket requires this)
	connected bool
	closed    bool

	// Reconnection settings
	maxReconnectAttempts int
	baseDelay            time.Duration
	maxDelay             time.Duration

	// Channels for lifecycle management
	reconnectCh chan struct{}
	closeCh     chan struct{}
}

// NewWebSocketManager creates a new WebSocket manager
func NewWebSocketManager(url, apiKey string) *WebSocketManager {
	return &WebSocketManager{
		url:                  url,
		apiKey:               apiKey,
		maxReconnectAttempts: 5,
		baseDelay:            1 * time.Second,
		maxDelay:             60 * time.Second,
		reconnectCh:          make(chan struct{}, 1),
		closeCh:              make(chan struct{}),
	}
}

// Connect establishes a WebSocket connection to the Gemini Live API
func (wm *WebSocketManager) Connect(ctx context.Context) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	if wm.closed {
		return errors.New(ErrManagerClosed)
	}

	if wm.connected {
		return nil // Already connected
	}

	// Create dialer with context
	dialer := websocket.Dialer{
		HandshakeTimeout: 45 * time.Second,
	}

	// Create headers with API key authentication
	// Per Gemini Live API docs: use x-goog-api-key header
	headers := http.Header{}
	headers.Set("x-goog-api-key", wm.apiKey)

	// Connect
	conn, resp, err := dialer.DialContext(ctx, wm.url, headers)
	if err != nil {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
			return fmt.Errorf("websocket dial failed (status %d): %w", resp.StatusCode, err)
		}
		return fmt.Errorf("websocket dial failed: %w", err)
	}
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}

	wm.conn = conn
	wm.connected = true

	// Set read limit to prevent memory exhaustion from oversized messages
	conn.SetReadLimit(MaxMessageSize)

	return nil
}

// IsConnected returns true if the WebSocket is connected
func (wm *WebSocketManager) IsConnected() bool {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	return wm.connected && !wm.closed
}

// Send sends a message through the WebSocket
func (wm *WebSocketManager) Send(msg interface{}) error {
	wm.mu.RLock()
	closed := wm.closed
	conn := wm.conn
	connected := wm.connected
	wm.mu.RUnlock()

	if closed {
		return errors.New(ErrManagerClosed)
	}

	if !connected || conn == nil {
		return errors.New(ErrNotConnected)
	}

	// Marshal to JSON
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Serialize writes - gorilla/websocket requires this for concurrent access
	wm.writeMu.Lock()
	err = conn.WriteMessage(websocket.TextMessage, data)
	wm.writeMu.Unlock()

	if err != nil {
		// Mark disconnected without nested locking
		wm.mu.Lock()
		wm.connected = false
		wm.mu.Unlock()
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

// Receive reads a message from the WebSocket
func (wm *WebSocketManager) Receive(ctx context.Context, v interface{}) error {
	wm.mu.RLock()
	conn := wm.conn
	connected := wm.connected
	wm.mu.RUnlock()

	if !connected || conn == nil {
		return errors.New(ErrNotConnected)
	}

	// Read message (don't hold lock during I/O)
	messageType, data, err := conn.ReadMessage()
	if err != nil {
		// Mark disconnected without nested locking
		wm.mu.Lock()
		wm.connected = false
		wm.mu.Unlock()

		// Check if this is a close error from the remote (Gemini)
		var closeErr *websocket.CloseError
		if errors.As(err, &closeErr) {
			return fmt.Errorf("REMOTE_CLOSED: code=%d reason=%q: %w", closeErr.Code, closeErr.Text, err)
		}
		return fmt.Errorf("failed to read message: %w", err)
	}

	// Accept both text and binary messages (Gemini Live API uses both)
	if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
		return fmt.Errorf("unexpected message type: %d", messageType)
	}

	// Unmarshal JSON
	err = json.Unmarshal(data, v)
	if err != nil {
		return fmt.Errorf("failed to unmarshal message: %w", err)
	}

	return nil
}

// SendPing sends a WebSocket ping to keep the connection alive
func (wm *WebSocketManager) SendPing() error {
	wm.mu.RLock()
	conn := wm.conn
	connected := wm.connected
	wm.mu.RUnlock()

	if !connected || conn == nil {
		return errors.New(ErrNotConnected)
	}

	// Serialize writes - gorilla/websocket requires this for concurrent access
	wm.writeMu.Lock()
	err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(5*time.Second))
	wm.writeMu.Unlock()

	if err != nil {
		// Mark disconnected without nested locking
		wm.mu.Lock()
		wm.connected = false
		wm.mu.Unlock()
		return fmt.Errorf("failed to send ping: %w", err)
	}

	return nil
}

// StartHeartbeat starts a goroutine that sends periodic pings
func (wm *WebSocketManager) StartHeartbeat(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-wm.closeCh:
				return
			case <-ticker.C:
				if wm.IsConnected() {
					_ = wm.SendPing() // Ignore errors, will be detected by read/write
				}
			}
		}
	}()
}

// ConnectWithRetry connects with exponential backoff retry logic
func (wm *WebSocketManager) ConnectWithRetry(ctx context.Context) error {
	for attempt := 0; attempt < wm.maxReconnectAttempts; attempt++ {
		err := wm.Connect(ctx)
		if err == nil {
			return nil
		}

		// Check if we should retry
		if !shouldRetry(err) {
			return fmt.Errorf("non-retryable error: %w", err)
		}

		// Calculate backoff delay with jitter
		delay := wm.calculateBackoff(attempt)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			// Continue to next attempt
		}
	}

	return fmt.Errorf("failed to connect after %d attempts", wm.maxReconnectAttempts)
}

// Close gracefully closes the WebSocket connection
func (wm *WebSocketManager) Close() error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	if wm.closed {
		return nil // Already closed
	}

	wm.closed = true
	close(wm.closeCh)

	if wm.conn != nil {
		// Serialize writes - gorilla/websocket requires this for concurrent access
		wm.writeMu.Lock()
		closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
		_ = wm.conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(time.Second))
		wm.writeMu.Unlock()

		// Close connection
		err := wm.conn.Close()
		wm.conn = nil
		wm.connected = false
		return err
	}

	return nil
}

// calculateBackoff calculates the backoff delay with exponential backoff and jitter
func (wm *WebSocketManager) calculateBackoff(attempt int) time.Duration {
	// Exponential backoff: baseDelay * 2^attempt
	delay := float64(wm.baseDelay) * math.Pow(2, float64(attempt))

	// Cap at maxDelay BEFORE adding jitter
	if delay > float64(wm.maxDelay) {
		delay = float64(wm.maxDelay)
	}

	// Add jitter: ±25%
	// Note: Using math/rand (not crypto/rand) is intentional here. This is for
	// retry backoff jitter to prevent thundering herd, NOT for security purposes.
	// Predictable randomness is acceptable for connection retry timing.
	jitter := delay * 0.25 * (rand.Float64()*2 - 1)
	delay += jitter

	// Ensure positive
	if delay < 0 {
		delay = float64(wm.baseDelay)
	}

	// Final cap at maxDelay (in case jitter pushed it over)
	if delay > float64(wm.maxDelay) {
		delay = float64(wm.maxDelay)
	}

	return time.Duration(delay)
}

// shouldRetry determines if an error is retryable
func shouldRetry(err error) bool {
	if err == nil {
		return false
	}

	// Check error message for non-retryable conditions
	errStr := err.Error()

	// Don't retry authentication failures
	if contains(errStr, "401") || contains(errStr, "authentication") {
		return false
	}

	// Don't retry bad requests
	if contains(errStr, "400") || contains(errStr, "invalid") {
		return false
	}

	// Don't retry policy violations
	if contains(errStr, "policy") || contains(errStr, "1008") {
		return false
	}

	// Retry everything else (network errors, 503, etc.)
	return true
}

// contains checks if a string contains a substring (case-insensitive helper)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
