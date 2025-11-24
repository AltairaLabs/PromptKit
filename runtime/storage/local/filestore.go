package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// FileStoreConfig configures the local filesystem storage backend.
type FileStoreConfig struct {
	// BaseDir is the root directory for media storage
	BaseDir string

	// Organization determines how files are organized in directories
	Organization storage.OrganizationMode

	// EnableDeduplication enables content-based deduplication using SHA-256 hashing
	EnableDeduplication bool

	// DefaultPolicy is the default retention policy to apply to new media
	DefaultPolicy string
}

// FileStore implements MediaStorageService using local filesystem storage.
type FileStore struct {
	config FileStoreConfig

	// dedupIndex maps content hashes to file paths for deduplication
	dedupIndex map[string]string
	dedupMu    sync.RWMutex

	// refCounts tracks how many references exist for each deduplicated file
	refCounts map[string]int
	refMu     sync.RWMutex
}

// NewFileStore creates a new local filesystem storage backend.
func NewFileStore(config FileStoreConfig) (*FileStore, error) {
	if config.BaseDir == "" {
		return nil, fmt.Errorf("base directory is required")
	}

	// Create base directory if it doesn't exist
	if err := os.MkdirAll(config.BaseDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create base directory: %w", err)
	}

	// Default to by-session organization
	if config.Organization == "" {
		config.Organization = storage.OrganizationBySession
	}

	fs := &FileStore{
		config:     config,
		dedupIndex: make(map[string]string),
		refCounts:  make(map[string]int),
	}

	// Load existing deduplication index if enabled
	if config.EnableDeduplication {
		if err := fs.loadDedupIndex(); err != nil {
			// Log but don't fail - we'll rebuild as needed
			fmt.Printf("Warning: failed to load deduplication index: %v\n", err)
		}
	}

	return fs, nil
}

// StoreMedia implements MediaStorageService.StoreMedia
func (fs *FileStore) StoreMedia(ctx context.Context, content *types.MediaContent, metadata *storage.MediaMetadata) (storage.StorageReference, error) {
	if err := content.Validate(); err != nil {
		return "", fmt.Errorf("invalid media content: %w", err)
	}

	// Get the media data
	data, err := fs.getMediaData(content)
	if err != nil {
		return "", fmt.Errorf("failed to get media data: %w", err)
	}

	// Compute hash if deduplication is enabled
	var hash string
	if fs.config.EnableDeduplication {
		hash = fs.computeHash(data)

		// Check if we already have this content
		fs.dedupMu.RLock()
		existingPath, exists := fs.dedupIndex[hash]
		fs.dedupMu.RUnlock()

		if exists {
			// Increment reference count
			fs.refMu.Lock()
			fs.refCounts[existingPath]++
			fs.refMu.Unlock()

			return storage.StorageReference(existingPath), nil
		}
	}

	// Generate file path based on organization mode
	filePath, err := fs.generateFilePath(metadata, hash, content.MIMEType)
	if err != nil {
		return "", fmt.Errorf("failed to generate file path: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// Write file atomically (write to temp, then rename)
	if err := fs.writeFileAtomic(filePath, data); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	// Update deduplication index
	if fs.config.EnableDeduplication && hash != "" {
		fs.dedupMu.Lock()
		fs.dedupIndex[hash] = filePath
		fs.dedupMu.Unlock()

		fs.refMu.Lock()
		fs.refCounts[filePath] = 1
		fs.refMu.Unlock()

		// Persist index
		_ = fs.saveDedupIndex()
	}

	// Store metadata alongside the file
	if err := fs.storeMetadata(filePath, metadata); err != nil {
		// Log but don't fail
		fmt.Printf("Warning: failed to store metadata: %v\n", err)
	}

	return storage.StorageReference(filePath), nil
}

// RetrieveMedia implements MediaStorageService.RetrieveMedia
func (fs *FileStore) RetrieveMedia(ctx context.Context, reference storage.StorageReference) (*types.MediaContent, error) {
	filePath := string(reference)

	// Validate file exists and is readable
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("media not found: %s", filePath)
		}
		return nil, fmt.Errorf("failed to access media: %w", err)
	}

	if info.IsDir() {
		return nil, fmt.Errorf("reference points to directory, not file: %s", filePath)
	}

	// Load metadata to get MIME type and other info
	metadata, err := fs.loadMetadata(filePath)
	if err != nil {
		// Try to infer MIME type from extension if metadata not available
		mimeType := inferMIMETypeFromPath(filePath)
		if mimeType == "" {
			return nil, fmt.Errorf("failed to load metadata and cannot infer MIME type: %w", err)
		}

		return &types.MediaContent{
			FilePath: &filePath,
			MIMEType: mimeType,
		}, nil
	}

	policyName := metadata.PolicyName
	return &types.MediaContent{
		FilePath:   &filePath,
		MIMEType:   metadata.MIMEType,
		PolicyName: &policyName,
	}, nil
}

// DeleteMedia implements MediaStorageService.DeleteMedia
func (fs *FileStore) DeleteMedia(ctx context.Context, reference storage.StorageReference) error {
	filePath := string(reference)

	// Check reference count if deduplication is enabled
	if fs.config.EnableDeduplication {
		fs.refMu.Lock()
		count := fs.refCounts[filePath]
		if count > 1 {
			fs.refCounts[filePath]--
			fs.refMu.Unlock()
			return nil // Don't delete, still referenced
		}
		delete(fs.refCounts, filePath)
		fs.refMu.Unlock()
	}

	// Delete metadata file
	metadataPath := filePath + ".meta"
	_ = os.Remove(metadataPath)

	// Delete the file
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete media: %w", err)
	}

	// Clean up deduplication index
	if fs.config.EnableDeduplication {
		fs.dedupMu.Lock()
		for hash, path := range fs.dedupIndex {
			if path == filePath {
				delete(fs.dedupIndex, hash)
				break
			}
		}
		fs.dedupMu.Unlock()
		_ = fs.saveDedupIndex()
	}

	// Try to remove empty parent directories
	fs.cleanupEmptyDirs(filepath.Dir(filePath))

	return nil
}

// GetURL implements MediaStorageService.GetURL
func (fs *FileStore) GetURL(ctx context.Context, reference storage.StorageReference, expiry time.Duration) (string, error) {
	filePath := string(reference)

	// Validate file exists
	if _, err := os.Stat(filePath); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("media not found: %s", filePath)
		}
		return "", fmt.Errorf("failed to access media: %w", err)
	}

	// Return file:// URL (expiry is ignored for local files)
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	return "file://" + absPath, nil
}

// Helper methods

func (fs *FileStore) getMediaData(content *types.MediaContent) ([]byte, error) {
	if content.Data != nil {
		// Decode base64 data
		reader, err := content.ReadData()
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		return io.ReadAll(reader)
	}

	if content.FilePath != nil {
		return os.ReadFile(*content.FilePath)
	}

	if content.URL != nil {
		return nil, fmt.Errorf("URL-based media not yet supported for storage")
	}

	return nil, fmt.Errorf("no data source available")
}

func (fs *FileStore) computeHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func (fs *FileStore) generateFilePath(metadata *storage.MediaMetadata, hash, mimeType string) (string, error) {
	// Get file extension from MIME type
	ext := getExtensionFromMIME(mimeType)

	// Use hash as filename if available, otherwise generate one
	filename := hash
	if filename == "" {
		filename = fmt.Sprintf("%d_%d_%d", metadata.MessageIdx, metadata.PartIdx, time.Now().UnixNano())
	}
	filename += ext

	// Generate path based on organization mode
	var subdir string
	switch fs.config.Organization {
	case storage.OrganizationBySession:
		if metadata.SessionID == "" {
			return "", fmt.Errorf("session ID required for by-session organization")
		}
		subdir = filepath.Join("sessions", sanitizeFilename(metadata.SessionID))
	case storage.OrganizationByConversation:
		if metadata.ConversationID == "" {
			return "", fmt.Errorf("conversation ID required for by-conversation organization")
		}
		subdir = filepath.Join("conversations", sanitizeFilename(metadata.ConversationID))
	case storage.OrganizationByRun:
		if metadata.RunID == "" {
			return "", fmt.Errorf("run ID required for by-run organization")
		}
		subdir = filepath.Join("runs", sanitizeFilename(metadata.RunID))
	default:
		return "", fmt.Errorf("unknown organization mode: %s", fs.config.Organization)
	}

	return filepath.Join(fs.config.BaseDir, subdir, filename), nil
}

func (fs *FileStore) writeFileAtomic(path string, data []byte) error {
	// Write to temporary file
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0600); err != nil {
		return err
	}

	// Rename to final path (atomic on POSIX systems)
	return os.Rename(tempPath, path)
}

func (fs *FileStore) storeMetadata(filePath string, metadata *storage.MediaMetadata) error {
	metadataPath := filePath + ".meta"

	// Apply default policy if none specified
	if metadata.PolicyName == "" && fs.config.DefaultPolicy != "" {
		metadata.PolicyName = fs.config.DefaultPolicy
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(metadataPath, data, 0600)
}

func (fs *FileStore) loadMetadata(filePath string) (*storage.MediaMetadata, error) {
	metadataPath := filePath + ".meta"

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, err
	}

	var metadata storage.MediaMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}

	return &metadata, nil
}

func (fs *FileStore) loadDedupIndex() error {
	indexPath := filepath.Join(fs.config.BaseDir, ".dedup_index.json")

	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Index doesn't exist yet, that's ok
		}
		return err
	}

	fs.dedupMu.Lock()
	defer fs.dedupMu.Unlock()

	return json.Unmarshal(data, &fs.dedupIndex)
}

func (fs *FileStore) saveDedupIndex() error {
	indexPath := filepath.Join(fs.config.BaseDir, ".dedup_index.json")

	fs.dedupMu.RLock()
	data, err := json.MarshalIndent(fs.dedupIndex, "", "  ")
	fs.dedupMu.RUnlock()

	if err != nil {
		return err
	}

	return os.WriteFile(indexPath, data, 0600)
}

func (fs *FileStore) cleanupEmptyDirs(dir string) {
	// Don't delete the base directory
	if dir == fs.config.BaseDir || !strings.HasPrefix(dir, fs.config.BaseDir) {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) > 0 {
		return
	}

	_ = os.Remove(dir)

	// Recursively clean parent
	fs.cleanupEmptyDirs(filepath.Dir(dir))
}

// Helper functions

func sanitizeFilename(name string) string {
	// Replace invalid characters with underscores
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	return replacer.Replace(name)
}

func getExtensionFromMIME(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "audio/mpeg":
		return ".mp3"
	case "audio/wav":
		return ".wav"
	case "audio/ogg":
		return ".ogg"
	case "audio/webm":
		return ".weba"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "video/ogg":
		return ".ogv"
	default:
		return ".bin"
	}
}

func inferMIMETypeFromPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".ogg", ".oga":
		return "audio/ogg"
	case ".weba":
		return "audio/webm"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".ogv":
		return "video/ogg"
	default:
		return ""
	}
}
