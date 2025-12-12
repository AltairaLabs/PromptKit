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

type bidirectionalSession struct {
	id       string
	store    statestore.Store
	pipeline *pipeline.Pipeline
	mu       sync.RWMutex
}

func newBidirectionalSession(cfg BidirectionalConfig) (*bidirectionalSession, error) {
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

	return &bidirectionalSession{
		id:       cfg.ConversationID,
		store:    cfg.StateStore,
		pipeline: cfg.Pipeline,
	}, nil
}

func (s *bidirectionalSession) ID() string {
	return s.id
}

func (s *bidirectionalSession) Connect(ctx context.Context, providerSession providers.StreamInputSession) error {
	if providerSession == nil {
		return fmt.Errorf("provider session is required")
	}

	// Monitor the provider session for turn events
	// Caller accesses output via providerSession.Response()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-providerSession.Done():
			return providerSession.Error()
		case chunk, ok := <-providerSession.Response():
			if !ok {
				return nil
			}
			// TODO: Detect turn events from chunk.Metadata and execute pipeline
			_ = chunk
		}
	}
}
