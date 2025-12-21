package gemini

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// finishReasonComplete is the finish reason indicating the turn completed normally.
const finishReasonComplete = "complete"

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
	defer close(s.responseCh)

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

// handleToolCalls processes tool calls from the server.
func (s *StreamSession) handleToolCalls(toolCall *ToolCallMsg) error {
	toolCalls := make([]types.MessageToolCall, len(toolCall.FunctionCalls))
	for i, fc := range toolCall.FunctionCalls {
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

	finishReason := "tool_calls"
	response := providers.StreamChunk{
		ToolCalls:    toolCalls,
		FinishReason: &finishReason,
	}

	if err := s.sendChunk(&response); err != nil {
		return err
	}
	logger.Debug("Gemini tool calls emitted", "count", len(toolCalls))
	return nil
}

// buildCostInfo creates cost information from usage metadata.
func (s *StreamSession) buildCostInfo(usage *UsageMetadata) *types.CostInfo {
	if usage == nil {
		return nil
	}

	inputTokens := usage.PromptTokenCount
	outputTokens := usage.ResponseTokenCount

	var inputCostUSD, outputCostUSD, totalCost float64
	if s.inputCostPer1K > 0 && s.outputCostPer1K > 0 {
		inputCostUSD = float64(inputTokens) / tokensPerThousand * s.inputCostPer1K
		outputCostUSD = float64(outputTokens) / tokensPerThousand * s.outputCostPer1K
		totalCost = inputCostUSD + outputCostUSD
	}

	return &types.CostInfo{
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		InputCostUSD:  inputCostUSD,
		OutputCostUSD: outputCostUSD,
		TotalCost:     totalCost,
	}
}

// sendCostInfoIfPresent sends a chunk with cost info if present.
func (s *StreamSession) sendCostInfoIfPresent(costInfo *types.CostInfo) error {
	if costInfo == nil {
		return nil
	}
	return s.sendChunk(&providers.StreamChunk{CostInfo: costInfo})
}

// sendChunk sends a chunk to the response channel.
func (s *StreamSession) sendChunk(chunk *providers.StreamChunk) error {
	select {
	case s.responseCh <- *chunk:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// processServerContent handles server content including transcriptions and model turns.
func (s *StreamSession) processServerContent(content *ServerContent, costInfo *types.CostInfo) error {
	if content.Interrupted {
		return s.sendChunk(&providers.StreamChunk{Interrupted: true})
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
		finishReason := finishReasonComplete
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
