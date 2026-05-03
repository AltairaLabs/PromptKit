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

// ErrConnectionLost is the error type emitted on the Response channel
// when the WebSocket connection drops mid-session (network failure,
// heartbeat timeout, server-initiated close with a non-graceful code).
// Callers can check for this with errors.Is to distinguish recoverable
// connection loss from clean session termination or server-side errors.
var ErrConnectionLost = errors.New("websocket connection lost")

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

	// Cost tracking (can be overridden via provider config)
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

	logger.Debug("OpenAI Realtime: waiting for session.created event")
	data, err := s.ws.Receive(ctx)
	if err != nil {
		logger.Error("OpenAI Realtime: failed to receive session.created", "error", err)
		return fmt.Errorf("failed to receive session.created: %w", err)
	}
	logger.Debug("OpenAI Realtime: received data from WebSocket", "length", len(data), "data", string(data))

	event, err := ParseServerEvent(data)
	if err != nil {
		// Truncate data for logging to avoid huge error messages
		const maxLogDataLen = 200
		truncatedData := data
		if len(data) > maxLogDataLen {
			truncatedData = data[:maxLogDataLen]
		}
		logger.Error("OpenAI Realtime: failed to parse event", "error", err, "data", string(truncatedData))
		return fmt.Errorf("failed to parse session.created: %w", err)
	}

	created, ok := event.(*SessionCreatedEvent)
	if !ok {
		logger.Error("OpenAI Realtime: unexpected event type", "got", event)
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

// buildSessionConfig converts our internal RealtimeSessionConfig into
// the GA Realtime session.update payload. Output modalities flatten
// (audio implies text-too); codec, voice, VAD, and transcription nest
// under audio.{input,output}; temperature dropped (GA moved it to
// per-response.create); voice moved into audio.output.voice.
func (s *RealtimeSession) buildSessionConfig() SessionConfig {
	cfg := SessionConfig{
		Type:             "realtime",
		Instructions:     s.config.Instructions,
		OutputModalities: outputModalities(s.config.Modalities),
		Audio: &RealtimeAudioConfig{
			Input: &RealtimeAudioInput{
				Format: pcmFormat(s.config.InputAudioFormat, DefaultRealtimeSampleRate),
			},
			Output: &RealtimeAudioOutput{
				Format: pcmFormat(s.config.OutputAudioFormat, DefaultRealtimeSampleRate),
				Voice:  s.config.Voice,
			},
		},
	}

	if s.config.InputAudioTranscription != nil {
		cfg.Audio.Input.Transcription = &TranscriptionConfig{
			Model: s.config.InputAudioTranscription.Model,
		}
	}

	if s.config.TurnDetection != nil {
		cfg.Audio.Input.TurnDetection = &TurnDetectionConfig{
			Type:              s.config.TurnDetection.Type,
			Threshold:         s.config.TurnDetection.Threshold,
			PrefixPaddingMs:   s.config.TurnDetection.PrefixPaddingMs,
			SilenceDurationMs: s.config.TurnDetection.SilenceDurationMs,
			CreateResponse:    s.config.TurnDetection.CreateResponse,
		}
	}
	// When TurnDetection is nil we leave audio.input.turn_detection nil;
	// the pointer-without-omitempty tag marshals it as
	// `"turn_detection":null` — the GA signal for "manual turn control".

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
		cfg.MaxOutputTokens = s.config.MaxResponseOutputTokens
	}

	return cfg
}

// outputModalities maps our internal modality list onto the GA
// output_modalities field. The GA API accepts ["audio"] (which implies
// text-too) or ["text"] for text-only.
func outputModalities(in []string) []string {
	for _, m := range in {
		if m == "audio" {
			return []string{"audio"}
		}
	}
	if len(in) == 0 {
		return []string{"audio"} // safe default for a Realtime session
	}
	return []string{"text"}
}

// pcmFormat returns the GA-shape audio format descriptor for a legacy
// "pcm16"-style codec name. Empty / unknown formats fall through as nil
// so the server uses its default.
func pcmFormat(legacy string, rate int) *RealtimeAudioFormat {
	if legacy != "" && legacy != "pcm16" {
		return nil
	}
	return &RealtimeAudioFormat{Type: "audio/pcm", Rate: rate}
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

// SendSystemContext sends a partial session.update that only modifies
// instructions, leaving codec/voice/VAD untouched. The GA API merges
// partial session.update events; type is included because GA requires
// it on every session.update.
func (s *RealtimeSession) SendSystemContext(ctx context.Context, text string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.mu.Unlock()

	event := SessionUpdateEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    "session.update",
		},
		Session: SessionConfig{
			Type:         "realtime",
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

// Config returns the session configuration. Callers can use this to
// create a new session with the same settings after a connection loss:
//
//	if errors.Is(session.Error(), openai.ErrConnectionLost) {
//	    newSession, _ := openai.NewRealtimeSession(ctx, apiKey, session.Config())
//	}
//
// Note: server-side conversation state is lost on reconnection —
// this only preserves the client-side configuration.
func (s *RealtimeSession) Config() *RealtimeSessionConfig {
	cfg := s.config
	return &cfg
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
//
// In server_vad mode the server auto-commits when it detects end-of-speech
// in the audio buffer and (with create_response: true) auto-creates a
// response. Sending manual input_audio_buffer.commit + response.create from
// the client races with that auto-handling and causes the server to reject
// audio chunks with a cryptic "Invalid 'audio'... got an invalid value"
// error (verified via integration bisection). Callers that want server_vad
// behavior must include trailing silence in the streamed audio so the VAD
// can detect end-of-speech — pumpTTSChunks already does this via
// streamSilenceTail.
//
// In manual turn-control mode (TurnDetection == nil, i.e. vad_disabled),
// the client owns turn boundaries: commit the buffer and trigger a response.
func (s *RealtimeSession) EndInput() {
	if s.config.TurnDetection != nil {
		logger.Debug("OpenAI Realtime: EndInput no-op (server_vad mode — server auto-commits and auto-responds)")
		return
	}

	logger.Debug("OpenAI Realtime: EndInput called (manual turn mode)")

	if err := s.CommitAudioBuffer(); err != nil {
		logger.Error("OpenAI Realtime: EndInput failed to commit audio", "error", err)
		return
	}
	if err := s.TriggerResponse(nil); err != nil {
		logger.Error("OpenAI Realtime: EndInput failed to trigger response", "error", err)
	}
}

// receiveLoop continuously receives and processes messages from the server.
func (s *RealtimeSession) receiveLoop() {
	logger.Debug("OpenAI Realtime: receiveLoop started")
	defer func() {
		logger.Debug("OpenAI Realtime: receiveLoop exiting, closing responseCh")
		close(s.responseCh)
	}()

	msgCh := make(chan []byte, responseChannelSize)
	errCh := make(chan error, 1)

	go func() {
		errCh <- s.ws.ReceiveLoop(s.ctx, msgCh)
	}()

	for {
		select {
		case <-s.ctx.Done():
			logger.Debug("OpenAI Realtime: receiveLoop context done")
			return
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				connErr := fmt.Errorf("%w: %w", ErrConnectionLost, err)
				logger.Error("OpenAI Realtime: connection lost", "error", err)

				// Emit a terminal error chunk so callers reading
				// Response() learn the session died with a
				// distinguishable error type. errors.Is(chunk.Error,
				// ErrConnectionLost) returns true.
				reason := "error"
				select {
				case s.responseCh <- providers.StreamChunk{
					Error:        connErr,
					FinishReason: &reason,
				}:
				default:
				}

				// Propagate to errCh for callers using Error().
				select {
				case s.errCh <- connErr:
				default:
				}

				// Cancel the session context so Done() fires.
				// Callers watching Done() + Error() now have a
				// complete signal: Done fired, Error returns
				// ErrConnectionLost, Response channel has the
				// terminal chunk.
				s.cancel()
			} else {
				logger.Debug("OpenAI Realtime: WebSocket receive loop ended", "error", err)
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

// outputSampleRate returns the configured output sample rate, defaulting to 24kHz.
func (s *RealtimeSession) outputSampleRate() int {
	if s.config.OutputSampleRate > 0 {
		return s.config.OutputSampleRate
	}
	return DefaultRealtimeSampleRate
}

func (s *RealtimeSession) handleAudioDelta(e *ResponseAudioDeltaEvent) {
	rawBytes, err := base64.StdEncoding.DecodeString(e.Delta)
	if err != nil {
		logger.Warn("failed to decode base64 audio from OpenAI Realtime", "error", err)
		return
	}

	s.responseCh <- providers.StreamChunk{
		MediaData: &providers.StreamMediaData{
			Data:       rawBytes,
			MIMEType:   "audio/pcm",
			SampleRate: s.outputSampleRate(),
			Channels:   1,
		},
		Metadata: map[string]interface{}{
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
		Delta: e.Delta, // DuplexProviderStage accumulates this for output transcription
		Metadata: map[string]interface{}{
			"type":          "output_transcription",
			"item_id":       e.ItemID,
			"response_id":   e.ResponseID,
			"content_index": e.ContentIndex,
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
			"type":          "input_transcription",
			"transcription": e.Transcript, // DuplexProviderStage expects this key
			"item_id":       e.ItemID,
			"content_index": e.ContentIndex,
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

// gpt-realtime GA pricing per 1K tokens (USD). Audio costs an order of
// magnitude more than text — pricing one against the other vastly mis-
// estimates the bill. The wire `usage` payload includes a per-type
// breakdown (input_token_details / output_token_details), and we use it.
//
// Cached input is billed at the same per-1K rate as fresh input on the
// gpt-realtime GA model (10% discount for prompt-cached audio is folded
// in by the server already; we don't double-count).
//
// Source: https://platform.openai.com/docs/pricing (gpt-realtime row).
const (
	defaultAudioInputCostPer1K  = 0.032 // $32 / 1M
	defaultAudioOutputCostPer1K = 0.064 // $64 / 1M
	defaultTextInputCostPer1K   = 0.004 // $4  / 1M
	defaultTextOutputCostPer1K  = 0.016 // $16 / 1M
)

func (s *RealtimeSession) calculateCost(usage *UsageInfo) *types.CostInfo {
	if usage == nil {
		return nil
	}

	// Default to GA gpt-realtime rates. The provider config can override
	// the audio rates via inputCostPer1K / outputCostPer1K — those are
	// applied to AUDIO tokens, since that's where the bulk of the bill
	// lives in a realtime session.
	audioInRate := s.inputCostPer1K
	if audioInRate == 0 {
		audioInRate = defaultAudioInputCostPer1K
	}
	audioOutRate := s.outputCostPer1K
	if audioOutRate == 0 {
		audioOutRate = defaultAudioOutputCostPer1K
	}

	inAudio := usage.InputTokenDetails.AudioTokens
	inText := usage.InputTokenDetails.TextTokens
	outAudio := usage.OutputTokenDetails.AudioTokens
	outText := usage.OutputTokenDetails.TextTokens

	// Fall back to "all tokens are audio" if the breakdown is missing
	// (older API responses, mock providers, etc.) so we don't silently
	// report $0 — overcounting text at audio rates is preferable to
	// undercounting audio at text rates for a Realtime session.
	if inAudio == 0 && inText == 0 {
		inAudio = usage.InputTokens
	}
	if outAudio == 0 && outText == 0 {
		outAudio = usage.OutputTokens
	}

	inputCost := float64(inAudio)/tokensPerThousand*audioInRate +
		float64(inText)/tokensPerThousand*defaultTextInputCostPer1K
	outputCost := float64(outAudio)/tokensPerThousand*audioOutRate +
		float64(outText)/tokensPerThousand*defaultTextOutputCostPer1K

	return &types.CostInfo{
		InputTokens:   usage.InputTokens,
		OutputTokens:  usage.OutputTokens,
		InputCostUSD:  inputCost,
		OutputCostUSD: outputCost,
		TotalCost:     inputCost + outputCost,
	}
}
