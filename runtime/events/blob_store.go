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
type FileBlobStore struct {
	baseDir string
	mu      sync.RWMutex
}

// Blob storage constants.
const (
	blobDirPermissions  = 0750
	blobFilePermissions = 0600
	extWebm             = ".webm"
)

// NewFileBlobStore creates a file-based blob store.
func NewFileBlobStore(dir string) (*FileBlobStore, error) {
	if err := os.MkdirAll(dir, blobDirPermissions); err != nil {
		return nil, fmt.Errorf("create blob store directory: %w", err)
	}
	return &FileBlobStore{
		baseDir: dir,
	}, nil
}

// Store saves binary data and returns a storage reference.
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

	// Create session directory
	sessionDir := filepath.Join(s.baseDir, sessionID)
	if err := os.MkdirAll(sessionDir, blobDirPermissions); err != nil {
		return nil, fmt.Errorf("create session blob directory: %w", err)
	}

	// Write the file
	path := filepath.Join(sessionDir, filename)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if file already exists (deduplication)
	if _, err := os.Stat(path); err == nil {
		// File exists, return reference without rewriting
		return &BinaryPayload{
			StorageRef: s.pathToRef(path),
			MIMEType:   mimeType,
			Size:       int64(len(data)),
			Checksum:   "sha256:" + hashStr,
		}, nil
	}

	if err := os.WriteFile(path, data, blobFilePermissions); err != nil {
		return nil, fmt.Errorf("write blob: %w", err)
	}

	return &BinaryPayload{
		StorageRef: s.pathToRef(path),
		MIMEType:   mimeType,
		Size:       int64(len(data)),
		Checksum:   "sha256:" + hashStr,
	}, nil
}

// StoreReader saves binary data from a reader and returns a storage reference.
func (s *FileBlobStore) StoreReader(
	ctx context.Context, sessionID string, r io.Reader, mimeType string,
) (*BinaryPayload, error) {
	// For simplicity, read all data into memory
	// A more sophisticated implementation could stream to a temp file and rename
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read blob data: %w", err)
	}
	return s.Store(ctx, sessionID, data, mimeType)
}

// Load retrieves binary data by storage reference.
func (s *FileBlobStore) Load(ctx context.Context, ref string) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	path := s.refToPath(ref)
	s.mu.RLock()
	defer s.mu.RUnlock()

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
	s.mu.RLock()
	defer s.mu.RUnlock()

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
	s.mu.Lock()
	defer s.mu.Unlock()

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
		return "file://" + path
	}
	return "file://" + rel
}

// refToPath converts a storage reference to a file path.
func (s *FileBlobStore) refToPath(ref string) string {
	// Strip file:// prefix if present
	if len(ref) > 7 && ref[:7] == "file://" {
		ref = ref[7:]
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
