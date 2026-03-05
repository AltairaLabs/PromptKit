// Package events provides event storage for session recording and replay.
package events

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// BlobStore provides storage for binary payloads referenced by events.
// This separates large binary data (audio, video, images) from the event stream.
type BlobStore interface {
	// Store saves binary data and returns a storage reference.
	// The reference can be used to retrieve the data later.
	Store(ctx context.Context, sessionID string, data []byte, mimeType string) (*BinaryPayload, error)

	// StoreReader saves binary data from a reader and returns a storage reference.
	// This is more efficient for large payloads.
	StoreReader(ctx context.Context, sessionID string, r io.Reader, mimeType string) (*BinaryPayload, error)

	// Load retrieves binary data by storage reference.
	Load(ctx context.Context, ref string) ([]byte, error)

	// LoadReader returns a reader for binary data by storage reference.
	// The caller is responsible for closing the reader.
	LoadReader(ctx context.Context, ref string) (io.ReadCloser, error)

	// Delete removes binary data by storage reference.
	Delete(ctx context.Context, ref string) error

	// Close releases any resources held by the store.
	Close() error
}

// FileBlobStore implements BlobStore using the local filesystem.
// Blobs are stored in a directory structure: baseDir/sessionID/hash.ext
//
// The store uses atomic write-to-temp-then-rename for crash safety and
// holds locks only for the in-memory deduplication map, not during file I/O.
type FileBlobStore struct {
	baseDir string
	mu      sync.RWMutex
	known   map[string]struct{} // tracks hashes already stored on disk
}

// Blob storage constants.
const (
	blobDirPermissions  = 0750
	blobFilePermissions = 0600
	extWebm             = ".webm"
	fileScheme          = "file://"
)

// NewFileBlobStore creates a file-based blob store.
func NewFileBlobStore(dir string) (*FileBlobStore, error) {
	if err := os.MkdirAll(dir, blobDirPermissions); err != nil {
		return nil, fmt.Errorf("create blob store directory: %w", err)
	}
	return &FileBlobStore{
		baseDir: dir,
		known:   make(map[string]struct{}),
	}, nil
}

// Store saves binary data and returns a storage reference.
// File I/O is performed outside the lock; the lock only protects the
// in-memory deduplication map. Writes use atomic temp-file-then-rename
// to avoid partial-write corruption.
func (s *FileBlobStore) Store(
	ctx context.Context, sessionID string, data []byte, mimeType string,
) (*BinaryPayload, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Calculate content hash for deduplication and integrity
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])

	// Determine file extension from MIME type
	ext := extensionFromMIME(mimeType)
	filename := hashStr + ext

	// Create session directory (safe to call concurrently)
	sessionDir := filepath.Join(s.baseDir, sessionID)
	if err := os.MkdirAll(sessionDir, blobDirPermissions); err != nil {
		return nil, fmt.Errorf("create session blob directory: %w", err)
	}

	path := filepath.Join(sessionDir, filename)
	ref := s.pathToRef(path)
	payload := &BinaryPayload{
		StorageRef: ref,
		MIMEType:   mimeType,
		Size:       int64(len(data)),
		Checksum:   "sha256:" + hashStr,
	}

	// Fast-path: check in-memory dedup map (read lock only)
	s.mu.RLock()
	_, alreadyKnown := s.known[hashStr]
	s.mu.RUnlock()
	if alreadyKnown {
		return payload, nil
	}

	// Check filesystem — no lock held
	if _, err := os.Stat(path); err == nil {
		// File exists on disk; record in map for future fast-path
		s.mu.Lock()
		s.known[hashStr] = struct{}{}
		s.mu.Unlock()
		return payload, nil
	}

	// Atomic write: temp file → rename (no lock held during I/O)
	if err := atomicWriteFile(sessionDir, path, data); err != nil {
		return nil, fmt.Errorf("write blob: %w", err)
	}

	// Record in dedup map
	s.mu.Lock()
	s.known[hashStr] = struct{}{}
	s.mu.Unlock()

	return payload, nil
}

// atomicWriteFile writes data to a temp file in dir and renames it to dest.
func atomicWriteFile(dir, dest string, data []byte) error {
	tmp, err := os.CreateTemp(dir, ".blob-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, writeErr := tmp.Write(data); writeErr != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return writeErr
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, blobFilePermissions); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, dest)
}

// StoreReader saves binary data from a reader and returns a storage reference.
// Data is streamed directly to a temp file, hashed, and then renamed to the
// content-addressable path. This avoids reading the entire stream into memory.
func (s *FileBlobStore) StoreReader(
	ctx context.Context, sessionID string, r io.Reader, mimeType string,
) (*BinaryPayload, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Create session directory
	sessionDir := filepath.Join(s.baseDir, sessionID)
	if err := os.MkdirAll(sessionDir, blobDirPermissions); err != nil {
		return nil, fmt.Errorf("create session blob directory: %w", err)
	}

	// Stream to temp file while computing hash
	tmp, err := os.CreateTemp(sessionDir, ".blob-*")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	hasher := sha256.New()
	writer := io.MultiWriter(tmp, hasher)

	size, err := io.Copy(writer, r)
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("stream blob data: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("close temp file: %w", err)
	}

	hashStr := hex.EncodeToString(hasher.Sum(nil))
	ext := extensionFromMIME(mimeType)
	filename := hashStr + ext
	path := filepath.Join(sessionDir, filename)

	payload := &BinaryPayload{
		StorageRef: s.pathToRef(path),
		MIMEType:   mimeType,
		Size:       size,
		Checksum:   "sha256:" + hashStr,
	}

	// Check dedup
	s.mu.RLock()
	_, alreadyKnown := s.known[hashStr]
	s.mu.RUnlock()
	if alreadyKnown {
		_ = os.Remove(tmpPath)
		return payload, nil
	}

	// Check filesystem
	if _, statErr := os.Stat(path); statErr == nil {
		_ = os.Remove(tmpPath)
		s.mu.Lock()
		s.known[hashStr] = struct{}{}
		s.mu.Unlock()
		return payload, nil
	}

	// Rename temp file to final path
	if err := os.Chmod(tmpPath, blobFilePermissions); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("chmod blob: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("rename blob: %w", err)
	}

	s.mu.Lock()
	s.known[hashStr] = struct{}{}
	s.mu.Unlock()

	return payload, nil
}

// Load retrieves binary data by storage reference.
func (s *FileBlobStore) Load(ctx context.Context, ref string) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	path := s.refToPath(ref)

	data, err := os.ReadFile(path) //nolint:gosec // path from trusted reference
	if err != nil {
		return nil, fmt.Errorf("read blob: %w", err)
	}
	return data, nil
}

// LoadReader returns a reader for binary data by storage reference.
func (s *FileBlobStore) LoadReader(ctx context.Context, ref string) (io.ReadCloser, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	path := s.refToPath(ref)

	f, err := os.Open(path) //nolint:gosec // path from trusted reference
	if err != nil {
		return nil, fmt.Errorf("open blob: %w", err)
	}
	return f, nil
}

// Delete removes binary data by storage reference.
func (s *FileBlobStore) Delete(ctx context.Context, ref string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	path := s.refToPath(ref)

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete blob: %w", err)
	}
	return nil
}

// Close releases any resources.
func (s *FileBlobStore) Close() error {
	// No resources to release for file-based store
	return nil
}

// pathToRef converts a file path to a storage reference.
func (s *FileBlobStore) pathToRef(path string) string {
	rel, err := filepath.Rel(s.baseDir, path)
	if err != nil {
		return fileScheme + path
	}
	return fileScheme + rel
}

// refToPath converts a storage reference to a file path.
func (s *FileBlobStore) refToPath(ref string) string {
	// Strip file:// prefix if present
	if len(ref) > len(fileScheme) && ref[:len(fileScheme)] == fileScheme {
		ref = ref[len(fileScheme):]
	}
	// If absolute path, return as-is
	if filepath.IsAbs(ref) {
		return ref
	}
	// Otherwise, join with base directory
	return filepath.Join(s.baseDir, ref)
}

// extensionFromMIME returns a file extension for common MIME types.
func extensionFromMIME(mimeType string) string {
	switch mimeType {
	// Audio
	case "audio/wav", "audio/wave", "audio/x-wav":
		return ".wav"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/ogg":
		return ".ogg"
	case "audio/opus":
		return ".opus"
	case "audio/flac":
		return ".flac"
	case "audio/L16", "audio/pcm":
		return ".pcm"
	case "audio/webm":
		return extWebm
	// Video
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return extWebm
	case "video/quicktime":
		return ".mov"
	case "video/x-msvideo":
		return ".avi"
	// Images
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	case "image/bmp":
		return ".bmp"
	// Default
	default:
		return ".bin"
	}
}

// Ensure FileBlobStore implements BlobStore.
var _ BlobStore = (*FileBlobStore)(nil)

// EventStoreWithBlobs combines an EventStore with a BlobStore for multimodal recording.
type EventStoreWithBlobs struct {
	EventStore
	BlobStore
}

// NewEventStoreWithBlobs creates a combined event and blob store.
func NewEventStoreWithBlobs(dir string) (*EventStoreWithBlobs, error) {
	eventStore, err := NewFileEventStore(dir)
	if err != nil {
		return nil, fmt.Errorf("create event store: %w", err)
	}

	blobStore, err := NewFileBlobStore(filepath.Join(dir, "blobs"))
	if err != nil {
		return nil, fmt.Errorf("create blob store: %w", err)
	}

	return &EventStoreWithBlobs{
		EventStore: eventStore,
		BlobStore:  blobStore,
	}, nil
}

// Close releases resources from both stores.
func (s *EventStoreWithBlobs) Close() error {
	var errs []error
	if err := s.EventStore.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := s.BlobStore.Close(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("close stores: %v", errs)
	}
	return nil
}
