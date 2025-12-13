package session

import (
	"context"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/google/uuid"
)

type textSession struct {
	id        string
	store     statestore.Store
	pipeline  *pipeline.Pipeline
	variables map[string]string
	mu        sync.RWMutex
}

// NewTextSession creates a new text session.
func NewTextSession(cfg TextConfig) (TextSession, error) {
	if cfg.ConversationID == "" {
		cfg.ConversationID = uuid.New().String()
	}
	if cfg.StateStore == nil {
		cfg.StateStore = statestore.NewMemoryStore()
	}
	if cfg.Pipeline == nil {
		return nil, fmt.Errorf("pipeline is required")
	}

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

	return &textSession{
		id:        cfg.ConversationID,
		store:     cfg.StateStore,
		pipeline:  cfg.Pipeline,
		variables: cfg.Variables,
	}, nil
}

func (s *textSession) ID() string {
	return s.id
}

func (s *textSession) Execute(ctx context.Context, role, content string) (*pipeline.ExecutionResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pipeline.Execute(ctx, role, content)
}

func (s *textSession) ExecuteWithMessage(ctx context.Context, message types.Message) (*pipeline.ExecutionResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pipeline.ExecuteWithMessage(ctx, message)
}

func (s *textSession) ExecuteStream(ctx context.Context, role, content string) (<-chan providers.StreamChunk, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pipeline.ExecuteStream(ctx, role, content)
}

func (s *textSession) ExecuteStreamWithMessage(ctx context.Context, message types.Message) (<-chan providers.StreamChunk, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// TODO: Add ExecuteStreamWithMessage to pipeline
	return s.pipeline.ExecuteStream(ctx, message.Role, message.Content)
}

// SetVar sets a template variable that will be available for substitution.
func (s *textSession) SetVar(name, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.variables == nil {
		s.variables = make(map[string]string)
	}
	s.variables[name] = value
}

// GetVar retrieves the value of a template variable.
func (s *textSession) GetVar(name string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.variables[name]
}

// Variables returns a copy of all template variables.
func (s *textSession) Variables() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	vars := make(map[string]string, len(s.variables))
	for k, v := range s.variables {
		vars[k] = v
	}
	return vars
}

// StateStore returns the session's state store.
func (s *textSession) StateStore() statestore.Store {
	return s.store
}
