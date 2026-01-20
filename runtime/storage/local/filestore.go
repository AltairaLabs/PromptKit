// Package local provides local filesystem-based storage implementation.
package local

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	audioMIMETypePCM   = "audio/pcm"
	audioMIMETypeL16   = "audio/L16"
	audioMIMETypeWAV   = "audio/wav"
	wavHeaderSize      = 44
	wavFmtChunkSize    = 16
	wavChunkSizeOffset = 36 // Offset for RIFF chunk size calculation
	audioBitsPerByte   = 8
	geminiumSampleRate = 24000
	geminiumBitDepth   = 16
	geminiumChannels   = 1
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

// validatePath checks that the given path is within the base directory.
// This prevents path traversal attacks (e.g., ../../etc/passwd).
// It also resolves symlinks to prevent symlink-based escapes.
func (fs *FileStore) validatePath(path string) error {
	// Get cleaned absolute path of base directory
	absBase, err := filepath.Abs(fs.config.BaseDir)
	if err != nil {
		return fmt.Errorf("failed to resolve base directory: %w", err)
	}
	absBase = filepath.Clean(absBase)

	// Get cleaned absolute path of the target
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}
	absPath = filepath.Clean(absPath)

	// First, do a quick check using cleaned paths (handles ../ traversal)
	if !strings.HasPrefix(absPath+string(filepath.Separator), absBase+string(filepath.Separator)) &&
		absPath != absBase {
		return fmt.Errorf("path %q is outside base directory %q", path, fs.config.BaseDir)
	}

	// For existing files, also check resolved symlinks to prevent symlink attacks
	if _, err := os.Lstat(absPath); err == nil {
		// Path exists, resolve symlinks on both paths for symlink attack prevention
		realBase, err := filepath.EvalSymlinks(absBase)
		if err != nil {
			realBase = absBase
		}

		realPath, err := filepath.EvalSymlinks(absPath)
		if err != nil {
			return fmt.Errorf("failed to resolve symlinks: %w", err)
		}

		if !strings.HasPrefix(realPath+string(filepath.Separator), realBase+string(filepath.Separator)) &&
			realPath != realBase {
			return fmt.Errorf("path %q resolves outside base directory (symlink attack)", path)
		}
	}

	return nil
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
			logger.Warn("Failed to load deduplication index", "error", err)
		}
	}

	return fs, nil
}

// StoreMedia implements MediaStorageService.StoreMedia
func (fs *FileStore) StoreMedia(ctx context.Context, content *types.MediaContent, metadata *storage.MediaMetadata) (storage.Reference, error) {
	if err := content.Validate(); err != nil {
		return "", fmt.Errorf("invalid media content: %w", err)
	}

	// Get the media data
	data, err := fs.getMediaData(content)
	if err != nil {
		return "", fmt.Errorf("failed to get media data: %w", err)
	}

	// Wrap raw PCM audio in WAV header for playability
	// Gemini Live API outputs 24kHz, 16-bit, mono PCM
	if isPCMAudio(content.MIMEType) {
		data = wrapPCMInWAV(data, geminiumSampleRate, geminiumBitDepth, geminiumChannels)
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

			return storage.Reference(existingPath), nil
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
		logger.Warn("Failed to store metadata", "path", filePath, "error", err)
	}

	return storage.Reference(filePath), nil
}

// RetrieveMedia implements MediaStorageService.RetrieveMedia
func (fs *FileStore) RetrieveMedia(ctx context.Context, reference storage.Reference) (*types.MediaContent, error) {
	filePath := string(reference)

	// Validate path is within base directory (prevents path traversal attacks)
	if err := fs.validatePath(filePath); err != nil {
		return nil, fmt.Errorf("invalid media reference: %w", err)
	}

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
func (fs *FileStore) DeleteMedia(ctx context.Context, reference storage.Reference) error {
	filePath := string(reference)

	// Validate path is within base directory (prevents path traversal attacks)
	if err := fs.validatePath(filePath); err != nil {
		return fmt.Errorf("invalid media reference: %w", err)
	}

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
func (fs *FileStore) GetURL(ctx context.Context, reference storage.Reference, expiry time.Duration) (string, error) {
	filePath := string(reference)

	// Validate path is within base directory (prevents path traversal attacks)
	if err := fs.validatePath(filePath); err != nil {
		return "", fmt.Errorf("invalid media reference: %w", err)
	}

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
	case audioMIMETypeWAV:
		return ".wav"
	case "audio/ogg":
		return ".ogg"
	case "audio/webm":
		return ".weba"
	case audioMIMETypePCM, audioMIMETypeL16:
		// PCM will be wrapped in WAV header for playability
		return ".wav"
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
		return audioMIMETypeWAV
	case ".pcm":
		return "audio/pcm"
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

// wrapPCMInWAV wraps raw PCM audio data in a WAV header for playability.
// Assumes 24kHz, 16-bit, mono PCM (Gemini Live API output format).
//
//nolint:gosec // Integer conversions are safe for audio parameters
func wrapPCMInWAV(pcmData []byte, sampleRate, bitsPerSample, numChannels int) []byte {
	dataSize := len(pcmData)
	byteRate := sampleRate * numChannels * bitsPerSample / audioBitsPerByte
	blockAlign := numChannels * bitsPerSample / audioBitsPerByte

	// WAV header is 44 bytes
	header := make([]byte, wavHeaderSize)

	// RIFF chunk descriptor
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(wavChunkSizeOffset+dataSize)) // ChunkSize
	copy(header[8:12], "WAVE")

	// "fmt " sub-chunk
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], wavFmtChunkSize)       // Subchunk1Size (16 for PCM)
	binary.LittleEndian.PutUint16(header[20:22], 1)                     // AudioFormat (1 = PCM)
	binary.LittleEndian.PutUint16(header[22:24], uint16(numChannels))   // NumChannels
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))    // SampleRate
	binary.LittleEndian.PutUint32(header[28:32], uint32(byteRate))      // ByteRate
	binary.LittleEndian.PutUint16(header[32:34], uint16(blockAlign))    // BlockAlign
	binary.LittleEndian.PutUint16(header[34:36], uint16(bitsPerSample)) // BitsPerSample

	// "data" sub-chunk
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(dataSize)) // Subchunk2Size

	// Combine header and data
	wav := make([]byte, wavHeaderSize+dataSize)
	copy(wav[0:wavHeaderSize], header)
	copy(wav[wavHeaderSize:], pcmData)

	return wav
}

// isPCMAudio returns true if the MIME type represents raw PCM audio.
func isPCMAudio(mimeType string) bool {
	return mimeType == audioMIMETypePCM || mimeType == audioMIMETypeL16
}
