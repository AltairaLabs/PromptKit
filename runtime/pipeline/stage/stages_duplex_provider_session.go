package stage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
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

	// turnState is the per-Turn shared state; SystemPrompt is sourced
	// from TurnState.SystemPrompt at session-creation time. Nil-safe
	// (the stage uses an empty system prompt when not wired).
	turnState *TurnState

	// Session created on first element with system_prompt
	session          providers.StreamInputSession
	systemPromptSent bool

	// Response accumulation for the current turn
	// Reset when turn completes (FinishReason received)
	accumulatedText            strings.Builder
	accumulatedReasoning       strings.Builder         // reasoning/thinking text (kept off spoken content)
	accumulatedOpaqueReasoning []types.OpaqueReasoning // provider round-trip reasoning tokens
	accumulatedMedia           []byte
	accumulatedToolCalls       []types.MessageToolCall

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

	// Turn latency tracking - when the current turn started
	// Set when user input is received, used to calculate LatencyMs on turn complete
	turnStartTime time.Time

	// Event emitter for recording audio events (optional, for session recording)
	emitter *events.Emitter

	// onSession, when set, is called once with the streaming session right after
	// it is created (lazily, on the first element). Consumers use it to reach the
	// live session (e.g. its BargeIn() channel), which only exists once created.
	// Set via SetSessionObserver.
	onSession func(providers.StreamInputSession)
}

// SetSessionObserver registers a callback invoked once with the streaming
// session immediately after it is created. It is optional; nil means no
// observer. Used by the interactive console to wire barge-in (the session's
// out-of-band BargeIn() channel) to playback flushing.
func (s *DuplexProviderStage) SetSessionObserver(fn func(providers.StreamInputSession)) {
	s.onSession = fn
}

// NewDuplexProviderStage creates a new duplex provider stage. The session
// is created lazily when the first element arrives. Prefer
// NewDuplexProviderStageWithTurnState in production so the system prompt
// is sourced from the per-Turn shared state.
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

// NewDuplexProviderStageWithEmitter creates a new duplex provider stage with event emission support.
// The emitter is used to emit audio.input and audio.output events for session recording.
func NewDuplexProviderStageWithEmitter(
	provider providers.StreamInputSupport,
	baseConfig *providers.StreamingInputConfig,
	emitter *events.Emitter,
) *DuplexProviderStage {
	s := NewDuplexProviderStage(provider, baseConfig)
	s.emitter = emitter
	return s
}

// NewDuplexProviderStageWithTurnState creates a new duplex provider stage
// that sources system_prompt from the shared *TurnState. The emitter
// remains optional.
func NewDuplexProviderStageWithTurnState(
	provider providers.StreamInputSupport,
	baseConfig *providers.StreamingInputConfig,
	emitter *events.Emitter,
	turnState *TurnState,
) *DuplexProviderStage {
	s := NewDuplexProviderStage(provider, baseConfig)
	s.emitter = emitter
	s.turnState = turnState
	return s
}

// Process implements the Stage interface.
// Handles bidirectional streaming between input channel and WebSocket session.
//
// For duplex streaming (Gemini Live API), this runs until:
// - Context is canceled (user stops the session)
// - Session response channel is closed (server ends session)
// - Input channel is closed (upstream ends)
//
// If no session is pre-configured, the session is created lazily when the
// first element arrives. The system_prompt from TurnState (when wired) is
// used as the SystemInstruction for session creation.
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

		// Wait for first element to get system_prompt from TurnState
		firstElem, ok := <-input
		if !ok {
			return errors.New("duplex provider stage: input channel closed before receiving first element")
		}

		systemPrompt := ""
		if s.turnState != nil {
			systemPrompt = s.turnState.SystemPrompt
		}

		// Create session config with system instruction
		sessionConfig := s.baseConfig
		if sessionConfig == nil {
			sessionConfig = &providers.StreamingInputConfig{}
		}
		sessionConfig.SystemInstruction = systemPrompt

		logger.Debug("DuplexProviderStage: creating session with system instruction",
			"system_prompt_length", len(systemPrompt))

		// Buffer elements during session creation to avoid losing data
		// when upstream stages close their output channels before session connects.
		// This is critical for providers with slow session creation (e.g., OpenAI WebSocket ~600ms).
		//
		// We buffer until we see EndOfStream (which marks end of first turn input),
		// or until session creation completes (for fast providers like mocks).
		// Subsequent turn elements will be read directly by forwardInputElements.
		// Bounded: the drain goroutine below reads the input channel directly,
		// which removes the pipeline's backpressure, so an unpaced producer would
		// otherwise accumulate without limit for as long as session creation takes.
		buffer := newPreSessionBuffer(defaultPreSessionBufferBytes)
		buffer.add(firstElem)
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
							"bufferedCount", buffer.len())
						bufferMu.Unlock()
						return
					case elem, ok := <-input:
						if !ok {
							bufferMu.Lock()
							logger.Debug("DuplexProviderStage: input channel closed during buffering",
								"bufferedCount", buffer.len())
							bufferMu.Unlock()
							return
						}
						bufferMu.Lock()
						buffer.add(elem)
						logger.Debug("DuplexProviderStage: buffered element",
							"hasAudio", elem.Audio != nil,
							"hasText", elem.Text != nil,
							"endOfStream", elem.EndOfStream,
							"bufferedCount", buffer.len(),
							"bufferedAudioBytes", buffer.bytes())
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
		if s.onSession != nil {
			s.onSession(s.session)
		}

		// Signal drain goroutine that session is ready - it can stop buffering
		close(sessionCreated)

		// Wait for buffering to complete (stops at EndOfStream or sessionCreated)
		<-drainDone
		bufferMu.Lock()
		numBuffered := buffer.len()
		bufferMu.Unlock()
		logger.Debug("DuplexProviderStage: replaying buffered elements",
			"count", numBuffered)

		// Re-inject buffered elements followed by original input for subsequent turns
		input = s.replayAndMerge(ctx, buffer.elements(), input)
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
				//
				// IMPORTANT — debugging "long delay between user-turn end
				// and provider's turnComplete signal" with provider-native
				// (ASM) turn detection:
				//
				// Do NOT add a session.CompleteTurn() call here when the
				// scenario uses turn_detection.mode = "asm". CompleteTurn
				// is the *client-side* turn-end signal, used only for
				// VAD/manual turn-control mode. In ASM mode the provider
				// (e.g. Gemini) decides turn boundaries from the audio
				// stream content itself.
				//
				// What ASM expects: a continuous audio stream (silence
				// inclusive). It detects end-of-turn when it observes
				// silence *in the audio*. If we stop sending audio
				// packets entirely after the user utterance, ASM has no
				// audio to apply VAD to and falls back to a wall-clock
				// timeout (~6s observed empirically with Gemini).
				//
				// The fix for that delay, if you ever need to chase it,
				// is upstream — keep the audio stream alive with a tail
				// of silent packets after the user utterance ends, long
				// enough for ASM to detect end-of-turn from content. It
				// is NOT a CompleteTurn call here. See also
				// runtime/providers/gemini/stream_session_integration.go
				// (CompleteTurn doc) for the same warning.
				logger.Debug("DuplexProviderStage: input channel closed, signaling input done")
				s.inputDoneOnce.Do(func() {
					close(s.inputDoneCh)
				})
				done <- nil
				return
			}

			// Inbound audio is activity: reset the pipeline idle timer so a live
			// conversation is not killed by the 30s idle timeout while the user is
			// speaking. Without this every streaming voice session dies at ~30s.
			ResetIdleFromContext(ctx)

			// Check for "all responses received" signal from executor.
			// This tells us that all expected responses have been received synchronously
			// and we can skip the finalResponseTimeout when input closes.
			if elem.Meta.AllResponsesReceived {
				logger.Debug("DuplexProviderStage: all responses received signal, will skip final timeout")
				s.allResponsesReceivedOnce.Do(func() {
					close(s.allResponsesReceivedCh)
				})
				// A graceful drain (Close) sends this on an EndOfStream element to
				// end input AND close the session immediately — otherwise draining a
				// live streaming session blocks the full finalResponseTimeout (~30s),
				// because the provider's response channel never closes on its own. A
				// plain end-of-turn EndOfStream (with input payload) does NOT set this
				// flag, so it still waits briefly for the turn's response.
				if elem.EndOfStream {
					logger.Debug("DuplexProviderStage: drain signal; ending input for prompt close")
					s.inputDoneOnce.Do(func() {
						close(s.inputDoneCh)
					})
					done <- nil
					return
				}
				continue // executor mid-stream signal; keep forwarding
			}

			// Check for tool result messages to forward to output for state store capture.
			// These are created by the executor after executing tools requested by the model.
			if toolMsgs := elem.Meta.ToolResultMessages; len(toolMsgs) > 0 {
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
				// Continue to process the element (may also have tool responses for provider)
			}

			// Forward Message elements to output for state store capture
			if elem.Message != nil {
				logger.Debug("DuplexProviderStage: forwarding user message to output", "role", elem.Message.Role)
				// Track turn start time for latency calculation on user messages
				if elem.Message.Role == roleUser {
					s.turnStartTime = time.Now()
					logger.Debug("DuplexProviderStage: started turn timing",
						"turnStartTime", s.turnStartTime.Format(time.RFC3339Nano))
				}
				// Extract turn_id for correlating transcription events
				if elem.Meta.TurnID != nil && *elem.Meta.TurnID != "" {
					turnID := *elem.Meta.TurnID
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
	// Check for tool responses to send back to the provider.
	// Tool responses are sent by the executor after executing tools requested by the model.
	// Falls through to the rest of the function so the same element can also carry
	// EndOfStream or other content — we don't want to silently drop those.
	if toolResponses := elem.Meta.ToolResponses; len(toolResponses) > 0 {
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
	}

	// Source the system prompt from TurnState.
	if !s.systemPromptSent {
		systemPrompt := ""
		if s.turnState != nil {
			systemPrompt = s.turnState.SystemPrompt
		}
		if systemPrompt != "" {
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

	// Process media/text content BEFORE handling EndOfStream
	// This ensures content in the final element is sent before signaling end of input
	if elem.Audio != nil && len(elem.Audio.Samples) > 0 {
		s.sendAudioElement(ctx, elem)
	} else if elem.Video != nil && len(elem.Video.Data) > 0 {
		s.sendVideoElement(ctx, elem)
	} else if elem.Image != nil && len(elem.Image.Data) > 0 {
		s.sendImageElement(ctx, elem)
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
			logger.Debug("DuplexProviderStage: marking input done (provider decides what to send — no-op in ASM mode)")
			endInputter.EndInput()
			logger.Debug("DuplexProviderStage: input-done signal handled by provider")
		} else {
			logger.Debug("DuplexProviderStage: session does not implement EndInputter",
				"sessionType", fmt.Sprintf("%T", s.session))
		}
	}
}

// sendAudioElement sends an audio element to the session.
func (s *DuplexProviderStage) sendAudioElement(ctx context.Context, elem *StreamElement) {
	// Reset transcription accumulator and chunk counter when starting a new user turn
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

// sendVideoElement sends a video element to the session.
// Video frames/chunks are sent with high priority for low-latency streaming.
func (s *DuplexProviderStage) sendVideoElement(ctx context.Context, elem *StreamElement) {
	logger.Debug("DuplexProviderStage: forwarding video to session",
		"bytes", len(elem.Video.Data),
		"mime_type", elem.Video.MIMEType,
		"is_key_frame", elem.Video.IsKeyFrame,
		"frame_num", elem.Video.FrameNum,
	)

	mediaChunk := &types.MediaChunk{
		Data:      elem.Video.Data,
		Timestamp: elem.Video.Timestamp,
		IsLast:    false,
		Metadata: map[string]string{
			"mime_type":    elem.Video.MIMEType,
			"is_key_frame": fmt.Sprintf("%v", elem.Video.IsKeyFrame),
		},
	}

	if err := s.session.SendChunk(ctx, mediaChunk); err != nil {
		logger.Error("DuplexProviderStage: failed to send video chunk to session", "error", err)
	}
}

// sendImageElement sends an image element to the session.
// Image frames are sent for realtime vision scenarios (webcam, screen share).
func (s *DuplexProviderStage) sendImageElement(ctx context.Context, elem *StreamElement) {
	logger.Debug("DuplexProviderStage: forwarding image to session",
		"bytes", len(elem.Image.Data),
		"mime_type", elem.Image.MIMEType,
		"frame_num", elem.Image.FrameNum,
	)

	// Use current time if no timestamp set
	timestamp := elem.Image.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	mediaChunk := &types.MediaChunk{
		Data:      elem.Image.Data,
		Timestamp: timestamp,
		IsLast:    false,
		Metadata: map[string]string{
			"mime_type": elem.Image.MIMEType,
		},
	}

	if err := s.session.SendChunk(ctx, mediaChunk); err != nil {
		logger.Error("DuplexProviderStage: failed to send image to session", "error", err)
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
			logger.Info("Session closure: context canceled")
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

				msg.Parts = s.buildAssistantParts(accumulatedText)
				msg.Reasoning = s.takeReasoning()

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
				s.accumulatedReasoning.Reset()
				s.accumulatedOpaqueReasoning = nil
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
					logger.Info("Input complete, all responses already received, closing session")
					weClosedSession = true
					if s.session != nil {
						_ = s.session.Close()
					}
					// Continue to drain any remaining chunks
				default:
					// Not all responses received - wait for final response with timeout
					logger.Info("Input complete, waiting for final response",
						"timeout", finalResponseTimeout)
					finalResponseTimer = time.After(finalResponseTimeout)
				}
			}

		case <-finalResponseTimer:
			// Timeout waiting for final response - close session and emit partial content
			logger.Warn("Session closure: timeout waiting for final response",
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
					logger.Info("Session ended, response channel closed (we initiated closure)")
				} else {
					logger.Warn("Session closure: provider closed the connection unexpectedly",
						"has_accumulated_content", hasContent,
						"accumulated_text_len", len(accumulatedText),
						"accumulated_media_len", len(s.accumulatedMedia))
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

					msg.Parts = s.buildAssistantParts(accumulatedText)
					msg.Reasoning = s.takeReasoning()

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
					s.accumulatedReasoning.Reset()
					s.accumulatedOpaqueReasoning = nil
					s.accumulatedMedia = nil
				}

				return nil
			}

			// Outbound audio/text is activity: reset the pipeline idle timer so a
			// long reply is not cut off by the 30s idle timeout mid-stream.
			ResetIdleFromContext(ctx)

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

// transcriptionFinal reports whether a chunk carries the normalized
// Metadata["transcription_final"] == true marker, set by providers on the FINAL
// input-transcription chunk of a user turn. It is provider-agnostic: the stage
// keys only off this bool, never off provider names.
func (s *DuplexProviderStage) transcriptionFinal(chunk *providers.StreamChunk) bool {
	if chunk.Metadata == nil {
		return false
	}
	final, _ := chunk.Metadata["transcription_final"].(bool)
	return final
}

// hasQueuedTurnID reports whether a turn_id is queued, i.e. the scenario path
// (a user Message was pre-created with a turn_id). In that case neither the fast
// path nor the EndOfStream fallback materializes a new user Message — the
// existing overwrite-on-EndOfStream behavior owns the user turn.
func (s *DuplexProviderStage) hasQueuedTurnID() bool {
	s.turnIDMu.Lock()
	defer s.turnIDMu.Unlock()
	return len(s.turnIDQueue) > 0
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

	// Fast-path streaming user-turn materialization (provider-agnostic).
	//
	// When a provider marks an input_transcription chunk as the FINAL transcript
	// of the user turn (Metadata["transcription_final"] == true), the user's input
	// is known the moment they stop speaking — long before the assistant finishes
	// responding. Emit the user Message immediately so the UI shows the user turn
	// without waiting for the assistant's EndOfStream (which adds the whole
	// assistant-response duration of lag).
	//
	// Guards (all required):
	//   - transcription_final == true on this chunk
	//   - streaming case only: no turn_id queued (the scenario path pre-creates the
	//     user Message and overwrites it on EndOfStream — leave it untouched)
	//   - buffered transcript is non-empty (accumulated deltas + this final)
	//
	// After emitting we RESET the input-transcription buffer. The EndOfStream
	// fallback (in this same method, below) is guarded on a non-empty buffer, so
	// once we reset it cannot double-emit this turn. Providers that never send the
	// marker fall through to that fallback unchanged.
	if isInputTranscription && s.transcriptionFinal(chunk) && s.inputTranscription.Len() > 0 && !s.hasQueuedTurnID() {
		transcript := s.inputTranscription.String()
		userMsg := &types.Message{
			Role:    roleUser,
			Content: transcript,
			Parts: []types.ContentPart{{
				Type: types.ContentTypeText,
				Text: &transcript,
			}},
		}
		logger.Debug("DuplexProviderStage: fast-path materializing user turn from final transcript",
			"transcriptLen", len(transcript))
		select {
		case output <- StreamElement{Message: userMsg}:
		case <-ctx.Done():
			return ctx.Err()
		}
		// Reset so the EndOfStream fallback below cannot re-emit this turn, and so
		// the next streaming turn starts with a clean buffer.
		s.inputTranscription.Reset()
		return nil
	}

	// Provider-agnostic user-turn materialization at assistant-response start.
	//
	// Some providers (Gemini Live) never mark a "final" input transcription, and
	// under barge-in the assistant turn never reaches a clean EndOfStream — so
	// neither the transcription_final fast-path above nor the EndOfStream fallback
	// below fires, and the user's turn is silently lost (the buffer just keeps
	// growing across turns). The reliable cross-provider boundary is the assistant
	// beginning to respond: the moment any assistant content (audio, model text,
	// or output transcription) arrives, the preceding user utterance is complete.
	// Emit it as a user Message — ordered before this assistant content — exactly
	// once per utterance. Skips the scenario path (a turn_id is pre-queued there)
	// and no-ops once the buffer is reset, so it can't double-emit with the
	// fast-path or EndOfStream paths.
	hasAudio := chunk.MediaData != nil && len(chunk.MediaData.Data) > 0
	isAssistantContent := isOutputTranscription || chunk.Content != "" || hasAudio
	if isAssistantContent && !s.transcriptionCaptured && s.inputTranscription.Len() > 0 && !s.hasQueuedTurnID() {
		transcript := s.inputTranscription.String()
		userMsg := &types.Message{
			Role:    roleUser,
			Content: transcript,
			Parts:   []types.ContentPart{{Type: types.ContentTypeText, Text: &transcript}},
		}
		logger.Debug("DuplexProviderStage: materializing user turn at assistant response start",
			"transcriptLen", len(transcript))
		select {
		case output <- StreamElement{Message: userMsg}:
		case <-ctx.Done():
			return ctx.Err()
		}
		// Reset + mark captured so late transcription chunks for this utterance are
		// ignored (sendAudioElement clears the flag when the next user turn starts).
		s.inputTranscription.Reset()
		s.transcriptionCaptured = true
	}

	if isOutputTranscription && chunk.Delta != "" {
		// outputTranscription sends incremental text - append it
		s.accumulatedText.WriteString(chunk.Delta)
	} else if chunk.Content != "" {
		// ModelTurn text - use Content (Delta is the same, so we only use one)
		s.accumulatedText.WriteString(chunk.Content)
	}

	// Accumulate reasoning/thinking separately from spoken text (it lands on
	// Message.Reasoning at turn complete, never as content), and emit a live
	// non-content ReasoningDelta so the UI can stream thinking as it arrives.
	if chunk.Reasoning != "" || len(chunk.OpaqueReasoning) > 0 {
		s.accumulatedReasoning.WriteString(chunk.Reasoning)
		s.accumulatedOpaqueReasoning = append(s.accumulatedOpaqueReasoning, chunk.OpaqueReasoning...)
		if s.emitter != nil && chunk.Reasoning != "" {
			s.emitter.ReasoningDelta(chunk.Reasoning)
		}
		select {
		case output <- StreamElement{Reasoning: &ReasoningDelta{Text: chunk.Reasoning, Opaque: chunk.OpaqueReasoning}}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Accumulate media content for this turn
	// MediaData.Data is already raw bytes — providers decode base64 at source
	if chunk.MediaData != nil && len(chunk.MediaData.Data) > 0 {
		s.accumulatedMedia = append(s.accumulatedMedia, chunk.MediaData.Data...)
	}

	// Accumulate tool calls for this turn. Tool calls and FinishReason can
	// arrive in separate chunks (e.g. OpenAI Realtime emits ToolCalls on
	// response.output_item.done and FinishReason on response.done) — without
	// accumulation the message-build step at turn-complete reads chunk.ToolCalls
	// from the FinishReason chunk and finds it empty.
	if len(chunk.ToolCalls) > 0 {
		s.accumulatedToolCalls = append(s.accumulatedToolCalls, chunk.ToolCalls...)
	}

	// Convert chunk to element (uses accumulated content for final chunk)
	elem := s.chunkToElement(chunk)

	// Reset accumulation after turn completes (after creating the element)
	// BUT: if this was an interrupted turn completion (no EndOfStream), don't reset -
	// we need to accumulate the real response content that comes next
	if chunk.FinishReason != nil && *chunk.FinishReason != "" && elem.EndOfStream {
		s.accumulatedText.Reset()
		s.accumulatedReasoning.Reset()
		s.accumulatedOpaqueReasoning = nil
		s.accumulatedMedia = nil
		s.accumulatedToolCalls = nil
		// Mark transcription as captured - don't reset yet!
		// Late-arriving transcription chunks will be ignored until a new user turn starts.
		// The transcription buffer will be reset when sendAudioElement is called.
		s.transcriptionCaptured = true
	}

	// Continuous-streaming user-turn materialization (provider-agnostic).
	//
	// In the scenario path a user Message is pre-created with a turn_id, so
	// chunkToElement releases the transcript by attaching elem.Meta.Transcription
	// + elem.Meta.TurnID (consumed by ArenaStateStoreSaveStage to overwrite the
	// pre-created user message). In the continuous-streaming path (interactive
	// console / SDK realtime) NO user Message is pre-queued, so turnID is empty
	// and chunkToElement leaves elem.Meta.Transcription nil — the transcript
	// would otherwise be silently dropped.
	//
	// Here we detect that case (turn-completing EndOfStream, a buffered input
	// transcript, and no scenario-path transcription attached) and emit a NEW
	// user Message element from the buffered transcript, ordered BEFORE the
	// assistant element for this turn. The buffer is then reset so each
	// subsequent streaming turn materializes its own user message.
	//
	// This keys only off s.inputTranscription (populated from the normalized
	// input_transcription chunk that both OpenAI Realtime and Gemini Live emit)
	// and the shared EndOfStream machinery — no per-provider event names.
	if elem.EndOfStream && s.inputTranscription.Len() > 0 && elem.Meta.Transcription == nil {
		transcript := s.inputTranscription.String()
		userMsg := &types.Message{
			Role:    roleUser,
			Content: transcript,
			Parts: []types.ContentPart{{
				Type: types.ContentTypeText,
				Text: &transcript,
			}},
		}
		userElem := StreamElement{Message: userMsg}
		logger.Debug("DuplexProviderStage: materializing streaming user turn from transcript",
			"transcriptLen", len(transcript))
		select {
		case output <- userElem:
		case <-ctx.Done():
			return ctx.Err()
		}
		// Reset the per-turn transcription buffer so the next streaming turn
		// starts clean (mirrors the reset in sendAudioElement for the scenario
		// path). transcriptionCaptured is already set above on turn completion.
		s.inputTranscription.Reset()
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

// chunkToElement, buildAssistantParts, and takeReasoning — the pure transforms —
// now live in stages_duplex_provider.go (coverage-gated).
