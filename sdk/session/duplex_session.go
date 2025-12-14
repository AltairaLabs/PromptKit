// Package session provides session abstractions for managing conversations.
// Sessions wrap pipelines and provide convenient APIs for text and duplex streaming interactions.
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

// duplexSession implements BidirectionalSession with Pipeline integration.
// It manages streaming input/output through the Pipeline middleware chain.
//
// Two modes:
// - ASM mode: Creates persistent providerSession, one long-running pipeline execution
// - VAD mode: No providerSession, VAD triggers multiple pipeline executions with one-shot calls
type duplexSession struct {
	id              string
	store           statestore.Store
	pipeline        *pipeline.Pipeline
	provider        providers.Provider           // Provider for LLM calls
	providerSession providers.StreamInputSession // nil for VAD mode, set for ASM mode
	variables       map[string]string
	varsMu          sync.RWMutex
	closeMu         sync.Mutex
	closed          bool

	// Streaming channels - always present for continuous input/output
	streamInput  chan providers.StreamChunk // Consumer sends chunks here
	streamOutput chan providers.StreamChunk // Consumer receives chunks here

	// Pipeline execution control
	executionStarted bool
	executionMu      sync.Mutex
}

//nolint:unused // Used by tests
const streamBufferSize = 100 // Size of buffered channels for streaming

// NewDuplexSession creates a bidirectional session from a config.
// PipelineBuilder and Provider are required.
//
// If Config is provided (ASM mode):
//   - Creates persistent provider session (provider must support StreamInputSupport)
//   - Calls PipelineBuilder with provider and session
//   - Single long-running pipeline execution
//
// If Config is nil (VAD mode):
//   - No provider session created
//   - Calls PipelineBuilder with provider and nil session
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

	// Conditionally create provider session for ASM mode
	var providerSession providers.StreamInputSession
	if cfg.Config != nil {
		// ASM mode: provider must support streaming
		streamProvider, ok := cfg.Provider.(providers.StreamInputSupport)
		if !ok {
			return nil, fmt.Errorf("provider must implement StreamInputSupport for ASM mode")
		}
		providerSession, err = streamProvider.CreateStreamSession(ctx, cfg.Config)
		if err != nil {
			return nil, fmt.Errorf("failed to create provider session: %w", err)
		}
	}

	// Build pipeline with provider and session (session is nil for VAD mode)
	builtPipeline, err := cfg.PipelineBuilder(ctx, cfg.Provider, providerSession, conversationID, store)
	if err != nil {
		if providerSession != nil {
			_ = providerSession.Close()
		}
		return nil, fmt.Errorf("failed to build pipeline: %w", err)
	}

	// Initialize variables
	vars := make(map[string]string)
	for k, v := range cfg.Variables {
		vars[k] = v
	}

	// Create streaming channels for continuous input/output
	streamInput := make(chan providers.StreamChunk, streamBufferSize)
	streamOutput := make(chan providers.StreamChunk, streamBufferSize)

	sess := &duplexSession{
		id:              conversationID,
		store:           store,
		pipeline:        builtPipeline,
		provider:        cfg.Provider,
		providerSession: providerSession, // nil for VAD mode, set for ASM mode
		variables:       vars,
		streamInput:     streamInput,
		streamOutput:    streamOutput,
	}

	return sess, nil
}

// ID returns the unique session identifier.
func (s *duplexSession) ID() string {
	return s.id
}

// SendChunk sends a chunk to the session via the Pipeline.
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

	// Send chunk to Pipeline via StreamInput
	select {
	case s.streamInput <- *chunk:
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

// executePipeline runs the Pipeline with StreamInput/StreamOutput channels.
// This is called once when the first chunk is received.
func (s *duplexSession) executePipeline(ctx context.Context) {
	// Execute Pipeline with pre-configured channels
	// Pipeline middleware processes chunks from streamInput and writes to streamOutput
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
	close(s.streamInput)

	// Close provider session if it exists (ASM mode)
	if s.providerSession != nil {
		return s.providerSession.Close()
	}

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
