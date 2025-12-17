// Package gemini provides Gemini Live API streaming support.
//
// IMPORTANT: Response Modality Limitation
//
// The Gemini Live API does NOT support requesting both TEXT and AUDIO response
// modalities simultaneously. Attempting to set ResponseModalities to ["TEXT", "AUDIO"]
// will result in a WebSocket error:
//
//	websocket: close 1007 (invalid payload data): Request contains an invalid argument.
//
// Valid configurations:
//   - ["TEXT"]  - Text responses only (default)
//   - ["AUDIO"] - Audio responses only
//
// If you need both text and audio, you must choose one primary modality.
// For audio responses with transcription, the API may provide output transcription
// separately via the OutputTranscription field.
package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Common error messages
const (
	ErrSessionClosed = "session is closed"
)

// sliceContains checks if a string slice contains a value
func sliceContains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

// truncateInlineData recursively truncates large data fields for logging
func truncateInlineData(v interface{}) {
	switch val := v.(type) {
	case map[string]interface{}:
		// Check for inlineData.data
		if data, ok := val["data"].(string); ok && len(data) > 100 {
			val["data"] = fmt.Sprintf("[%d bytes base64]", len(data))
		}
		// Recurse into all values
		for _, child := range val {
			truncateInlineData(child)
		}
	case []interface{}:
		for _, item := range val {
			truncateInlineData(item)
		}
	}
}

// StreamSession implements StreamInputSession for Gemini Live API
type StreamSession struct {
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
	ResponseModalities []string // "TEXT" or "AUDIO" - NOT both! See package doc for details.
	SystemInstruction  string   // System prompt/instruction for the model
}

// NewStreamSession creates a new streaming session
func NewStreamSession(ctx context.Context, wsURL, apiKey string, config StreamSessionConfig) (*StreamSession, error) {
	// Default to TEXT if no modalities specified
	modalities := config.ResponseModalities
	if len(modalities) == 0 {
		modalities = []string{"TEXT"}
	}

	// IMPORTANT: Gemini Live API does NOT support TEXT+AUDIO simultaneously
	// Reject this configuration early with a clear error message
	if len(modalities) > 1 && sliceContains(modalities, "TEXT") && sliceContains(modalities, "AUDIO") {
		return nil, fmt.Errorf(
			"invalid response modalities: Gemini Live API does not support TEXT and AUDIO " +
				"simultaneously. Use either [\"TEXT\"] or [\"AUDIO\"], not both")
	}

	sessionCtx, cancel := context.WithCancel(ctx)

	ws := NewWebSocketManager(wsURL, apiKey)
	if err := ws.ConnectWithRetry(sessionCtx); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	session := &StreamSession{
		ws:         ws,
		ctx:        sessionCtx,
		cancel:     cancel,
		responseCh: make(chan providers.StreamChunk, 10),
		errCh:      make(chan error, 1),
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
	generationConfig := map[string]interface{}{
		"responseModalities": modalities,
	}

	// Add speech config for audio responses
	if sliceContains(modalities, "AUDIO") {
		generationConfig["speechConfig"] = map[string]interface{}{
			"voiceConfig": map[string]interface{}{
				"prebuiltVoiceConfig": map[string]interface{}{
					"voiceName": "Puck", // Default voice
				},
			},
		}
	}

	setupContent := map[string]interface{}{
		"model":            modelPath,
		"generationConfig": generationConfig,
		// Note: inputAudioTranscription and outputAudioTranscription removed
		// as they may cause "invalid argument" errors with some configurations
	}

	// Add system instruction if provided
	if config.SystemInstruction != "" {
		setupContent["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]interface{}{
				{"text": config.SystemInstruction},
			},
		}
	}

	setupMsg := map[string]interface{}{
		"setup": setupContent,
	}

	// Log setup message at debug level
	if logger.DefaultLogger != nil {
		if setupJSON, err := json.MarshalIndent(setupMsg, "", "  "); err == nil {
			logger.DefaultLogger.Debug("Gemini setup message", "setup", string(setupJSON))
		}
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
func (s *StreamSession) SendChunk(ctx context.Context, chunk *types.MediaChunk) error {
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
func (s *StreamSession) SendText(ctx context.Context, text string) error {
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

// SendSystemContext sends a text message as context without completing the turn.
// Use this for system prompts that should provide context but not trigger a response.
// The audio/text that follows will be processed with this context in mind.
func (s *StreamSession) SendSystemContext(ctx context.Context, text string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(ErrSessionClosed)
	}
	s.mu.Unlock()

	// Build text message WITHOUT turn_complete
	// This provides context without triggering an immediate response
	msg := buildTextMessage(text, false)

	return s.ws.Send(msg)
}

// CompleteTurn signals that the current turn is complete
func (s *StreamSession) CompleteTurn(ctx context.Context) error {
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

// EndInput implements the EndInputter interface expected by DuplexProviderStage.
// It signals that the user's input turn is complete and the model should respond.
//
// For Gemini Live API with realtime_input (audio streaming), the end of turn is
// normally derived from VAD detecting silence. Since we're sending pre-recorded
// audio files, we send a brief text prompt to trigger the model's response.
func (s *StreamSession) EndInput() {
	// Send a minimal prompt to trigger response after audio input
	// This works because SendText sets turn_complete=true
	if err := s.SendText(s.ctx, "Please respond to what you just heard."); err != nil {
		logger.Error("EndInput: failed to send trigger prompt", "error", err)
	}
}

// Response returns the channel for receiving responses
func (s *StreamSession) Response() <-chan providers.StreamChunk {
	return s.responseCh
}

// Done returns a channel that's closed when the session ends
func (s *StreamSession) Done() <-chan struct{} {
	return s.ctx.Done()
}

// Close closes the session
func (s *StreamSession) Close() error {
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
func (s *StreamSession) Error() error {
	select {
	case err := <-s.errCh:
		return err
	default:
		return nil
	}
}

// receiveLoop continuously receives messages from the WebSocket
func (s *StreamSession) receiveLoop() {
	defer close(s.responseCh)

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			// First receive as raw JSON to see what we're getting
			var rawMsg json.RawMessage
			if err := s.ws.Receive(s.ctx, &rawMsg); err != nil {
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

			// Parse into ServerMessage
			var serverMsg ServerMessage
			if err := json.Unmarshal(rawMsg, &serverMsg); err != nil {
				select {
				case s.errCh <- fmt.Errorf("failed to parse server message: %w", err):
				default:
				}
				continue
			}

			// Log raw message for debugging (truncate audio data but show structure)
			if logger.DefaultLogger != nil {
				// Parse into generic map to see full structure
				var logMsg map[string]interface{}
				json.Unmarshal(rawMsg, &logMsg)

				// Build summary of what's in the message
				var keys []string
				for k := range logMsg {
					keys = append(keys, k)
				}

				// Truncate inlineData.data but keep everything else
				truncateInlineData(logMsg)

				logBytes, _ := json.MarshalIndent(logMsg, "", "  ")
				logger.DefaultLogger.Debug("Gemini message", "keys", keys, "content", string(logBytes))
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
func (s *StreamSession) processServerMessage(msg *ServerMessage) error {
	// Check for setup_complete
	if msg.SetupComplete != nil {
		return nil // Setup acknowledged
	}

	// Handle tool calls
	if msg.ToolCall != nil {
		// TODO: Implement tool call handling
		return nil
	}

	// Store usage metadata for cost calculation
	var costInfo *types.CostInfo
	if msg.UsageMetadata != nil {
		costInfo = &types.CostInfo{
			InputTokens:  msg.UsageMetadata.PromptTokenCount,
			OutputTokens: msg.UsageMetadata.ResponseTokenCount,
			// Cost calculation would require pricing info from provider
		}
	}

	if msg.ServerContent == nil {
		// Even without server content, we might have usage metadata
		if costInfo != nil {
			// Send a chunk with just cost info
			response := providers.StreamChunk{
				CostInfo: costInfo,
			}
			select {
			case s.responseCh <- response:
			case <-s.ctx.Done():
				return s.ctx.Err()
			}
		}
		return nil
	}

	content := msg.ServerContent

	// Handle interruption - user started speaking while model was responding
	if content.Interrupted {
		response := providers.StreamChunk{
			Interrupted: true,
		}
		select {
		case s.responseCh <- response:
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
		return nil
	}

	// Handle input transcription (what user said)
	if content.InputTranscription != nil && content.InputTranscription.Text != "" {
		response := providers.StreamChunk{
			Metadata: map[string]interface{}{
				"type":          "input_transcription",
				"transcription": content.InputTranscription.Text,
				"turn_complete": content.TurnComplete,
			},
		}
		select {
		case s.responseCh <- response:
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
	}

	// Handle output transcription (what model said - text version of audio)
	if content.OutputTranscription != nil && content.OutputTranscription.Text != "" {
		response := providers.StreamChunk{
			Delta: content.OutputTranscription.Text,
			Metadata: map[string]interface{}{
				"type":          "output_transcription",
				"turn_complete": content.TurnComplete,
			},
		}
		select {
		case s.responseCh <- response:
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
	}

	// Handle turn complete without model turn (e.g., after interruption)
	if content.TurnComplete && content.ModelTurn == nil {
		finishReason := "complete"
		response := providers.StreamChunk{
			FinishReason: &finishReason,
			CostInfo:     costInfo,
		}
		select {
		case s.responseCh <- response:
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
		return nil
	}

	// Process model turn
	if content.ModelTurn != nil {
		return s.processModelTurn(content.ModelTurn, content.TurnComplete, costInfo)
	}

	return nil
}

// processModelTurn processes a model turn from the server
func (s *StreamSession) processModelTurn(turn *ModelTurn, turnComplete bool, costInfo *types.CostInfo) error {
	response := providers.StreamChunk{
		Content: "",
	}

	// Extract text and audio from parts
	for _, part := range turn.Parts {
		if part.Text != "" {
			response.Content += part.Text
			response.Delta = part.Text
		}

		// Handle audio/media data
		if part.InlineData != nil {
			// The data is base64 encoded PCM audio from Gemini
			// Use MediaDelta for first-class media content
			response.MediaDelta = &types.MediaContent{
				Data:     &part.InlineData.Data, // Already base64 encoded
				MIMEType: part.InlineData.MimeType,
			}

			// Add audio-specific metadata if this is audio
			if strings.HasPrefix(part.InlineData.MimeType, "audio/") {
				// Gemini uses 16kHz mono audio
				channels := 1
				sampleRate := 16000
				response.MediaDelta.Channels = &channels
				response.MediaDelta.BitRate = &sampleRate // Store sample rate in BitRate field
			}
		}
	}

	// Mark turn completion and include cost info
	if turnComplete {
		finishReason := "complete"
		response.FinishReason = &finishReason
		response.CostInfo = costInfo
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
func buildClientMessage(chunk types.MediaChunk, _ bool) map[string]interface{} {
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

// ServerMessage represents a message from the Gemini server (BidiGenerateContentServerMessage)
type ServerMessage struct {
	SetupComplete *SetupComplete `json:"setupComplete,omitempty"`
	ServerContent *ServerContent `json:"serverContent,omitempty"`
	ToolCall      *ToolCallMsg   `json:"toolCall,omitempty"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
}

// UsageMetadata contains token usage information
type UsageMetadata struct {
	PromptTokenCount   int `json:"promptTokenCount,omitempty"`
	ResponseTokenCount int `json:"responseTokenCount,omitempty"`
	TotalTokenCount    int `json:"totalTokenCount,omitempty"`
}

// SetupComplete indicates setup is complete (empty object per docs)
type SetupComplete struct{}

// ToolCallMsg represents a tool call from the model
type ToolCallMsg struct {
	FunctionCalls []FunctionCall `json:"functionCalls,omitempty"`
}

// FunctionCall represents a function call
type FunctionCall struct {
	Name string                 `json:"name,omitempty"`
	ID   string                 `json:"id,omitempty"`
	Args map[string]interface{} `json:"args,omitempty"`
}

// ServerContent represents the server content (BidiGenerateContentServerContent)
type ServerContent struct {
	ModelTurn           *ModelTurn     `json:"modelTurn,omitempty"`
	TurnComplete        bool           `json:"turnComplete,omitempty"`
	GenerationComplete  bool           `json:"generationComplete,omitempty"`
	Interrupted         bool           `json:"interrupted,omitempty"`
	InputTranscription  *Transcription `json:"inputTranscription,omitempty"`  // User speech transcription
	OutputTranscription *Transcription `json:"outputTranscription,omitempty"` // Model speech transcription
}

// Transcription represents audio transcription (BidiGenerateContentTranscription)
type Transcription struct {
	Text string `json:"text,omitempty"`
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

// UnmarshalJSON unmarshals ServerMessage from JSON with custom handling.
func (s *ServerMessage) UnmarshalJSON(data []byte) error {
	type Alias ServerMessage
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	}
	return json.Unmarshal(data, aux)
}
