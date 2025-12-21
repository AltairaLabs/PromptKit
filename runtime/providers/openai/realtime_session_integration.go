// Package openai provides OpenAI Realtime API streaming support.
package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Common error messages
const (
	errSessionClosed = "session is closed"
)

// Configuration constants
const (
	responseChannelSize = 10
	heartbeatInterval   = 30 * time.Second
	setupTimeout        = 10 * time.Second
	tokensPerThousand   = 1000.0
)

// Ensure RealtimeSession implements StreamInputSession
var _ providers.StreamInputSession = (*RealtimeSession)(nil)

// RealtimeSession implements StreamInputSession for OpenAI Realtime API.
type RealtimeSession struct {
	ws         *RealtimeWebSocket
	ctx        context.Context
	cancel     context.CancelFunc
	responseCh chan providers.StreamChunk
	errCh      chan error
	mu         sync.Mutex
	closed     bool
	eventID    atomic.Int64

	// Configuration stored for the session
	config RealtimeSessionConfig

	// Cost tracking
	inputCostPer1K  float64
	outputCostPer1K float64

	// Session state
	sessionInfo *SessionInfo // Received from session.created
}

// NewRealtimeSession creates a new OpenAI Realtime streaming session.
func NewRealtimeSession(ctx context.Context, apiKey string, config *RealtimeSessionConfig) (*RealtimeSession, error) {
	if config == nil {
		defaultConfig := DefaultRealtimeSessionConfig()
		config = &defaultConfig
	}

	sessionCtx, cancel := context.WithCancel(ctx)

	// Create WebSocket connection
	ws := NewRealtimeWebSocket(config.Model, apiKey)
	if err := ws.ConnectWithRetry(sessionCtx); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	session := &RealtimeSession{
		ws:         ws,
		ctx:        sessionCtx,
		cancel:     cancel,
		responseCh: make(chan providers.StreamChunk, responseChannelSize),
		errCh:      make(chan error, 1),
		config:     *config,
	}

	// Wait for session.created event
	if err := session.waitForSessionCreated(); err != nil {
		_ = ws.Close()
		cancel()
		return nil, fmt.Errorf("failed to receive session.created: %w", err)
	}

	// Send session.update with configuration
	if err := session.sendSessionUpdate(); err != nil {
		_ = ws.Close()
		cancel()
		return nil, fmt.Errorf("failed to send session.update: %w", err)
	}

	// Start heartbeat
	ws.StartHeartbeat(sessionCtx, heartbeatInterval)

	// Start receive loop
	go session.receiveLoop()

	return session, nil
}

// waitForSessionCreated waits for the initial session.created event from the server.
func (s *RealtimeSession) waitForSessionCreated() error {
	ctx, cancel := context.WithTimeout(s.ctx, setupTimeout)
	defer cancel()

	data, err := s.ws.Receive(ctx)
	if err != nil {
		return fmt.Errorf("failed to receive session.created: %w", err)
	}

	event, err := ParseServerEvent(data)
	if err != nil {
		return fmt.Errorf("failed to parse session.created: %w", err)
	}

	created, ok := event.(*SessionCreatedEvent)
	if !ok {
		return fmt.Errorf("expected session.created, got: %T", event)
	}

	s.sessionInfo = &created.Session
	logger.Info("OpenAI Realtime: session created",
		"session_id", created.Session.ID,
		"model", created.Session.Model)

	return nil
}

// sendSessionUpdate sends a session.update event to configure the session.
func (s *RealtimeSession) sendSessionUpdate() error {
	sessionConfig := s.buildSessionConfig()

	event := SessionUpdateEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    "session.update",
		},
		Session: sessionConfig,
	}

	if logger.DefaultLogger != nil {
		if configJSON, err := json.MarshalIndent(event, "", "  "); err == nil {
			logger.Debug("OpenAI Realtime: sending session.update", "config", string(configJSON))
		}
	}

	return s.ws.Send(event)
}

// buildSessionConfig converts our config to the API format.
func (s *RealtimeSession) buildSessionConfig() SessionConfig {
	cfg := SessionConfig{
		Modalities:        s.config.Modalities,
		Instructions:      s.config.Instructions,
		Voice:             s.config.Voice,
		InputAudioFormat:  s.config.InputAudioFormat,
		OutputAudioFormat: s.config.OutputAudioFormat,
		Temperature:       s.config.Temperature,
	}

	if s.config.InputAudioTranscription != nil {
		cfg.InputAudioTranscription = &TranscriptionConfig{
			Model: s.config.InputAudioTranscription.Model,
		}
	}

	if s.config.TurnDetection != nil {
		cfg.TurnDetection = &TurnDetectionConfig{
			Type:              s.config.TurnDetection.Type,
			Threshold:         s.config.TurnDetection.Threshold,
			PrefixPaddingMs:   s.config.TurnDetection.PrefixPaddingMs,
			SilenceDurationMs: s.config.TurnDetection.SilenceDurationMs,
			CreateResponse:    s.config.TurnDetection.CreateResponse,
		}
	}

	if len(s.config.Tools) > 0 {
		cfg.Tools = make([]RealtimeToolDef, len(s.config.Tools))
		for i, tool := range s.config.Tools {
			cfg.Tools[i] = RealtimeToolDef{
				Type:        "function",
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			}
		}
	}

	if s.config.MaxResponseOutputTokens != nil {
		cfg.MaxResponseOutputTokens = s.config.MaxResponseOutputTokens
	}

	return cfg
}

// nextEventID generates a unique event ID.
func (s *RealtimeSession) nextEventID() string {
	return fmt.Sprintf("evt_%d", s.eventID.Add(1))
}

// SendChunk sends an audio chunk to the server.
func (s *RealtimeSession) SendChunk(ctx context.Context, chunk *types.MediaChunk) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.mu.Unlock()

	if chunk == nil || len(chunk.Data) == 0 {
		return nil
	}

	// Base64 encode the audio data
	audioBase64 := base64.StdEncoding.EncodeToString(chunk.Data)

	event := InputAudioBufferAppendEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    "input_audio_buffer.append",
		},
		Audio: audioBase64,
	}

	return s.ws.Send(event)
}

// SendText sends a text message and triggers a response.
func (s *RealtimeSession) SendText(ctx context.Context, text string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.mu.Unlock()

	// Create a user message with text content
	item := ConversationItem{
		Type: "message",
		Role: "user",
		Content: []ConversationContent{
			{
				Type: "input_text",
				Text: text,
			},
		},
	}

	createEvent := ConversationItemCreateEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    "conversation.item.create",
		},
		Item: item,
	}

	if err := s.ws.Send(createEvent); err != nil {
		return fmt.Errorf("failed to send text: %w", err)
	}

	// Trigger a response
	responseEvent := ResponseCreateEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    "response.create",
		},
	}

	return s.ws.Send(responseEvent)
}

// SendSystemContext sends a text message as context without completing the turn.
func (s *RealtimeSession) SendSystemContext(ctx context.Context, text string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.mu.Unlock()

	// For OpenAI Realtime, we update the session instructions
	// This provides context without triggering a response
	event := SessionUpdateEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    "session.update",
		},
		Session: SessionConfig{
			Instructions: text,
		},
	}

	return s.ws.Send(event)
}

// CommitAudioBuffer commits the current audio buffer for processing.
func (s *RealtimeSession) CommitAudioBuffer() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.mu.Unlock()

	event := InputAudioBufferCommitEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    "input_audio_buffer.commit",
		},
	}

	return s.ws.Send(event)
}

// ClearAudioBuffer clears the current audio buffer.
func (s *RealtimeSession) ClearAudioBuffer() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.mu.Unlock()

	event := InputAudioBufferClearEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    "input_audio_buffer.clear",
		},
	}

	return s.ws.Send(event)
}

// TriggerResponse manually triggers a response from the model.
func (s *RealtimeSession) TriggerResponse(config *ResponseConfig) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.mu.Unlock()

	event := ResponseCreateEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    "response.create",
		},
		Response: config,
	}

	return s.ws.Send(event)
}

// CancelResponse cancels an in-progress response.
func (s *RealtimeSession) CancelResponse() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.mu.Unlock()

	event := ResponseCancelEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    "response.cancel",
		},
	}

	return s.ws.Send(event)
}

// Response returns the channel for receiving responses.
func (s *RealtimeSession) Response() <-chan providers.StreamChunk {
	return s.responseCh
}

// Done returns a channel that's closed when the session ends.
func (s *RealtimeSession) Done() <-chan struct{} {
	return s.ctx.Done()
}

// Error returns any error that occurred during the session.
func (s *RealtimeSession) Error() error {
	select {
	case err := <-s.errCh:
		return err
	default:
		return nil
	}
}

// Close closes the session.
func (s *RealtimeSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	s.cancel()
	return s.ws.Close()
}

// EndInput signals the end of user input.
// For OpenAI Realtime with server VAD, this commits the audio buffer.
// For manual turn control, this commits and triggers a response.
func (s *RealtimeSession) EndInput() {
	logger.Debug("OpenAI Realtime: EndInput called")

	if err := s.CommitAudioBuffer(); err != nil {
		logger.Error("OpenAI Realtime: EndInput failed to commit audio", "error", err)
		return
	}

	// If VAD is disabled (nil), manually trigger response
	if s.config.TurnDetection == nil {
		if err := s.TriggerResponse(nil); err != nil {
			logger.Error("OpenAI Realtime: EndInput failed to trigger response", "error", err)
		}
	}
}

// receiveLoop continuously receives and processes messages from the server.
func (s *RealtimeSession) receiveLoop() {
	defer close(s.responseCh)

	msgCh := make(chan []byte, responseChannelSize)
	errCh := make(chan error, 1)

	go func() {
		errCh <- s.ws.ReceiveLoop(s.ctx, msgCh)
	}()

	for {
		select {
		case <-s.ctx.Done():
			return
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("OpenAI Realtime: receive loop error", "error", err)
				select {
				case s.errCh <- err:
				default:
				}
			}
			return
		case data := <-msgCh:
			s.handleMessage(data)
		}
	}
}

// handleMessage processes a single message from the server.
func (s *RealtimeSession) handleMessage(data []byte) {
	event, err := ParseServerEvent(data)
	if err != nil {
		logger.Warn("OpenAI Realtime: failed to parse event", "error", err)
		return
	}

	switch e := event.(type) {
	case *ErrorEvent:
		s.handleError(e)
	case *SessionUpdatedEvent:
		s.handleSessionUpdated(e)
	case *ResponseTextDeltaEvent:
		s.handleTextDelta(e)
	case *ResponseTextDoneEvent:
		s.handleTextDone(e)
	case *ResponseAudioDeltaEvent:
		s.handleAudioDelta(e)
	case *ResponseAudioDoneEvent:
		s.handleAudioDone(e)
	case *ResponseAudioTranscriptDeltaEvent:
		s.handleTranscriptDelta(e)
	case *ResponseAudioTranscriptDoneEvent:
		s.handleTranscriptDone(e)
	case *ResponseFunctionCallArgumentsDoneEvent:
		s.handleFunctionCallDone(e)
	case *ResponseDoneEvent:
		s.handleResponseDone(e)
	case *InputAudioBufferSpeechStartedEvent:
		logger.Debug("OpenAI Realtime: speech started", "item_id", e.ItemID)
	case *InputAudioBufferSpeechStoppedEvent:
		logger.Debug("OpenAI Realtime: speech stopped", "item_id", e.ItemID)
	case *ConversationItemInputAudioTranscriptionCompletedEvent:
		s.handleInputTranscription(e)
	case *RateLimitsUpdatedEvent:
		s.handleRateLimits(e)
	default:
		// Log unknown events at debug level
		var base ServerEvent
		if json.Unmarshal(data, &base) == nil {
			logger.Debug("OpenAI Realtime: unhandled event", "type", base.Type)
		}
	}
}

func (s *RealtimeSession) handleError(e *ErrorEvent) {
	logger.Error("OpenAI Realtime: server error",
		"type", e.Error.Type,
		"code", e.Error.Code,
		"message", e.Error.Message)

	s.responseCh <- providers.StreamChunk{
		Error: fmt.Errorf("server error: %s", e.Error.Message),
	}
}

func (s *RealtimeSession) handleSessionUpdated(e *SessionUpdatedEvent) {
	s.sessionInfo = &e.Session
	logger.Debug("OpenAI Realtime: session updated", "modalities", e.Session.Modalities)
}

func (s *RealtimeSession) handleTextDelta(e *ResponseTextDeltaEvent) {
	s.responseCh <- providers.StreamChunk{
		Delta: e.Delta,
		Metadata: map[string]interface{}{
			"item_id":       e.ItemID,
			"response_id":   e.ResponseID,
			"content_index": e.ContentIndex,
		},
	}
}

func (s *RealtimeSession) handleTextDone(e *ResponseTextDoneEvent) {
	s.responseCh <- providers.StreamChunk{
		Content: e.Text,
		Metadata: map[string]interface{}{
			"item_id":       e.ItemID,
			"response_id":   e.ResponseID,
			"content_index": e.ContentIndex,
			"text_done":     true,
		},
	}
}

func (s *RealtimeSession) handleAudioDelta(e *ResponseAudioDeltaEvent) {
	// Decode base64 audio
	audioData, err := base64.StdEncoding.DecodeString(e.Delta)
	if err != nil {
		logger.Warn("OpenAI Realtime: failed to decode audio", "error", err)
		return
	}

	s.responseCh <- providers.StreamChunk{
		Metadata: map[string]interface{}{
			"audio_data":    audioData,
			"item_id":       e.ItemID,
			"response_id":   e.ResponseID,
			"content_index": e.ContentIndex,
		},
	}
}

func (s *RealtimeSession) handleAudioDone(e *ResponseAudioDoneEvent) {
	s.responseCh <- providers.StreamChunk{
		Metadata: map[string]interface{}{
			"item_id":       e.ItemID,
			"response_id":   e.ResponseID,
			"content_index": e.ContentIndex,
			"audio_done":    true,
		},
	}
}

func (s *RealtimeSession) handleTranscriptDelta(e *ResponseAudioTranscriptDeltaEvent) {
	s.responseCh <- providers.StreamChunk{
		Metadata: map[string]interface{}{
			"transcript_delta": e.Delta,
			"item_id":          e.ItemID,
			"response_id":      e.ResponseID,
			"content_index":    e.ContentIndex,
		},
	}
}

func (s *RealtimeSession) handleTranscriptDone(e *ResponseAudioTranscriptDoneEvent) {
	s.responseCh <- providers.StreamChunk{
		Metadata: map[string]interface{}{
			"transcript":      e.Transcript,
			"item_id":         e.ItemID,
			"response_id":     e.ResponseID,
			"content_index":   e.ContentIndex,
			"transcript_done": true,
		},
	}
}

func (s *RealtimeSession) handleFunctionCallDone(e *ResponseFunctionCallArgumentsDoneEvent) {
	toolCall := types.MessageToolCall{
		ID:   e.CallID,
		Name: e.Name,
		Args: json.RawMessage(e.Arguments),
	}

	s.responseCh <- providers.StreamChunk{
		ToolCalls: []types.MessageToolCall{toolCall},
		Metadata: map[string]interface{}{
			"item_id":     e.ItemID,
			"response_id": e.ResponseID,
		},
	}
}

func (s *RealtimeSession) handleResponseDone(e *ResponseDoneEvent) {
	chunk := providers.StreamChunk{
		FinishReason: &e.Response.Status,
		Metadata: map[string]interface{}{
			"response_id": e.Response.ID,
		},
	}

	if e.Response.Usage != nil {
		chunk.TokenCount = e.Response.Usage.TotalTokens
		chunk.CostInfo = s.calculateCost(e.Response.Usage)
	}

	s.responseCh <- chunk
}

func (s *RealtimeSession) handleInputTranscription(e *ConversationItemInputAudioTranscriptionCompletedEvent) {
	s.responseCh <- providers.StreamChunk{
		Metadata: map[string]interface{}{
			"input_transcript": e.Transcript,
			"item_id":          e.ItemID,
			"content_index":    e.ContentIndex,
		},
	}
}

func (s *RealtimeSession) handleRateLimits(e *RateLimitsUpdatedEvent) {
	for _, rl := range e.RateLimits {
		logger.Debug("OpenAI Realtime: rate limit updated",
			"name", rl.Name,
			"limit", rl.Limit,
			"remaining", rl.Remaining,
			"reset_seconds", rl.ResetSeconds)
	}
}

func (s *RealtimeSession) calculateCost(usage *UsageInfo) *types.CostInfo {
	if usage == nil {
		return nil
	}

	inputTokens := usage.InputTokens
	outputTokens := usage.OutputTokens

	// Use configured pricing or defaults
	inputCostPer1K := s.inputCostPer1K
	outputCostPer1K := s.outputCostPer1K

	if inputCostPer1K == 0 {
		inputCostPer1K = 0.06 // Default for gpt-4o-realtime
	}
	if outputCostPer1K == 0 {
		outputCostPer1K = 0.24 // Default for gpt-4o-realtime
	}

	inputCost := float64(inputTokens) / tokensPerThousand * inputCostPer1K
	outputCost := float64(outputTokens) / tokensPerThousand * outputCostPer1K

	return &types.CostInfo{
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		InputCostUSD:  inputCost,
		OutputCostUSD: outputCost,
		TotalCost:     inputCost + outputCost,
	}
}
