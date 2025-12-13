package session

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// bidirectionalSession implements BidirectionalSession with Pipeline integration.
// It manages streaming input/output through the Pipeline middleware chain.
type bidirectionalSession struct {
	id              string
	store           statestore.Store
	providerSession providers.StreamInputSession
	pipeline        *pipeline.Pipeline
	variables       map[string]string
	varsMu          sync.RWMutex
	closeMu         sync.Mutex
	closed          bool

	// Streaming channels for Pipeline integration
	streamInput  chan providers.StreamChunk // Input chunks sent to VAD middleware
	streamOutput chan providers.StreamChunk // Output chunks from Pipeline

	// Pipeline execution control
	executionStarted bool
	executionMu      sync.Mutex
}

const streamBufferSize = 100 // Size of buffered channels for streaming

// NewBidirectionalSession creates a bidirectional session from a config.
// Either Pipeline or ProviderSession must be provided.
func NewBidirectionalSession(cfg *BidirectionalConfig) (BidirectionalSession, error) {
	if cfg.Pipeline == nil && cfg.ProviderSession == nil {
		return nil, fmt.Errorf("either Pipeline or ProviderSession is required")
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
	_, err := store.Load(context.Background(), conversationID)
	if err != nil {
		initialState := &statestore.ConversationState{
			ID:       conversationID,
			UserID:   cfg.UserID,
			Messages: []types.Message{},
			Metadata: cfg.Metadata,
		}
		if err := store.Save(context.Background(), initialState); err != nil {
			return nil, fmt.Errorf("failed to initialize conversation state: %w", err)
		}
	}

	// Initialize variables
	vars := make(map[string]string)
	for k, v := range cfg.Variables {
		vars[k] = v
	}

	sess := &bidirectionalSession{
		id:              conversationID,
		store:           store,
		providerSession: cfg.ProviderSession,
		pipeline:        cfg.Pipeline,
		variables:       vars,
	}

	// Initialize channels if Pipeline mode
	if cfg.Pipeline != nil {
		sess.streamInput = make(chan providers.StreamChunk, streamBufferSize)
		sess.streamOutput = make(chan providers.StreamChunk, streamBufferSize)
	}

	return sess, nil
}

// NewBidirectionalSessionFromProvider creates a bidirectional session from a provider.
// This is the only public constructor - it creates the provider session internally.
func NewBidirectionalSessionFromProvider(
	ctx context.Context,
	conversationID string,
	store statestore.Store,
	provider providers.StreamInputSupport,
	request *providers.StreamInputRequest,
	variables map[string]string,
) (BidirectionalSession, error) {
	// Create the provider session internally
	providerSession, err := provider.CreateStreamSession(ctx, request)
	if err != nil {
		return nil, err
	}

	// Generate conversation ID if not provided
	if conversationID == "" {
		conversationID = uuid.New().String()
	}
	if store == nil {
		store = statestore.NewMemoryStore()
	}

	// Initialize conversation state if it doesn't exist
	_, err = store.Load(context.Background(), conversationID)
	if err != nil {
		initialState := &statestore.ConversationState{
			ID:       conversationID,
			Messages: []types.Message{},
		}
		if err := store.Save(context.Background(), initialState); err != nil {
			return nil, fmt.Errorf("failed to initialize conversation state: %w", err)
		}
	}

	// Initialize variables
	vars := make(map[string]string)
	for k, v := range variables {
		vars[k] = v
	}

	sess := &bidirectionalSession{
		id:              conversationID,
		store:           store,
		providerSession: providerSession,
		variables:       vars,
	}

	return sess, nil
}

// ID returns the unique session identifier.
func (s *bidirectionalSession) ID() string {
	return s.id
}

// SendChunk sends a chunk to the session.
// In Pipeline mode: sends to StreamInput channel for VAD middleware
// In Provider mode: sends directly to provider session
func (s *bidirectionalSession) SendChunk(ctx context.Context, chunk *providers.StreamChunk) error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return fmt.Errorf("session is closed")
	}
	s.closeMu.Unlock()

	if chunk == nil {
		return fmt.Errorf("chunk cannot be nil")
	}

	// Pipeline mode: send to StreamInput for VAD middleware
	if s.pipeline != nil {
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

		// Send chunk to VAD middleware via StreamInput
		select {
		case s.streamInput <- *chunk:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Provider mode: send directly to provider
	// Handle media chunk
	if chunk.MediaDelta != nil {
		return s.sendMediaChunk(ctx, chunk)
	}

	// Handle text chunk
	if chunk.Content != "" || chunk.Delta != "" {
		return s.sendTextChunk(ctx, chunk)
	}

	return fmt.Errorf("chunk must contain either MediaDelta or Content/Delta")
}

// sendMediaChunk handles sending media chunks to the provider.
func (s *bidirectionalSession) sendMediaChunk(ctx context.Context, chunk *providers.StreamChunk) error {
	// Extract data from MediaContent
	var data []byte
	if chunk.MediaDelta.Data != nil {
		data = []byte(*chunk.MediaDelta.Data)
	}

	// Convert Metadata to map[string]string for MediaChunk
	metadata := make(map[string]string)
	if chunk.Metadata != nil {
		for k, v := range chunk.Metadata {
			if str, ok := v.(string); ok {
				metadata[k] = str
			}
		}
	}

	mediaChunk := &types.MediaChunk{
		Data:     data,
		Metadata: metadata,
	}
	return s.providerSession.SendChunk(ctx, mediaChunk)
}

// sendTextChunk handles sending text chunks to the provider.
func (s *bidirectionalSession) sendTextChunk(ctx context.Context, chunk *providers.StreamChunk) error {
	text := chunk.Content
	if text == "" {
		text = chunk.Delta
	}
	return s.providerSession.SendText(ctx, text)
}

// SendText is a convenience method for sending text.
func (s *bidirectionalSession) SendText(ctx context.Context, text string) error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return fmt.Errorf("session is closed")
	}
	s.closeMu.Unlock()

	// Pipeline mode
	if s.pipeline != nil {
		chunk := &providers.StreamChunk{
			Content: text,
		}
		return s.SendChunk(ctx, chunk)
	}

	// Provider mode
	return s.providerSession.SendText(ctx, text)
}

// executePipeline runs the Pipeline with StreamInput/StreamOutput channels.
// This is called once when the first chunk is received.
func (s *bidirectionalSession) executePipeline(ctx context.Context) {
	// Execute Pipeline with pre-configured channels
	// The VAD middleware will block reading from streamInput until turn complete
	// The Provider middleware will stream to streamOutput
	// The TTS middleware will add audio via StreamChunk hook
	err := s.pipeline.ExecuteStreamWithInput(ctx, s.streamInput, s.streamOutput)

	if err != nil {
		// If failed to start, send error and close
		finishReason := "error"
		select {
		case s.streamOutput <- providers.StreamChunk{
			Error:        err,
			FinishReason: &finishReason,
		}:
		default:
		}
		close(s.streamOutput)
	}
	// Note: streamOutput will be closed automatically by Pipeline when execution completes
}

// Response returns the response channel.
// In Pipeline mode: returns StreamOutput from Pipeline
// In Provider mode: returns provider's response channel
func (s *bidirectionalSession) Response() <-chan providers.StreamChunk {
	if s.pipeline != nil {
		return s.streamOutput
	}
	return s.providerSession.Response()
}

// Close ends the session.
func (s *bidirectionalSession) Close() error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true

	// Pipeline mode: close input channel
	if s.pipeline != nil {
		close(s.streamInput)
		return nil
	}

	// Provider mode: close provider session
	return s.providerSession.Close()
}

// Done returns a channel that's closed when the session ends.
func (s *bidirectionalSession) Done() <-chan struct{} {
	if s.pipeline != nil {
		// In Pipeline mode, create done channel that closes when output closes
		done := make(chan struct{})
		go func() {
			// Wait for all output to be consumed
			for range s.streamOutput { //nolint:revive // intentionally draining channel
			}
			close(done)
		}()
		return done
	}
	return s.providerSession.Done()
}

// Error returns any error from the session.
func (s *bidirectionalSession) Error() error {
	if s.pipeline != nil {
		// In Pipeline mode, errors are sent as chunks
		return nil
	}
	return s.providerSession.Error()
}

// StateStore returns the session's state store.
func (s *bidirectionalSession) StateStore() statestore.Store {
	return s.store
}

// Variables returns a copy of the current variables.
func (s *bidirectionalSession) Variables() map[string]string {
	s.varsMu.RLock()
	defer s.varsMu.RUnlock()

	vars := make(map[string]string, len(s.variables))
	for k, v := range s.variables {
		vars[k] = v
	}
	return vars
}

// SetVar sets a session variable.
func (s *bidirectionalSession) SetVar(name, value string) {
	s.varsMu.Lock()
	defer s.varsMu.Unlock()
	s.variables[name] = value
}

// GetVar retrieves a session variable.
func (s *bidirectionalSession) GetVar(name string) (string, bool) {
	s.varsMu.RLock()
	defer s.varsMu.RUnlock()
	val, ok := s.variables[name]
	return val, ok
}
