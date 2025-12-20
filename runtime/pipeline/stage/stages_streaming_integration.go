package stage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Transcriber converts audio bytes to text.
// Follows Go naming convention for single-method interfaces.
type Transcriber interface {
	Transcribe(ctx context.Context, audio []byte) (string, error)
}

// VADConfig contains configuration for VAD accumulator stage.
type VADConfig struct {
	// Threshold for silence detection (0.0 = silence, 1.0 = speech)
	Threshold float64

	// MinSpeechDuration is the minimum duration of speech before turn can complete
	MinSpeechDuration time.Duration

	// MaxTurnDuration is the maximum duration before forcing turn completion
	MaxTurnDuration time.Duration

	// SilenceDuration is how long silence must persist to trigger turn complete
	SilenceDuration time.Duration
}

const (
	defaultVADThreshold        = 0.3
	defaultMinSpeechDurationMs = 300
	defaultMaxTurnDurationSec  = 30
	defaultSilenceDurationMs   = 700

	// Audio calculation constants
	// 16kHz 16-bit mono = 32000 bytes/sec (16000 samples/sec * 2 bytes/sample)
	audioBytesPerSecond16kHz = 32000.0
	// msPerSecFloat is milliseconds per second for audio position calculations.
	msPerSecFloat = 1000.0

	// Timeout for sending partial responses when context is canceled
	partialResponseSendTimeout = 500 * time.Millisecond

	// Maximum characters for content preview in logs
	contentPreviewMaxLen = 60
)

// DefaultVADConfig returns sensible defaults for VAD configuration.
func DefaultVADConfig() VADConfig {
	return VADConfig{
		Threshold:         defaultVADThreshold,
		MinSpeechDuration: defaultMinSpeechDurationMs * time.Millisecond,
		MaxTurnDuration:   defaultMaxTurnDurationSec * time.Second,
		SilenceDuration:   defaultSilenceDurationMs * time.Millisecond,
	}
}

// VADAccumulatorStage reads streaming audio chunks, detects turn boundaries via VAD,
// and emits a single Message element with the transcribed text.
//
// This is an Accumulate stage: N audio chunks → 1 message element
type VADAccumulatorStage struct {
	BaseStage
	analyzer    audio.VADAnalyzer
	transcriber Transcriber
	config      VADConfig
}

// NewVADAccumulatorStage creates a new VAD accumulator stage.
func NewVADAccumulatorStage(
	analyzer audio.VADAnalyzer,
	transcriber Transcriber,
	config VADConfig,
) *VADAccumulatorStage {
	return &VADAccumulatorStage{
		BaseStage:   NewBaseStage("vad_accumulator", StageTypeAccumulate),
		analyzer:    analyzer,
		transcriber: transcriber,
		config:      config,
	}
}

// Process implements the Stage interface.
// Accumulates audio chunks until turn complete, then transcribes and emits a message.
func (s *VADAccumulatorStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	state := &vadState{
		turnStart: time.Now(),
	}

	for elem := range input {
		// Pass through non-audio elements immediately
		if elem.Audio == nil {
			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}

		// Process audio chunk
		if err := s.processAudioElement(ctx, &elem, state); err != nil {
			logger.Error("VAD audio processing failed", "error", err)
			output <- NewErrorElement(err)
			return err
		}

		// Check if turn is complete
		if s.shouldCompleteTurn(state) {
			return s.emitTranscribedMessage(ctx, state, output)
		}
	}

	// Stream closed - process any remaining audio
	if len(state.audioBuffer) > 0 {
		return s.emitTranscribedMessage(ctx, state, output)
	}

	return nil
}

// vadState holds the state for VAD processing.
type vadState struct {
	audioBuffer    []byte
	speechDetected bool
	silenceStart   time.Time
	turnStart      time.Time
}

// processAudioElement processes a single audio element.
func (s *VADAccumulatorStage) processAudioElement(
	ctx context.Context,
	elem *StreamElement,
	state *vadState,
) error {
	if elem.Audio == nil || len(elem.Audio.Samples) == 0 {
		return nil
	}

	// Append audio data to buffer
	state.audioBuffer = append(state.audioBuffer, elem.Audio.Samples...)

	// Run VAD analysis
	score, err := s.analyzer.Analyze(ctx, elem.Audio.Samples)
	if err != nil {
		return err
	}

	s.updateVADState(state, score)
	return nil
}

// updateVADState updates the VAD state based on the VAD score.
func (s *VADAccumulatorStage) updateVADState(state *vadState, score float64) {
	if score >= s.config.Threshold {
		// Speech detected
		state.speechDetected = true
		state.silenceStart = time.Time{} // Reset silence timer
		logger.Debug("VAD: speech detected", "score", score)
		return
	}

	// Silence detected after speech - start silence timer
	if state.speechDetected && state.silenceStart.IsZero() {
		state.silenceStart = time.Now()
		logger.Debug("VAD: silence started after speech")
	}
}

// shouldCompleteTurn checks if the turn should be completed.
func (s *VADAccumulatorStage) shouldCompleteTurn(state *vadState) bool {
	// Check silence duration
	if state.speechDetected && !state.silenceStart.IsZero() {
		if time.Since(state.silenceStart) >= s.config.SilenceDuration {
			logger.Debug("VAD: turn complete - silence duration exceeded")
			return true
		}
	}

	// Check max turn duration
	if time.Since(state.turnStart) >= s.config.MaxTurnDuration {
		logger.Debug("VAD: turn complete - max duration exceeded")
		return true
	}

	return false
}

// emitTranscribedMessage transcribes the audio buffer and emits a message element.
func (s *VADAccumulatorStage) emitTranscribedMessage(
	ctx context.Context,
	state *vadState,
	output chan<- StreamElement,
) error {
	if len(state.audioBuffer) == 0 {
		return errors.New("no audio to transcribe")
	}

	// Transcribe audio to text
	text, err := s.transcriber.Transcribe(ctx, state.audioBuffer)
	if err != nil {
		return err
	}

	if text == "" {
		return errors.New("transcription returned empty text")
	}

	logger.Debug("VAD: transcribed audio", "text_length", len(text))

	// Create message element
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeText,
				Text: &text,
			},
		},
	}

	elem := NewMessageElement(&msg)

	// Emit message
	select {
	case output <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TTSService converts text to audio.
type TTSService interface {
	// Synthesize converts text to audio bytes.
	Synthesize(ctx context.Context, text string) ([]byte, error)

	// MIMEType returns the MIME type of the synthesized audio.
	MIMEType() string
}

// TTSConfig contains configuration for TTS stage.
type TTSConfig struct {
	// SkipEmpty skips synthesis for empty or whitespace-only text
	SkipEmpty bool

	// MinTextLength is the minimum text length to synthesize (0 = no minimum)
	MinTextLength int
}

// DefaultTTSConfig returns sensible defaults for TTS configuration.
func DefaultTTSConfig() TTSConfig {
	return TTSConfig{
		SkipEmpty:     true,
		MinTextLength: 1,
	}
}

// TTSStage synthesizes audio for streaming text elements.
// It reads text elements from input and adds audio data to them.
//
// This is a Transform stage: text element → text+audio element (1:1)
type TTSStage struct {
	BaseStage
	tts    TTSService
	config TTSConfig
}

// NewTTSStage creates a new TTS stage.
func NewTTSStage(tts TTSService, config TTSConfig) *TTSStage {
	return &TTSStage{
		BaseStage: NewBaseStage("tts", StageTypeTransform),
		tts:       tts,
		config:    config,
	}
}

// Process implements the Stage interface.
// Synthesizes audio for each text element and adds it to the element.
func (s *TTSStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		// Process element with TTS if it has text content
		if err := s.processElement(ctx, &elem); err != nil {
			logger.Error("TTS synthesis failed", "error", err)
			// Continue processing other elements rather than failing the entire pipeline
			elem.Error = err
		}

		// Forward element (with or without audio)
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// processElement synthesizes audio for an element if it contains text.
func (s *TTSStage) processElement(ctx context.Context, elem *StreamElement) error {
	text := s.extractText(elem)
	if text == "" {
		return nil // No text to synthesize
	}

	// Check configuration filters
	if !s.shouldSynthesize(text) {
		return nil
	}

	// Synthesize audio
	audioData, err := s.tts.Synthesize(ctx, text)
	if err != nil {
		return err
	}

	// Add audio to element
	elem.Audio = &AudioData{
		Samples:    audioData,
		SampleRate: 24000, // Default sample rate - could be configurable
		Format:     AudioFormatPCM16,
	}

	logger.Debug("TTS: synthesized audio", "text_length", len(text), "audio_bytes", len(audioData))
	return nil
}

// extractText extracts text content from an element.
func (s *TTSStage) extractText(elem *StreamElement) string {
	if elem.Text != nil && *elem.Text != "" {
		return *elem.Text
	}

	if elem.Message != nil {
		// Extract text from message content
		if elem.Message.Content != "" {
			return elem.Message.Content
		}

		// Extract from parts
		for _, part := range elem.Message.Parts {
			if part.Text != nil && *part.Text != "" {
				return *part.Text
			}
		}
	}

	return ""
}

// shouldSynthesize checks if text should be synthesized based on config.
func (s *TTSStage) shouldSynthesize(text string) bool {
	if s.config.SkipEmpty && len(text) == 0 {
		return false
	}

	if len(text) < s.config.MinTextLength {
		return false
	}

	return true
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
// Default timeout for waiting for final response after input closes
const finalResponseTimeout = 30 * time.Second

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

		logger.Debug("DuplexProviderStage: creating session with system instruction",
			"system_prompt_length", len(systemPrompt))

		// Create the session
		var err error
		s.session, err = s.provider.CreateStreamSession(ctx, sessionConfig)
		if err != nil {
			return fmt.Errorf("duplex provider stage: failed to create session: %w", err)
		}
		logger.Debug("DuplexProviderStage: session created")
		defer s.session.Close()
		s.systemPromptSent = true // System instruction sent at session creation

		// Re-inject the first element into a new channel that includes it
		// This ensures the first element's audio/message content is processed
		input = s.prependElement(ctx, &firstElem, input)
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

// EndInputter is an optional interface for sessions that support explicit end-of-input signaling.
// This is primarily used by mock sessions to trigger responses after all audio has been sent.
type EndInputter interface {
	EndInput()
}

// prependElement creates a new channel that yields the first element followed by all elements
// from the original input channel. This is used after consuming the first element to extract
// system_prompt, ensuring the element's audio/message content is still processed.
func (s *DuplexProviderStage) prependElement(
	ctx context.Context,
	first *StreamElement,
	rest <-chan StreamElement,
) <-chan StreamElement {
	merged := make(chan StreamElement)
	go func() {
		defer close(merged)
		// Send the first element
		select {
		case merged <- *first:
		case <-ctx.Done():
			return
		}
		// Forward all remaining elements
		for elem := range rest {
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
	// Check for end of stream (end of turn input)
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
		return
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

	if elem.Audio != nil && len(elem.Audio.Samples) > 0 {
		s.sendAudioElement(ctx, elem)
	} else if elem.Text != nil && *elem.Text != "" {
		s.sendTextElement(ctx, elem)
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
		logger.Debug("DuplexProviderStage: reset transcription for new user turn")
	}

	now := time.Now()

	// Track timing for first chunk of a new audio stream
	if s.audioChunkCount == 0 || s.lastAudioChunkTime.IsZero() {
		s.audioStreamStart = now
		s.audioChunkCount = 0
		s.audioBytesSent = 0
		logger.Debug("DuplexProviderStage: starting new audio stream")

		// If session supports manual turn control (VAD disabled), send activityStart
		// before the first audio chunk to signal the start of user input
		if activitySignaler, ok := s.session.(providers.ActivitySignaler); ok && activitySignaler.IsVADDisabled() {
			logger.Debug("DuplexProviderStage: sending activityStart for manual turn control")
			if err := activitySignaler.SendActivityStart(); err != nil {
				logger.Error("DuplexProviderStage: failed to send activityStart", "error", err)
			}
		}
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
					mediaData := string(s.accumulatedMedia)
					msg.Parts = append(msg.Parts, types.ContentPart{
						Type: types.ContentTypeAudio,
						Media: &types.MediaContent{
							Data:     &mediaData,
							MIMEType: "audio/pcm",
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
						mediaData := string(s.accumulatedMedia)
						msg.Parts = append(msg.Parts, types.ContentPart{
							Type: types.ContentTypeAudio,
							Media: &types.MediaContent{
								Data:     &mediaData,
								MIMEType: "audio/pcm",
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
	if chunk.MediaDelta != nil && chunk.MediaDelta.Data != nil {
		s.accumulatedMedia = append(s.accumulatedMedia, []byte(*chunk.MediaDelta.Data)...)
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
				mediaData := string(s.accumulatedMedia)
				msg.Parts = append(msg.Parts, types.ContentPart{
					Type: types.ContentTypeAudio,
					Media: &types.MediaContent{
						Data:     &mediaData,
						MIMEType: "audio/pcm",
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

		// Create Message if there's content or cost info
		if hasContent || hasCostInfo {
			msg := &types.Message{
				Role:     "assistant",
				Content:  accumulatedText,
				Parts:    []types.ContentPart{},
				CostInfo: chunk.CostInfo,
				Meta: map[string]interface{}{
					"finish_reason": *chunk.FinishReason,
				},
			}

			if accumulatedText != "" {
				msg.Parts = append(msg.Parts, types.ContentPart{
					Type: types.ContentTypeText,
					Text: &accumulatedText,
				})
			}

			if len(s.accumulatedMedia) > 0 {
				mediaData := string(s.accumulatedMedia)
				msg.Parts = append(msg.Parts, types.ContentPart{
					Type: types.ContentTypeAudio,
					Media: &types.MediaContent{
						Data:     &mediaData,
						MIMEType: "audio/pcm",
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

	// Add input transcription to metadata if present
	if s.inputTranscription.Len() > 0 && elem.EndOfStream {
		if elem.Metadata == nil {
			elem.Metadata = make(map[string]interface{})
		}
		elem.Metadata["input_transcription"] = s.inputTranscription.String()

		// Pop turn_id from queue for correlation
		s.turnIDMu.Lock()
		turnID := ""
		if len(s.turnIDQueue) > 0 {
			turnID = s.turnIDQueue[0]
			s.turnIDQueue = s.turnIDQueue[1:]
		}
		s.turnIDMu.Unlock()
		if turnID != "" {
			elem.Metadata["transcription_turn_id"] = turnID
		}
		logger.Debug("DuplexProviderStage: adding input transcription",
			"transcriptionLen", s.inputTranscription.Len(),
			"turnID", turnID)
	}

	return elem
}
