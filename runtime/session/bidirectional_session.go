package session

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// bidirectionalSession implements BidirectionalSession by wrapping a provider's StreamInputSession.
type bidirectionalSession struct {
	id              string
	store           statestore.Store
	providerSession providers.StreamInputSession
	variables       map[string]string
	varsMu          sync.RWMutex
	closeMu         sync.Mutex
	closed          bool
}

// newBidirectionalSession creates a new bidirectional session implementation.
func newBidirectionalSession(cfg *BidirectionalConfig) (*bidirectionalSession, error) {
	if cfg.ConversationID == "" {
		cfg.ConversationID = uuid.New().String()
	}
	if cfg.StateStore == nil {
		cfg.StateStore = statestore.NewMemoryStore()
	}
	if cfg.ProviderSession == nil {
		return nil, fmt.Errorf("provider session is required")
	}

	// Initialize conversation state if it doesn't exist
	_, err := cfg.StateStore.Load(context.Background(), cfg.ConversationID)
	if err != nil {
		initialState := &statestore.ConversationState{
			ID:       cfg.ConversationID,
			UserID:   cfg.UserID,
			Messages: []types.Message{},
			Metadata: cfg.Metadata,
		}
		if err := cfg.StateStore.Save(context.Background(), initialState); err != nil {
			return nil, fmt.Errorf("failed to initialize conversation state: %w", err)
		}
	}

	// Initialize variables
	variables := make(map[string]string)
	if cfg.Variables != nil {
		for k, v := range cfg.Variables {
			variables[k] = v
		}
	}

	return &bidirectionalSession{
		id:              cfg.ConversationID,
		store:           cfg.StateStore,
		providerSession: cfg.ProviderSession,
		variables:       variables,
	}, nil
}

// ID returns the unique session identifier.
func (s *bidirectionalSession) ID() string {
	return s.id
}

// SendChunk sends a chunk to the provider session.
// Input chunks should populate MediaDelta for media or Content for text.
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

	return s.providerSession.SendText(ctx, text)
}

// Response returns the response channel from the provider session.
func (s *bidirectionalSession) Response() <-chan providers.StreamChunk {
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
	return s.providerSession.Close()
}

// Done returns a channel that's closed when the session ends.
func (s *bidirectionalSession) Done() <-chan struct{} {
	return s.providerSession.Done()
}

// Error returns any error from the provider session.
func (s *bidirectionalSession) Error() error {
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
