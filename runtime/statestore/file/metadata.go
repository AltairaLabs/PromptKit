package file

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
)

// LoadMetadata returns the metadata map for the conversation. Returns
// ErrNotFound if no state.json exists for the conv. The returned map is a
// deep copy safe for caller mutation.
func (s *Store) LoadMetadata(_ context.Context, id string) (map[string]any, error) {
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
	out := make(map[string]any, len(snap.Metadata))
	for k, v := range snap.Metadata {
		out[k] = v
	}
	return out, nil
}

// MergeMetadata atomically merges updates into the conversation's metadata.
// Auto-creates the conversation. Read-modify-write under the per-conv lock.
func (s *Store) MergeMetadata(
	_ context.Context, id string, updates map[string]any,
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

	snap, err := s.readStateSnapshot(id)
	if err != nil && !errors.Is(err, statestore.ErrNotFound) {
		return err
	}
	if snap == nil {
		snap = &stateSnapshot{ID: id}
	}
	if snap.Metadata == nil {
		snap.Metadata = make(map[string]any, len(updates))
	}
	for k, v := range updates {
		snap.Metadata[k] = v
	}
	snap.LastAccessedAt = time.Now()

	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("filestore: marshal snapshot: %w", err)
	}
	return writeFileAtomic(s.stateFile(id), data, s.fsync)
}
