package file

import (
	"context"
	"fmt"
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
)

// LoadSummaries returns all summaries for the conversation, or nil if none.
func (s *Store) LoadSummaries(_ context.Context, id string) ([]statestore.Summary, error) {
	if id == "" {
		return nil, statestore.ErrInvalidID
	}
	return scanJSONLines[statestore.Summary](s.summariesFile(id))
}

// SaveSummary appends a summary to summaries.jsonl. Auto-creates the conv.
func (s *Store) SaveSummary(_ context.Context, id string, summary statestore.Summary) error {
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
	return appendJSONLine(s.summariesFile(id), &summary, s.fsync)
}
