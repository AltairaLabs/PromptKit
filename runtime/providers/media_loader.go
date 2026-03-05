package providers

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	defaultHTTPTimeout     = 30 * time.Second // Default timeout for HTTP requests
	defaultMaxURLSizeBytes = 50 * 1024 * 1024 // 50MB default max size for URL content

	// MaxAggregateMediaSize is the maximum total size of all media items in a single
	// request (100MB). This prevents excessive memory usage when processing messages
	// with many media attachments.
	MaxAggregateMediaSize int64 = 100 * 1024 * 1024

	// MaxMediaItems is the maximum number of media items allowed per message/request.
	// This prevents abuse and excessive resource consumption.
	MaxMediaItems = 20

	// largeFileWarningThreshold is the size above which a warning is logged about
	// the memory impact of base64 encoding (base64 adds ~33% overhead).
	largeFileWarningThreshold int64 = 5 * 1024 * 1024 // 5MB

	// base64ExpansionRatio is the approximate overhead factor for base64 encoding.
	// Base64 encodes 3 bytes into 4 characters, so the output is ~1.33x the input.
	base64ExpansionRatio float64 = 1.34
)

// MediaLoader handles loading media content from various sources (inline data, files, URLs, storage).
// It provides a unified interface for providers to access media regardless of the source.
type MediaLoader struct {
	storageService storage.MediaStorageService
	httpClient     *http.Client
	maxURLSize     int64

	// aggregateSize tracks cumulative media bytes loaded by this loader instance.
	aggregateSize int64
	// itemCount tracks the number of media items loaded by this loader instance.
	itemCount int
}

// MediaLoaderConfig configures the MediaLoader behavior.
type MediaLoaderConfig struct {
	// StorageService is optional - required only for loading from storage references
	StorageService storage.MediaStorageService

	// HTTPTimeout for URL fetching (default: 30s)
	HTTPTimeout time.Duration

	// MaxURLSizeBytes is the maximum size for URL-based media (default: 50MB)
	MaxURLSizeBytes int64
}

// NewMediaLoader creates a new MediaLoader with the given configuration.
func NewMediaLoader(config MediaLoaderConfig) *MediaLoader {
	timeout := config.HTTPTimeout
	if timeout == 0 {
		timeout = defaultHTTPTimeout
	}

	maxSize := config.MaxURLSizeBytes
	if maxSize == 0 {
		maxSize = defaultMaxURLSizeBytes
	}

	return &MediaLoader{
		storageService: config.StorageService,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		maxURLSize: maxSize,
	}
}

// GetBase64Data loads media content and returns it as base64-encoded data.
// It handles all media sources: inline data, file paths, URLs, and storage references.
// It enforces per-item size limits, aggregate size limits, and item count limits.
func (ml *MediaLoader) GetBase64Data(ctx context.Context, media *types.MediaContent) (string, error) {
	if media == nil {
		return "", fmt.Errorf("media content is nil")
	}

	// Check item count limit
	if ml.itemCount >= MaxMediaItems {
		return "", fmt.Errorf(
			"media item limit exceeded: already loaded %d items (maximum %d)",
			ml.itemCount, MaxMediaItems,
		)
	}

	// Priority order: Data > StorageReference > FilePath > URL

	// 1. Inline base64 data (already encoded)
	if media.Data != nil && *media.Data != "" {
		ml.itemCount++
		return *media.Data, nil
	}

	// 2. Storage reference (externalized media)
	if media.StorageReference != nil && *media.StorageReference != "" {
		return ml.loadFromStorage(ctx, *media.StorageReference)
	}

	// 3. File path (local filesystem)
	if media.FilePath != nil && *media.FilePath != "" {
		return ml.loadFromFile(*media.FilePath)
	}

	// 4. URL (remote resource)
	if media.URL != nil && *media.URL != "" {
		return ml.loadFromURL(ctx, *media.URL)
	}

	return "", fmt.Errorf("no media source available (data, storage_reference, file_path, or url)")
}

// trackSize adds rawSize to the aggregate tracker and returns an error if the
// aggregate limit is exceeded. It also increments the item count.
func (ml *MediaLoader) trackSize(rawSize int64, source string) error {
	ml.aggregateSize += rawSize
	ml.itemCount++
	if ml.aggregateSize > MaxAggregateMediaSize {
		return fmt.Errorf(
			"aggregate media size limit exceeded: total %d bytes exceeds maximum %d bytes (while loading %s)",
			ml.aggregateSize, MaxAggregateMediaSize, source,
		)
	}
	return nil
}

// warnIfLargeFile logs a warning when base64 encoding a file larger than 5MB,
// since the encoding adds approximately 33% memory overhead.
func warnIfLargeFile(rawSize int64, source string) {
	if rawSize > largeFileWarningThreshold {
		encodedSize := int64(float64(rawSize) * base64ExpansionRatio)
		logger.Warn(
			"Large media file being base64-encoded",
			"source", source,
			"raw_bytes", rawSize,
			"estimated_encoded_bytes", encodedSize,
			"overhead_bytes", encodedSize-rawSize,
		)
	}
}

// loadFromStorage retrieves media from storage backend and returns base64 data.
// Storage may return media with Data (base64) or FilePath - we handle both cases.
func (ml *MediaLoader) loadFromStorage(ctx context.Context, ref string) (string, error) {
	if ml.storageService == nil {
		return "", fmt.Errorf("storage service not configured but storage reference provided: %s", ref)
	}

	media, err := ml.storageService.RetrieveMedia(ctx, storage.Reference(ref))
	if err != nil {
		return "", fmt.Errorf("failed to retrieve media from storage %s: %w", ref, err)
	}

	// Storage can return media with either Data or FilePath
	if media.Data != nil && *media.Data != "" {
		ml.itemCount++
		return *media.Data, nil
	}

	if media.FilePath != nil && *media.FilePath != "" {
		return ml.loadFromFile(*media.FilePath)
	}

	return "", fmt.Errorf("storage returned media without data or file path for reference: %s", ref)
}

// loadFromFile reads a file and returns base64-encoded data.
// It enforces the per-file size limit (maxURLSize) using io.LimitReader and
// tracks cumulative size against the aggregate limit.
func (ml *MediaLoader) loadFromFile(filePath string) (string, error) {
	// #nosec G304 - filePath is explicitly provided by user/config for media loading
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}
	defer f.Close()

	// Check file size before reading
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat file %s: %w", filePath, err)
	}

	if info.Size() > ml.maxURLSize {
		return "", fmt.Errorf(
			"file size %d bytes exceeds maximum %d bytes for %s",
			info.Size(), ml.maxURLSize, filePath,
		)
	}

	// Read with size limit as a safety net
	limitReader := io.LimitReader(f, ml.maxURLSize+1)
	data, err := io.ReadAll(limitReader)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	rawSize := int64(len(data))
	if err := ml.trackSize(rawSize, filePath); err != nil {
		return "", err
	}

	warnIfLargeFile(rawSize, filePath)

	return base64.StdEncoding.EncodeToString(data), nil
}

// loadFromURL fetches media from a URL and returns base64-encoded data.
func (ml *MediaLoader) loadFromURL(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("failed to create request for %s: %w", url, err)
	}

	resp, err := ml.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch URL %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d fetching URL %s", resp.StatusCode, url)
	}

	// Check content length
	if resp.ContentLength > 0 && resp.ContentLength > ml.maxURLSize {
		return "", fmt.Errorf("URL content size %d bytes exceeds maximum %d bytes", resp.ContentLength, ml.maxURLSize)
	}

	// Read with size limit
	limitReader := io.LimitReader(resp.Body, ml.maxURLSize+1)
	data, err := io.ReadAll(limitReader)
	if err != nil {
		return "", fmt.Errorf("failed to read URL response %s: %w", url, err)
	}

	if int64(len(data)) > ml.maxURLSize {
		return "", fmt.Errorf("URL response size %d bytes exceeds maximum %d bytes", len(data), ml.maxURLSize)
	}

	rawSize := int64(len(data))
	if err := ml.trackSize(rawSize, url); err != nil {
		return "", err
	}

	warnIfLargeFile(rawSize, url)

	return base64.StdEncoding.EncodeToString(data), nil
}
