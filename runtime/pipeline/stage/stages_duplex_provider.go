package stage

import (
	"encoding/base64"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// This file holds the pure, side-effect-free transforms used by the duplex
// provider stage (no session I/O or channel plumbing — those live in
// stages_duplex_provider_session.go).

// finishReasonMetaKey is the Message.Meta key carrying the turn's finish reason.
const finishReasonMetaKey = "finish_reason"

// finishReasonInterrupted marks a partial turn cut short by user barge-in.
const finishReasonInterrupted = "interrupted"

// buildAssistantParts assembles the assistant message content parts for the
// current turn: spoken text, then accumulated audio. Reasoning is NOT a content
// part — it lives on Message.Reasoning (see takeReasoning), off Parts, so it's
// excluded from content/exports/future context by construction.
func (s *DuplexProviderStage) buildAssistantParts(accumulatedText string) []types.ContentPart {
	parts := []types.ContentPart{}
	if accumulatedText != "" {
		text := accumulatedText
		parts = append(parts, types.ContentPart{Type: types.ContentTypeText, Text: &text})
	}
	if len(s.accumulatedMedia) > 0 {
		mediaData := base64.StdEncoding.EncodeToString(s.accumulatedMedia)
		parts = append(parts, types.ContentPart{
			Type:  types.ContentTypeAudio,
			Media: &types.MediaContent{Data: &mediaData, MIMEType: mimeTypeAudioPCM},
		})
	}
	return parts
}

// takeReasoning returns the accumulated reasoning trace for the turn, or nil if
// none. Attached to the assistant Message.Reasoning at each build site.
func (s *DuplexProviderStage) takeReasoning() *types.ReasoningTrace {
	text := s.accumulatedReasoning.String()
	if text == "" && len(s.accumulatedOpaqueReasoning) == 0 {
		return nil
	}
	return &types.ReasoningTrace{Text: text, Opaque: s.accumulatedOpaqueReasoning}
}

// chunkToElement converts a StreamChunk to a StreamElement.
// Creates a Message with role="assistant" for text and/or media responses.
// On the final chunk (with FinishReason), uses accumulated content from the entire turn.
//
// This function is PROVIDER-AGNOSTIC. It handles:
// - Interruptions: Captured as partial responses with finish_reason="interrupted"
// - Turn completion: Creates Message with accumulated content
// - Streaming content: Passes through text/audio for real-time display/playback
// turn-complete / streaming passthrough); splitting fragments the turn-handling
// logic. Relocated here unchanged from the integration file and now 98.7% covered.
//
//nolint:gocognit // Cohesive provider-agnostic chunk dispatcher (interruption /
func (s *DuplexProviderStage) chunkToElement(chunk *providers.StreamChunk) StreamElement {
	elem := StreamElement{}

	// Handle interruptions - provider detected user started speaking during response
	// Capture the partial response and signal turn completion
	if chunk.Interrupted {
		accumulatedText := s.accumulatedText.String()
		hasContent := accumulatedText != "" || len(s.accumulatedMedia) > 0

		logger.Debug("DuplexProviderStage: response interrupted",
			"accumulatedTextLen", len(accumulatedText),
			"accumulatedMediaLen", len(s.accumulatedMedia))

		// Create an interrupted assistant message if there's content
		if hasContent {
			msg := &types.Message{
				Role:    roleAssistant,
				Content: accumulatedText,
				Parts:   []types.ContentPart{},
				Meta: map[string]interface{}{
					finishReasonMetaKey: finishReasonInterrupted,
					"interrupted_at":    time.Now().Format(time.RFC3339Nano),
					"is_partial":        true,
				},
			}

			// Calculate turn latency if we have a start time
			if !s.turnStartTime.IsZero() {
				msg.LatencyMs = time.Since(s.turnStartTime).Milliseconds()
			}

			msg.Parts = s.buildAssistantParts(accumulatedText)
			msg.Reasoning = s.takeReasoning()

			elem.Message = msg
		}

		// Clear accumulated content - provider will start new response
		s.accumulatedText.Reset()
		s.accumulatedReasoning.Reset()
		s.accumulatedOpaqueReasoning = nil
		s.accumulatedMedia = nil

		// Mark that we saw an interruption - the next turnComplete without content should be skipped
		s.wasInterrupted = true

		interruptedReason := finishReasonInterrupted
		elem.Meta.Interrupted = true
		elem.Meta.FinishReason = &interruptedReason
		// Note: NOT setting EndOfStream - the provider will continue with new response
		return elem
	}

	// Add text if present (for real-time streaming display)
	if chunk.Content != "" {
		elem.Text = &chunk.Content
	}

	// Add audio if present (for real-time playback)
	if chunk.MediaData != nil && len(chunk.MediaData.Data) > 0 {
		sampleRate := chunk.MediaData.SampleRate
		if sampleRate == 0 {
			sampleRate = 24000
		}
		channels := chunk.MediaData.Channels
		if channels == 0 {
			channels = 1
		}
		elem.Audio = &AudioData{
			Samples:    chunk.MediaData.Data,
			SampleRate: sampleRate,
			Channels:   channels,
			Format:     AudioFormatPCM16,
		}
	}

	// Create Message on turn completion (FinishReason received)
	if chunk.FinishReason != nil && *chunk.FinishReason != "" {
		accumulatedText := s.accumulatedText.String()
		hasContent := accumulatedText != "" || len(s.accumulatedMedia) > 0
		hasCostInfo := chunk.CostInfo != nil && (chunk.CostInfo.InputTokens > 0 || chunk.CostInfo.OutputTokens > 0)

		// Create content preview for logging
		contentPreview := accumulatedText
		if len(contentPreview) > contentPreviewMaxLen {
			contentPreview = contentPreview[:contentPreviewMaxLen] + "..."
		}

		logger.Debug("DuplexProviderStage: turn complete",
			"finishReason", *chunk.FinishReason,
			"accumulatedTextLen", len(accumulatedText),
			"accumulatedTextPreview", contentPreview,
			"accumulatedMediaLen", len(s.accumulatedMedia),
			"hasContent", hasContent,
			"hasCostInfo", hasCostInfo,
			"wasInterrupted", s.wasInterrupted)

		// Handle "interrupted turn complete" - turnComplete with no content after an interruption.
		// This happens when Gemini detects a pause mid-utterance, starts responding, then more
		// audio arrives causing an interruption. Gemini sends turnComplete with no ModelTurn
		// for the interrupted response. We should skip this and wait for the real response.
		// Note: Gemini may still send cost info for interrupted turns, but without content
		// we should skip it - the real response will have its own cost info.
		if !hasContent && s.wasInterrupted {
			logger.Debug("DuplexProviderStage: detected interrupted turn complete (empty response after interruption), skipping",
				"hasCostInfo", hasCostInfo)
			finishReason := *chunk.FinishReason
			elem.Meta.InterruptedTurnComplete = true
			elem.Meta.FinishReason = &finishReason
			// Note: NOT setting EndOfStream - we're still waiting for the real response
			// Keep wasInterrupted true since we're still in the interrupted state
			return elem
		}

		// Check if there are tool calls to capture (accumulated across chunks
		// for providers that emit ToolCalls and FinishReason in separate chunks)
		toolCalls := s.accumulatedToolCalls
		if len(toolCalls) == 0 && len(chunk.ToolCalls) > 0 {
			toolCalls = chunk.ToolCalls
		}
		hasToolCalls := len(toolCalls) > 0

		// Create Message if there's content, cost info, or tool calls
		if hasContent || hasCostInfo || hasToolCalls {
			msg := &types.Message{
				Role:      roleAssistant,
				Content:   accumulatedText,
				Parts:     []types.ContentPart{},
				ToolCalls: toolCalls,
				CostInfo:  chunk.CostInfo,
				Meta: map[string]interface{}{
					finishReasonMetaKey: *chunk.FinishReason,
				},
			}

			// Calculate turn latency if we have a start time
			if !s.turnStartTime.IsZero() {
				msg.LatencyMs = time.Since(s.turnStartTime).Milliseconds()
				logger.Debug("DuplexProviderStage: calculated turn latency",
					"latencyMs", msg.LatencyMs)
			}

			if hasToolCalls {
				logger.Debug("DuplexProviderStage: captured tool calls",
					"count", len(chunk.ToolCalls))
			}

			msg.Parts = s.buildAssistantParts(accumulatedText)
			msg.Reasoning = s.takeReasoning()

			elem.Message = msg
			logger.Debug("DuplexProviderStage: created message for turn",
				"role", msg.Role,
				"contentLen", len(msg.Content),
				"partsCount", len(msg.Parts),
				"latencyMs", msg.LatencyMs)
		}
		elem.EndOfStream = true
		// Clear the interrupted flag - we got a real turn complete
		s.wasInterrupted = false
	}

	// Pop turn_id from queue on EndOfStream to keep queue in sync with turns.
	// This must happen regardless of whether transcription was captured, because
	// some turns may not produce transcription (e.g., short audio, no speech detected).
	// If we only pop when there's transcription, the queue gets out of sync and
	// subsequent transcriptions are correlated with the wrong user messages.
	//
	// We use turnIDPopped to prevent double-popping when there are multiple
	// EndOfStream events per turn (e.g., tool call response + final response).
	var turnID string
	if elem.EndOfStream && !s.turnIDPopped {
		s.turnIDMu.Lock()
		if len(s.turnIDQueue) > 0 {
			turnID = s.turnIDQueue[0]
			s.turnIDQueue = s.turnIDQueue[1:]
			s.turnIDPopped = true
		}
		s.turnIDMu.Unlock()
	}

	// Add input transcription to typed metadata if present.
	// Only add if we just popped a turn_id (turnID != ""), which means this is
	// the first EndOfStream for this user turn. Subsequent EndOfStream events
	// (e.g., after tool execution) should not re-add transcription.
	if s.inputTranscription.Len() > 0 && elem.EndOfStream && turnID != "" {
		elem.Meta.Transcription = &Transcription{Text: s.inputTranscription.String()}
		turnIDCopy := turnID
		elem.Meta.TurnID = &turnIDCopy
		logger.Debug("DuplexProviderStage: adding input transcription",
			"transcriptionLen", s.inputTranscription.Len(),
			"turnID", turnID)
	} else if elem.EndOfStream && turnID != "" {
		logger.Debug("DuplexProviderStage: turn complete without transcription",
			"turnID", turnID)
	}

	return elem
}
