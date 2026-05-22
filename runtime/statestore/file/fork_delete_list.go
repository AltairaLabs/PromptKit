package file

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
)

const defaultListLimit = 100

// Fork copies all files in <root>/conv-<sourceID>/ to <root>/conv-<newID>/.
// Returns ErrNotFound when source is missing.
func (s *Store) Fork(_ context.Context, sourceID, newID string) error {
	if sourceID == "" || newID == "" {
		return statestore.ErrInvalidID
	}
	if err := s.checkOpen(); err != nil {
		return err
	}

	// Lock in stable order so two goroutines forking in opposite directions
	// can't deadlock.
	first, second := sourceID, newID
	if first > second {
		first, second = second, first
	}
	l1, l2 := s.convLock(first), s.convLock(second)
	l1.Lock()
	defer l1.Unlock()
	if first != second {
		l2.Lock()
		defer l2.Unlock()
	}

	if _, err := os.Stat(s.stateFile(sourceID)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return statestore.ErrNotFound
		}
		return fmt.Errorf("filestore: stat source: %w", err)
	}

	dstDir := s.convDir(newID)
	if err := os.MkdirAll(dstDir, dirMode); err != nil {
		return fmt.Errorf("filestore: mkdir dest: %w", err)
	}
	return copyDirContents(s.convDir(sourceID), dstDir, newID)
}

// Delete removes the entire conv-<id> directory. No-op when absent.
func (s *Store) Delete(_ context.Context, id string) error {
	if id == "" {
		return statestore.ErrInvalidID
	}
	if err := s.checkOpen(); err != nil {
		return err
	}
	lock := s.convLock(id)
	lock.Lock()
	defer lock.Unlock()

	if err := os.RemoveAll(s.convDir(id)); err != nil {
		return fmt.Errorf("filestore: remove conv: %w", err)
	}
	s.mu.Lock()
	delete(s.locks, id)
	s.mu.Unlock()
	return nil
}

// List returns conversation IDs filtered by ListOptions. The UserID filter
// reads each candidate's state.json (no separate user index in v1; ample for
// typical scales).
func (s *Store) List(_ context.Context, opts statestore.ListOptions) ([]string, error) {
	if err := s.checkOpen(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("filestore: read root: %w", err)
	}

	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), convDirPrefix) {
			continue
		}
		id := strings.TrimPrefix(e.Name(), convDirPrefix)
		if opts.UserID != "" {
			snap, rerr := s.readStateSnapshot(id)
			if rerr != nil || snap.UserID != opts.UserID {
				continue
			}
		}
		out = append(out, id)
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if opts.Offset >= len(out) {
		return []string{}, nil
	}
	out = out[opts.Offset:]
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func copyDirContents(srcDir, dstDir, newID string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("filestore: read src: %w", err)
	}
	for _, e := range entries {
		srcPath := filepath.Join(srcDir, e.Name())
		dstPath := filepath.Join(dstDir, e.Name())
		if e.IsDir() {
			if mkErr := os.MkdirAll(dstPath, dirMode); mkErr != nil {
				return fmt.Errorf("filestore: mkdir nested: %w", mkErr)
			}
			if recErr := copyDirContents(srcPath, dstPath, newID); recErr != nil {
				return recErr
			}
			continue
		}
		if cpErr := copyFile(srcPath, dstPath); cpErr != nil {
			return cpErr
		}
	}
	return rewriteForkedStateID(dstDir, newID)
}

func copyFile(src, dst string) error {
	//nolint:gosec // src is built from store-controlled convDir + listed entries
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("filestore: open src file: %w", err)
	}
	defer func() { _ = in.Close() }()

	//nolint:gosec // dst is built from store-controlled convDir + listed entries
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fileMode)
	if err != nil {
		return fmt.Errorf("filestore: create dst file: %w", err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("filestore: copy file: %w", err)
	}
	return nil
}

// rewriteForkedStateID updates the ID inside the forked state.json so a
// subsequent Load(newID) returns a state whose ID matches the new conv.
func rewriteForkedStateID(dstDir, newID string) error {
	statePath := filepath.Join(dstDir, stateFilename)
	//nolint:gosec // statePath is built from store-controlled convDir + constant filename
	data, err := os.ReadFile(statePath)
	if err != nil {
		return fmt.Errorf("filestore: read forked state: %w", err)
	}
	var snap stateSnapshot
	if uerr := json.Unmarshal(data, &snap); uerr != nil {
		return fmt.Errorf("filestore: unmarshal forked state: %w", uerr)
	}
	snap.ID = newID
	out, err := json.Marshal(&snap)
	if err != nil {
		return fmt.Errorf("filestore: marshal forked state: %w", err)
	}
	return os.WriteFile(statePath, out, fileMode)
}
