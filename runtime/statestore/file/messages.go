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

// LogAppend appends messages with clamp-and-skip idempotent dedup. Mirrors
// MemoryStore.LogAppend / RedisStore.LogAppend semantics exactly. Also writes
// a minimal state.json stub on first append so Load(id) works during crash
// recovery before the end-of-turn Save runs.
func (s *Store) LogAppend(
	_ context.Context, id string, startSeq int, messages []types.Message,
) (int, error) {
	if id == "" {
		return 0, statestore.ErrInvalidID
	}
	if err := s.checkOpen(); err != nil {
		return 0, err
	}

	lock := s.convLock(id)
	lock.Lock()
	defer lock.Unlock()

	current, err := countLines(s.messagesFile(id))
	if err != nil {
		return 0, err
	}
	skip := current - startSeq
	if skip < 0 {
		skip = 0
	}
	if skip >= len(messages) {
		return current, nil
	}
	delta := messages[skip:]

	if err := os.MkdirAll(s.convDir(id), dirMode); err != nil {
		return 0, fmt.Errorf("filestore: mkdir conv: %w", err)
	}
	if err := s.ensureStateStub(id); err != nil {
		return 0, err
	}
	for i := range delta {
		if appendErr := appendJSONLine(s.messagesFile(id), &delta[i], s.fsync); appendErr != nil {
			return 0, appendErr
		}
	}
	return current + len(delta), nil
}

// LogLoad returns messages for the conversation. If recent > 0, returns only
// the last N. Returns nil (not an error) when the file doesn't exist.
func (s *Store) LogLoad(_ context.Context, id string, recent int) ([]types.Message, error) {
	if id == "" {
		return nil, statestore.ErrInvalidID
	}
	all, err := scanJSONLines[types.Message](s.messagesFile(id))
	if err != nil {
		return nil, err
	}
	if recent > 0 && recent < len(all) {
		return all[len(all)-recent:], nil
	}
	return all, nil
}

// LogLen returns the message count, or 0 if absent.
func (s *Store) LogLen(_ context.Context, id string) (int, error) {
	if id == "" {
		return 0, statestore.ErrInvalidID
	}
	return countLines(s.messagesFile(id))
}

// LoadRecentMessages returns the last n messages. Returns ErrNotFound if the
// conversation has no state.json (the caller asked about a conv that doesn't
// exist).
func (s *Store) LoadRecentMessages(
	ctx context.Context, id string, n int,
) ([]types.Message, error) {
	if id == "" {
		return nil, statestore.ErrInvalidID
	}
	exists, err := s.convExists(id)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, statestore.ErrNotFound
	}
	return s.LogLoad(ctx, id, n)
}

// MessageCount returns the total number of messages. Returns ErrNotFound when
// the conversation has no state.json.
func (s *Store) MessageCount(_ context.Context, id string) (int, error) {
	if id == "" {
		return 0, statestore.ErrInvalidID
	}
	exists, err := s.convExists(id)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, statestore.ErrNotFound
	}
	return countLines(s.messagesFile(id))
}

// AppendMessages appends without dedup. Auto-creates the conversation.
// Differs from LogAppend in that it takes no startSeq — used by pipeline
// paths that already track their own offset.
func (s *Store) AppendMessages(
	_ context.Context, id string, messages []types.Message,
) error {
	if id == "" {
		return statestore.ErrInvalidID
	}
	if err := s.checkOpen(); err != nil {
		return err
	}
	lock := s.convLock(id)
	lock.Lock()
	defer lock.Unlock()

	if err := os.MkdirAll(s.convDir(id), dirMode); err != nil {
		return fmt.Errorf("filestore: mkdir conv: %w", err)
	}
	if err := s.ensureStateStub(id); err != nil {
		return err
	}
	for i := range messages {
		if appendErr := appendJSONLine(s.messagesFile(id), &messages[i], s.fsync); appendErr != nil {
			return appendErr
		}
	}
	return nil
}

// ensureStateStub writes a minimal state.json if none exists, so Load(id)
// resolves after a crash before any Save has run. Idempotent — Save will
// overwrite the stub with the full state.
func (s *Store) ensureStateStub(id string) error {
	path := s.stateFile(id)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	snap := stateSnapshot{ID: id, LastAccessedAt: time.Now()}
	data, err := json.Marshal(&snap)
	if err != nil {
		return fmt.Errorf("filestore: marshal stub: %w", err)
	}
	return writeFileAtomic(path, data, s.fsync)
}

// convExists reports whether a state.json exists for id.
func (s *Store) convExists(id string) (bool, error) {
	_, err := os.Stat(s.stateFile(id))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("filestore: stat state: %w", err)
}
