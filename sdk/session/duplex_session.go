// Package session provides session abstractions for managing conversations.
// Sessions wrap pipelines and provide convenient APIs for text and duplex streaming interactions.
package session

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// duplexSession implements BidirectionalSession with stage-based StreamPipeline.
// It manages streaming input/output through the stage pipeline.
//
// Two modes:
// - ASM mode: DuplexProviderStage creates session lazily, one long-running pipeline execution
// - VAD mode: No providerSession, VAD triggers multiple pipeline executions with one-shot calls
type duplexSession struct {
	id       string
	store    statestore.Store
	pipeline *stage.StreamPipeline
	provider providers.Provider // Provider for LLM calls
	// Note: Session is NOT stored here - DuplexProviderStage creates and manages it
	variables map[string]string
	varsMu    sync.RWMutex
	closeMu   sync.Mutex
	closed    bool

	// Internal pipeline channel (stage.StreamElement)
	stageInput chan stage.StreamElement // Feeds converted elements to pipeline

	// External API channel (providers.StreamChunk)
	streamOutput chan providers.StreamChunk // Consumer receives chunks here

	// Pipeline execution control
	executionStarted bool
	executionMu      sync.Mutex
}

//nolint:unused // Used by tests
const streamBufferSize = 100 // Size of buffered channels for streaming

// initConversationState initializes state for a new conversation if it doesn't exist.
func initConversationState(ctx context.Context, store statestore.Store, cfg *DuplexSessionConfig, convID string) error {
	_, err := store.Load(ctx, convID)
	if err != nil {
		initialState := &statestore.ConversationState{
			ID:       convID,
			UserID:   cfg.UserID,
			Messages: []types.Message{},
			Metadata: cfg.Metadata,
		}
		if err := store.Save(ctx, initialState); err != nil {
			return fmt.Errorf("failed to initialize conversation state: %w", err)
		}
	}
	return nil
}

// NewDuplexSession creates a bidirectional session from a config.
// PipelineBuilder and Provider are required.
//
// If Config is provided (ASM mode):
//   - Passes streaming provider + base config to PipelineBuilder
//   - DuplexProviderStage creates session lazily using system_prompt from element metadata
//   - Single long-running pipeline execution
//
// If Config is nil (VAD mode):
//   - No streaming provider or config
//   - Calls PipelineBuilder with nil streaming provider
//   - VAD middleware triggers multiple pipeline executions
func NewDuplexSession(ctx context.Context, cfg *DuplexSessionConfig) (DuplexSession, error) {
	if cfg.PipelineBuilder == nil {
		return nil, fmt.Errorf("pipeline builder is required")
	}
	if cfg.Provider == nil {
		return nil, fmt.Errorf("provider is required")
	}

	conversationID := cfg.ConversationID
	if conversationID == "" {
		conversationID = uuid.New().String()
	}

	store := cfg.StateStore
	if store == nil {
		store = statestore.NewMemoryStore()
	}

	// Initialize conversation state if it doesn't exist
	if err := initConversationState(ctx, store, cfg, conversationID); err != nil {
		return nil, err
	}

	// For ASM mode, extract the streaming provider (session is created lazily by pipeline)
	var streamProvider providers.StreamInputSupport
	if cfg.Config != nil {
		// ASM mode: provider must support streaming
		var ok bool
		streamProvider, ok = cfg.Provider.(providers.StreamInputSupport)
		if !ok {
			return nil, fmt.Errorf("provider must implement StreamInputSupport for ASM mode")
		}
		// Note: Session is NOT created here - DuplexProviderStage creates it lazily
		// using system_prompt from element metadata (set by PromptAssemblyStage)
	}

	// Build pipeline with streaming provider + config (provider creates session lazily)
	builtPipeline, err := cfg.PipelineBuilder(ctx, cfg.Provider, streamProvider, cfg.Config, conversationID, store)
	if err != nil {
		return nil, fmt.Errorf("failed to build pipeline: %w", err)
	}

	// Initialize variables
	vars := make(map[string]string)
	for k, v := range cfg.Variables {
		vars[k] = v
	}

	// Create streaming channels
	// stageInput feeds stage.StreamElement to the pipeline
	// streamOutput receives providers.StreamChunk for the external API
	stageInput := make(chan stage.StreamElement, streamBufferSize)
	streamOutput := make(chan providers.StreamChunk, streamBufferSize)

	sess := &duplexSession{
		id:           conversationID,
		store:        store,
		pipeline:     builtPipeline,
		provider:     cfg.Provider,
		variables:    vars,
		stageInput:   stageInput,
		streamOutput: streamOutput,
	}

	return sess, nil
}

// ID returns the unique session identifier.
func (s *duplexSession) ID() string {
	return s.id
}

// SendChunk sends a chunk to the session via the Pipeline.
// Converts providers.StreamChunk to stage.StreamElement at the boundary.
func (s *duplexSession) SendChunk(ctx context.Context, chunk *providers.StreamChunk) error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return fmt.Errorf("session is closed")
	}
	s.closeMu.Unlock()

	if chunk == nil {
		return fmt.Errorf("chunk cannot be nil")
	}

	// Start Pipeline execution on first chunk
	s.executionMu.Lock()
	if !s.executionStarted {
		s.executionStarted = true
		s.executionMu.Unlock()

		// Start Pipeline execution in background
		go s.executePipeline(ctx)
	} else {
		s.executionMu.Unlock()
	}

	// Convert StreamChunk to StreamElement at the boundary
	elem := streamChunkToStreamElement(chunk)

	// Send converted element to Pipeline
	select {
	case s.stageInput <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SendText is a convenience method for sending text.
func (s *duplexSession) SendText(ctx context.Context, text string) error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return fmt.Errorf("session is closed")
	}
	s.closeMu.Unlock()

	chunk := &providers.StreamChunk{
		Content: text,
	}
	return s.SendChunk(ctx, chunk)
}

// SendFrame sends an image frame to the session for realtime video scenarios.
// This is a convenience method that wraps SendChunk with proper image formatting.
func (s *duplexSession) SendFrame(ctx context.Context, frame *ImageFrame) error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return fmt.Errorf("session is closed")
	}
	s.closeMu.Unlock()

	if frame == nil || len(frame.Data) == 0 {
		return fmt.Errorf("frame data is required")
	}

	// Convert frame data to string for MediaContent
	dataStr := string(frame.Data)

	chunk := &providers.StreamChunk{
		MediaDelta: &types.MediaContent{
			MIMEType: frame.MIMEType,
			Data:     &dataStr,
		},
		Metadata: map[string]any{
			"width":     frame.Width,
			"height":    frame.Height,
			"timestamp": frame.Timestamp,
			"frame_num": frame.FrameNum,
		},
	}
	return s.SendChunk(ctx, chunk)
}

// SendVideoChunk sends a video chunk to the session for encoded video streaming.
// This is a convenience method that wraps SendChunk with proper video formatting.
func (s *duplexSession) SendVideoChunk(ctx context.Context, vchunk *VideoChunk) error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return fmt.Errorf("session is closed")
	}
	s.closeMu.Unlock()

	if vchunk == nil || len(vchunk.Data) == 0 {
		return fmt.Errorf("video chunk data is required")
	}

	// Convert chunk data to string for MediaContent
	dataStr := string(vchunk.Data)

	chunk := &providers.StreamChunk{
		MediaDelta: &types.MediaContent{
			MIMEType: vchunk.MIMEType,
			Data:     &dataStr,
		},
		Metadata: map[string]any{
			"width":        vchunk.Width,
			"height":       vchunk.Height,
			"timestamp":    vchunk.Timestamp,
			"frame_num":    int64(vchunk.ChunkIndex),
			"is_key_frame": vchunk.IsKeyFrame,
		},
	}
	return s.SendChunk(ctx, chunk)
}

// executePipeline runs the stage pipeline with streaming input/output.
// This is called once when the first chunk is received.
// Converts stage.StreamElement output to providers.StreamChunk at the boundary.
func (s *duplexSession) executePipeline(ctx context.Context) {
	defer close(s.streamOutput)

	// Start the stage pipeline with our input channel
	stageOutput, err := s.pipeline.Execute(ctx, s.stageInput)
	if err != nil {
		finishReason := "error"
		select {
		case s.streamOutput <- providers.StreamChunk{
			Error:        err,
			FinishReason: &finishReason,
		}:
		default:
		}
		return
	}

	// Forward stage output to streamOutput, converting StreamElement to StreamChunk
	for {
		select {
		case <-ctx.Done():
			errChunk := providers.StreamChunk{Error: ctx.Err()}
			select {
			case s.streamOutput <- errChunk:
			default:
			}
			return
		case elem, ok := <-stageOutput:
			if !ok {
				// Stage pipeline finished
				return
			}
			chunk := streamElementToStreamChunk(&elem)
			select {
			case s.streamOutput <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}
}

// Response returns the response channel from the Pipeline.
func (s *duplexSession) Response() <-chan providers.StreamChunk {
	return s.streamOutput
}

// Close ends the session.
func (s *duplexSession) Close() error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true
	close(s.stageInput)
	// Note: DuplexProviderStage manages the provider session and closes it when pipeline completes

	return nil
}

// Done returns a channel that's closed when the session ends.
func (s *duplexSession) Done() <-chan struct{} {
	// Create done channel that closes when output closes
	done := make(chan struct{})
	go func() {
		// Wait for all output to be consumed
		for range s.streamOutput { //nolint:revive // intentionally draining channel
		}
		close(done)
	}()
	return done
}

// Error returns any error from the session.
// In Pipeline mode, errors are sent as chunks in the response stream.
func (s *duplexSession) Error() error {
	return nil
}

// Variables returns a copy of the current variables.
func (s *duplexSession) Variables() map[string]string {
	s.varsMu.RLock()
	defer s.varsMu.RUnlock()

	vars := make(map[string]string, len(s.variables))
	for k, v := range s.variables {
		vars[k] = v
	}
	return vars
}

// SetVar sets a session variable.
func (s *duplexSession) SetVar(name, value string) {
	s.varsMu.Lock()
	defer s.varsMu.Unlock()
	s.variables[name] = value
}

// GetVar retrieves a session variable.
func (s *duplexSession) GetVar(name string) (string, bool) {
	s.varsMu.RLock()
	defer s.varsMu.RUnlock()
	val, ok := s.variables[name]
	return val, ok
}

// Messages implements BaseSession.
func (s *duplexSession) Messages(ctx context.Context) ([]types.Message, error) {
	state, err := s.store.Load(ctx, s.id)
	if err != nil {
		return nil, err
	}
	return state.Messages, nil
}

// Clear implements BaseSession.
func (s *duplexSession) Clear(ctx context.Context) error {
	state := &statestore.ConversationState{
		ID:       s.id,
		Messages: nil,
	}
	return s.store.Save(ctx, state)
}

// ForkSession implements DuplexSession.
func (s *duplexSession) ForkSession(
	ctx context.Context,
	forkID string,
	pipelineBuilder PipelineBuilder,
) (DuplexSession, error) {
	// Fork the state in the store
	if err := s.store.Fork(ctx, s.id, forkID); err != nil {
		return nil, fmt.Errorf("failed to fork state: %w", err)
	}

	// Copy variables
	s.varsMu.RLock()
	forkVars := make(map[string]string, len(s.variables))
	for k, v := range s.variables {
		forkVars[k] = v
	}
	s.varsMu.RUnlock()

	// Create new duplex session with the builder
	return NewDuplexSession(ctx, &DuplexSessionConfig{
		ConversationID:  forkID,
		StateStore:      s.store,
		PipelineBuilder: pipelineBuilder,
		Provider:        s.provider,
		Variables:       forkVars,
	})
}

// streamChunkToStreamElement converts a providers.StreamChunk to stage.StreamElement.
// This is the boundary conversion for input data.
// Routes media based on MIME type: video/* → VideoData, image/* → ImageData, audio/* → AudioData.
func streamChunkToStreamElement(chunk *providers.StreamChunk) stage.StreamElement {
	elem := stage.StreamElement{
		Metadata: make(map[string]interface{}),
	}

	// Convert media data from MediaDelta based on MIME type
	if chunk.MediaDelta != nil && chunk.MediaDelta.Data != nil {
		mimeType := chunk.MediaDelta.MIMEType
		mediaData := []byte(*chunk.MediaDelta.Data)

		switch {
		case strings.HasPrefix(mimeType, "video/"):
			// Video handling
			elem.Video = &stage.VideoData{
				Data:     mediaData,
				MIMEType: mimeType,
			}
			elem.Priority = stage.PriorityHigh
			// Extract video metadata if available
			if chunk.Metadata != nil {
				if w, ok := chunk.Metadata["width"].(int); ok {
					elem.Video.Width = w
				}
				if h, ok := chunk.Metadata["height"].(int); ok {
					elem.Video.Height = h
				}
				if ts, ok := chunk.Metadata["timestamp"].(time.Time); ok {
					elem.Video.Timestamp = ts
				}
				if kf, ok := chunk.Metadata["is_key_frame"].(bool); ok {
					elem.Video.IsKeyFrame = kf
				}
				if fn, ok := chunk.Metadata["frame_num"].(int64); ok {
					elem.Video.FrameNum = fn
				}
			}

		case strings.HasPrefix(mimeType, "image/"):
			// Image handling
			elem.Image = &stage.ImageData{
				Data:     mediaData,
				MIMEType: mimeType,
			}
			// Extract image metadata if available
			if chunk.Metadata != nil {
				if w, ok := chunk.Metadata["width"].(int); ok {
					elem.Image.Width = w
				}
				if h, ok := chunk.Metadata["height"].(int); ok {
					elem.Image.Height = h
				}
				if ts, ok := chunk.Metadata["timestamp"].(time.Time); ok {
					elem.Image.Timestamp = ts
				}
				if fn, ok := chunk.Metadata["frame_num"].(int64); ok {
					elem.Image.FrameNum = fn
				}
			}

		default:
			// Audio handling (default for backwards compatibility)
			const defaultSampleRate = 16000 // Default for speech
			elem.Audio = &stage.AudioData{
				Samples:    mediaData,
				SampleRate: defaultSampleRate,
				Format:     stage.AudioFormatPCM16,
			}
		}
	}

	// Convert text content
	if chunk.Delta != "" {
		elem.Text = &chunk.Delta
	} else if chunk.Content != "" {
		elem.Text = &chunk.Content
	}

	// Copy metadata
	if chunk.Metadata != nil {
		for k, v := range chunk.Metadata {
			elem.Metadata[k] = v
		}
		// Check for end_of_stream signal in metadata
		if eos, ok := chunk.Metadata["end_of_stream"].(bool); ok && eos {
			elem.EndOfStream = true
		}
	}

	return elem
}

// streamElementToStreamChunk converts a stage.StreamElement to providers.StreamChunk.
// This is the boundary conversion for output data.
func streamElementToStreamChunk(elem *stage.StreamElement) providers.StreamChunk {
	chunk := providers.StreamChunk{}

	// Convert audio data from Audio to MediaDelta
	if elem.Audio != nil && len(elem.Audio.Samples) > 0 {
		audioStr := string(elem.Audio.Samples)
		chunk.MediaDelta = &types.MediaContent{
			MIMEType: types.MIMETypeAudioWAV,
			Data:     &audioStr,
		}
	}

	// Convert text content
	if elem.Text != nil && *elem.Text != "" {
		chunk.Delta = *elem.Text
		chunk.Content = *elem.Text
	}

	// Handle errors
	if elem.Error != nil {
		chunk.Error = elem.Error
	}

	// Copy metadata
	if elem.Metadata != nil {
		chunk.Metadata = elem.Metadata
	}

	return chunk
}
