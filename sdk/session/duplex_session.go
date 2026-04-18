// Package session provides session abstractions for managing conversations.
// Sessions wrap pipelines and provide convenient APIs for text and duplex streaming interactions.
package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/streaming"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const errSessionClosed = "session is closed"

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
	toolRegistry     *tools.Registry  // Optional: for executing tool calls (client tools, etc.)
	asyncToolChecker AsyncToolChecker // Optional: HITL gate for tool calls
	variables        map[string]string
	varsMu           sync.RWMutex
	closeMu          sync.Mutex
	closed           bool

	// Internal pipeline channel (stage.StreamElement)
	stageInput chan stage.StreamElement // Feeds converted elements to pipeline

	// External API channel (providers.StreamChunk)
	streamOutput chan providers.StreamChunk // Consumer receives chunks here

	// Pipeline execution control
	executionStarted bool
	executionMu      sync.Mutex

	// sessionCtx is a session-level context created during NewDuplexSession.
	// It is used for pipeline execution instead of the first SendChunk's context,
	// ensuring the pipeline outlives any single RPC call.
	sessionCtx    context.Context
	sessionCancel context.CancelFunc

	// pipelineDone is closed when executePipeline returns, signaling that the session has ended.
	// This is a separate signal from streamOutput so that Done() does not drain the output channel.
	pipelineDone chan struct{}

	// Done channel (initialized once via sync.Once)
	doneCh   chan struct{}
	doneOnce sync.Once
}

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

	sessionCtx, sessionCancel := context.WithCancel(context.Background())

	sess := &duplexSession{
		id:               conversationID,
		store:            store,
		pipeline:         builtPipeline,
		provider:         cfg.Provider,
		toolRegistry:     cfg.ToolRegistry,
		asyncToolChecker: cfg.AsyncToolChecker,
		variables:        vars,
		stageInput:       stageInput,
		streamOutput:     streamOutput,
		sessionCtx:       sessionCtx,
		sessionCancel:    sessionCancel,
		pipelineDone:     make(chan struct{}),
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
		return errors.New(errSessionClosed)
	}
	s.closeMu.Unlock()

	if chunk == nil {
		return fmt.Errorf("chunk cannot be nil")
	}

	// Start Pipeline execution on first chunk using session-level context
	s.executionMu.Lock()
	if !s.executionStarted {
		s.executionStarted = true
		s.executionMu.Unlock()

		// Start Pipeline execution in background using the session-level context
		// rather than the first SendChunk's context, so the pipeline outlives any
		// single caller.
		go s.executePipeline(s.sessionCtx)
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
		return errors.New(errSessionClosed)
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
		return errors.New(errSessionClosed)
	}
	s.closeMu.Unlock()

	if frame == nil || len(frame.Data) == 0 {
		return fmt.Errorf("frame data is required")
	}

	chunk := &providers.StreamChunk{
		MediaData: &providers.StreamMediaData{
			Data:     frame.Data,
			MIMEType: frame.MIMEType,
			Width:    frame.Width,
			Height:   frame.Height,
			FrameNum: frame.FrameNum,
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
		return errors.New(errSessionClosed)
	}
	s.closeMu.Unlock()

	if vchunk == nil || len(vchunk.Data) == 0 {
		return fmt.Errorf("video chunk data is required")
	}

	chunk := &providers.StreamChunk{
		MediaData: &providers.StreamMediaData{
			Data:       vchunk.Data,
			MIMEType:   vchunk.MIMEType,
			Width:      vchunk.Width,
			Height:     vchunk.Height,
			FrameNum:   int64(vchunk.ChunkIndex),
			IsKeyFrame: vchunk.IsKeyFrame,
		},
	}
	return s.SendChunk(ctx, chunk)
}

// executePipeline runs the stage pipeline with streaming input/output.
// This is called once when the first chunk is received.
// Converts stage.StreamElement output to providers.StreamChunk at the boundary.
//
// When a tool registry is configured, tool calls from the LLM are intercepted:
//   - Sync-handled tools: results are sent back to the pipeline via stageInput
//   - Pending (deferred) tools: surfaced to the caller via pending_tools metadata
//
//nolint:gocognit // Complexity is acceptable for pipeline orchestration with tool handling
func (s *duplexSession) executePipeline(ctx context.Context) {
	defer close(s.pipelineDone)
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

	// Forward stage output to streamOutput, converting StreamElement to StreamChunk.
	// When a tool registry is present, intercept tool call elements.
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

			action, actionErr := streaming.ProcessResponseElement(&elem, "duplexSession")

			switch action {
			case streaming.ResponseActionToolCalls:
				if err := s.handleToolCalls(ctx, &elem); err != nil {
					logger.Error("duplexSession: tool handling failed", "error", err)
					errChunk := providers.StreamChunk{Error: err}
					select {
					case s.streamOutput <- errChunk:
					case <-ctx.Done():
					}
					return
				}

			case streaming.ResponseActionError:
				// Forward the error to the caller
				errChunk := providers.StreamChunk{Error: actionErr}
				select {
				case s.streamOutput <- errChunk:
				case <-ctx.Done():
				}
				return

			case streaming.ResponseActionContinue, streaming.ResponseActionComplete:
				chunk := streamElementToStreamChunk(&elem)
				select {
				case s.streamOutput <- chunk:
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

// handleToolCalls processes tool calls from a response element.
// If no tool registry is configured, the element is forwarded as-is.
// Otherwise, tools are executed via the registry:
//   - Completed tools: results sent back to the pipeline
//   - Pending tools: forwarded to the caller with pending_tools metadata
func (s *duplexSession) handleToolCalls(ctx context.Context, elem *stage.StreamElement) error {
	// No registry — forward the element as-is (caller must handle tool calls)
	if s.toolRegistry == nil {
		chunk := streamElementToStreamChunk(elem)
		select {
		case s.streamOutput <- chunk:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}

	toolCalls := elem.Message.ToolCalls
	logger.Debug("duplexSession: intercepting tool calls",
		"count", len(toolCalls))

	duplexResult := executeDuplexToolCalls(s.toolRegistry, toolCalls, s.asyncToolChecker)

	// Send completed tool results back to the pipeline
	if len(duplexResult.Completed.ProviderResponses) > 0 {
		if err := streaming.SendToolResults(ctx, duplexResult.Completed, s.stageInput); err != nil {
			return fmt.Errorf("failed to send tool results: %w", err)
		}
	}

	// Surface pending tools to the caller
	if len(duplexResult.Pending) > 0 {
		if elem.Metadata == nil {
			elem.Metadata = make(map[string]interface{})
		}
		elem.Metadata["pending_tools"] = duplexResult.Pending
		chunk := streamElementToStreamChunk(elem)
		select {
		case s.streamOutput <- chunk:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// Response returns the response channel from the Pipeline.
func (s *duplexSession) Response() <-chan providers.StreamChunk {
	return s.streamOutput
}

// DefaultDrainTimeout is the maximum time to wait for pipeline completion during Drain.
const DefaultDrainTimeout = 30 * time.Second

// Drain gracefully stops the session by sending an EndOfStream signal to the
// pipeline and waiting for it to finish processing. If the context expires
// before the pipeline completes, Drain falls back to a hard Close.
func (s *duplexSession) Drain(ctx context.Context) error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return nil
	}
	s.closeMu.Unlock()

	// Send EndOfStream signal so the pipeline knows input is done.
	select {
	case s.stageInput <- stage.StreamElement{EndOfStream: true}:
	case <-ctx.Done():
		// Context expired before we could send — fall back to hard close.
		return s.Close()
	}

	// Wait for pipeline to complete or context to expire.
	select {
	case <-s.pipelineDone:
		// Pipeline finished gracefully — now close.
		return s.Close()
	case <-ctx.Done():
		// Timeout — force close.
		_ = s.Close()
		return fmt.Errorf("drain timed out: %w", ctx.Err())
	}
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
	s.sessionCancel()
	// Note: DuplexProviderStage manages the provider session and closes it when pipeline completes

	return nil
}

// Done returns a channel that's closed when the session ends.
// The channel is initialized once on first call; subsequent calls return the same channel.
// Done monitors the session's done signal rather than draining streamOutput, so it
// does not compete with Response() for output chunks.
func (s *duplexSession) Done() <-chan struct{} {
	s.doneOnce.Do(func() {
		s.doneCh = make(chan struct{})
		go func() {
			// Wait for the pipeline to finish by monitoring streamOutput closure
			// without consuming any elements. This avoids stealing chunks from
			// consumers reading via Response().
			<-s.pipelineDone
			close(s.doneCh)
		}()
	})
	return s.doneCh
}

// Error returns any error from the session.
// In Pipeline mode, errors are sent as chunks in the response stream.
func (s *duplexSession) Error() error {
	return nil
}

// SubmitToolResults sends resolved/rejected tool results back into the
// duplex pipeline so they flow to the provider via ToolResponseSupport.
func (s *duplexSession) SubmitToolResults(ctx context.Context, responses []providers.ToolResponse) error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.closeMu.Unlock()

	result := &streaming.ToolExecutionResult{
		ProviderResponses: responses,
		ResultMessages:    make([]types.Message, 0, len(responses)),
	}

	for _, resp := range responses {
		toolResult := types.NewTextToolResult(resp.ToolCallID, "", resp.Result)
		if resp.IsError {
			toolResult.Error = resp.Result
		}
		result.ResultMessages = append(result.ResultMessages, types.NewToolResultMessage(toolResult))
	}

	return streaming.SendToolResults(ctx, result, s.stageInput)
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
		ConversationID:   forkID,
		StateStore:       s.store,
		PipelineBuilder:  pipelineBuilder,
		Provider:         s.provider,
		ToolRegistry:     s.toolRegistry,
		AsyncToolChecker: s.asyncToolChecker,
		Variables:        forkVars,
	})
}

// applyVideoMetadata extracts video-specific metadata from chunk metadata into VideoData.
func applyVideoMetadata(video *stage.VideoData, metadata map[string]any) {
	if metadata == nil {
		return
	}
	if w, ok := metadata["width"].(int); ok {
		video.Width = w
	}
	if h, ok := metadata["height"].(int); ok {
		video.Height = h
	}
	if ts, ok := metadata["timestamp"].(time.Time); ok {
		video.Timestamp = ts
	}
	if kf, ok := metadata["is_key_frame"].(bool); ok {
		video.IsKeyFrame = kf
	}
	if fn, ok := metadata["frame_num"].(int64); ok {
		video.FrameNum = fn
	}
}

// applyImageMetadata extracts image-specific metadata from chunk metadata into ImageData.
func applyImageMetadata(image *stage.ImageData, metadata map[string]any) {
	if metadata == nil {
		return
	}
	if w, ok := metadata["width"].(int); ok {
		image.Width = w
	}
	if h, ok := metadata["height"].(int); ok {
		image.Height = h
	}
	if ts, ok := metadata["timestamp"].(time.Time); ok {
		image.Timestamp = ts
	}
	if fn, ok := metadata["frame_num"].(int64); ok {
		image.FrameNum = fn
	}
}

// convertMediaData converts a chunk's MediaData to the appropriate stage element field.
func convertMediaData(elem *stage.StreamElement, chunk *providers.StreamChunk) {
	if chunk.MediaData == nil || len(chunk.MediaData.Data) == 0 {
		return
	}

	mimeType := chunk.MediaData.MIMEType
	mediaData := chunk.MediaData.Data // already []byte, no copy

	switch {
	case strings.HasPrefix(mimeType, "video/"):
		elem.Video = &stage.VideoData{
			Data:       mediaData,
			MIMEType:   mimeType,
			Width:      chunk.MediaData.Width,
			Height:     chunk.MediaData.Height,
			FrameRate:  chunk.MediaData.FrameRate,
			IsKeyFrame: chunk.MediaData.IsKeyFrame,
			FrameNum:   chunk.MediaData.FrameNum,
		}
		elem.Priority = stage.PriorityHigh
		applyVideoMetadata(elem.Video, chunk.Metadata)

	case strings.HasPrefix(mimeType, "image/"):
		elem.Image = &stage.ImageData{
			Data:     mediaData,
			MIMEType: mimeType,
			Width:    chunk.MediaData.Width,
			Height:   chunk.MediaData.Height,
			FrameNum: chunk.MediaData.FrameNum,
		}
		applyImageMetadata(elem.Image, chunk.Metadata)

	default:
		// Audio handling (default for backwards compatibility)
		sampleRate := chunk.MediaData.SampleRate
		if sampleRate == 0 {
			sampleRate = 16000
		}
		channels := chunk.MediaData.Channels
		if channels == 0 {
			channels = 1
		}
		elem.Audio = &stage.AudioData{
			Samples:    mediaData,
			SampleRate: sampleRate,
			Channels:   channels,
			Format:     stage.AudioFormatPCM16,
		}
	}
}

// streamChunkToStreamElement converts a providers.StreamChunk to stage.StreamElement.
// This is the boundary conversion for input data.
// Routes media based on MIME type: video/* -> VideoData, image/* -> ImageData, audio/* -> AudioData.
func streamChunkToStreamElement(chunk *providers.StreamChunk) stage.StreamElement {
	elem := stage.StreamElement{
		Metadata: make(map[string]interface{}),
	}

	convertMediaData(&elem, chunk)

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

	// Convert audio data from Audio to MediaData
	if elem.Audio != nil && len(elem.Audio.Samples) > 0 {
		chunk.MediaData = &providers.StreamMediaData{
			Data:       elem.Audio.Samples,
			MIMEType:   "audio/pcm",
			SampleRate: elem.Audio.SampleRate,
			Channels:   elem.Audio.Channels,
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

		// Detect pending client tools (pipeline suspended)
		if pt, ok := elem.Metadata["pending_tools"]; ok {
			if pending, ok := pt.([]tools.PendingToolExecution); ok && len(pending) > 0 {
				chunk.PendingTools = pending
				chunk.FinishReason = strPtr("pending_tools")
			}
		}
	}

	return chunk
}
