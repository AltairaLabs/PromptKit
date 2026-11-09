package file

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
)

const listsDirname = "lists"

// listFile returns the path of the named list file for a conversation.
func (s *Store) listFile(id, listName string) string {
	return filepath.Join(s.convDir(id), listsDirname, listName+".jsonl")
}

// AppendList appends opaque-byte items to the named list. Each item becomes
// one line in lists/<listName>.jsonl. Auto-creates the conversation and list.
func (s *Store) AppendList(
	_ context.Context, id, listName string, items [][]byte,
) error {
	if id == "" {
		return statestore.ErrInvalidID
	}
	if listName == "" {
		return fmt.Errorf("filestore: list name is required")
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
	for _, item := range items {
		if err := appendJSONBytes(s.listFile(id, listName), item, s.fsync); err != nil {
			return err
		}
	}
	return nil
}

// LoadList returns all items of the named list in append order. Returns
// (nil, nil) when the list is empty or missing, and ErrNotFound when the
// conversation itself does not exist.
func (s *Store) LoadList(
	_ context.Context, id, listName string,
) ([][]byte, error) {
	if id == "" {
		return nil, statestore.ErrInvalidID
	}
	if err := s.requireConv(id); err != nil {
		return nil, err
	}
	f, err := os.Open(s.listFile(id, listName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("filestore: open list: %w", err)
	}
	defer func() { _ = f.Close() }()

	var out [][]byte
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, bufio.MaxScanTokenSize), scanLineMax)
	for scanner.Scan() {
		b := append([]byte(nil), scanner.Bytes()...)
		out = append(out, b)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("filestore: scan list: %w", err)
	}
	return out, nil
}

// ListLen returns the number of items in the named list: 0 for a missing
// list, ErrNotFound when the conversation itself does not exist.
func (s *Store) ListLen(_ context.Context, id, listName string) (int, error) {
	if id == "" {
		return 0, statestore.ErrInvalidID
	}
	if err := s.requireConv(id); err != nil {
		return 0, err
	}
	return countLines(s.listFile(id, listName))
}

// requireConv returns ErrStoreClosed after Close, and ErrNotFound when the
// conversation has no state.json.
func (s *Store) requireConv(id string) error {
	if err := s.checkOpen(); err != nil {
		return err
	}
	exists, err := s.convExists(id)
	if err != nil {
		return err
	}
	if !exists {
		return statestore.ErrNotFound
	}
	return nil
}
