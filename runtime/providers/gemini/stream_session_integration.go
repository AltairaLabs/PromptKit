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

	// Session setup constants
	heartbeatIntervalSec  = 30 // Heartbeat interval for new sessions
	setupTimeoutSec       = 10 // Timeout for initial setup response
	defaultReconnectTries = 3  // Default max reconnection attempts
)

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

// NewStreamSession creates a new streaming session
func NewStreamSession(ctx context.Context, wsURL, apiKey string, config *StreamSessionConfig) (*StreamSession, error) {
	modalities := getResponseModalities(config.ResponseModalities)

	if err := validateModalities(modalities); err != nil {
		return nil, err
	}

	sessionCtx, cancel := context.WithCancel(ctx)

	ws := NewWebSocketManager(wsURL, apiKey)
	if err := ws.ConnectWithRetry(sessionCtx); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	session := createSession(sessionCtx, ws, cancel, wsURL, apiKey, config)
	setupMsg := buildSetupMessage(config, modalities)
	session.setupMsg = setupMsg

	logSetupMessage(setupMsg)

	if err := sendAndWaitForSetup(sessionCtx, ws, cancel, setupMsg); err != nil {
		return nil, err
	}

	ws.StartHeartbeat(sessionCtx, heartbeatIntervalSec*time.Second)
	go session.receiveLoop()

	return session, nil
}

// getResponseModalities returns modalities with TEXT as default
func getResponseModalities(modalities []string) []string {
	if len(modalities) == 0 {
		return []string{"TEXT"}
	}
	return modalities
}

// validateModalities checks for invalid modality combinations
func validateModalities(modalities []string) error {
	if len(modalities) > 1 && sliceContains(modalities, "TEXT") && sliceContains(modalities, "AUDIO") {
		return fmt.Errorf(
			"invalid response modalities: Gemini Live API does not support TEXT and AUDIO " +
				"simultaneously. Use either [\"TEXT\"] or [\"AUDIO\"], not both")
	}
	return nil
}

// createSession creates a new StreamSession with the given configuration
func createSession(
	ctx context.Context,
	ws *WebSocketManager,
	cancel context.CancelFunc,
	wsURL, apiKey string,
	config *StreamSessionConfig,
) *StreamSession {
	maxReconnectTries := config.MaxReconnectTries
	if maxReconnectTries <= 0 {
		maxReconnectTries = defaultReconnectTries
	}

	return &StreamSession{
		ws:                ws,
		ctx:               ctx,
		cancel:            cancel,
		responseCh:        make(chan providers.StreamChunk, responseChannelSize),
		errCh:             make(chan error, 1),
		inputCostPer1K:    config.InputCostPer1K,
		outputCostPer1K:   config.OutputCostPer1K,
		wsURL:             wsURL,
		apiKey:            apiKey,
		config:            *config,
		autoReconnect:     config.AutoReconnect,
		maxReconnectTries: maxReconnectTries,
	}
}

// buildSetupMessage constructs the initial setup message for Gemini Live API
func buildSetupMessage(config *StreamSessionConfig, modalities []string) map[string]interface{} {
	modelPath := getModelPath(config.Model)
	generationConfig := buildGenerationConfig(modalities)

	setupContent := map[string]interface{}{
		"model":            modelPath,
		"generationConfig": generationConfig,
	}

	addTranscriptionConfig(setupContent, modalities)
	addVADConfig(setupContent, config.VAD)
	addSystemInstruction(setupContent, config.SystemInstruction)
	addToolsConfig(setupContent, config.Tools)

	return map[string]interface{}{
		"setup": setupContent,
	}
}

// getModelPath ensures model is in correct format: models/{model}
func getModelPath(model string) string {
	if model == "" {
		return "models/gemini-2.0-flash-exp"
	}
	if len(model) < 7 || model[:7] != "models/" {
		return "models/" + model
	}
	return model
}

// buildGenerationConfig creates the generation configuration
func buildGenerationConfig(modalities []string) map[string]interface{} {
	config := map[string]interface{}{
		"responseModalities": modalities,
	}

	if sliceContains(modalities, "AUDIO") {
		config["speechConfig"] = map[string]interface{}{
			"voiceConfig": map[string]interface{}{
				"prebuiltVoiceConfig": map[string]interface{}{
					"voiceName": "Puck",
				},
			},
		}
	}

	return config
}

// addTranscriptionConfig adds transcription settings for AUDIO mode
func addTranscriptionConfig(setupContent map[string]interface{}, modalities []string) {
	if sliceContains(modalities, "AUDIO") {
		setupContent["outputAudioTranscription"] = map[string]interface{}{}
		setupContent["inputAudioTranscription"] = map[string]interface{}{}
	}
}

// addVADConfig adds VAD configuration if provided
func addVADConfig(setupContent map[string]interface{}, vad *VADConfig) {
	if vad == nil {
		return
	}

	vadConfig := buildVADConfigMap(vad)
	if len(vadConfig) > 0 {
		setupContent["realtimeInputConfig"] = map[string]interface{}{
			"automaticActivityDetection": vadConfig,
		}
		logger.Debug("Gemini VAD config added to setup", "vadConfig", vadConfig)
	}
}

// buildVADConfigMap converts VADConfig to a map for the API
func buildVADConfigMap(vad *VADConfig) map[string]interface{} {
	vadConfig := map[string]interface{}{}

	if vad.Disabled {
		vadConfig["disabled"] = true
		return vadConfig
	}

	if vad.StartOfSpeechSensitivity != "" {
		vadConfig["startOfSpeechSensitivity"] = vad.StartOfSpeechSensitivity
	}
	if vad.EndOfSpeechSensitivity != "" {
		vadConfig["endOfSpeechSensitivity"] = vad.EndOfSpeechSensitivity
	}
	if vad.PrefixPaddingMs > 0 {
		vadConfig["prefixPaddingMs"] = vad.PrefixPaddingMs
	}
	if vad.SilenceThresholdMs > 0 {
		vadConfig["silenceDurationMs"] = vad.SilenceThresholdMs
	}

	return vadConfig
}

// addSystemInstruction adds system instruction if provided
func addSystemInstruction(setupContent map[string]interface{}, instruction string) {
	if instruction != "" {
		setupContent["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]interface{}{
				{"text": instruction},
			},
		}
	}
}

// addToolsConfig adds tools configuration if provided
func addToolsConfig(setupContent map[string]interface{}, tools []ToolDefinition) {
	if len(tools) == 0 {
		return
	}

	functionDeclarations := make([]map[string]interface{}, len(tools))
	for i, tool := range tools {
		functionDeclarations[i] = buildFunctionDeclaration(tool)
	}

	setupContent["tools"] = []map[string]interface{}{
		{"functionDeclarations": functionDeclarations},
	}
	logger.Debug("Gemini tools added to setup", "tool_count", len(tools))
}

// buildFunctionDeclaration converts a ToolDefinition to API format
func buildFunctionDeclaration(tool ToolDefinition) map[string]interface{} {
	funcDecl := map[string]interface{}{
		"name": tool.Name,
	}
	if tool.Description != "" {
		funcDecl["description"] = tool.Description
	}
	if len(tool.Parameters) > 0 {
		funcDecl["parameters"] = tool.Parameters
	}
	return funcDecl
}

// logSetupMessage logs the setup message at debug level
func logSetupMessage(setupMsg map[string]interface{}) {
	if logger.DefaultLogger != nil {
		if setupJSON, err := json.MarshalIndent(setupMsg, "", "  "); err == nil {
			logger.DefaultLogger.Debug("Gemini setup message", "setup", string(setupJSON))
		}
	}
}

// sendAndWaitForSetup sends the setup message and waits for confirmation
func sendAndWaitForSetup(
	ctx context.Context,
	ws *WebSocketManager,
	cancel context.CancelFunc,
	setupMsg map[string]interface{},
) error {
	if err := ws.Send(setupMsg); err != nil {
		_ = ws.Close()
		cancel()
		return fmt.Errorf("failed to send setup message: %w", err)
	}

	setupCtx, setupCancel := context.WithTimeout(ctx, setupTimeoutSec*time.Second)
	defer setupCancel()

	var setupResponse ServerMessage
	if err := ws.Receive(setupCtx, &setupResponse); err != nil {
		_ = ws.Close()
		cancel()
		return fmt.Errorf("failed to receive setup response: %w", err)
	}

	if setupResponse.SetupComplete == nil {
		_ = ws.Close()
		cancel()
		return fmt.Errorf("invalid setup response: setup_complete not received")
	}

	return nil
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
