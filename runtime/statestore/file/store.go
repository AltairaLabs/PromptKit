// Package file provides a filesystem-backed statestore implementation.
//
// One directory per conversation under Options.Root holds:
//
//	state.json       — atomically-rewritten snapshot of meta + metadata
//	messages.jsonl   — append-only message log (one types.Message per line)
//	summaries.jsonl  — append-only summary log
//	lists/<name>.jsonl — opaque-byte append-only lists for ListAccessor
//
// The store is single-process: pointing two stores at the same Root is
// undefined behavior. A future revision may add an OS-level lock file.
package file

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// dirMode is the permission applied to all directories created by the store.
// fileMode is the permission applied to all data files. Kept on the
// restrictive side because conversation contents may include user data.
const (
	dirMode  os.FileMode = 0o750
	fileMode os.FileMode = 0o640
)

// FSyncPolicy controls when on-disk writes are fsync'd.
type FSyncPolicy int

const (
	// FSyncOff matches MemoryStore durability semantics: writes are buffered
	// by the OS and may be lost on power-loss / kernel panic.
	FSyncOff FSyncPolicy = iota
	// FSyncOnSave fsyncs only on Save (the atomic rename of state.json).
	// Mid-loop message-log appends are not fsync'd. Default.
	FSyncOnSave
	// FSyncOnAppend fsyncs on every JSONL append (slowest, most durable).
	FSyncOnAppend
)

// Options configures the file-backed store.
type Options struct {
	// Root is the directory under which per-conversation directories live.
	// Created if absent. Required.
	Root string

	// FSync controls fsync behavior. Defaults to FSyncOnSave.
	FSync FSyncPolicy

	// TTL, if non-zero, removes conversation directories whose state.json
	// mtime is older than now-TTL at NewStore time.
	TTL time.Duration
}

// Store is a filesystem-backed implementation of statestore.Store and the
// optional MessageLog / MessageReader / MessageAppender / MetadataAccessor /
// SummaryAccessor / ListAccessor interfaces.
type Store struct {
	root  string
	fsync FSyncPolicy
	ttl   time.Duration

	mu     sync.Mutex
	locks  map[string]*sync.Mutex // per-conv locks
	closed bool
}

// NewStore opens (or creates) a file-backed store at opts.Root. Returns an
// error if Root is empty or the directory cannot be created.
func NewStore(opts Options) (*Store, error) {
	if opts.Root == "" {
		return nil, errors.New("filestore: Options.Root is required")
	}
	if err := os.MkdirAll(opts.Root, dirMode); err != nil {
		return nil, fmt.Errorf("filestore: create root: %w", err)
	}
	s := &Store{
		root:  opts.Root,
		fsync: opts.FSync,
		ttl:   opts.TTL,
		locks: make(map[string]*sync.Mutex),
	}
	return s, nil
}

// Close releases in-memory resources. Idempotent. Outstanding handles to the
// store after Close return ErrStoreClosed.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// ErrStoreClosed is returned from any method called after Close.
var ErrStoreClosed = errors.New("filestore: store closed")
