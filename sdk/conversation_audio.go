package sdk

import (
	"context"
	"fmt"
	"io"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// OpenAudioSession creates a bidirectional audio streaming session.
//
// Requires a provider that implements StreamInputSupport (e.g., Gemini).
// Returns an audio.Session with VAD and turn detection if configured.
//
//	session, err := conv.OpenAudioSession(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer session.Close()
//
//	// Send audio chunks
//	for chunk := range audioSource {
//	    session.SendChunk(ctx, chunk)
//	}
//
//	// Listen for responses
//	for chunk := range session.Response() {
//	    // Handle streaming audio response
//	}
func (c *Conversation) OpenAudioSession(ctx context.Context, opts ...AudioSessionOption) (*audio.Session, error) {
	if err := c.checkClosed(); err != nil {
		return nil, err
	}

	// Check provider supports streaming input
	streamProvider, ok := c.provider.(providers.StreamInputSupport)
	if !ok {
		return nil, ErrProviderNotStreamCapable
	}

	// Apply session options
	cfg := &audioSessionConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Use conversation-level turn detector if not overridden
	turnDetector := cfg.turnDetector
	if turnDetector == nil {
		turnDetector = c.config.turnDetector
	}

	// Build system message from prompt with variable substitution
	// Note: This uses static variables from session. Variable providers
	// would need to be resolved separately for audio sessions since the
	// system message is sent upfront before pipeline execution.
	systemMsg := c.buildSystemMessage()

	// Use provided media config or default audio config
	var mediaConfig types.StreamingMediaConfig
	if cfg.mediaConfig != nil {
		mediaConfig = *cfg.mediaConfig
	} else {
		// Default audio streaming configuration
		mediaConfig = types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			ChunkSize:  3200, // 100ms at 16kHz
			SampleRate: 16000,
			Channels:   1,
			BitDepth:   16,
			Encoding:   "pcm_linear16",
		}
	}

	// Create streaming session request
	req := &providers.StreamInputRequest{
		Config:    mediaConfig,
		SystemMsg: systemMsg,
	}

	// Set response modalities if configured (e.g., ["AUDIO"] for voice responses)
	if len(cfg.responseModalities) > 0 {
		if req.Metadata == nil {
			req.Metadata = make(map[string]interface{})
		}
		req.Metadata["response_modalities"] = cfg.responseModalities
	}

	// TODO: This should create a BidirectionalSession from the session layer
	// instead of directly managing the provider session. For now, we create
	// the provider session directly and wrap it with audio.Session.
	// Future refactor: create BidirectionalSession with the pipeline, which
	// manages the provider session and handles variable provider resolution.

	// Create underlying stream session
	underlying, err := streamProvider.CreateStreamSession(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create stream session: %w", err)
	}

	// Wrap with audio session for VAD and turn detection
	sessionCfg := audio.SessionConfig{
		VAD:                  cfg.vad,
		TurnDetector:         turnDetector,
		InterruptionStrategy: cfg.interruptionStrategy,
		AutoCompleteTurn:     cfg.autoCompleteTurn,
	}

	session, err := audio.NewSession(underlying, sessionCfg)
	if err != nil {
		// Close underlying session on error
		_ = underlying.Close()
		return nil, fmt.Errorf("failed to create audio session: %w", err)
	}

	return session, nil
}

// SpeakResponse converts a text response to audio using the configured TTS service.
//
// Requires WithTTS() to be configured when opening the conversation.
//
//	conv, _ := sdk.Open("./assistant.pack.json", "voice",
//	    sdk.WithTTS(tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))),
//	)
//
//	resp, _ := conv.Send(ctx, "Tell me a joke")
//	audioReader, _ := conv.SpeakResponse(ctx, resp)
//	defer audioReader.Close()
//
//	io.Copy(speaker, audioReader)
func (c *Conversation) SpeakResponse(ctx context.Context, resp *Response, opts ...TTSOption) (io.ReadCloser, error) {
	if err := c.checkClosed(); err != nil {
		return nil, err
	}

	if c.config.ttsService == nil {
		return nil, ErrNoTTSConfigured
	}

	// Get response text
	text := resp.Text()
	if text == "" {
		return nil, tts.ErrEmptyText
	}

	// Apply TTS options
	cfg := &ttsConfig{
		speed: 1.0, // Default speed
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Build synthesis config
	synthConfig := tts.SynthesisConfig{
		Voice:    cfg.voice,
		Format:   cfg.format,
		Speed:    cfg.speed,
		Pitch:    cfg.pitch,
		Language: cfg.language,
		Model:    cfg.model,
	}

	return c.config.ttsService.Synthesize(ctx, text, synthConfig)
}

// SpeakResponseStream converts a text response to streaming audio.
//
// Requires WithTTS() configured with a StreamingService provider.
// Returns a channel of audio chunks for lower latency playback.
//
//	conv, _ := sdk.Open("./assistant.pack.json", "voice",
//	    sdk.WithTTS(tts.NewCartesia(os.Getenv("CARTESIA_API_KEY"))),
//	)
//
//	resp, _ := conv.Send(ctx, "Tell me a story")
//	chunks, _ := conv.SpeakResponseStream(ctx, resp)
//
//	for chunk := range chunks {
//	    if chunk.Error != nil {
//	        log.Fatal(chunk.Error)
//	    }
//	    playAudio(chunk.Data)
//	}
func (c *Conversation) SpeakResponseStream(
	ctx context.Context, resp *Response, opts ...TTSOption,
) (<-chan tts.AudioChunk, error) {
	if err := c.checkClosed(); err != nil {
		return nil, err
	}

	if c.config.ttsService == nil {
		return nil, ErrNoTTSConfigured
	}

	// Check if TTS service supports streaming
	streamingTTS, ok := c.config.ttsService.(tts.StreamingService)
	if !ok {
		return nil, fmt.Errorf("TTS service %q does not support streaming", c.config.ttsService.Name())
	}

	// Get response text
	text := resp.Text()
	if text == "" {
		return nil, tts.ErrEmptyText
	}

	// Apply TTS options
	cfg := &ttsConfig{
		speed: 1.0, // Default speed
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Build synthesis config
	synthConfig := tts.SynthesisConfig{
		Voice:    cfg.voice,
		Format:   cfg.format,
		Speed:    cfg.speed,
		Pitch:    cfg.pitch,
		Language: cfg.language,
		Model:    cfg.model,
	}

	return streamingTTS.SynthesizeStream(ctx, text, synthConfig)
}
