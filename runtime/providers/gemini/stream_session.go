package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Common error messages
const (
	ErrSessionClosed = "session is closed"
)

// GeminiStreamSession implements StreamInputSession for Gemini Live API
type GeminiStreamSession struct {
	ws          *WebSocketManager
	ctx         context.Context
	cancel      context.CancelFunc
	responseCh  chan providers.StreamChunk
	errCh       chan error
	mu          sync.Mutex
	closed      bool
	sequenceNum int64
}

// NewGeminiStreamSession creates a new streaming session
func NewGeminiStreamSession(ctx context.Context, wsURL, apiKey string) (*GeminiStreamSession, error) {
	sessionCtx, cancel := context.WithCancel(ctx)

	ws := NewWebSocketManager(wsURL, apiKey)
	if err := ws.ConnectWithRetry(sessionCtx); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	session := &GeminiStreamSession{
		ws:         ws,
		ctx:        sessionCtx,
		cancel:     cancel,
		responseCh: make(chan providers.StreamChunk, 10),
		errCh:      make(chan error, 1),
	}

	// Start heartbeat
	ws.StartHeartbeat(sessionCtx, 30*time.Second)

	// Start receiver goroutine
	go session.receiveLoop()

	return session, nil
}

// SendChunk sends a media chunk to the server
func (s *GeminiStreamSession) SendChunk(ctx context.Context, chunk *types.MediaChunk) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(ErrSessionClosed)
	}
	s.mu.Unlock()

	// Build client message
	msg := buildClientMessage(*chunk, false)

	return s.ws.Send(msg)
}

// SendText sends a text message to the server
func (s *GeminiStreamSession) SendText(ctx context.Context, text string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(ErrSessionClosed)
	}
	s.mu.Unlock()

	// Build text message
	msg := buildTextMessage(text, false)

	return s.ws.Send(msg)
}

// CompleteTurn signals that the current turn is complete
func (s *GeminiStreamSession) CompleteTurn(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(ErrSessionClosed)
	}
	s.mu.Unlock()

	// Send turn_complete message
	msg := map[string]interface{}{
		"client_content": map[string]interface{}{
			"turn_complete": true,
		},
	}

	return s.ws.Send(msg)
}

// Response returns the channel for receiving responses
func (s *GeminiStreamSession) Response() <-chan providers.StreamChunk {
	return s.responseCh
}

// Done returns a channel that's closed when the session ends
func (s *GeminiStreamSession) Done() <-chan struct{} {
	return s.ctx.Done()
}

// Close closes the session
func (s *GeminiStreamSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	// Cancel context
	s.cancel()

	// Close WebSocket
	return s.ws.Close()
}

// Err returns the error that caused the session to close
func (s *GeminiStreamSession) Error() error {
	select {
	case err := <-s.errCh:
		return err
	default:
		return nil
	}
}

// receiveLoop continuously receives messages from the WebSocket
func (s *GeminiStreamSession) receiveLoop() {
	defer close(s.responseCh)

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			var serverMsg ServerMessage
			if err := s.ws.Receive(&serverMsg); err != nil {
				// Check if session was closed
				s.mu.Lock()
				closed := s.closed
				s.mu.Unlock()

				if !closed {
					// Send error to error channel
					select {
					case s.errCh <- err:
					default:
					}
				}
				return
			}

			// Process server message
			if err := s.processServerMessage(&serverMsg); err != nil {
				select {
				case s.errCh <- err:
				default:
				}
				return
			}
		}
	}
}

// processServerMessage processes a message from the server
func (s *GeminiStreamSession) processServerMessage(msg *ServerMessage) error {
	if msg.ServerContent == nil {
		return nil
	}

	content := msg.ServerContent

	// Check for setup_complete
	if content.SetupComplete {
		return nil // Setup acknowledged
	}

	// Process model turn
	if content.ModelTurn != nil {
		return s.processModelTurn(content.ModelTurn, content.TurnComplete)
	}

	return nil
}

// processModelTurn processes a model turn from the server
func (s *GeminiStreamSession) processModelTurn(turn *ModelTurn, turnComplete bool) error {
	response := providers.StreamChunk{
		Content: "",
	}

	// Extract text from parts
	for _, part := range turn.Parts {
		if part.Text != "" {
			response.Content += part.Text
		}
		// Media data would be handled here if needed
	}

	// Send response to channel
	select {
	case s.responseCh <- response:
		s.sequenceNum++
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// buildClientMessage builds a client message with media chunk
func buildClientMessage(chunk types.MediaChunk, turnComplete bool) map[string]interface{} {
	// Encode binary data as base64 would happen here in real implementation
	// For now, we'll use the data directly (assuming it's already encoded)

	part := map[string]interface{}{
		"inline_data": map[string]interface{}{
			"mime_type": "audio/pcm;rate=16000", // Default to PCM audio
			"data":      string(chunk.Data),     // Should be base64 encoded
		},
	}

	return map[string]interface{}{
		"client_content": map[string]interface{}{
			"turns": []map[string]interface{}{
				{
					"role":  "user",
					"parts": []interface{}{part},
				},
			},
			"turn_complete": turnComplete,
		},
	}
}

// buildTextMessage builds a client message with text
func buildTextMessage(text string, turnComplete bool) map[string]interface{} {
	part := map[string]interface{}{
		"text": text,
	}

	return map[string]interface{}{
		"client_content": map[string]interface{}{
			"turns": []map[string]interface{}{
				{
					"role":  "user",
					"parts": []interface{}{part},
				},
			},
			"turn_complete": turnComplete,
		},
	}
}

// ServerMessage represents a message from the Gemini server
type ServerMessage struct {
	ServerContent *ServerContent `json:"server_content,omitempty"`
}

// ServerContent represents the server content
type ServerContent struct {
	ModelTurn     *ModelTurn `json:"model_turn,omitempty"`
	TurnComplete  bool       `json:"turn_complete,omitempty"`
	SetupComplete bool       `json:"setup_complete,omitempty"`
}

// ModelTurn represents a model response turn
type ModelTurn struct {
	Parts []Part `json:"parts,omitempty"`
}

// Part represents a content part (text or inline data)
type Part struct {
	Text       string      `json:"text,omitempty"`
	InlineData *InlineData `json:"inline_data,omitempty"`
}

// InlineData represents inline media data
type InlineData struct {
	MimeType string `json:"mime_type,omitempty"`
	Data     string `json:"data,omitempty"` // Base64 encoded
}

// Marshal methods for easier JSON serialization
func (s *ServerMessage) UnmarshalJSON(data []byte) error {
	type Alias ServerMessage
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	}
	return json.Unmarshal(data, aux)
}
