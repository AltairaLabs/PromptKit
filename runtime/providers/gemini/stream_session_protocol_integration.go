package gemini

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// finishReasonComplete is the finish reason for a turn the model completed
// normally, in the canonical vocabulary every provider reports.
const finishReasonComplete = types.FinishReasonStop

// audioPCMMime is the MIME type for PCM audio used by Gemini Live API.
const audioPCMMime = "audio/pcm"

// audioPCMMimeRate is the audio input MIME type for the current Gemini Live wire
// format, which requires the sample rate. Gemini Live input is 16 kHz mono PCM16.
const audioPCMMimeRate = "audio/pcm;rate=16000"

// Gemini Live client-message wire keys.
const (
	liveKeyClientContent      = "client_content"
	liveKeyTurns              = "turns"
	liveKeyTurnComplete       = "turn_complete"
	liveKeyRealtimeInput      = "realtime_input"
	liveKeyRealtimeInputCamel = "realtimeInput"
)

// StreamChunk metadata keys for transcription chunks.
const (
	metaKeyType         = "type"
	metaKeyTurnComplete = "turn_complete"
	// metaKeyTurnCompleteReason carries Gemini's turnCompleteReason on the
	// chunk that ends the turn.
	metaKeyTurnCompleteReason = "turn_complete_reason"
)

// defaultOutputSampleRate is the default output sample rate for Gemini native audio models (24kHz).
const defaultOutputSampleRate = 24000

// outputSampleRate returns the configured output sample rate, defaulting to 24kHz.
func (s *StreamSession) outputSampleRate() int {
	if s.config.OutputSampleRate > 0 {
		return s.config.OutputSampleRate
	}
	return defaultOutputSampleRate
}

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
		if data, ok := val[wireKeyData].(string); ok && len(data) > 100 {
			val[wireKeyData] = fmt.Sprintf("[%d bytes base64]", len(data))
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

// receiveLoop continuously receives messages from the WebSocket
func (s *StreamSession) receiveLoop() {
	// Signal the pump that no more chunks are coming and let it drain its queue
	// and close Response() before this goroutine returns.
	defer func() {
		s.flushCarriedUsage()
		close(s.emitCh)
		s.Wait()
	}()

	for {
		select {
		case <-s.ctx.Done():
			logger.Debug("StreamSession: receiveLoop exiting due to context done")
			return
		default:
			if s.receiveAndProcessMessage() {
				return
			}
		}
	}
}

// receiveAndProcessMessage receives a single message and processes it.
// Returns true if the receive loop should exit.
func (s *StreamSession) receiveAndProcessMessage() bool {
	var rawMsg json.RawMessage
	if err := s.ws.Receive(s.ctx, &rawMsg); err != nil {
		return s.handleReceiveError(err)
	}

	var serverMsg ServerMessage
	if err := json.Unmarshal(rawMsg, &serverMsg); err != nil {
		s.sendError(fmt.Errorf("failed to parse server message: %w", err))
		return false // Continue to next message
	}

	s.logRawMessage(rawMsg)

	if err := s.processServerMessage(&serverMsg); err != nil {
		s.sendError(err)
		return true // Exit on processing error
	}

	return false
}

// handleReceiveError handles WebSocket receive errors.
// Returns true if the receive loop should exit.
func (s *StreamSession) handleReceiveError(err error) bool {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()

	if closed {
		logger.Debug("StreamSession: receiveLoop exiting due to intentional close")
		return true
	}

	errMsg := err.Error()
	isTimeout := strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "deadline")

	s.logReceiveError(err, errMsg, isTimeout)

	if s.autoReconnect && !isTimeout && s.reconnect() {
		logger.Info("StreamSession: continuing after successful reconnection")
		return false
	}

	s.sendError(fmt.Errorf("gemini websocket error: %w", err))
	return true
}

// logReceiveError logs details about a WebSocket receive error.
func (s *StreamSession) logReceiveError(err error, errMsg string, isTimeout bool) {
	isEOF := strings.Contains(errMsg, "EOF") || strings.Contains(errMsg, "unexpected EOF")
	isCloseErr := strings.Contains(errMsg, "close") || strings.Contains(errMsg, "websocket")

	logger.Warn("StreamSession: WebSocket receive error",
		"error", err,
		"isEOF", isEOF,
		"isCloseError", isCloseErr,
		"isTimeout", isTimeout,
		"autoReconnect", s.autoReconnect)
}

// sendError sends an error to the error channel without blocking.
func (s *StreamSession) sendError(err error) {
	select {
	case s.errCh <- err:
	default:
		logger.Debug("StreamSession: error channel full, dropping error")
	}
}

// logRawMessage logs the raw WebSocket message for debugging.
func (s *StreamSession) logRawMessage(rawMsg json.RawMessage) {
	var logMsg map[string]interface{}
	_ = json.Unmarshal(rawMsg, &logMsg)

	var keys []string
	for k := range logMsg {
		keys = append(keys, k)
	}

	truncateInlineData(logMsg)
	logBytes, _ := json.MarshalIndent(logMsg, "", "  ")
	logger.Debug("Gemini message", "keys", keys, "content", string(logBytes))
}

// processServerMessage processes a message from the server
func (s *StreamSession) processServerMessage(msg *ServerMessage) error {
	if msg.SetupComplete != nil {
		return nil
	}

	// Usage (live_usage.go): Gemini Live reports each generation's own usage
	// on the turnComplete that ends it.
	if msg.ToolCall != nil && len(msg.ToolCall.FunctionCalls) > 0 {
		if msg.UsageMetadata != nil {
			// Never seen live; the generation's turnComplete reports it, so
			// counting it here too would count it twice.
			logger.Debug("StreamSession: ignoring usage on a toolCall message")
		}
		s.noteToolCall(msg.ToolCall)
		if !s.turnOpen {
			s.gen++ // the tool calls are a generation of their own
		}
		return s.handleToolCalls(msg.ToolCall)
	}

	s.noteToolGenerationComplete(msg.ServerContent)
	if s.carryToolCallUsage(msg) {
		return nil
	}

	if msg.ServerContent == nil {
		return s.usageOnly(msg.UsageMetadata)
	}

	s.holdUsage(msg.UsageMetadata)
	return s.processServerContent(msg.ServerContent)
}

// carryToolCallUsage reports the tool-call generation Gemini ends with a
// turnComplete that has no content, and reports whether msg was that
// (endsToolGeneration, live_tool_turn.go). Its usage is reported at once as a
// usage-only chunk for that generation, which the stage reports on the
// tool-call provider call (provider.call.usage); reported as a turn of its
// own, it would read as an empty reply after every tool call. Otherwise the
// turnComplete is the reply's completion, which can be empty: a model that
// reports no tool-call turnComplete (Gemini 2.5 Live) reports the usage of
// the whole server turn on one turnComplete after the reply.
func (s *StreamSession) carryToolCallUsage(msg *ServerMessage) bool {
	sc := msg.ServerContent
	if sc != nil && hasModelOutput(sc) {
		s.toolCallTurn = false // the model is answering: the turn is the reply's
		s.toolGenEnding = false
		return false
	}
	if !s.toolCallTurn || sc == nil || !sc.TurnComplete {
		return false
	}
	s.toolCallTurn = false
	ends := s.endsToolGeneration()
	s.toolGenEnding = false
	if !ends {
		// The reply's completion, possibly empty. An empty one is still a
		// generation of its own: the tool-call generation already ended.
		if !s.turnOpen {
			s.gen++
		}
		return false
	}
	s.ClearDrop()
	if err := s.handleInputTranscription(sc); err != nil {
		logger.Debug("StreamSession: input transcription not delivered", "error", err)
	}
	if err := s.usageOnly(msg.UsageMetadata); err != nil {
		logger.Debug("StreamSession: tool-call turn usage not delivered", "error", err)
	}
	return true
}

// holdUsage adds a server message's usage to the usage held for the chunk
// that ends the turn. Receive goroutine only.
func (s *StreamSession) holdUsage(usage *UsageMetadata) {
	if usage == nil {
		return
	}
	tokens := usage.tokens()
	if s.carriedUsage != nil {
		tokens = addTokenUsage(tokens, *s.carriedUsage)
	}
	s.carriedUsage = &tokens
}

// takeTurnCost prices the usage held for the turn and clears it; nil when
// none is held. Receive goroutine only.
func (s *StreamSession) takeTurnCost() *types.CostInfo {
	if s.carriedUsage == nil {
		return nil
	}
	ci := base.PriceUsage(s.pricing, s.providerID, base.ProviderTypeInference, *s.carriedUsage, nil, 0)
	s.carriedUsage = nil
	return &ci
}

// flushCarriedUsage reports usage no turn's end took when the session ends,
// without blocking: the receive loop is
// exiting. Receive goroutine only.
func (s *StreamSession) flushCarriedUsage() {
	if s.carriedUsage == nil {
		return
	}
	select {
	case s.emitCh <- providers.StreamChunk{CostInfo: s.takeTurnCost(), Generation: s.gen}:
	default:
		logger.Warn("StreamSession: a turn's end was not reported at session end: the response queue is full")
	}
}

// addTokenUsage returns the field-wise sum of a and b's token counts. Gemini
// usage sets no Extra units.
func addTokenUsage(a, b base.TokenUsage) base.TokenUsage {
	return base.TokenUsage{
		Input: a.Input + b.Input, CacheRead: a.CacheRead + b.CacheRead, CacheWrite: a.CacheWrite + b.CacheWrite,
		Output: a.Output + b.Output, Reasoning: a.Reasoning + b.Reasoning,
		AudioInput: a.AudioInput + b.AudioInput, AudioOutput: a.AudioOutput + b.AudioOutput,
		ImageInput: a.ImageInput + b.ImageInput, VideoInput: a.VideoInput + b.VideoInput,
		ImageOutput: a.ImageOutput + b.ImageOutput,
	}
}

// sendChunk hands a chunk to the shared pump (via the session-owned emitCh),
// which drains it to Response() without back-pressuring this receive loop.
func (s *StreamSession) sendChunk(chunk *providers.StreamChunk) error {
	chunk.Generation = s.gen
	select {
	case s.emitCh <- *chunk:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// processServerContent handles server content including transcriptions and model turns.
func (s *StreamSession) processServerContent(content *ServerContent) error {
	if content.Interrupted {
		// Barge-in handled by the shared StreamPump (consistent with every
		// provider): fire the out-of-band signal so a paced consumer flushes
		// immediately, drop the interrupted response's queued audio, and skip
		// still-arriving audio until the turn completes — plus the in-band
		// Interrupted chunk for the pipeline.
		s.withPlayout(func(p *audio.Playout) { p.Stop() })
		s.turnOpen = false // the interruption ends the turn; later usage is its
		s.Barge()
		marker := providers.BargeMarker()
		if err := s.sendChunk(&marker); err != nil {
			return err
		}
		// The interruption ends the turn: it carries the usage held for it.
		return s.sendChunk(&providers.StreamChunk{Interrupted: true, CostInfo: s.takeTurnCost()})
	}

	// A completed turn (clean, or the canceled one after barge-in) ends the
	// audio-drop window: any subsequent audio belongs to the next response.
	if content.TurnComplete {
		s.ClearDrop()
	}
	if content.ModelTurn != nil || content.OutputTranscription != nil {
		s.replyStarted()
	}

	if err := s.bargeInAfterReply(content); err != nil {
		return err
	}
	if err := s.handleInputTranscription(content); err != nil {
		return err
	}
	if content.TurnComplete {
		defer s.turnEnded()
	}

	if err := s.handleOutputTranscription(content); err != nil {
		return err
	}

	if content.TurnComplete && content.ModelTurn == nil {
		chunk := providers.StreamChunk{CostInfo: s.takeTurnCost()}
		setTurnComplete(&chunk, content.TurnCompleteReason)
		return s.sendChunk(&chunk)
	}

	if content.ModelTurn != nil {
		if err := s.processModelTurn(content); err != nil {
			return err
		}
	}
	return s.handleGenerationComplete(content)
}

// handleGenerationComplete reports a reply's generationComplete, which comes
// about a reply's playing time before its turnComplete, as a chunk marked
// GenerationComplete: nothing more of the reply follows. One that arrives
// with or after the turn's completion is not reported, the completion says as
// much, nor is a tool-call generation's, whose tool calls end its turn.
func (s *StreamSession) handleGenerationComplete(content *ServerContent) error {
	if !content.GenerationComplete || content.TurnComplete || !s.turnOpen {
		return nil
	}
	return s.sendChunk(&providers.StreamChunk{GenerationComplete: true})
}

// handleInputTranscription sends input transcription if present.
func (s *StreamSession) handleInputTranscription(content *ServerContent) error {
	if content.InputTranscription == nil || content.InputTranscription.Text == "" {
		return nil
	}
	return s.sendChunk(&providers.StreamChunk{
		Metadata: map[string]interface{}{
			metaKeyType:         "input_transcription",
			"transcription":     content.InputTranscription.Text,
			metaKeyTurnComplete: content.TurnComplete,
		},
	})
}

// handleOutputTranscription sends output transcription if present.
func (s *StreamSession) handleOutputTranscription(content *ServerContent) error {
	if content.OutputTranscription == nil || content.OutputTranscription.Text == "" {
		return nil
	}
	return s.sendChunk(&providers.StreamChunk{
		Delta: content.OutputTranscription.Text,
		Metadata: map[string]interface{}{
			metaKeyType:         "output_transcription",
			metaKeyTurnComplete: content.TurnComplete,
		},
	})
}

// setTurnComplete marks chunk as the end of the turn: its finish reason from
// the turnCompleteReason, which is kept under turn_complete_reason when set.
func setTurnComplete(chunk *providers.StreamChunk, reason string) {
	finishReason := liveTurnFinishReason(reason)
	chunk.FinishReason = &finishReason
	if reason == "" {
		return
	}
	if chunk.Metadata == nil {
		chunk.Metadata = map[string]interface{}{}
	}
	chunk.Metadata[metaKeyTurnCompleteReason] = reason
}

// processModelTurn processes a server content's model turn.
func (s *StreamSession) processModelTurn(content *ServerContent) error {
	turn := content.ModelTurn
	response := providers.StreamChunk{
		Content: "",
	}

	// Extract text and audio from parts
	for _, part := range turn.Parts {
		appendTurnText(&response, &part)

		// Handle audio/media data. While the pump is dropping (post-barge-in,
		// until the turn completes), skip audio so the interrupted response stops
		// playing — but keep any text so a partial transcript still flows.
		if part.InlineData != nil && s.Dropping() && strings.HasPrefix(part.InlineData.MimeType, "audio/") {
			continue
		}
		if part.InlineData != nil {
			if media := s.decodeTurnMedia(part.InlineData); media != nil {
				response.MediaData = media
				s.countDeliveredAudio(audio.PCM16Duration(len(media.Data), media.SampleRate, media.Channels))
			}
		}
	}

	// Mark turn completion and include the turn's cost
	if content.TurnComplete {
		setTurnComplete(&response, content.TurnCompleteReason)
		response.CostInfo = s.takeTurnCost()
	}

	// Send response to the shared pump via the session-owned emitCh.
	response.Generation = s.gen
	select {
	case s.emitCh <- response:
		s.sequenceNum++
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// appendTurnText adds a model turn part's text to the response chunk.
func appendTurnText(response *providers.StreamChunk, part *Part) {
	if part.Text == "" {
		return
	}
	if part.Thought {
		// Reasoning, not spoken output — surface separately so it doesn't
		// leak into the transcript (the spoken text arrives via
		// outputTranscription).
		response.Reasoning += part.Text
		return
	}
	response.Content += part.Text
	response.Delta = part.Text
}

// decodeTurnMedia decodes a model turn's inline media. Base64 is decoded at
// source so downstream receives raw bytes; a decode failure is logged and
// yields nil.
func (s *StreamSession) decodeTurnMedia(data *InlineData) *providers.StreamMediaData {
	rawBytes, decodeErr := base64.StdEncoding.DecodeString(data.Data)
	if decodeErr != nil {
		logger.Warn("failed to decode base64 media from Gemini", "error", decodeErr)
		return nil
	}
	media := &providers.StreamMediaData{
		Data:     rawBytes,
		MIMEType: data.MimeType,
	}
	if strings.HasPrefix(data.MimeType, "audio/") {
		media.SampleRate = s.outputSampleRate()
		media.Channels = 1
	}
	return media
}

// buildClientMessage builds a realtime input message with media chunk.
// For images/video, uses the format from the TypeScript SDK:
//
//	session.sendRealtimeInput({ media: { data: base64Data, mimeType: 'image/jpeg' } })
//
// Which translates to wire format: { "realtimeInput": { "media": { "data": "...", "mimeType": "..." } } }
// For audio, uses the current realtimeInput.audio field. The legacy
// realtime_input.media_chunks form is deprecated and closes current Gemini Live
// models (e.g. gemini-*-live) with websocket close 1007 (#1666).
func buildClientMessage(chunk types.MediaChunk, _ bool) map[string]interface{} {
	// Determine MIME type from metadata or default to audio/pcm
	mimeType := audioPCMMime
	if chunk.Metadata != nil {
		if mt := chunk.Metadata["mime_type"]; mt != "" {
			mimeType = mt
		}
	}

	var base64Data string

	// Handle different media types
	if strings.HasPrefix(mimeType, "image/") || strings.HasPrefix(mimeType, "video/") {
		// For images/video, use the format from TypeScript SDK:
		// sendRealtimeInput({ media: { data: base64Data, mimeType: 'image/jpeg' } })
		base64Data = base64.StdEncoding.EncodeToString(chunk.Data)

		// Use camelCase keys to match the TypeScript SDK / protobuf JSON encoding
		return map[string]interface{}{
			liveKeyRealtimeInputCamel: map[string]interface{}{
				"media": map[string]interface{}{
					wireKeyData:     base64Data,
					wireKeyMimeType: mimeType,
				},
			},
		}
	}

	// For audio, use the current realtimeInput.audio field (camelCase, matching
	// the image/video branch and the protobuf JSON encoding). Gemini Live requires
	// the sample rate in the mimeType.
	encoder := NewAudioEncoder()
	var err error
	base64Data, err = encoder.EncodePCM(chunk.Data)
	if err != nil {
		// If encoding fails, use empty string (should not happen with valid PCM data)
		base64Data = ""
	}

	return map[string]interface{}{
		liveKeyRealtimeInputCamel: map[string]interface{}{
			"audio": map[string]interface{}{
				wireKeyData:     base64Data,
				wireKeyMimeType: audioPCMMimeRate,
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
		liveKeyClientContent: map[string]interface{}{
			liveKeyTurns: []map[string]interface{}{
				{
					wireKeyRole:  roleUser,
					wireKeyParts: []interface{}{part},
				},
			},
			liveKeyTurnComplete: turnComplete,
		},
	}
}
