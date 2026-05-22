package file

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// stateSnapshot is the on-disk representation of state.json. Messages live in
// messages.jsonl, summaries in summaries.jsonl — both are excluded here so
// state.json stays small and append-only updates to messages/summaries are
// cheap.
type stateSnapshot struct {
	ID             string         `json:"id"`
	UserID         string         `json:"user_id,omitempty"`
	SystemPrompt   string         `json:"system_prompt,omitempty"`
	TokenCount     int            `json:"token_count,omitempty"`
	LastAccessedAt time.Time      `json:"last_accessed_at"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// Save atomically writes state.json and reconciles messages.jsonl /
// summaries.jsonl with the provided state. When the on-disk message log is
// shorter than state.Messages, only the delta is appended (matches the Redis
// delta-append branch). When it is longer, the file is rewritten in full.
func (s *Store) Save(_ context.Context, state *statestore.ConversationState) error {
	if state == nil {
		return statestore.ErrInvalidState
	}
	if state.ID == "" {
		return statestore.ErrInvalidID
	}
	if err := s.checkOpen(); err != nil {
		return err
	}

	lock := s.convLock(state.ID)
	lock.Lock()
	defer lock.Unlock()

	if err := os.MkdirAll(s.convDir(state.ID), dirMode); err != nil {
		return fmt.Errorf("filestore: mkdir conv: %w", err)
	}
	if err := s.writeStateSnapshot(state); err != nil {
		return err
	}
	if err := s.reconcileMessagesOnDisk(state.ID, state.Messages); err != nil {
		return err
	}
	return s.reconcileSummariesOnDisk(state.ID, state.Summaries)
}

func (s *Store) writeStateSnapshot(state *statestore.ConversationState) error {
	snap := stateSnapshot{
		ID:             state.ID,
		UserID:         state.UserID,
		SystemPrompt:   state.SystemPrompt,
		TokenCount:     state.TokenCount,
		LastAccessedAt: time.Now(),
		Metadata:       state.Metadata,
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("filestore: marshal snapshot: %w", err)
	}
	return writeFileAtomic(s.stateFile(state.ID), data, s.fsync)
}

func (s *Store) reconcileMessagesOnDisk(id string, msgs []types.Message) error {
	path := s.messagesFile(id)
	onDisk, err := countLines(path)
	if err != nil {
		return err
	}
	want := len(msgs)
	switch {
	case onDisk == want:
		return nil
	case onDisk < want:
		for i := onDisk; i < want; i++ {
			if appendErr := appendJSONLine(path, &msgs[i], s.fsync); appendErr != nil {
				return appendErr
			}
		}
		return nil
	default:
		return s.rewriteMessages(id, msgs)
	}
}

func (s *Store) rewriteMessages(id string, msgs []types.Message) error {
	if len(msgs) == 0 {
		return writeFileAtomic(s.messagesFile(id), nil, s.fsync)
	}
	buf := make([]byte, 0, perMessageBufHint*len(msgs))
	for i := range msgs {
		line, err := json.Marshal(&msgs[i])
		if err != nil {
			return fmt.Errorf("filestore: marshal message: %w", err)
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	return writeFileAtomic(s.messagesFile(id), buf, s.fsync)
}

func (s *Store) reconcileSummariesOnDisk(id string, sums []statestore.Summary) error {
	path := s.summariesFile(id)
	onDisk, err := countLines(path)
	if err != nil {
		return err
	}
	want := len(sums)
	switch {
	case onDisk == want:
		return nil
	case onDisk < want:
		for i := onDisk; i < want; i++ {
			if appendErr := appendJSONLine(path, &sums[i], s.fsync); appendErr != nil {
				return appendErr
			}
		}
		return nil
	default:
		if len(sums) == 0 {
			return writeFileAtomic(path, nil, s.fsync)
		}
		buf := make([]byte, 0, perMessageBufHint*len(sums))
		for i := range sums {
			line, err := json.Marshal(&sums[i])
			if err != nil {
				return fmt.Errorf("filestore: marshal summary: %w", err)
			}
			buf = append(buf, line...)
			buf = append(buf, '\n')
		}
		return writeFileAtomic(path, buf, s.fsync)
	}
}

// Load reads state.json + messages.jsonl + summaries.jsonl into a
// ConversationState. Returns ErrNotFound when state.json is absent.
func (s *Store) Load(_ context.Context, id string) (*statestore.ConversationState, error) {
	if id == "" {
		return nil, statestore.ErrInvalidID
	}
	if err := s.checkOpen(); err != nil {
		return nil, err
	}

	lock := s.convLock(id)
	lock.Lock()
	defer lock.Unlock()

	snap, err := s.readStateSnapshot(id)
	if err != nil {
		return nil, err
	}
	messages, err := scanJSONLines[types.Message](s.messagesFile(id))
	if err != nil {
		return nil, err
	}
	summaries, err := scanJSONLines[statestore.Summary](s.summariesFile(id))
	if err != nil {
		return nil, err
	}

	return &statestore.ConversationState{
		ID:             snap.ID,
		UserID:         snap.UserID,
		SystemPrompt:   snap.SystemPrompt,
		TokenCount:     snap.TokenCount,
		LastAccessedAt: snap.LastAccessedAt,
		Messages:       messages,
		Summaries:      summaries,
		Metadata:       snap.Metadata,
	}, nil
}

// readStateSnapshot reads state.json. Returns ErrNotFound when absent.
// Caller must hold the per-conv lock.
func (s *Store) readStateSnapshot(id string) (*stateSnapshot, error) {
	data, err := os.ReadFile(s.stateFile(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, statestore.ErrNotFound
		}
		return nil, fmt.Errorf("filestore: read state: %w", err)
	}
	var snap stateSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("filestore: unmarshal state: %w", err)
	}
	return &snap, nil
}

// checkOpen returns ErrStoreClosed if the store has been closed.
func (s *Store) checkOpen() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrStoreClosed
	}
	return nil
}

// perMessageBufHint is a rough initial-buffer hint for rewriting messages.
// Picked to comfortably fit a typical short message without re-allocs; the
// slice grows as needed for longer messages.
const perMessageBufHint = 256
