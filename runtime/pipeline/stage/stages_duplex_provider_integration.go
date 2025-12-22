package stage

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	// Audio calculation constants
	// 16kHz 16-bit mono = 32000 bytes/sec (16000 samples/sec * 2 bytes/sample)
	audioBytesPerSecond16kHz = 32000.0
	// msPerSecFloat is milliseconds per second for audio position calculations.
	msPerSecFloat = 1000.0

	// Timeout for sending partial responses when context is canceled
	partialResponseSendTimeout = 500 * time.Millisecond

	// Maximum characters for content preview in logs
	contentPreviewMaxLen = 60

	// Default timeout for waiting for final response after input closes
	finalResponseTimeout = 30 * time.Second

	// MIME type for PCM audio
	mimeTypeAudioPCM = "audio/pcm"
)

// EndInputter is an optional interface for sessions that support explicit end-of-input signaling.
// This is primarily used by mock sessions to trigger responses after all audio has been sent.
type EndInputter interface {
	EndInput()
}

// DuplexProviderStage handles bidirectional streaming through a session.
// It forwards elements from input to the provider's session and forwards
// responses from the session to output.
//
// This stage is PROVIDER-AGNOSTIC. Provider-specific behaviors (interruptions,
// reconnection, protocol quirks) are handled BY the provider internally.
//
// System Prompt Handling:
// The first element received may contain a system prompt in metadata["system_prompt"].
// This is sent to the session via SendSystemContext() before processing audio/text.
//
// Response Accumulation:
// Streaming providers send text/audio responses in chunks. This stage accumulates
// content across chunks and creates a Message on turn completion (FinishReason).
//
// Session Closure:
// When the session closes unexpectedly, any accumulated content is emitted as a
// partial response. The executor is responsible for session recreation if needed.
//
// This is a Bidirectional stage: input elements ⟷ session ⟷ output elements
type DuplexProviderStage struct {
	BaseStage
	// Provider and config for lazy session creation
	provider   providers.StreamInputSupport
	baseConfig *providers.StreamingInputConfig

	// Session created on first element with system_prompt
	session          providers.StreamInputSession
	systemPromptSent bool

	// Response accumulation for the current turn
	// Reset when turn completes (FinishReason received)
	accumulatedText  strings.Builder
	accumulatedMedia []byte

	// Input transcription for the current turn (what user said)
	// Captured from provider's inputTranscription events (if supported)
	inputTranscription strings.Builder

	// Flag to track if we've already captured transcription for this turn
	transcriptionCaptured bool

	// Flag to track if we've already popped turn_id for this turn
	// This prevents double-popping when there are multiple EndOfStream events
	// (e.g., tool call response + final response)
	turnIDPopped bool

	// Channel to signal that input forwarding is done and we're waiting for final response
	inputDoneCh   chan struct{}
	inputDoneOnce sync.Once

	// Channel to signal that all responses have been received and we can skip finalResponseTimeout
	// This is used by synchronous turn executors that wait for each response before proceeding
	allResponsesReceivedCh   chan struct{}
	allResponsesReceivedOnce sync.Once

	// Turn ID queue for correlating transcription events with user messages
	turnIDQueue []string
	turnIDMu    sync.Mutex

	// Audio timing tracking for debugging streaming gaps
	audioStreamStart   time.Time
	lastAudioChunkTime time.Time
	audioChunkCount    int
	audioBytesSent     int

	// Track if we recently saw an interruption
	// Used to detect "interrupted turn complete" (turnComplete with no content after interruption)
	wasInterrupted bool
}

// NewDuplexProviderStage creates a new duplex provider stage.
// The session is created lazily when the first element arrives,
// using system_prompt from element metadata. This allows the pipeline
// to be the single source of truth for prompt assembly.
func NewDuplexProviderStage(
	provider providers.StreamInputSupport,
	baseConfig *providers.StreamingInputConfig,
) *DuplexProviderStage {
	return &DuplexProviderStage{
		BaseStage:              NewBaseStage("duplex_provider", StageTypeBidirectional),
		inputDoneCh:            make(chan struct{}),
		allResponsesReceivedCh: make(chan struct{}),
		provider:               provider,
		baseConfig:             baseConfig,
		systemPromptSent:       false,
	}
}

// Process implements the Stage interface.
// Handles bidirectional streaming between input channel and WebSocket session.
//
// For duplex streaming (Gemini Live API), this runs until:
// - Context is canceled (user stops the session)
// - Session response channel is closed (server ends session)
// - Input channel is closed (upstream ends)
//
// If no session is pre-configured, the session is created lazily when the first
// element arrives. The system_prompt from element metadata is used as the
// SystemInstruction for session creation.
func (s *DuplexProviderStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	// Create session lazily if not pre-configured
	if s.session == nil {
		if s.provider == nil {
			return errors.New("duplex provider stage: no provider or session configured")
		}

		// Wait for first element to get system_prompt from metadata
		firstElem, ok := <-input
		if !ok {
			return errors.New("duplex provider stage: input channel closed before receiving first element")
		}

		// Extract system_prompt from metadata
		systemPrompt := ""
		if firstElem.Metadata != nil {
			if sp, ok := firstElem.Metadata["system_prompt"].(string); ok {
				systemPrompt = sp
			}
		}

		// Create session config with system instruction
		sessionConfig := s.baseConfig
		if sessionConfig == nil {
			sessionConfig = &providers.StreamingInputConfig{}
		}
		sessionConfig.SystemInstruction = systemPrompt

		// Extract tools from metadata if provided
		// Tools are passed from the duplex executor via element metadata
		if firstElem.Metadata != nil {
			if tools, ok := firstElem.Metadata["tools"].([]providers.StreamingToolDefinition); ok {
				sessionConfig.Tools = tools
				logger.Debug("DuplexProviderStage: tools extracted from metadata", "tool_count", len(tools))
			}
		}

		logger.Debug("DuplexProviderStage: creating session with system instruction",
			"system_prompt_length", len(systemPrompt))

		// Buffer elements during session creation to avoid losing data
		// when upstream stages close their output channels before session connects.
		// This is critical for providers with slow session creation (e.g., OpenAI WebSocket ~600ms).
		//
		// We buffer until we see EndOfStream (which marks end of first turn input),
		// or until session creation completes (for fast providers like mocks).
		// Subsequent turn elements will be read directly by forwardInputElements.
		bufferedElements := []StreamElement{firstElem}
		bufferMu := &sync.Mutex{}
		drainDone := make(chan struct{})
		sessionCreated := make(chan struct{})

		// If the first element already has EndOfStream, skip buffering entirely
		// This is common in tests and when there's only one element to send
		if firstElem.EndOfStream {
			logger.Debug("DuplexProviderStage: first element has EndOfStream, skipping buffer drain")
			close(drainDone)
		} else {
			go func() {
				logger.Debug("DuplexProviderStage: drain goroutine started")
				defer close(drainDone)
				for {
					select {
					case <-ctx.Done():
						logger.Debug("DuplexProviderStage: drain goroutine canceled by context")
						return
					case <-sessionCreated:
						// Session is ready - stop buffering and let forwardInputElements handle the rest
						bufferMu.Lock()
						logger.Debug("DuplexProviderStage: session created, stopping buffer",
							"bufferedCount", len(bufferedElements))
						bufferMu.Unlock()
						return
					case elem, ok := <-input:
						if !ok {
							bufferMu.Lock()
							logger.Debug("DuplexProviderStage: input channel closed during buffering",
								"bufferedCount", len(bufferedElements))
							bufferMu.Unlock()
							return
						}
						bufferMu.Lock()
						logger.Debug("DuplexProviderStage: buffered element",
							"hasAudio", elem.Audio != nil,
							"hasText", elem.Text != nil,
							"endOfStream", elem.EndOfStream,
							"bufferedCount", len(bufferedElements)+1)
						bufferedElements = append(bufferedElements, elem)
						isEOS := elem.EndOfStream
						bufferMu.Unlock()
						// Stop buffering on EndOfStream - the first turn's input is complete
						if isEOS {
							logger.Debug("DuplexProviderStage: EndOfStream received, stopping buffer")
							return
						}
					}
				}
			}()
		}

		// Create the session (may take several hundred milliseconds for WebSocket connection)
		var err error
		s.session, err = s.provider.CreateStreamSession(ctx, sessionConfig)
		if err != nil {
			// Wait for drain goroutine to complete before returning
			<-drainDone
			return fmt.Errorf("duplex provider stage: failed to create session: %w", err)
		}
		logger.Debug("DuplexProviderStage: session created")
		defer s.session.Close()
		s.systemPromptSent = true // System instruction sent at session creation

		// Signal drain goroutine that session is ready - it can stop buffering
		close(sessionCreated)

		// Wait for buffering to complete (stops at EndOfStream or sessionCreated)
		<-drainDone
		bufferMu.Lock()
		numBuffered := len(bufferedElements)
		bufferMu.Unlock()
		logger.Debug("DuplexProviderStage: replaying buffered elements",
			"count", numBuffered)

		// Re-inject buffered elements followed by original input for subsequent turns
		input = s.replayAndMerge(ctx, bufferedElements, input)
	} else {
		// Session was pre-configured, ensure it's closed on exit
		defer s.session.Close()
	}

	logger.Debug("DuplexProviderStage: starting bidirectional streaming")

	// Create cancellable context for input forwarding
	// This allows us to stop input forwarding when response forwarding ends
	inputCtx, cancelInput := context.WithCancel(ctx)
	defer cancelInput()

	// Channel to signal when input forwarding is done
	inputDone := make(chan error, 1)

	// Start input forwarding goroutine
	// Pass output channel so Message elements can be forwarded for state store capture
	go s.forwardInputElements(inputCtx, input, output, inputDone)

	// Forward responses from session to output (blocks until complete)
	responseErr := s.forwardResponseElements(ctx, output)

	// Cancel input forwarding context to signal the goroutine to stop
	cancelInput()

	// Wait for input forwarding to complete with a short grace period
	// The goroutine should exit quickly since we canceled its context
	const gracePeriod = 100 * time.Millisecond
	select {
	case inputErr := <-inputDone:
		// Don't override response error with context.Canceled from our cancellation
		if inputErr != nil && !errors.Is(inputErr, context.Canceled) && responseErr == nil {
			responseErr = inputErr
		}
	case <-time.After(gracePeriod):
		// Very short timeout - goroutine should have exited by now
		logger.Debug("DuplexProviderStage: input forwarding cleanup completed")
	}

	return responseErr
}

// forwardInputElements forwards elements from input channel to session.
// It also sends Message elements to the output channel for state store capture.
//
// When the input channel closes, this goroutine signals that input is done but does NOT
// close the session. This allows the response goroutine to wait for the final response
// with a timeout. The session is closed by Process() after forwardResponseElements completes.
func (s *DuplexProviderStage) forwardInputElements(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
	done chan error,
) {
	for {
		select {
		case <-ctx.Done():
			done <- ctx.Err()
			return

		case elem, ok := <-input:
			if !ok {
				// Signal that input is done - we may be waiting for final response
				// Don't close session here - let forwardResponseElements wait with timeout
				logger.Debug("DuplexProviderStage: input channel closed, signaling input done")
				s.inputDoneOnce.Do(func() {
					close(s.inputDoneCh)
				})
				done <- nil
				return
			}

			// Check for "all_responses_received" signal from executor
			// This tells us that all expected responses have been received synchronously
			// and we can skip the finalResponseTimeout when input closes
			if elem.Metadata != nil {
				if allReceived, ok := elem.Metadata["all_responses_received"].(bool); ok && allReceived {
					logger.Debug("DuplexProviderStage: all responses received signal, will skip final timeout")
					s.allResponsesReceivedOnce.Do(func() {
						close(s.allResponsesReceivedCh)
					})
					continue // Don't forward this signal element
				}

				// Check for tool result messages to forward to output for state store capture
				// These are created by the executor after executing tools requested by the model
				if toolMsgs, ok := elem.Metadata["tool_result_messages"].([]types.Message); ok && len(toolMsgs) > 0 {
					logger.Debug("DuplexProviderStage: forwarding tool result messages to output",
						"count", len(toolMsgs))
					for i := range toolMsgs {
						toolElem := StreamElement{
							Message:     &toolMsgs[i],
							EndOfStream: false,
						}
						select {
						case output <- toolElem:
							logger.Debug("DuplexProviderStage: tool result message forwarded",
								"tool", toolMsgs[i].ToolResult.Name)
						case <-ctx.Done():
							done <- ctx.Err()
							return
						}
					}
					// Continue to process the element (may also have tool_responses for provider)
				}
			}

			// Forward Message elements to output for state store capture
			if elem.Message != nil {
				logger.Debug("DuplexProviderStage: forwarding user message to output", "role", elem.Message.Role)
				// Extract turn_id for correlating transcription events
				if turnID, ok := elem.Metadata["turn_id"].(string); ok && turnID != "" {
					s.turnIDMu.Lock()
					s.turnIDQueue = append(s.turnIDQueue, turnID)
					logger.Debug("DuplexProviderStage: pushed turn ID to queue",
						"turn_id", turnID, "queueLen", len(s.turnIDQueue))
					s.turnIDMu.Unlock()
				}
				select {
				case output <- elem:
				case <-ctx.Done():
					done <- ctx.Err()
					return
				}
			}

			s.sendElementToSession(ctx, &elem)
		}
	}
}

// replayAndMerge creates a channel that yields buffered elements followed by remaining input.
// This is used after buffering during session creation to replay buffered elements
// and then forward any subsequent elements from the original input channel.
func (s *DuplexProviderStage) replayAndMerge(
	ctx context.Context,
	elements []StreamElement,
	remaining <-chan StreamElement,
) <-chan StreamElement {
	merged := make(chan StreamElement)
	go func() {
		defer close(merged)
		// First, replay buffered elements
		for i := range elements {
			select {
			case merged <- elements[i]:
			case <-ctx.Done():
				return
			}
		}
		// Then, forward remaining elements from original input
		for elem := range remaining {
			select {
			case merged <- elem:
			case <-ctx.Done():
				return
			}
		}
	}()
	return merged
}

// sendElementToSession sends a single element to the WebSocket session.
func (s *DuplexProviderStage) sendElementToSession(ctx context.Context, elem *StreamElement) {
	// Check for tool responses to send back to the provider
	// Tool responses are sent by the executor after executing tools requested by the model
	if elem.Metadata != nil {
		if toolResponses, ok := elem.Metadata["tool_responses"].([]providers.ToolResponse); ok && len(toolResponses) > 0 {
			// Check if session supports tool responses
			if toolSession, ok := s.session.(providers.ToolResponseSupport); ok {
				logger.Debug("DuplexProviderStage: sending tool responses to session",
					"count", len(toolResponses))
				if err := toolSession.SendToolResponses(ctx, toolResponses); err != nil {
					logger.Error("DuplexProviderStage: failed to send tool responses", "error", err)
				} else {
					logger.Debug("DuplexProviderStage: tool responses sent successfully")
				}
			} else {
				logger.Warn("DuplexProviderStage: session does not support tool responses",
					"sessionType", fmt.Sprintf("%T", s.session))
			}
			return
		}
	}

	// Check for system prompt in metadata (sent once at the start)
	if !s.systemPromptSent && elem.Metadata != nil {
		if systemPrompt, ok := elem.Metadata["system_prompt"].(string); ok && systemPrompt != "" {
			logger.Debug("DuplexProviderStage: sending system prompt as context (no turn_complete)",
				"promptLength", len(systemPrompt))
			// Use SendSystemContext to send prompt WITHOUT triggering a response
			// This allows the system prompt to be applied before audio input starts
			if err := s.session.SendSystemContext(ctx, systemPrompt); err != nil {
				logger.Error("DuplexProviderStage: failed to send system prompt", "error", err)
			}
			s.systemPromptSent = true
		}
	}

	// Process audio/text content BEFORE handling EndOfStream
	// This ensures content in the final element is sent before signaling end of input
	if elem.Audio != nil && len(elem.Audio.Samples) > 0 {
		s.sendAudioElement(ctx, elem)
	} else if elem.Text != nil && *elem.Text != "" {
		s.sendTextElement(ctx, elem)
	}

	// Check for end of stream (end of turn input) AFTER processing content
	if elem.EndOfStream {
		logger.Debug("DuplexProviderStage: end of stream signal received",
			"audio_chunks_sent", s.audioChunkCount,
			"audio_bytes_sent", s.audioBytesSent,
		)
		// Reset audio timing tracking for next turn
		s.audioChunkCount = 0
		s.audioBytesSent = 0
		s.lastAudioChunkTime = time.Time{}

		// Signal end of input to session to trigger model response
		// This is critical for pre-recorded audio files without trailing silence
		if endInputter, ok := s.session.(EndInputter); ok {
			logger.Debug("DuplexProviderStage: calling EndInput() to trigger response")
			endInputter.EndInput()
			logger.Debug("DuplexProviderStage: EndInput() completed")
		} else {
			logger.Debug("DuplexProviderStage: session does not implement EndInputter",
				"sessionType", fmt.Sprintf("%T", s.session))
		}
	}
}

// sendAudioElement sends an audio element to the session.
func (s *DuplexProviderStage) sendAudioElement(ctx context.Context, elem *StreamElement) {
	// Reset transcription accumulator when starting a new user turn
	// This prevents late-arriving transcription chunks from the previous turn
	// from being mixed with this turn's transcription
	if s.transcriptionCaptured {
		s.inputTranscription.Reset()
		s.transcriptionCaptured = false
		s.turnIDPopped = false // Reset so we can pop turn_id for the new turn
		logger.Debug("DuplexProviderStage: reset transcription for new user turn")
	}

	now := time.Now()

	// Track timing for first chunk of a new audio stream
	if s.audioChunkCount == 0 || s.lastAudioChunkTime.IsZero() {
		s.audioStreamStart = now
		s.audioChunkCount = 0
		s.audioBytesSent = 0
		logger.Debug("DuplexProviderStage: starting new audio stream")
	}

	// Calculate gap from last chunk
	var gapFromLastMs int64
	if !s.lastAudioChunkTime.IsZero() {
		gapFromLastMs = now.Sub(s.lastAudioChunkTime).Milliseconds()
	}

	s.audioChunkCount++
	s.audioBytesSent += len(elem.Audio.Samples)

	// Log every 50th chunk or if there's a suspicious gap (>30ms for 20ms chunks)
	if s.audioChunkCount%50 == 1 || gapFromLastMs > 30 {
		// Calculate expected audio position based on bytes sent
		expectedAudioPosMs := float64(s.audioBytesSent) / audioBytesPerSecond16kHz * msPerSecFloat
		logger.Debug("DuplexProviderStage: audio chunk received",
			"chunk_idx", s.audioChunkCount,
			"audio_pos_ms", fmt.Sprintf("%.1f", expectedAudioPosMs),
			"elapsed_ms", now.Sub(s.audioStreamStart).Milliseconds(),
			"gap_from_last_ms", gapFromLastMs,
			"chunk_bytes", len(elem.Audio.Samples),
		)
	}

	s.lastAudioChunkTime = now

	mediaChunk := &types.MediaChunk{
		Data:      elem.Audio.Samples,
		Timestamp: now,
		IsLast:    false,
	}

	if err := s.session.SendChunk(ctx, mediaChunk); err != nil {
		logger.Error("DuplexProviderStage: failed to send chunk to session", "error", err)
	}
}

// sendTextElement sends a text element to the session.
func (s *DuplexProviderStage) sendTextElement(ctx context.Context, elem *StreamElement) {
	text := *elem.Text

	logger.Debug("DuplexProviderStage: forwarding text to session", "length", len(text))

	if err := s.session.SendText(ctx, text); err != nil {
		logger.Error("DuplexProviderStage: failed to send text to session", "error", err)
	}
}

// forwardResponseElements forwards responses from session to output channel.
// For duplex streaming (Gemini Live API), this runs until:
// - Context is canceled
// - Session response channel is closed
// Note: We do NOT exit on FinishReason/turnComplete - that's just a turn
// boundary in Gemini Live API, not end of session.
func (s *DuplexProviderStage) forwardResponseElements(
	ctx context.Context,
	output chan<- StreamElement,
) error {
	responseChannel := s.session.Response()
	if responseChannel == nil {
		return errors.New("session response channel is nil")
	}

	// Timer for final response timeout (starts after input is done)
	var finalResponseTimer <-chan time.Time
	inputDone := false
	weClosedSession := false // Track who closed session for clear logging

	for {
		select {
		case <-ctx.Done():
			logger.Info("SESSION CLOSURE: Context canceled (user or timeout)")
			// Emit any accumulated content as partial response before returning
			accumulatedText := s.accumulatedText.String()
			hasContent := accumulatedText != "" || len(s.accumulatedMedia) > 0
			if hasContent {
				msg := &types.Message{
					Role:    "assistant",
					Content: accumulatedText,
					Parts:   []types.ContentPart{},
					Meta: map[string]interface{}{
						"finish_reason": "complete",
					},
				}

				if accumulatedText != "" {
					msg.Parts = append(msg.Parts, types.ContentPart{
						Type: types.ContentTypeText,
						Text: &accumulatedText,
					})
				}

				if len(s.accumulatedMedia) > 0 {
					mediaData := base64.StdEncoding.EncodeToString(s.accumulatedMedia)
					msg.Parts = append(msg.Parts, types.ContentPart{
						Type: types.ContentTypeAudio,
						Media: &types.MediaContent{
							Data:     &mediaData,
							MIMEType: mimeTypeAudioPCM,
						},
					})
				}

				elem := StreamElement{
					Message:     msg,
					EndOfStream: true,
				}

				logger.Debug("DuplexProviderStage: emitting response on context cancel",
					"textLen", len(accumulatedText),
					"mediaLen", len(s.accumulatedMedia))

				// Use a short timeout for sending - the downstream stage needs a chance to receive
				// even though context is canceled. This is critical for capturing partial responses.
				// NOSONAR: Intentional background context - main ctx is canceled, need fresh context
				sendCtx, sendCancel := context.WithTimeout(context.Background(), partialResponseSendTimeout)
				select {
				case output <- elem:
					logger.Debug("DuplexProviderStage: response sent successfully on context cancel")
				case <-sendCtx.Done():
					logger.Warn("DuplexProviderStage: timeout sending response on context cancel")
				}
				sendCancel()

				// Clear accumulators
				s.accumulatedText.Reset()
				s.accumulatedMedia = nil
			}
			return ctx.Err()

		case <-s.inputDoneCh:
			// Input forwarding is done - check if we should wait for final response
			if !inputDone {
				inputDone = true

				// Check if all responses were already received synchronously
				// (signaled by executor via "all_responses_received" metadata)
				select {
				case <-s.allResponsesReceivedCh:
					// All responses received - close session immediately, no need to wait
					logger.Info("INPUT COMPLETE: All responses already received, closing session immediately")
					weClosedSession = true
					if s.session != nil {
						_ = s.session.Close()
					}
					// Continue to drain any remaining chunks
				default:
					// Not all responses received - wait for final response with timeout
					logger.Info("INPUT COMPLETE: All turns sent, waiting for final response",
						"timeout", finalResponseTimeout)
					finalResponseTimer = time.After(finalResponseTimeout)
				}
			}

		case <-finalResponseTimer:
			// Timeout waiting for final response - close session and emit partial content
			logger.Warn("SESSION CLOSURE: WE are closing (timeout waiting for final response)",
				"timeout", finalResponseTimeout)
			weClosedSession = true
			if s.session != nil {
				_ = s.session.Close()
			}
			// Continue to process any remaining chunks until channel closes

		case chunk, ok := <-responseChannel:
			if !ok {
				// Session closed - emit any accumulated content as partial response
				accumulatedText := s.accumulatedText.String()
				hasContent := accumulatedText != "" || len(s.accumulatedMedia) > 0

				if weClosedSession {
					logger.Info("SESSION ENDED: Response channel closed (we initiated closure)")
				} else {
					logger.Warn("SESSION CLOSURE: GEMINI closed the connection (unexpected)",
						"hasAccumulatedContent", hasContent,
						"accumulatedTextLen", len(accumulatedText),
						"accumulatedMediaLen", len(s.accumulatedMedia))
				}

				if hasContent {
					// Emit response so it's not lost
					msg := &types.Message{
						Role:    "assistant",
						Content: accumulatedText,
						Parts:   []types.ContentPart{},
						Meta: map[string]interface{}{
							"finish_reason": "complete",
						},
					}

					if accumulatedText != "" {
						msg.Parts = append(msg.Parts, types.ContentPart{
							Type: types.ContentTypeText,
							Text: &accumulatedText,
						})
					}

					if len(s.accumulatedMedia) > 0 {
						mediaData := base64.StdEncoding.EncodeToString(s.accumulatedMedia)
						msg.Parts = append(msg.Parts, types.ContentPart{
							Type: types.ContentTypeAudio,
							Media: &types.MediaContent{
								Data:     &mediaData,
								MIMEType: mimeTypeAudioPCM,
							},
						})
					}

					elem := StreamElement{
						Message:     msg,
						EndOfStream: true,
					}

					logger.Debug("DuplexProviderStage: emitting response on session close",
						"textLen", len(accumulatedText),
						"mediaLen", len(s.accumulatedMedia))

					select {
					case output <- elem:
					case <-ctx.Done():
						return ctx.Err()
					}

					// Clear accumulators
					s.accumulatedText.Reset()
					s.accumulatedMedia = nil
				}

				return nil
			}

			if err := s.handleResponseChunk(ctx, &chunk, output); err != nil {
				return err
			}

			// Log turn completions but DON'T exit - keep session running
			// Gemini Live API's turnComplete is a turn marker, not end of session
			if chunk.FinishReason != nil && *chunk.FinishReason != "" {
				logger.Debug("DuplexProviderStage: turn completed", "reason", *chunk.FinishReason)
			}
		}
	}
}

// handleResponseChunk processes and forwards a single response chunk.
// It accumulates text and media content across chunks within a turn,
// then resets the accumulation when the turn completes.
func (s *DuplexProviderStage) handleResponseChunk(
	ctx context.Context,
	chunk *providers.StreamChunk,
	output chan<- StreamElement,
) error {
	if chunk.Error != nil {
		logger.Error("DuplexProviderStage: chunk error from session", "error", chunk.Error)
		elem := NewErrorElement(chunk.Error)
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
		return chunk.Error
	}

	// Accumulate text content for this turn
	// For Gemini Live API, text comes via three paths:
	// 1. outputTranscription: Delta only, with metadata["type"] == "output_transcription"
	//    This is incremental transcription of model audio - APPEND to accumulated text
	// 2. inputTranscription: metadata["type"] == "input_transcription" with metadata["transcription"]
	//    This is transcription of user audio input - stored separately
	// 3. ModelTurn: Both Content and Delta set to the same value (partial streaming text)
	//    Use ONLY Content to avoid duplication
	//
	// Check metadata to distinguish the source
	isOutputTranscription := false
	isInputTranscription := false
	if chunk.Metadata != nil {
		if metaType, ok := chunk.Metadata["type"].(string); ok {
			switch metaType {
			case "output_transcription":
				isOutputTranscription = true
			case "input_transcription":
				isInputTranscription = true
			}
		}
	}

	// Handle input transcription (what user said)
	// Only accumulate if we haven't already captured transcription for this turn
	// This prevents late-arriving transcription chunks from bleeding into the next turn
	if isInputTranscription && !s.transcriptionCaptured {
		if transcript, ok := chunk.Metadata["transcription"].(string); ok && transcript != "" {
			s.inputTranscription.WriteString(transcript)
			logger.Debug("DuplexProviderStage: captured input transcription",
				"transcriptLen", len(transcript),
				"totalInputTranscriptLen", s.inputTranscription.Len())
		}
	} else if isInputTranscription && s.transcriptionCaptured {
		logger.Debug("DuplexProviderStage: ignoring late transcription chunk (turn already complete)")
	}

	if isOutputTranscription && chunk.Delta != "" {
		// outputTranscription sends incremental text - append it
		s.accumulatedText.WriteString(chunk.Delta)
	} else if chunk.Content != "" {
		// ModelTurn text - use Content (Delta is the same, so we only use one)
		s.accumulatedText.WriteString(chunk.Content)
	}

	// Accumulate media content for this turn
	// MediaDelta.Data is base64-encoded, so decode it to raw bytes for accumulation
	if chunk.MediaDelta != nil && chunk.MediaDelta.Data != nil {
		rawBytes, err := base64.StdEncoding.DecodeString(*chunk.MediaDelta.Data)
		if err != nil {
			logger.Warn("DuplexProviderStage: failed to decode base64 audio chunk", "error", err)
		} else {
			s.accumulatedMedia = append(s.accumulatedMedia, rawBytes...)
		}
	}

	// Convert chunk to element (uses accumulated content for final chunk)
	elem := s.chunkToElement(chunk)

	// Reset accumulation after turn completes (after creating the element)
	// BUT: if this was an interrupted turn completion (no EndOfStream), don't reset -
	// we need to accumulate the real response content that comes next
	if chunk.FinishReason != nil && *chunk.FinishReason != "" && elem.EndOfStream {
		s.accumulatedText.Reset()
		s.accumulatedMedia = nil
		// Mark transcription as captured - don't reset yet!
		// Late-arriving transcription chunks will be ignored until a new user turn starts.
		// The transcription buffer will be reset when sendAudioElement is called.
		s.transcriptionCaptured = true
	}

	logger.Debug("DuplexProviderStage: forwarding response element",
		"hasText", elem.Text != nil,
		"hasAudio", elem.Audio != nil,
		"hasMessage", elem.Message != nil,
		"endOfStream", elem.EndOfStream)

	select {
	case output <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// chunkToElement converts a StreamChunk to a StreamElement.
// Creates a Message with role="assistant" for text and/or media responses.
// On the final chunk (with FinishReason), uses accumulated content from the entire turn.
//
// This function is PROVIDER-AGNOSTIC. It handles:
// - Interruptions: Captured as partial responses with finish_reason="interrupted"
// - Turn completion: Creates Message with accumulated content
// - Streaming content: Passes through text/audio for real-time display/playback
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
				Role:    "assistant",
				Content: accumulatedText,
				Parts:   []types.ContentPart{},
				Meta: map[string]interface{}{
					"finish_reason":  "interrupted",
					"interrupted_at": time.Now().Format(time.RFC3339Nano),
					"is_partial":     true,
				},
			}

			if accumulatedText != "" {
				msg.Parts = append(msg.Parts, types.ContentPart{
					Type: types.ContentTypeText,
					Text: &accumulatedText,
				})
			}

			if len(s.accumulatedMedia) > 0 {
				mediaData := base64.StdEncoding.EncodeToString(s.accumulatedMedia)
				msg.Parts = append(msg.Parts, types.ContentPart{
					Type: types.ContentTypeAudio,
					Media: &types.MediaContent{
						Data:     &mediaData,
						MIMEType: mimeTypeAudioPCM,
					},
				})
			}

			elem.Message = msg
		}

		// Clear accumulated content - provider will start new response
		s.accumulatedText.Reset()
		s.accumulatedMedia = nil

		// Mark that we saw an interruption - the next turnComplete without content should be skipped
		s.wasInterrupted = true

		elem.Metadata = map[string]interface{}{
			"interrupted":   true,
			"finish_reason": "interrupted",
		}
		// Note: NOT setting EndOfStream - the provider will continue with new response
		return elem
	}

	// Add text if present (for real-time streaming display)
	if chunk.Content != "" {
		elem.Text = &chunk.Content
	}

	// Add audio if present (for real-time playback)
	if chunk.MediaDelta != nil && chunk.MediaDelta.Data != nil {
		audioData := []byte(*chunk.MediaDelta.Data)
		elem.Audio = &AudioData{
			Samples:    audioData,
			SampleRate: 24000, // Default - could be extracted from metadata
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
			elem.Metadata = map[string]interface{}{
				"interrupted_turn_complete": true,
				"finish_reason":             *chunk.FinishReason,
			}
			// Note: NOT setting EndOfStream - we're still waiting for the real response
			// Keep wasInterrupted true since we're still in the interrupted state
			return elem
		}

		// Check if there are tool calls to capture
		hasToolCalls := len(chunk.ToolCalls) > 0

		// Create Message if there's content, cost info, or tool calls
		if hasContent || hasCostInfo || hasToolCalls {
			msg := &types.Message{
				Role:      "assistant",
				Content:   accumulatedText,
				Parts:     []types.ContentPart{},
				ToolCalls: chunk.ToolCalls, // Capture tool calls from the chunk
				CostInfo:  chunk.CostInfo,
				Meta: map[string]interface{}{
					"finish_reason": *chunk.FinishReason,
				},
			}

			if hasToolCalls {
				logger.Debug("DuplexProviderStage: captured tool calls",
					"count", len(chunk.ToolCalls))
			}

			if accumulatedText != "" {
				msg.Parts = append(msg.Parts, types.ContentPart{
					Type: types.ContentTypeText,
					Text: &accumulatedText,
				})
			}

			if len(s.accumulatedMedia) > 0 {
				mediaData := base64.StdEncoding.EncodeToString(s.accumulatedMedia)
				msg.Parts = append(msg.Parts, types.ContentPart{
					Type: types.ContentTypeAudio,
					Media: &types.MediaContent{
						Data:     &mediaData,
						MIMEType: mimeTypeAudioPCM,
					},
				})
			}

			elem.Message = msg
			logger.Debug("DuplexProviderStage: created message for turn",
				"role", msg.Role,
				"contentLen", len(msg.Content),
				"partsCount", len(msg.Parts))
		}
		elem.EndOfStream = true
		// Clear the interrupted flag - we got a real turn complete
		s.wasInterrupted = false
	}

	// Add metadata from chunk
	if chunk.Metadata != nil {
		elem.Metadata = chunk.Metadata
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

	// Add input transcription to metadata if present.
	// Only add if we just popped a turn_id (turnID != ""), which means this is
	// the first EndOfStream for this user turn. Subsequent EndOfStream events
	// (e.g., after tool execution) should not re-add transcription.
	if s.inputTranscription.Len() > 0 && elem.EndOfStream && turnID != "" {
		if elem.Metadata == nil {
			elem.Metadata = make(map[string]interface{})
		}
		elem.Metadata["input_transcription"] = s.inputTranscription.String()
		elem.Metadata["transcription_turn_id"] = turnID
		logger.Debug("DuplexProviderStage: adding input transcription",
			"transcriptionLen", s.inputTranscription.Len(),
			"turnID", turnID)
	} else if elem.EndOfStream && turnID != "" {
		logger.Debug("DuplexProviderStage: turn complete without transcription",
			"turnID", turnID)
	}

	return elem
}
