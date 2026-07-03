package gemini

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// finishReasonComplete is the finish reason indicating the turn completed normally.
const finishReasonComplete = "complete"

// audioPCMMime is the MIME type for PCM audio used by Gemini Live API.
const audioPCMMime = "audio/pcm"

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

// receiveLoop continuously receives messages from the WebSocket
func (s *StreamSession) receiveLoop() {
	// Signal the pump that no more chunks are coming and let it drain its queue
	// and close Response() before this goroutine returns.
	defer func() {
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
	if logger.DefaultLogger == nil {
		return
	}

	var logMsg map[string]interface{}
	_ = json.Unmarshal(rawMsg, &logMsg)

	var keys []string
	for k := range logMsg {
		keys = append(keys, k)
	}

	truncateInlineData(logMsg)
	logBytes, _ := json.MarshalIndent(logMsg, "", "  ")
	logger.DefaultLogger.Debug("Gemini message", "keys", keys, "content", string(logBytes))
}

// processServerMessage processes a message from the server
func (s *StreamSession) processServerMessage(msg *ServerMessage) error {
	if msg.SetupComplete != nil {
		return nil
	}

	if msg.ToolCall != nil && len(msg.ToolCall.FunctionCalls) > 0 {
		return s.handleToolCalls(msg.ToolCall)
	}

	costInfo := s.buildCostInfo(msg.UsageMetadata)

	if msg.ServerContent == nil {
		return s.sendCostInfoIfPresent(costInfo)
	}

	return s.processServerContent(msg.ServerContent, costInfo)
}

// sendCostInfoIfPresent sends a chunk with cost info if present.
func (s *StreamSession) sendCostInfoIfPresent(costInfo *types.CostInfo) error {
	if costInfo == nil {
		return nil
	}
	return s.sendChunk(&providers.StreamChunk{CostInfo: costInfo})
}

// sendChunk hands a chunk to the shared pump (via the session-owned emitCh),
// which drains it to Response() without back-pressuring this receive loop.
func (s *StreamSession) sendChunk(chunk *providers.StreamChunk) error {
	select {
	case s.emitCh <- *chunk:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// processServerContent handles server content including transcriptions and model turns.
func (s *StreamSession) processServerContent(content *ServerContent, costInfo *types.CostInfo) error {
	if content.Interrupted {
		// Barge-in handled by the shared StreamPump (consistent with every
		// provider): fire the out-of-band signal so a paced consumer flushes
		// immediately, drop the interrupted response's queued audio, and skip
		// still-arriving audio until the turn completes — plus the in-band
		// Interrupted chunk for the pipeline.
		s.Barge()
		return s.sendChunk(&providers.StreamChunk{Interrupted: true})
	}

	// A completed turn (clean, or the canceled one after barge-in) ends the
	// audio-drop window: any subsequent audio belongs to the next response.
	if content.TurnComplete {
		s.ClearDrop()
	}

	if err := s.handleInputTranscription(content); err != nil {
		return err
	}

	if err := s.handleOutputTranscription(content); err != nil {
		return err
	}

	if content.TurnComplete && content.ModelTurn == nil {
		finishReason := finishReasonComplete
		return s.sendChunk(&providers.StreamChunk{
			FinishReason: &finishReason,
			CostInfo:     costInfo,
		})
	}

	if content.ModelTurn != nil {
		return s.processModelTurn(content.ModelTurn, content.TurnComplete, costInfo)
	}

	return nil
}

// handleInputTranscription sends input transcription if present.
func (s *StreamSession) handleInputTranscription(content *ServerContent) error {
	if content.InputTranscription == nil || content.InputTranscription.Text == "" {
		return nil
	}
	return s.sendChunk(&providers.StreamChunk{
		Metadata: map[string]interface{}{
			"type":          "input_transcription",
			"transcription": content.InputTranscription.Text,
			"turn_complete": content.TurnComplete,
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
			"type":          "output_transcription",
			"turn_complete": content.TurnComplete,
		},
	})
}

// processModelTurn processes a model turn from the server
func (s *StreamSession) processModelTurn(turn *ModelTurn, turnComplete bool, costInfo *types.CostInfo) error {
	response := providers.StreamChunk{
		Content: "",
	}

	// Extract text and audio from parts
	for _, part := range turn.Parts {
		if part.Text != "" {
			if part.Thought {
				// Reasoning, not spoken output — surface separately so it doesn't
				// leak into the transcript (the spoken text arrives via
				// outputTranscription).
				response.Reasoning += part.Text
			} else {
				response.Content += part.Text
				response.Delta = part.Text
			}
		}

		// Handle audio/media data. While the pump is dropping (post-barge-in,
		// until the turn completes), skip audio so the interrupted response stops
		// playing — but keep any text so a partial transcript still flows.
		if part.InlineData != nil && s.Dropping() && strings.HasPrefix(part.InlineData.MimeType, "audio/") {
			continue
		}
		if part.InlineData != nil {
			// Decode base64 at source — downstream receives raw bytes
			rawBytes, decodeErr := base64.StdEncoding.DecodeString(part.InlineData.Data)
			if decodeErr != nil {
				logger.Warn("failed to decode base64 media from Gemini", "error", decodeErr)
			} else {
				media := &providers.StreamMediaData{
					Data:     rawBytes,
					MIMEType: part.InlineData.MimeType,
				}
				if strings.HasPrefix(part.InlineData.MimeType, "audio/") {
					media.SampleRate = s.outputSampleRate()
					media.Channels = 1
				}
				response.MediaData = media
			}
		}
	}

	// Mark turn completion and include cost info
	if turnComplete {
		finishReason := finishReasonComplete
		response.FinishReason = &finishReason
		response.CostInfo = costInfo
	}

	// Send response to the shared pump via the session-owned emitCh.
	select {
	case s.emitCh <- response:
		s.sequenceNum++
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// buildClientMessage builds a realtime input message with media chunk.
// For images/video, uses the format from the TypeScript SDK:
//
//	session.sendRealtimeInput({ media: { data: base64Data, mimeType: 'image/jpeg' } })
//
// Which translates to wire format: { "realtimeInput": { "media": { "data": "...", "mimeType": "..." } } }
// For audio, uses the legacy format with media_chunks array.
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
			"realtimeInput": map[string]interface{}{
				"media": map[string]interface{}{
					"data":     base64Data,
					"mimeType": mimeType,
				},
			},
		}
	}

	// For audio, use PCM encoder with legacy format
	encoder := NewAudioEncoder()
	var err error
	base64Data, err = encoder.EncodePCM(chunk.Data)
	if err != nil {
		// If encoding fails, use empty string (should not happen with valid PCM data)
		base64Data = ""
	}

	return map[string]interface{}{
		"realtime_input": map[string]interface{}{
			"media_chunks": []map[string]interface{}{
				{
					"mime_type": audioPCMMime,
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
