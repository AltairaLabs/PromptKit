package providers

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/httputil"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/storage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const (
	defaultHTTPTimeout     = 30 * time.Second // Default timeout for HTTP requests
	defaultMaxURLSizeBytes = 50 * 1024 * 1024 // 50MB default max size for URL content

	// MaxAggregateMediaSize is the maximum total size, in bytes, of the file,
	// URL and stored media one MediaLoader reads (100MB). Inline data does not
	// count toward it. Every built-in provider uses one loader per message, so
	// this is the most media one message can make a provider read; over it,
	// GetBase64Data returns an error wrapping ErrAggregateMediaSizeExceeded.
	MaxAggregateMediaSize int64 = 100 * 1024 * 1024

	// MaxMediaItems is the maximum number of media items one MediaLoader loads
	// through GetBase64Data, counting inline data, files, URLs and storage
	// references. Every built-in provider uses one loader per message, so this
	// is the most media parts one message can have loaded; media a provider
	// passes to the model as a URL is not loaded and does not count. Past the
	// limit, GetBase64Data returns an error wrapping ErrMediaItemLimitExceeded.
	MaxMediaItems = 20

	// largeFileWarningThreshold is the size above which a warning is logged about
	// the memory impact of base64 encoding (base64 adds ~33% overhead).
	largeFileWarningThreshold int64 = 5 * 1024 * 1024 // 5MB

	// base64ExpansionRatio is the approximate overhead factor for base64 encoding.
	// Base64 encodes 3 bytes into 4 characters, so the output is ~1.33x the input.
	base64ExpansionRatio float64 = 1.34
)

var (
	// ErrMediaItemLimitExceeded is returned by MediaLoader.GetBase64Data when
	// the loader has already loaded MaxMediaItems items. Every built-in
	// provider uses one loader per message, so it means a single message
	// carries more than MaxMediaItems media parts the provider must load.
	ErrMediaItemLimitExceeded = errors.New("media item limit exceeded")

	// ErrAggregateMediaSizeExceeded is returned by MediaLoader.GetBase64Data
	// when the file, URL and stored media the loader has read total more than
	// MaxAggregateMediaSize bytes. Every built-in provider uses one loader per
	// message, so it means a single message's media is over that size.
	ErrAggregateMediaSizeExceeded = errors.New("aggregate media size limit exceeded")
)

// MediaLoader handles loading media content from various sources (inline data, files, URLs, storage).
// It provides a unified interface for providers to access media regardless of the source.
// A loader counts what it loads against MaxMediaItems and MaxAggregateMediaSize,
// so share one loader across everything the limits should cover.
// MediaLoader is not safe for concurrent use.
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

	// MaxURLSizeBytes is the maximum size for URL-based media and for a local
	// file (default: 50MB)
	MaxURLSizeBytes int64

	// AllowPrivateNetworks lets URL media be fetched from loopback, private,
	// link-local and other non-public addresses. Off by default, and it should
	// stay off: a media URL can come from a remote client or a model, and
	// fetching it from the host is how such a URL reaches the host's cloud
	// metadata service or internal network. Turn it on only when every media
	// URL comes from a source the host trusts.
	AllowPrivateNetworks bool
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

	httpClient := httputil.NewPublicHTTPClient(timeout)
	if config.AllowPrivateNetworks {
		httpClient = &http.Client{Timeout: timeout}
	}

	return &MediaLoader{
		storageService: config.StorageService,
		httpClient:     httpClient,
		maxURLSize:     maxSize,
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
			"%w: already loaded %d items (maximum %d per message; inline data, files, URLs "+
				"and storage references all count); split the media across messages",
			ErrMediaItemLimitExceeded, ml.itemCount, MaxMediaItems,
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

// ResolveURL returns a URL a provider can hand to the model when the media can
// be represented as one. ok is false when the source cannot be a fetchable URL
// (inline data, local file path, a non-remote storage URL such as file://, or a
// storage reference with no store configured); callers then fall back to
// GetBase64Data. The loader passes an expiry of 0 to the store, so the store
// chooses its own URL expiry policy.
func (ml *MediaLoader) ResolveURL(ctx context.Context, media *types.MediaContent) (string, bool, error) {
	if media == nil {
		return "", false, nil
	}
	if media.URL != nil && *media.URL != "" {
		return *media.URL, true, nil
	}
	if media.StorageReference != nil && *media.StorageReference != "" {
		if ml.storageService == nil {
			return "", false, nil
		}
		u, err := ml.storageService.GetURL(ctx, storage.Reference(*media.StorageReference), 0)
		if err != nil {
			return "", false, fmt.Errorf("failed to resolve storage reference %s to URL: %w", *media.StorageReference, err)
		}
		if isRemoteURL(u) {
			return u, true, nil
		}
		return "", false, nil
	}
	return "", false, nil
}

// isRemoteURL reports whether raw is an http(s) URL the provider's model can fetch.
func isRemoteURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// trackSize adds rawSize to the aggregate tracker and returns an error if the
// aggregate limit is exceeded. It also increments the item count.
func (ml *MediaLoader) trackSize(rawSize int64, source string) error {
	ml.aggregateSize += rawSize
	ml.itemCount++
	if ml.aggregateSize > MaxAggregateMediaSize {
		return fmt.Errorf(
			"%w: total %d bytes exceeds maximum %d bytes per message of file, URL and stored media "+
				"(while loading %s); split the media across messages",
			ErrAggregateMediaSizeExceeded, ml.aggregateSize, MaxAggregateMediaSize, source,
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
	if media == nil {
		return "", fmt.Errorf("storage returned nil media for reference: %s", ref)
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
func (ml *MediaLoader) loadFromURL(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("failed to create request for %s: %w", rawURL, err)
	}

	resp, err := ml.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch URL %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d fetching URL %s", resp.StatusCode, rawURL)
	}

	// Check content length
	if resp.ContentLength > 0 && resp.ContentLength > ml.maxURLSize {
		return "", fmt.Errorf("URL content size %d bytes exceeds maximum %d bytes", resp.ContentLength, ml.maxURLSize)
	}

	// Read with size limit
	limitReader := io.LimitReader(resp.Body, ml.maxURLSize+1)
	data, err := io.ReadAll(limitReader)
	if err != nil {
		return "", fmt.Errorf("failed to read URL response %s: %w", rawURL, err)
	}

	if int64(len(data)) > ml.maxURLSize {
		return "", fmt.Errorf("URL response size %d bytes exceeds maximum %d bytes", len(data), ml.maxURLSize)
	}

	rawSize := int64(len(data))
	if err := ml.trackSize(rawSize, rawURL); err != nil {
		return "", err
	}

	warnIfLargeFile(rawSize, rawURL)

	return base64.StdEncoding.EncodeToString(data), nil
}
