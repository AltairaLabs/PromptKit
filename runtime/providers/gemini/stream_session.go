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

// StreamSessionConfig configures a streaming session
type StreamSessionConfig struct {
	Model              string   // Model name (will be prefixed with "models/" automatically)
	ResponseModalities []string // "TEXT" and/or "AUDIO"
}

// NewGeminiStreamSession creates a new streaming session
func NewGeminiStreamSession(ctx context.Context, wsURL, apiKey string, config StreamSessionConfig) (*GeminiStreamSession, error) {
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

	// Default to TEXT if no modalities specified
	modalities := config.ResponseModalities
	if len(modalities) == 0 {
		modalities = []string{"TEXT"}
	}

	// Ensure model is in correct format: models/{model}
	modelPath := config.Model
	if modelPath == "" {
		modelPath = "gemini-2.0-flash-exp" // Default model
	}
	if len(modelPath) < 7 || modelPath[:7] != "models/" {
		modelPath = "models/" + modelPath
	}

	// Send initial setup message (required by Gemini Live API)
	// Per docs: first message must be BidiGenerateContentSetup
	setupMsg := map[string]interface{}{
		"setup": map[string]interface{}{
			"model": modelPath,
			"generationConfig": map[string]interface{}{
				"responseModalities": modalities,
			},
		},
	}
	if err := ws.Send(setupMsg); err != nil {
		ws.Close()
		cancel()
		return nil, fmt.Errorf("failed to send setup message: %w", err)
	}

	// Wait for setup_complete response
	setupCtx, setupCancel := context.WithTimeout(sessionCtx, 10*time.Second)
	defer setupCancel()

	var setupResponse ServerMessage
	if err := ws.Receive(setupCtx, &setupResponse); err != nil {
		ws.Close()
		cancel()
		return nil, fmt.Errorf("failed to receive setup response: %w", err)
	}

	if setupResponse.SetupComplete == nil {
		ws.Close()
		cancel()
		return nil, fmt.Errorf("invalid setup response: setup_complete not received")
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

// SendText sends a text message to the server and marks the turn as complete
func (s *GeminiStreamSession) SendText(ctx context.Context, text string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(ErrSessionClosed)
	}
	s.mu.Unlock()

	// Build text message with turn_complete set to true
	// This signals to Gemini that we're done sending input and expecting a response
	msg := buildTextMessage(text, true)

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
			if err := s.ws.Receive(s.ctx, &serverMsg); err != nil {
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
	// Check for setup_complete
	if msg.SetupComplete != nil {
		return nil // Setup acknowledged
	}

	if msg.ServerContent == nil {
		return nil
	}

	content := msg.ServerContent

	// Process model turn
	if content.ModelTurn != nil {
		return s.processModelTurn(content.ModelTurn, content.TurnComplete)
	}

	return nil
}

// processModelTurn processes a model turn from the server
func (s *GeminiStreamSession) processModelTurn(turn *ModelTurn, turnComplete bool) error {
	response := providers.StreamChunk{
		Content:  "",
		Metadata: make(map[string]interface{}),
	}

	// Extract text and audio from parts
	for _, part := range turn.Parts {
		if part.Text != "" {
			response.Content += part.Text
			response.Delta = part.Text
		}

		// Handle audio/media data
		if part.InlineData != nil {
			// Store audio data in metadata
			// The data is base64 encoded PCM audio from Gemini
			response.Metadata["audio_mime_type"] = part.InlineData.MimeType
			response.Metadata["audio_data"] = part.InlineData.Data // Base64 encoded
			response.Metadata["has_audio"] = true
		}
	}

	// Mark turn completion
	if turnComplete {
		finishReason := "complete"
		response.FinishReason = &finishReason
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

// buildClientMessage builds a realtime input message with media chunk
func buildClientMessage(chunk types.MediaChunk, turnComplete bool) map[string]interface{} {
	// Encode binary PCM data as base64 for transmission
	encoder := NewAudioEncoder()
	base64Data, err := encoder.EncodePCM(chunk.Data)
	if err != nil {
		// If encoding fails, use empty string (should not happen with valid PCM data)
		base64Data = ""
	}

	return map[string]interface{}{
		"realtime_input": map[string]interface{}{
			"media_chunks": []map[string]interface{}{
				{
					"mime_type": "audio/pcm",
					"data":      base64Data,
				},
			},
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
	SetupComplete *SetupComplete `json:"setupComplete,omitempty"`
	ServerContent *ServerContent `json:"serverContent,omitempty"`
}

// SetupComplete indicates setup is complete (empty object per docs)
type SetupComplete struct{}

// ServerContent represents the server content
type ServerContent struct {
	ModelTurn    *ModelTurn `json:"modelTurn,omitempty"`
	TurnComplete bool       `json:"turnComplete,omitempty"`
	Interrupted  bool       `json:"interrupted,omitempty"`
}

// ModelTurn represents a model response turn
type ModelTurn struct {
	Parts []Part `json:"parts,omitempty"`
}

// Part represents a content part (text or inline data)
type Part struct {
	Text       string      `json:"text,omitempty"`
	InlineData *InlineData `json:"inlineData,omitempty"` // camelCase!
}

// InlineData represents inline media data
type InlineData struct {
	MimeType string `json:"mimeType,omitempty"` // camelCase!
	Data     string `json:"data,omitempty"`     // Base64 encoded
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
