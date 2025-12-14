package stage

import (
	"context"
	"errors"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TranscriptionService converts audio bytes to text.
type TranscriptionService interface {
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
	transcriber TranscriptionService
	config      VADConfig
}

// NewVADAccumulatorStage creates a new VAD accumulator stage.
func NewVADAccumulatorStage(
	analyzer audio.VADAnalyzer,
	transcriber TranscriptionService,
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

// DuplexProviderStage handles bidirectional streaming through a WebSocket session.
// It forwards elements from input to the provider's WebSocket session and
// forwards responses from the session to output.
//
// This is a Bidirectional stage: input elements ⟷ WebSocket session ⟷ output elements
type DuplexProviderStage struct {
	BaseStage
	session providers.StreamInputSession
}

// NewDuplexProviderStage creates a new duplex provider stage.
func NewDuplexProviderStage(session providers.StreamInputSession) *DuplexProviderStage {
	return &DuplexProviderStage{
		BaseStage: NewBaseStage("duplex_provider", StageTypeBidirectional),
		session:   session,
	}
}

// Process implements the Stage interface.
// Handles bidirectional streaming between input channel and WebSocket session.
func (s *DuplexProviderStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	if s.session == nil {
		return errors.New("duplex provider stage: no session configured")
	}

	logger.Debug("DuplexProviderStage: starting bidirectional streaming")

	// Channel to signal when input forwarding is done
	inputDone := make(chan error, 1)

	// Start input forwarding goroutine
	go s.forwardInputElements(ctx, input, inputDone)

	// Forward responses from session to output (blocks until complete)
	responseErr := s.forwardResponseElements(ctx, output)

	// Wait for input forwarding to complete
	select {
	case inputErr := <-inputDone:
		if inputErr != nil && responseErr == nil {
			responseErr = inputErr
		}
	case <-time.After(1 * time.Second):
		logger.Warn("DuplexProviderStage: timeout waiting for input forwarding to complete")
	}

	return responseErr
}

// forwardInputElements forwards elements from input channel to WebSocket session.
func (s *DuplexProviderStage) forwardInputElements(
	ctx context.Context,
	input <-chan StreamElement,
	done chan error,
) {
	for {
		select {
		case <-ctx.Done():
			done <- ctx.Err()
			return
		case elem, ok := <-input:
			if !ok {
				logger.Debug("DuplexProviderStage: input channel closed, ending session input")
				done <- nil
				return
			}
			s.sendElementToSession(ctx, &elem)
		}
	}
}

// sendElementToSession sends a single element to the WebSocket session.
func (s *DuplexProviderStage) sendElementToSession(ctx context.Context, elem *StreamElement) {
	if elem.Audio != nil && len(elem.Audio.Samples) > 0 {
		s.sendAudioElement(ctx, elem)
	} else if elem.Text != nil && *elem.Text != "" {
		s.sendTextElement(ctx, elem)
	}
}

// sendAudioElement sends an audio element to the session.
func (s *DuplexProviderStage) sendAudioElement(ctx context.Context, elem *StreamElement) {
	mediaChunk := &types.MediaChunk{
		Data:      elem.Audio.Samples,
		Timestamp: time.Now(),
		IsLast:    false,
	}

	logger.Debug("DuplexProviderStage: forwarding audio to session", "dataLen", len(mediaChunk.Data))

	if err := s.session.SendChunk(ctx, mediaChunk); err != nil {
		logger.Error("DuplexProviderStage: failed to send chunk to session", "error", err)
	}
}

// sendTextElement sends a text element to the session.
func (s *DuplexProviderStage) sendTextElement(ctx context.Context, elem *StreamElement) {
	text := *elem.Text

	logger.Debug("DuplexProviderStage: forwarding text to session", "content", text)

	if err := s.session.SendText(ctx, text); err != nil {
		logger.Error("DuplexProviderStage: failed to send text to session", "error", err)
	}
}

// forwardResponseElements forwards responses from session to output channel.
func (s *DuplexProviderStage) forwardResponseElements(
	ctx context.Context,
	output chan<- StreamElement,
) error {
	responseChannel := s.session.Response()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case chunk, ok := <-responseChannel:
			if !ok {
				logger.Debug("DuplexProviderStage: session response channel closed")
				return nil
			}

			if err := s.handleResponseChunk(ctx, &chunk, output); err != nil {
				return err
			}

			if s.isFinished(&chunk) {
				return nil
			}
		}
	}
}

// handleResponseChunk processes and forwards a single response chunk.
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

	// Convert chunk to element
	elem := s.chunkToElement(chunk)

	logger.Debug("DuplexProviderStage: forwarding response element",
		"hasText", elem.Text != nil,
		"hasAudio", elem.Audio != nil)

	select {
	case output <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// chunkToElement converts a StreamChunk to a StreamElement.
func (s *DuplexProviderStage) chunkToElement(chunk *providers.StreamChunk) StreamElement {
	elem := StreamElement{}

	// Add text if present
	if chunk.Content != "" {
		elem.Text = &chunk.Content
	}

	// Add audio if present
	if chunk.MediaDelta != nil && chunk.MediaDelta.Data != nil {
		// Convert audio data
		audioData := []byte(*chunk.MediaDelta.Data)
		elem.Audio = &AudioData{
			Samples:    audioData,
			SampleRate: 24000, // Default - could be extracted from metadata
			Format:     AudioFormatPCM16,
		}
	}

	// Add metadata
	if chunk.Metadata != nil {
		elem.Metadata = chunk.Metadata
	}

	return elem
}

// isFinished checks if the chunk indicates streaming is complete.
func (s *DuplexProviderStage) isFinished(chunk *providers.StreamChunk) bool {
	if chunk.FinishReason != nil && *chunk.FinishReason != "" {
		logger.Debug("DuplexProviderStage: streaming finished", "reason", *chunk.FinishReason)
		return true
	}
	return false
}
