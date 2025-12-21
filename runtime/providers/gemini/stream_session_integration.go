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

// Configuration constants
const (
	responseChannelSize = 10
	silenceFrameSize    = 16000
	silenceFrameCount   = 8
	silenceFrameDelayMS = 50
	tokensPerThousand   = 1000.0

	// Reconnection constants
	reconnectSetupTimeoutSec = 10 // Timeout for setup_complete after reconnect
	reconnectHeartbeatSec    = 30 // Heartbeat interval after successful reconnect
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
// with automatic reconnection on unexpected connection drops.
type StreamSession struct {
	ws              *WebSocketManager
	ctx             context.Context
	cancel          context.CancelFunc
	responseCh      chan providers.StreamChunk
	errCh           chan error
	mu              sync.Mutex
	closed          bool
	sequenceNum     int64
	inputCostPer1K  float64 // Cost per 1K input tokens
	outputCostPer1K float64 // Cost per 1K output tokens

	// Configuration stored for reconnection
	wsURL    string
	apiKey   string
	config   StreamSessionConfig
	setupMsg map[string]interface{} // Cached setup message for reconnection

	// Reconnection settings
	autoReconnect     bool // If true, attempt reconnection on unexpected drop
	maxReconnectTries int  // Maximum reconnection attempts (default: 3)
	reconnecting      bool // True while reconnection is in progress

	// Manual VAD control state
	activityStartSent bool // True after activityStart has been sent for current turn
}

// VADConfig configures Voice Activity Detection settings for Gemini Live API.
// These settings control when Gemini detects the end of speech and starts responding.
type VADConfig struct {
	// Disabled turns off automatic VAD (manual turn control only)
	Disabled bool
	// StartOfSpeechSensitivity controls how sensitive the VAD is to detecting speech start.
	// Valid values: "UNSPECIFIED", "LOW", "MEDIUM", "HIGH"
	StartOfSpeechSensitivity string
	// EndOfSpeechSensitivity controls how sensitive the VAD is to detecting silence.
	// Valid values: "UNSPECIFIED", "LOW", "MEDIUM", "HIGH"
	// Lower sensitivity = longer silence needed to trigger end of speech
	EndOfSpeechSensitivity string
	// PrefixPaddingMs is extra padding in milliseconds before speech detection
	PrefixPaddingMs int
	// SilenceThresholdMs is the duration of silence (in ms) to trigger end of speech.
	// This maps to Gemini's "suffixPaddingMs" parameter.
	// Default is typically ~500ms. Increase for TTS audio with natural pauses.
	SilenceThresholdMs int
}

// StreamSessionConfig configures a streaming session
type StreamSessionConfig struct {
	Model              string   // Model name (will be prefixed with "models/" automatically)
	ResponseModalities []string // "TEXT" or "AUDIO" - NOT both! See package doc for details.
	SystemInstruction  string   // System prompt/instruction for the model
	InputCostPer1K     float64  // Cost per 1K input tokens (for USD calculation)
	OutputCostPer1K    float64  // Cost per 1K output tokens (for USD calculation)

	// VAD configures Voice Activity Detection settings.
	// If nil, Gemini uses its default VAD settings.
	VAD *VADConfig

	// Tools defines the function declarations available to the model.
	// When tools are configured, the model will return structured tool calls
	// instead of speaking them as text. Tool definitions should match the
	// OpenAPI schema subset supported by Gemini.
	Tools []ToolDefinition

	// AutoReconnect enables automatic reconnection on unexpected connection drops.
	// When enabled, the session will attempt to reconnect and continue receiving
	// responses. Note: conversation context may be lost on reconnection.
	AutoReconnect     bool
	MaxReconnectTries int // Maximum reconnection attempts (default: 3)
}

// ToolDefinition represents a function/tool that the model can call.
// This follows the Gemini function calling schema.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"` // JSON Schema for parameters
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

	// Set default max reconnect tries
	maxReconnectTries := config.MaxReconnectTries
	if maxReconnectTries <= 0 {
		maxReconnectTries = 3
	}

	session := &StreamSession{
		ws:                ws,
		ctx:               sessionCtx,
		cancel:            cancel,
		responseCh:        make(chan providers.StreamChunk, responseChannelSize),
		errCh:             make(chan error, 1),
		inputCostPer1K:    config.InputCostPer1K,
		outputCostPer1K:   config.OutputCostPer1K,
		wsURL:             wsURL,
		apiKey:            apiKey,
		config:            config,
		autoReconnect:     config.AutoReconnect,
		maxReconnectTries: maxReconnectTries,
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
	}

	// Enable transcription for AUDIO mode to get text alongside audio
	// This allows capturing both the audio and its text transcription
	if sliceContains(modalities, "AUDIO") {
		// Output transcription: what the model says (audio response -> text)
		setupContent["outputAudioTranscription"] = map[string]interface{}{}
		// Input transcription: what the user says (audio input -> text)
		setupContent["inputAudioTranscription"] = map[string]interface{}{}
	}

	// Add VAD (Voice Activity Detection) configuration if provided
	// Gemini Live API uses camelCase for JSON field names and specific enum values
	// See: https://ai.google.dev/api/live
	if config.VAD != nil {
		vadConfig := map[string]interface{}{}

		if config.VAD.Disabled {
			vadConfig["disabled"] = true
		} else {
			// Use Gemini's enum values: START_SENSITIVITY_LOW/HIGH, END_SENSITIVITY_LOW/HIGH
			if config.VAD.StartOfSpeechSensitivity != "" {
				vadConfig["startOfSpeechSensitivity"] = config.VAD.StartOfSpeechSensitivity
			}
			if config.VAD.EndOfSpeechSensitivity != "" {
				vadConfig["endOfSpeechSensitivity"] = config.VAD.EndOfSpeechSensitivity
			}
			if config.VAD.PrefixPaddingMs > 0 {
				vadConfig["prefixPaddingMs"] = config.VAD.PrefixPaddingMs
			}
			if config.VAD.SilenceThresholdMs > 0 {
				// Gemini uses "silenceDurationMs" for the silence threshold
				vadConfig["silenceDurationMs"] = config.VAD.SilenceThresholdMs
			}
		}

		if len(vadConfig) > 0 {
			setupContent["realtimeInputConfig"] = map[string]interface{}{
				"automaticActivityDetection": vadConfig,
			}
			logger.Debug("Gemini VAD config added to setup", "vadConfig", vadConfig)
		}
	}

	// Add system instruction if provided
	if config.SystemInstruction != "" {
		setupContent["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]interface{}{
				{"text": config.SystemInstruction},
			},
		}
	}

	// Add tools if provided
	// Per Gemini docs: tools must be declared in BidiGenerateContentSetup
	if len(config.Tools) > 0 {
		functionDeclarations := make([]map[string]interface{}, len(config.Tools))
		for i, tool := range config.Tools {
			funcDecl := map[string]interface{}{
				"name": tool.Name,
			}
			if tool.Description != "" {
				funcDecl["description"] = tool.Description
			}
			if len(tool.Parameters) > 0 {
				funcDecl["parameters"] = tool.Parameters
			}
			functionDeclarations[i] = funcDecl
		}
		setupContent["tools"] = []map[string]interface{}{
			{"functionDeclarations": functionDeclarations},
		}
		logger.Debug("Gemini tools added to setup", "tool_count", len(config.Tools))
	}

	setupMsg := map[string]interface{}{
		"setup": setupContent,
	}

	// Store setup message for reconnection
	session.setupMsg = setupMsg

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

// SendChunk sends a media chunk to the server.
// When VAD is disabled (manual turn control), automatically sends activityStart
// before the first audio chunk of a turn.
func (s *StreamSession) SendChunk(ctx context.Context, chunk *types.MediaChunk) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(ErrSessionClosed)
	}

	// When VAD is disabled, send activityStart before first audio chunk
	// This signals to Gemini that user input is starting
	if s.isVADDisabled() && !s.activityStartSent {
		s.activityStartSent = true
		s.mu.Unlock()

		if err := s.sendActivityStart(); err != nil {
			logger.Error("StreamSession: failed to send activityStart", "error", err)
			// Continue anyway - the audio may still work
		}
	} else {
		s.mu.Unlock()
	}

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

// sendActivityStart signals the start of user audio input.
// Used internally when VAD is disabled (manual turn control mode).
func (s *StreamSession) sendActivityStart() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(ErrSessionClosed)
	}
	s.mu.Unlock()

	msg := map[string]interface{}{
		"realtime_input": map[string]interface{}{
			"activityStart": map[string]interface{}{},
		},
	}

	logger.Debug("Gemini StreamSession: sending activityStart")
	return s.ws.Send(msg)
}

// sendActivityEnd signals the end of user audio input.
// Used internally when VAD is disabled (manual turn control mode).
func (s *StreamSession) sendActivityEnd() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(ErrSessionClosed)
	}
	s.mu.Unlock()

	msg := map[string]interface{}{
		"realtime_input": map[string]interface{}{
			"activityEnd": map[string]interface{}{},
		},
	}

	logger.Debug("Gemini StreamSession: sending activityEnd")
	return s.ws.Send(msg)
}

// isVADDisabled returns true if automatic VAD is disabled for this session.
func (s *StreamSession) isVADDisabled() bool {
	return s.config.VAD != nil && s.config.VAD.Disabled
}

// EndInput implements the EndInputter interface expected by DuplexProviderStage.
// It signals that the user's input turn is complete and the model should respond.
//
// Behavior depends on VAD configuration:
// - If VAD is disabled: sends activityEnd signal for explicit turn control
// - If VAD is enabled: sends silence frames to trigger VAD end-of-speech detection
func (s *StreamSession) EndInput() {
	logger.Debug("Gemini StreamSession: EndInput called", "vad_disabled", s.isVADDisabled())

	// When VAD is disabled, use explicit turn signaling
	if s.isVADDisabled() {
		if err := s.sendActivityEnd(); err != nil {
			logger.Error("Gemini StreamSession: EndInput failed to send activityEnd", "error", err)
		}
		// Reset for next turn
		s.mu.Lock()
		s.activityStartSent = false
		s.mu.Unlock()
		return
	}

	// When VAD is enabled, send silence frames to help VAD detect end of speech.
	// This mimics the natural trailing silence at the end of human speech.
	// Gemini's ASM mode will detect this silence and trigger the model response.
	//
	// silenceFrameSize bytes = 500ms of silence at 16kHz 16-bit mono (16000 samples * 1 byte per sample)
	// We send 4 seconds total of silence to ensure VAD detection
	silentChunk := make([]byte, silenceFrameSize)
	for i := 0; i < silenceFrameCount; i++ {
		chunk := &types.MediaChunk{
			Data:      silentChunk,
			Timestamp: time.Now(),
		}
		if err := s.SendChunk(s.ctx, chunk); err != nil {
			logger.Error("Gemini StreamSession: EndInput failed to send silence", "error", err, "chunkIdx", i)
			break
		}
		// Small delay between chunks to spread them out
		time.Sleep(silenceFrameDelayMS * time.Millisecond)
	}

	logger.Debug("Gemini StreamSession: EndInput silence frames sent, waiting for VAD")
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

// reconnect attempts to reconnect the WebSocket and resend setup.
// Returns true if reconnection succeeded, false otherwise.
func (s *StreamSession) reconnect() bool {
	s.mu.Lock()
	if s.closed || s.reconnecting {
		s.mu.Unlock()
		return false
	}
	s.reconnecting = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.reconnecting = false
		s.mu.Unlock()
	}()

	for attempt := 1; attempt <= s.maxReconnectTries; attempt++ {
		logger.Info("StreamSession: attempting reconnection",
			"attempt", attempt,
			"maxAttempts", s.maxReconnectTries)

		// Close old WebSocket
		_ = s.ws.Close()

		// Create new WebSocket manager and connect
		s.ws = NewWebSocketManager(s.wsURL, s.apiKey)
		if err := s.ws.ConnectWithRetry(s.ctx); err != nil {
			logger.Warn("StreamSession: reconnection failed",
				"attempt", attempt,
				"error", err)
			continue
		}

		// Resend setup message
		if err := s.ws.Send(s.setupMsg); err != nil {
			logger.Warn("StreamSession: failed to send setup after reconnect",
				"attempt", attempt,
				"error", err)
			_ = s.ws.Close()
			continue
		}

		// Wait for setup_complete
		setupCtx, setupCancel := context.WithTimeout(s.ctx, reconnectSetupTimeoutSec*time.Second)
		var setupResponse ServerMessage
		err := s.ws.Receive(setupCtx, &setupResponse)
		setupCancel()

		if err != nil || setupResponse.SetupComplete == nil {
			logger.Warn("StreamSession: setup_complete not received after reconnect",
				"attempt", attempt,
				"error", err)
			_ = s.ws.Close()
			continue
		}

		// Restart heartbeat
		s.ws.StartHeartbeat(s.ctx, reconnectHeartbeatSec*time.Second)

		logger.Info("StreamSession: reconnection successful", "attempt", attempt)
		return true
	}

	logger.Error("StreamSession: all reconnection attempts failed")
	return false
}

// receiveLoop continuously receives messages from the WebSocket
func (s *StreamSession) receiveLoop() {
	defer close(s.responseCh)

	for {
		select {
		case <-s.ctx.Done():
			logger.Debug("StreamSession: receiveLoop exiting due to context done")
			return
		default:
			// First receive as raw JSON to see what we're getting
			var rawMsg json.RawMessage
			if err := s.ws.Receive(s.ctx, &rawMsg); err != nil {
				// Check if session was closed intentionally
				s.mu.Lock()
				closed := s.closed
				s.mu.Unlock()

				if !closed {
					// Log the error with details to help diagnose connection issues
					errMsg := err.Error()
					isEOF := strings.Contains(errMsg, "EOF") || strings.Contains(errMsg, "unexpected EOF")
					isCloseErr := strings.Contains(errMsg, "close") || strings.Contains(errMsg, "websocket")
					isTimeout := strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "deadline")

					logger.Warn("StreamSession: WebSocket receive error",
						"error", err,
						"isEOF", isEOF,
						"isCloseError", isCloseErr,
						"isTimeout", isTimeout,
						"autoReconnect", s.autoReconnect)

					// Attempt reconnection if enabled
					if s.autoReconnect && !isTimeout {
						if s.reconnect() {
							// Reconnection successful, continue receiving
							logger.Info("StreamSession: continuing after successful reconnection")
							continue
						}
					}

					// Reconnection failed or not enabled
					wrappedErr := fmt.Errorf("gemini websocket error: %w", err)
					select {
					case s.errCh <- wrappedErr:
					default:
						logger.Debug("StreamSession: error channel full, dropping error")
					}
				} else {
					logger.Debug("StreamSession: receiveLoop exiting due to intentional close")
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
				_ = json.Unmarshal(rawMsg, &logMsg) // Error ignored - best-effort logging

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

	// Handle tool calls - convert Gemini's BidiGenerateContentToolCall to StreamChunk
	if msg.ToolCall != nil && len(msg.ToolCall.FunctionCalls) > 0 {
		toolCalls := make([]types.MessageToolCall, len(msg.ToolCall.FunctionCalls))
		for i, fc := range msg.ToolCall.FunctionCalls {
			// Convert args map to JSON
			argsJSON, err := json.Marshal(fc.Args)
			if err != nil {
				argsJSON = []byte("{}")
			}
			toolCalls[i] = types.MessageToolCall{
				ID:   fc.ID,
				Name: fc.Name,
				Args: argsJSON,
			}
		}

		// Send tool calls as a chunk with tool_calls finish reason
		finishReason := "tool_calls"
		response := providers.StreamChunk{
			ToolCalls:    toolCalls,
			FinishReason: &finishReason,
		}
		select {
		case s.responseCh <- response:
			logger.Debug("Gemini tool calls emitted", "count", len(toolCalls))
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
		return nil
	}

	// Store usage metadata for cost calculation
	var costInfo *types.CostInfo
	if msg.UsageMetadata != nil {
		inputTokens := msg.UsageMetadata.PromptTokenCount
		outputTokens := msg.UsageMetadata.ResponseTokenCount

		// Calculate USD costs if pricing is configured
		var inputCostUSD, outputCostUSD, totalCost float64
		if s.inputCostPer1K > 0 && s.outputCostPer1K > 0 {
			inputCostUSD = float64(inputTokens) / tokensPerThousand * s.inputCostPer1K
			outputCostUSD = float64(outputTokens) / tokensPerThousand * s.outputCostPer1K
			totalCost = inputCostUSD + outputCostUSD
		}

		costInfo = &types.CostInfo{
			InputTokens:   inputTokens,
			OutputTokens:  outputTokens,
			InputCostUSD:  inputCostUSD,
			OutputCostUSD: outputCostUSD,
			TotalCost:     totalCost,
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

// Ensure StreamSession implements ToolResponseSupport
var _ providers.ToolResponseSupport = (*StreamSession)(nil)

// SendToolResponse sends a single tool execution result back to Gemini.
// The toolCallID must match the ID from the FunctionCall.
// The result should be a JSON-serializable string (typically JSON).
func (s *StreamSession) SendToolResponse(ctx context.Context, toolCallID, result string) error {
	return s.SendToolResponses(ctx, []providers.ToolResponse{
		{
			ToolCallID: toolCallID,
			Result:     result,
		},
	})
}

// SendToolResponses sends multiple tool execution results back to Gemini.
// This is used when the model makes parallel tool calls.
// After receiving the tool responses, Gemini will continue generating.
func (s *StreamSession) SendToolResponses(ctx context.Context, responses []providers.ToolResponse) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(ErrSessionClosed)
	}
	s.mu.Unlock()

	// Build Gemini's BidiGenerateContentToolResponse format
	// Per docs: toolResponse.functionResponses[].{id, name, response}
	functionResponses := make([]map[string]interface{}, len(responses))
	for i, resp := range responses {
		// Parse result as JSON if possible, otherwise wrap as string
		var resultObj interface{}
		if err := json.Unmarshal([]byte(resp.Result), &resultObj); err != nil {
			// Result is not valid JSON, wrap it
			resultObj = map[string]interface{}{"result": resp.Result}
		}

		funcResp := map[string]interface{}{
			"id":       resp.ToolCallID,
			"response": resultObj,
		}

		// Add error flag if the tool execution failed
		if resp.IsError {
			funcResp["error"] = true
		}

		functionResponses[i] = funcResp
	}

	msg := map[string]interface{}{
		"toolResponse": map[string]interface{}{
			"functionResponses": functionResponses,
		},
	}

	// Log tool response for debugging
	if logger.DefaultLogger != nil {
		if msgJSON, err := json.MarshalIndent(msg, "", "  "); err == nil {
			logger.DefaultLogger.Debug("Gemini sending tool response", "message", string(msgJSON))
		}
	}

	return s.ws.Send(msg)
}
