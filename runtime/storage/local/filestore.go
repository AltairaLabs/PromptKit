// Package local provides local filesystem-based storage implementation.
package local

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/storage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
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

	// DefaultMaxFileSize is the default maximum file size (50 MB).
	DefaultMaxFileSize = 50 * 1024 * 1024

	// filePermissions is the default file permission mode for created files.
	filePermissions = 0600

	// mediaDirPerm is the permission mode for media directories created
	// during streaming stores (rwxr-x---).
	mediaDirPerm = 0o750

	// errInvalidMediaReference wraps an inner validatePath error for Retrieve/
	// Delete/GetURL when the caller-supplied reference is outside the base dir.
	errInvalidMediaReference = "invalid media reference: %w"
)

// ErrFileTooLarge is returned when data exceeds the configured MaxFileSize.
var ErrFileTooLarge = fmt.Errorf("file size exceeds maximum allowed size")

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

	// MaxFileSize is the maximum allowed file size in bytes.
	// If zero, DefaultMaxFileSize (50 MB) is used.
	// Set to a negative value to disable the size limit.
	MaxFileSize int64

	// PolicyHandler, when set, is asked to apply each media item's retention
	// policy (its PolicyName, or DefaultPolicy when that is empty) before the
	// item is written. If it returns an error, StoreMedia fails and nothing is
	// written. If it also implements storage.MediaPolicyTracker, each item is
	// passed to TrackMedia once its metadata file is written. If it implements
	// storage.MediaWriteGuard, each item's files are written and tracked
	// between BeginMediaWrite and the end of the write. When nil, policy
	// names are stored without being checked.
	PolicyHandler storage.MediaPolicyHandler
}

// FileStoreOption is a functional option for configuring a FileStore.
type FileStoreOption func(*FileStoreConfig)

// WithMaxFileSize returns an option that sets the maximum file size in bytes.
// Set to a negative value to disable the size limit.
func WithMaxFileSize(size int64) FileStoreOption {
	return func(c *FileStoreConfig) {
		c.MaxFileSize = size
	}
}

// WithPolicyHandler returns an option that sets the PolicyHandler that applies
// retention policies to stored media.
func WithPolicyHandler(h storage.MediaPolicyHandler) FileStoreOption {
	return func(c *FileStoreConfig) {
		c.PolicyHandler = h
	}
}

// FileStore implements MediaStorageService using local filesystem storage.
type FileStore struct {
	config FileStoreConfig

	// dedupIndex maps content hashes to file paths for deduplication.
	// TODO: For very large stores (>100K entries), consider replacing with a disk-backed
	// index (e.g., BoltDB) to avoid unbounded memory growth. Acceptable for current usage.
	dedupIndex map[string]string
	dedupDirty bool // tracks whether the in-memory index has unsaved changes
	dedupMu    sync.RWMutex

	// saveMu serializes saveDedupIndex, so a later snapshot of the index is
	// never overwritten on disk by an earlier one.
	saveMu sync.Mutex

	// refCounts tracks how many references exist for each deduplicated file.
	// TODO: For very large stores, consider a disk-backed refcount index.
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
// Optional FileStoreOption values can be passed to override config fields.
func NewFileStore(config FileStoreConfig, opts ...FileStoreOption) (*FileStore, error) {
	// Apply functional options
	for _, opt := range opts {
		opt(&config)
	}

	if config.BaseDir == "" {
		return nil, fmt.Errorf("base directory is required")
	}

	// Create base directory if it doesn't exist
	if err := os.MkdirAll(config.BaseDir, mediaDirPerm); err != nil {
		return nil, fmt.Errorf("failed to create base directory: %w", err)
	}

	// Default to by-session organization
	if config.Organization == "" {
		config.Organization = storage.OrganizationBySession
	}

	// Default max file size
	if config.MaxFileSize == 0 {
		config.MaxFileSize = DefaultMaxFileSize
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
func (fs *FileStore) StoreMedia(
	ctx context.Context, content *types.MediaContent, metadata *storage.MediaMetadata,
) (storage.Reference, error) {
	if err := content.Validate(); err != nil {
		return "", fmt.Errorf("invalid media content: %w", err)
	}
	if err := fs.applyPolicy(ctx, metadata); err != nil {
		return "", err
	}

	// Streaming path for FilePath-only sources. Avoids buffering the
	// whole file into memory — important for long audio recordings
	// (e.g. multi-minute TTS turns) where the buffered path's 2× peak
	// (raw + WAV-wrapped copy) would be >100 MB.
	hasInline := content.Data != nil && *content.Data != ""
	if !hasInline && content.FilePath != nil && *content.FilePath != "" {
		return fs.storeMediaStreaming(ctx, content, metadata)
	}

	data, err := fs.prepareInlineData(content)
	if err != nil {
		return "", err
	}

	// Compute hash if deduplication is enabled
	var contentHash, fileName string
	if fs.config.EnableDeduplication {
		contentHash = fs.computeHash(data)

		// Hold write lock from check through index update to prevent race where
		// two goroutines both pass the existence check and write the same file.
		existingPath, name, hit := fs.lookupDedupLocked(contentHash, metadata)
		if hit {
			return storage.Reference(existingPath), nil
		}
		fileName = name
		// Keep dedupMu locked — will update index after write below
	}

	filePath, endWrite, err := fs.writeNewMediaFile(metadata, fileName, content.MIMEType, data)
	defer endWrite()
	if err != nil {
		if fs.config.EnableDeduplication {
			fs.dedupMu.Unlock()
		}
		return "", err
	}

	// Store metadata alongside the file, before releasing dedupMu, so a
	// concurrent hit on the new file cannot write its metadata in between.
	if err := fs.storeMetadata(filePath, metadata); err != nil {
		// Log but don't fail
		logger.Warn("Failed to store metadata", "path", filePath, "error", err)
	}

	// Update deduplication index (lock already held from check above)
	if fs.config.EnableDeduplication && contentHash != "" {
		fs.registerDedupLocked(contentHash, filePath)
	}

	return storage.Reference(filePath), nil
}

// prepareInlineData loads the media bytes, wraps raw PCM audio in a WAV
// header for playability (Gemini Live API outputs 24kHz, 16-bit, mono PCM),
// and checks the final size against MaxFileSize.
func (fs *FileStore) prepareInlineData(content *types.MediaContent) ([]byte, error) {
	data, err := fs.getMediaData(content)
	if err != nil {
		return nil, fmt.Errorf("failed to get media data: %w", err)
	}

	if isPCMAudio(content.MIMEType) {
		data = wrapPCMInWAV(data, geminiumSampleRate, geminiumBitDepth, geminiumChannels)
	}

	// Check final data size against MaxFileSize (after any transformations like WAV wrapping)
	if sizeErr := fs.checkSize(int64(len(data))); sizeErr != nil {
		return nil, fmt.Errorf("failed to store media: %w", sizeErr)
	}
	return data, nil
}

// lookupDedupLocked takes dedupMu and checks the index for contentHash. On a
// hit it releases dedupMu and returns the existing path, as dedupHitLocked
// does. On a miss it returns with dedupMu STILL HELD; the caller must release
// it (directly or via registerDedupLocked). The returned file name is the one
// to store the content under on a miss.
func (fs *FileStore) lookupDedupLocked(
	contentHash string, metadata *storage.MediaMetadata,
) (existingPath, fileName string, hit bool) {
	fs.dedupMu.Lock()
	return fs.dedupHitLocked(contentHash, metadata)
}

// dedupHitLocked checks the dedup index for contentHash; the caller holds
// dedupMu. On a hit it counts the new reference, extends the shared file's
// retention to cover this store, releases dedupMu and returns the path. On a
// miss dedupMu stays held, and fileName is the name to store the content
// under.
//
// It opens the policy handler's write guard for the indexed file before
// checking it, so enforcement cannot delete the file between the check and the
// retention update. dedupMu stays held through the update, so two hits on one
// file update its metadata one at a time.
//
// An entry whose file is gone, for example because a retention policy deleted
// it, is dropped along with its reference count, and the hash is treated as
// new. The content is then stored under a fresh name rather than the old path:
// references handed out before the deletion stay dead, so deleting through one
// cannot remove the copy stored afterwards.
func (fs *FileStore) dedupHitLocked(
	contentHash string, metadata *storage.MediaMetadata,
) (existingPath, fileName string, hit bool) {
	existingPath, exists := fs.dedupIndex[contentHash]
	if !exists {
		return "", contentHash, false
	}
	endWrite := fs.beginMediaWrite(existingPath)
	defer endWrite()

	if _, err := os.Stat(existingPath); os.IsNotExist(err) {
		delete(fs.dedupIndex, contentHash)
		fs.dedupDirty = true
		fs.refMu.Lock()
		delete(fs.refCounts, existingPath)
		fs.refMu.Unlock()
		return "", fmt.Sprintf("%s_%d", contentHash, time.Now().UnixNano()), false
	}

	fs.refMu.Lock()
	fs.refCounts[existingPath]++
	fs.refMu.Unlock()

	fs.extendRetention(existingPath, metadata)
	fs.dedupMu.Unlock()
	return existingPath, "", true
}

// extendRetention extends the retention of the shared file at filePath when
// the policy handler reports that this store's metadata retains the media
// longer than the metadata on disk does, so a dedup hit never hands out a
// reference that expires before the store asked. Only the retention changes:
// the metadata on disk takes this store's policy and expiry and keeps the rest,
// such as the run, session and timestamp of the store that wrote the file. A
// store with no policy, and no DefaultPolicy to fall back on, asks to keep the
// media forever, so it clears the file's retention: the metadata is left with
// no policy and no expiry, which the policy handler treats as never expiring.
// It does nothing when the handler does not implement
// storage.MediaExpiryReporter. The caller holds the file's write guard.
func (fs *FileStore) extendRetention(filePath string, metadata *storage.MediaMetadata) {
	reporter, ok := fs.config.PolicyHandler.(storage.MediaExpiryReporter)
	if !ok || metadata == nil {
		return
	}
	effective := *metadata
	if effective.PolicyName == "" {
		effective.PolicyName = fs.config.DefaultPolicy
	}
	nextAt, nextExpires, err := reporter.MediaExpiresAt(&effective)
	if err != nil {
		return
	}
	current, err := fs.loadMetadata(filePath)
	if err != nil {
		// Nothing on disk to keep; record this store's metadata.
		current = &effective
	} else if !outlasts(reporter, nextAt, nextExpires, current) {
		return
	}
	extended := *current
	extended.PolicyName = effective.PolicyName
	extended.ExpiresAt = nil
	if nextExpires {
		extended.ExpiresAt = &nextAt
	}
	if err := fs.storeMetadata(filePath, &extended); err != nil {
		logger.Warn("Failed to extend retention of deduplicated media", "path", filePath, "error", err)
	}
}

// outlasts reports whether media that expires at nextAt, or never when
// nextExpires is false, is retained longer than reporter retains media with
// metadata current.
func outlasts(reporter storage.MediaExpiryReporter, nextAt time.Time, nextExpires bool,
	current *storage.MediaMetadata) bool {
	currentAt, currentExpires, err := reporter.MediaExpiresAt(current)
	switch {
	case err != nil:
		return true
	case !currentExpires:
		return false
	case !nextExpires:
		return true
	default:
		return nextAt.After(currentAt)
	}
}

// beginMediaWrite opens the policy handler's write guard for the media at
// filePath when the handler implements storage.MediaWriteGuard, so policy
// enforcement does not delete the item's files while they are written and
// tracked. It returns the function that closes the guard.
func (fs *FileStore) beginMediaWrite(filePath string) func() {
	guard, ok := fs.config.PolicyHandler.(storage.MediaWriteGuard)
	if !ok {
		return func() {}
	}
	return guard.BeginMediaWrite(filePath + ".meta")
}

// writeNewMediaFile generates the file path for new media from fileName (a
// generated name when empty), opens the policy
// handler's write guard for it, creates its directory and writes data
// atomically (temp file then rename). It returns the function that closes the
// guard; the caller calls it once the item's metadata is written and tracked,
// including on error.
func (fs *FileStore) writeNewMediaFile(
	metadata *storage.MediaMetadata, fileName, mimeType string, data []byte,
) (filePath string, endWrite func(), err error) {
	filePath, err = fs.generateFilePath(metadata, fileName, mimeType)
	if err != nil {
		return "", func() {}, fmt.Errorf("failed to generate file path: %w", err)
	}
	endWrite = fs.beginMediaWrite(filePath)

	if err := os.MkdirAll(filepath.Dir(filePath), mediaDirPerm); err != nil {
		return "", endWrite, fmt.Errorf("failed to create directory: %w", err)
	}

	if err := fs.writeFileAtomic(filePath, data); err != nil {
		return "", endWrite, fmt.Errorf("failed to write file: %w", err)
	}
	return filePath, endWrite, nil
}

// registerDedupLocked records filePath under contentHash, releases dedupMu
// (which the caller holds from lookupDedupLocked), sets the reference count
// to one and persists the index.
func (fs *FileStore) registerDedupLocked(contentHash, filePath string) {
	fs.dedupIndex[contentHash] = filePath
	fs.dedupDirty = true
	fs.dedupMu.Unlock()

	fs.refMu.Lock()
	fs.refCounts[filePath] = 1
	fs.refMu.Unlock()

	// Persist index (skips write if not dirty)
	if err := fs.saveDedupIndex(); err != nil {
		logger.Warn("failed to persist dedup index", "error", err)
	}
}

// storeMediaStreaming is the streaming variant of StoreMedia, used when
// content.FilePath is set and content.Data is not. It avoids holding
// the entire payload in memory — essential for the duplex arena's
// long-utterance recordings, where TTS audio for a multi-minute turn
// can be tens of MB and the buffered path's WAV-wrap doubles the peak.
//
// Flow:
//  1. Open source + stat for size.
//  2. Compute final size (with optional WAV header for PCM).
//  3. Stream from source → MultiWriter(temp dest, sha256) so we get
//     the dedup hash in a single pass without re-reading.
//  4. Once the hash is known, perform the same dedup check + atomic
//     rename + index update as the buffered path.
//
//nolint:gocyclo,gocognit // sequential streaming flow with cleanup; splitting hurts readability
func (fs *FileStore) storeMediaStreaming(
	_ context.Context,
	content *types.MediaContent,
	metadata *storage.MediaMetadata,
) (storage.Reference, error) {
	// #nosec G304 -- FilePath validated by caller (turnAudioMirror writes
	// to a private temp file under the OS temp dir; arbitrary paths are
	// not exposed here).
	src, err := os.Open(*content.FilePath)
	if err != nil {
		return "", fmt.Errorf("failed to open source: %w", err)
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat source: %w", err)
	}
	srcSize := info.Size()

	// Compute final size (with optional WAV header) and check against
	// the configured limit BEFORE writing anything.
	pcm := isPCMAudio(content.MIMEType)
	finalSize := srcSize
	if pcm {
		finalSize += int64(wavHeaderSize)
	}
	if sizeErr := fs.checkSize(finalSize); sizeErr != nil {
		return "", fmt.Errorf("failed to store media: %w", sizeErr)
	}

	// Build the body reader — optionally prefixed with a WAV header.
	var body io.Reader = src
	if pcm {
		body = &wavStreamReader{
			header: buildWAVHeader(srcSize, geminiumSampleRate, geminiumBitDepth, geminiumChannels),
			body:   src,
		}
	}

	// Tee through a hasher only when dedup is enabled — otherwise the
	// hash is unused, and skipping it lets us match the buffered path's
	// "no hash, use messageIdx_partIdx_timestamp filename" behavior
	// (without that fallback, two concurrent stores of identical
	// content would race on an identical destination filename).
	copyReader := body
	var hasher hash.Hash
	if fs.config.EnableDeduplication {
		hasher = sha256.New()
		copyReader = io.TeeReader(body, hasher)
	}

	// Write to a temp file under BaseDir so the eventual rename is on
	// the same filesystem (atomic rename requirement). Cleanup happens
	// on every error path AND on the dedup-hit path; the success path
	// renames before the defer fires.
	tempFile, err := os.CreateTemp(fs.config.BaseDir, "store-stream-*.tmp")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	cleanupTemp := func() { _ = os.Remove(tempPath) }
	// If tempPath still exists at function return, remove it.
	// (If we successfully renamed, tempPath is gone and Remove is a
	// no-op except for the syscall cost.)
	defer cleanupTemp()

	if _, copyErr := io.Copy(tempFile, copyReader); copyErr != nil {
		_ = tempFile.Close()
		return "", fmt.Errorf("failed to copy media: %w", copyErr)
	}
	if closeErr := tempFile.Close(); closeErr != nil {
		return "", fmt.Errorf("failed to close temp file: %w", closeErr)
	}

	var hashStr string
	if hasher != nil {
		hashStr = hex.EncodeToString(hasher.Sum(nil))
	}

	// Dedup check (mirrors the buffered path's locking discipline).
	fileName := hashStr
	if fs.config.EnableDeduplication {
		fs.dedupMu.Lock()
		existingPath, name, hit := fs.dedupHitLocked(hashStr, metadata)
		if hit {
			// cleanupTemp will fire from the defer.
			return storage.Reference(existingPath), nil
		}
		fileName = name
		// Keep the lock held through index update below.
	}

	finalPath, err := fs.generateFilePath(metadata, fileName, content.MIMEType)
	if err != nil {
		if fs.config.EnableDeduplication {
			fs.dedupMu.Unlock()
		}
		return "", fmt.Errorf("failed to generate file path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(finalPath), mediaDirPerm); err != nil {
		if fs.config.EnableDeduplication {
			fs.dedupMu.Unlock()
		}
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	endWrite := fs.beginMediaWrite(finalPath)
	defer endWrite()

	// Atomic rename — same filesystem because both paths are under
	// BaseDir. After this, the temp path no longer exists, so the
	// deferred cleanupTemp becomes a no-op.
	if err := os.Rename(tempPath, finalPath); err != nil {
		if fs.config.EnableDeduplication {
			fs.dedupMu.Unlock()
		}
		return "", fmt.Errorf("failed to rename temp file: %w", err)
	}
	// Fix permissions to match writeFileAtomic's contract.
	if err := os.Chmod(finalPath, filePermissions); err != nil {
		logger.Warn("failed to chmod stored file", "path", finalPath, "error", err)
	}

	// Store metadata before releasing dedupMu, so a concurrent hit on the new
	// file cannot write its metadata in between.
	if err := fs.storeMetadata(finalPath, metadata); err != nil {
		logger.Warn("Failed to store metadata", "path", finalPath, "error", err)
	}

	if fs.config.EnableDeduplication {
		fs.registerDedupLocked(hashStr, finalPath)
	}

	// metadata.SizeBytes used elsewhere for SizeKB on MediaContent.
	// Set it now since we have the final size in hand and the buffered
	// path also fills it implicitly.
	if metadata != nil && metadata.SizeBytes == 0 {
		metadata.SizeBytes = finalSize
	}

	return storage.Reference(finalPath), nil
}

// wavStreamReader prepends a 44-byte WAV header in front of a body
// reader without buffering the body. Reads return the header byte-by-byte
// (or in chunks, depending on caller buffer) until exhausted, then
// fall through to body reads.
type wavStreamReader struct {
	header []byte
	off    int
	body   io.Reader
}

func (w *wavStreamReader) Read(p []byte) (int, error) {
	if w.off < len(w.header) {
		n := copy(p, w.header[w.off:])
		w.off += n
		return n, nil
	}
	return w.body.Read(p)
}

// RetrieveMedia implements MediaStorageService.RetrieveMedia
func (fs *FileStore) RetrieveMedia(ctx context.Context, reference storage.Reference) (*types.MediaContent, error) {
	filePath := string(reference)

	// Validate path is within base directory (prevents path traversal attacks)
	if err := fs.validatePath(filePath); err != nil {
		return nil, fmt.Errorf(errInvalidMediaReference, err)
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
		return fmt.Errorf(errInvalidMediaReference, err)
	}

	if !fs.config.EnableDeduplication {
		if err := removeMediaFiles(filePath); err != nil {
			return err
		}
		fs.cleanupEmptyDirs(filepath.Dir(filePath))
		return nil
	}

	// Hold dedupMu from the reference-count decision through removing the
	// files and the index entry, so a concurrent store cannot get this path
	// back as a dedup hit and then have the file deleted under it.
	fs.dedupMu.Lock()
	fs.refMu.Lock()
	if fs.refCounts[filePath] > 1 {
		fs.refCounts[filePath]--
		fs.refMu.Unlock()
		fs.dedupMu.Unlock()
		return nil // Don't delete, still referenced
	}
	delete(fs.refCounts, filePath)
	fs.refMu.Unlock()

	if err := removeMediaFiles(filePath); err != nil {
		fs.dedupMu.Unlock()
		return err
	}
	for hash, path := range fs.dedupIndex {
		if path == filePath {
			delete(fs.dedupIndex, hash)
			fs.dedupDirty = true
			break
		}
	}
	fs.dedupMu.Unlock()

	// Persist index (skips write if not dirty)
	if err := fs.saveDedupIndex(); err != nil {
		logger.Warn("failed to persist dedup index", "error", err)
	}

	// Try to remove empty parent directories
	fs.cleanupEmptyDirs(filepath.Dir(filePath))

	return nil
}

// removeMediaFiles removes a media file and its metadata file. A media file
// that is already gone is not an error.
func removeMediaFiles(filePath string) error {
	_ = os.Remove(filePath + ".meta")
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete media: %w", err)
	}
	return nil
}

// GetURL implements MediaStorageService.GetURL
func (fs *FileStore) GetURL(ctx context.Context, reference storage.Reference, expiry time.Duration) (string, error) {
	filePath := string(reference)

	// Validate path is within base directory (prevents path traversal attacks)
	if err := fs.validatePath(filePath); err != nil {
		return "", fmt.Errorf(errInvalidMediaReference, err)
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

// maxFileSize returns the effective max file size, or -1 if disabled.
func (fs *FileStore) maxFileSize() int64 {
	return fs.config.MaxFileSize
}

// checkSize validates that data does not exceed the configured max file size.
func (fs *FileStore) checkSize(size int64) error {
	maxSize := fs.maxFileSize()
	if maxSize > 0 && size > maxSize {
		return fmt.Errorf("%w: %d bytes exceeds limit of %d bytes", ErrFileTooLarge, size, maxSize)
	}
	return nil
}

func (fs *FileStore) getMediaData(content *types.MediaContent) ([]byte, error) {
	if content.Data != nil {
		return fs.readBase64Data(content)
	}

	if content.FilePath != nil {
		return fs.readFileData(content)
	}

	if content.URL != nil {
		return nil, fmt.Errorf("URL-based media not yet supported for storage")
	}

	return nil, fmt.Errorf("no data source available")
}

func (fs *FileStore) readBase64Data(content *types.MediaContent) ([]byte, error) {
	reader, err := content.ReadData()
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	maxSize := fs.maxFileSize()
	if maxSize > 0 {
		// Use LimitReader to prevent reading unbounded data.
		// Read up to maxSize+1 bytes so we can detect if the data exceeds the limit.
		limited := io.LimitReader(reader, maxSize+1)
		data, readErr := io.ReadAll(limited)
		if readErr != nil {
			return nil, readErr
		}
		if int64(len(data)) > maxSize {
			return nil, fmt.Errorf("%w: data exceeds limit of %d bytes", ErrFileTooLarge, maxSize)
		}
		return data, nil
	}
	return io.ReadAll(reader)
}

func (fs *FileStore) readFileData(content *types.MediaContent) ([]byte, error) {
	maxSize := fs.maxFileSize()
	if maxSize > 0 {
		info, err := os.Stat(*content.FilePath)
		if err != nil {
			return nil, err
		}
		if err := fs.checkSize(info.Size()); err != nil {
			return nil, err
		}
	}
	return os.ReadFile(*content.FilePath) //#nosec G304 -- path validated by caller
}

func (fs *FileStore) computeHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (fs *FileStore) generateFilePath(
	metadata *storage.MediaMetadata, contentHash, mimeType string,
) (string, error) {
	// Get file extension from MIME type
	ext := getExtensionFromMIME(mimeType)

	// Use hash as filename if available, otherwise generate one
	filename := contentHash
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

// writeFileAtomic writes data to path through a temporary file of its own in
// the same directory, then renames it into place, so readers never see a
// partial file and two concurrent writers never share a temporary file.
func (fs *FileStore) writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tempPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Chmod(tempPath, filePermissions); err != nil {
		_ = os.Remove(tempPath)
		return err
	}

	// Rename to final path (atomic on POSIX systems)
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

// applyPolicy asks the configured PolicyHandler to apply the media's effective
// retention policy. It does not modify metadata; storeMetadata records the
// default policy when the media is written.
func (fs *FileStore) applyPolicy(ctx context.Context, metadata *storage.MediaMetadata) error {
	if fs.config.PolicyHandler == nil || metadata == nil {
		return nil
	}
	effective := *metadata
	if effective.PolicyName == "" {
		effective.PolicyName = fs.config.DefaultPolicy
	}
	if err := fs.config.PolicyHandler.ApplyPolicy(ctx, &effective); err != nil {
		return fmt.Errorf("failed to apply retention policy %q: %w", effective.PolicyName, err)
	}
	return nil
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

	// Atomic, so a concurrent reader never sees a partly rewritten file.
	if err := fs.writeFileAtomic(metadataPath, data); err != nil {
		return err
	}
	return fs.trackMedia(metadataPath, metadata)
}

// trackMedia passes a stored item to the policy handler when it tracks media,
// so the handler enforces the item's policy without rescanning the store.
func (fs *FileStore) trackMedia(metadataPath string, metadata *storage.MediaMetadata) error {
	tracker, ok := fs.config.PolicyHandler.(storage.MediaPolicyTracker)
	if !ok {
		return nil
	}
	if err := tracker.TrackMedia(metadataPath, metadata); err != nil {
		return fmt.Errorf("failed to track media for its retention policy: %w", err)
	}
	return nil
}

func (fs *FileStore) loadMetadata(filePath string) (*storage.MediaMetadata, error) {
	metadataPath := filePath + ".meta"

	data, err := os.ReadFile(filepath.Clean(metadataPath))
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

	data, err := os.ReadFile(filepath.Clean(indexPath))
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

// saveDedupIndex writes the dedup index to disk when it has unsaved changes.
// Saves run one at a time, and each writes its snapshot atomically. The dirty
// flag is cleared when the snapshot is taken, so a change made while a save is
// writing marks the index dirty again for the next save. A failed write marks
// it dirty again too.
func (fs *FileStore) saveDedupIndex() error {
	indexPath := filepath.Join(fs.config.BaseDir, ".dedup_index.json")

	fs.saveMu.Lock()
	defer fs.saveMu.Unlock()

	fs.dedupMu.Lock()
	if !fs.dedupDirty {
		fs.dedupMu.Unlock()
		return nil
	}
	data, err := json.MarshalIndent(fs.dedupIndex, "", "  ")
	if err == nil {
		fs.dedupDirty = false
	}
	fs.dedupMu.Unlock()

	if err != nil {
		return err
	}

	if err := fs.writeFileAtomic(indexPath, data); err != nil {
		fs.dedupMu.Lock()
		fs.dedupDirty = true
		fs.dedupMu.Unlock()
		return err
	}
	return nil
}

// Flush persists any pending dedup index changes to disk.
// Call this periodically or before shutting down to ensure the index is saved.
func (fs *FileStore) Flush() error {
	if !fs.config.EnableDeduplication {
		return nil
	}
	return fs.saveDedupIndex()
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

// MIME types and file extensions mapped by getExtensionFromMIME and
// inferMIMETypeFromPath.
const (
	mimeImageJPEG = "image/jpeg"
	mimeImagePNG  = "image/png"
	mimeImageGIF  = "image/gif"
	mimeImageWebP = "image/webp"
	mimeAudioMPEG = "audio/mpeg"
	mimeAudioOGG  = "audio/ogg"
	mimeAudioWebM = "audio/webm"
	mimeVideoMP4  = "video/mp4"
	mimeVideoWebM = "video/webm"
	mimeVideoOGG  = "video/ogg"

	extJPG  = ".jpg"
	extPNG  = ".png"
	extGIF  = ".gif"
	extWebP = ".webp"
	extMP3  = ".mp3"
	extWAV  = ".wav"
	extOGG  = ".ogg"
	extWebA = ".weba"
	extMP4  = ".mp4"
	extWebM = ".webm"
	extOGV  = ".ogv"
)

func getExtensionFromMIME(mimeType string) string {
	switch mimeType {
	case mimeImageJPEG:
		return extJPG
	case mimeImagePNG:
		return extPNG
	case mimeImageGIF:
		return extGIF
	case mimeImageWebP:
		return extWebP
	case mimeAudioMPEG:
		return extMP3
	case audioMIMETypeWAV:
		return extWAV
	case mimeAudioOGG:
		return extOGG
	case mimeAudioWebM:
		return extWebA
	case audioMIMETypePCM, audioMIMETypeL16:
		// PCM will be wrapped in WAV header for playability
		return extWAV
	case mimeVideoMP4:
		return extMP4
	case mimeVideoWebM:
		return extWebM
	case mimeVideoOGG:
		return extOGV
	default:
		return ".bin"
	}
}

func inferMIMETypeFromPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case extJPG, ".jpeg":
		return mimeImageJPEG
	case extPNG:
		return mimeImagePNG
	case extGIF:
		return mimeImageGIF
	case extWebP:
		return mimeImageWebP
	case extMP3:
		return mimeAudioMPEG
	case extWAV:
		return audioMIMETypeWAV
	case ".pcm":
		return audioMIMETypePCM
	case extOGG, ".oga":
		return mimeAudioOGG
	case extWebA:
		return mimeAudioWebM
	case extMP4:
		return mimeVideoMP4
	case extWebM:
		return mimeVideoWebM
	case extOGV:
		return mimeVideoOGG
	default:
		return ""
	}
}

// wrapPCMInWAV wraps raw PCM audio data in a WAV header for playability.
// Assumes 24kHz, 16-bit, mono PCM (Gemini Live API output format).
func wrapPCMInWAV(pcmData []byte, sampleRate, bitsPerSample, numChannels int) []byte {
	dataSize := len(pcmData)
	header := buildWAVHeader(int64(dataSize), sampleRate, bitsPerSample, numChannels)
	wav := make([]byte, wavHeaderSize+dataSize)
	copy(wav[0:wavHeaderSize], header)
	copy(wav[wavHeaderSize:], pcmData)
	return wav
}

// buildWAVHeader returns the 44-byte WAV header for a PCM payload of
// the given size and audio params. Extracted from wrapPCMInWAV so the
// streaming path (storeMediaStreaming) can write the header up-front
// without buffering the body.
func buildWAVHeader(dataSize int64, sampleRate, bitsPerSample, numChannels int) []byte {
	byteRate := sampleRate * numChannels * bitsPerSample / audioBitsPerByte
	blockAlign := numChannels * bitsPerSample / audioBitsPerByte

	header := make([]byte, wavHeaderSize)

	// RIFF chunk descriptor
	copy(header[0:4], "RIFF")
	//nolint:gosec // dataSize is checked against MaxFileSize before reaching here
	binary.LittleEndian.PutUint32(header[4:8], uint32(int64(wavChunkSizeOffset)+dataSize)) // ChunkSize
	copy(header[8:12], "WAVE")

	// "fmt " sub-chunk
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], wavFmtChunkSize)       // Subchunk1Size (16 for PCM)
	binary.LittleEndian.PutUint16(header[20:22], 1)                     // AudioFormat (1 = PCM)
	binary.LittleEndian.PutUint16(header[22:24], wavU16(numChannels))   // NumChannels
	binary.LittleEndian.PutUint32(header[24:28], wavU32(sampleRate))    // SampleRate
	binary.LittleEndian.PutUint32(header[28:32], wavU32(byteRate))      // ByteRate
	binary.LittleEndian.PutUint16(header[32:34], wavU16(blockAlign))    // BlockAlign
	binary.LittleEndian.PutUint16(header[34:36], wavU16(bitsPerSample)) // BitsPerSample

	// "data" sub-chunk
	copy(header[36:40], "data")
	//nolint:gosec // dataSize is checked against MaxFileSize before reaching here
	binary.LittleEndian.PutUint32(header[40:44], uint32(dataSize)) // Subchunk2Size

	return header
}

// wavU32 converts an int audio parameter to the uint32 a WAV header field holds.
func wavU32(v int) uint32 {
	return uint32(v) //nolint:gosec // G115: WAV header fields are 32-bit by format; wraparound matches the prior behavior
}

// wavU16 converts an int audio parameter to the uint16 a WAV header field holds.
func wavU16(v int) uint16 {
	return uint16(v) //nolint:gosec // G115: WAV header fields are 16-bit by format; wraparound matches the prior behavior
}

// isPCMAudio returns true if the MIME type represents raw PCM audio.
func isPCMAudio(mimeType string) bool {
	return mimeType == audioMIMETypePCM || mimeType == audioMIMETypeL16
}
