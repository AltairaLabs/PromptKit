package turnexecutors

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	// Error message constants
	errMissingMediaConfig    = "missing media content configuration"
	errNoMediaSource         = "no URL, data, or file_path specified"
	errInlineDataMissingMIME = "inline data specified but mime_type is missing"
	errURLNoHTTPLoader       = "URL specified but HTTP loader not available"
	errStorageServiceMissing = "storage reference specified but storage service not available"
	errStorageRetrieveFailed = "failed to retrieve from storage"
	errStorageReturnedNoData = "storage returned media without data"

	// Content type constants
	contentTypeAudio    = "audio"
	contentTypeVideo    = "video"
	contentTypeDocument = "document"

	// HTTP constants
	maxRedirects = 10

	// WAV format constants
	wavHeaderSize         = 44
	wavFmtChunkSize       = 16
	wavChunkSizeOffset    = 36
	audioBitsPerByte      = 8
	geminiumSampleRate    = 16000
	geminiumBitDepth      = 16
	geminiumChannelsCount = 1
)

// HTTPMediaLoader handles loading media from HTTP/HTTPS URLs
type HTTPMediaLoader struct {
	client      *http.Client
	maxFileSize int64 // Maximum file size in bytes
}

// NewHTTPMediaLoader creates a new HTTP media loader with the specified timeout and max file size
func NewHTTPMediaLoader(timeout time.Duration, maxFileSize int64) *HTTPMediaLoader {
	return &HTTPMediaLoader{
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Allow up to maxRedirects redirects
				if len(via) >= maxRedirects {
					return fmt.Errorf("stopped after %d redirects", maxRedirects)
				}
				return nil
			},
		},
		maxFileSize: maxFileSize,
	}
}

// ConvertTurnPartsToMessageParts converts scenario turn parts to runtime message parts,
// loading media files from disk, URLs, or storage references as needed.
// The storageService parameter is optional and only needed when loading from storage references.
func ConvertTurnPartsToMessageParts(
	ctx context.Context,
	turnParts []config.TurnContentPart,
	baseDir string,
	httpLoader *HTTPMediaLoader,
	storageService storage.MediaStorageService,
) ([]types.ContentPart, error) {
	if len(turnParts) == 0 {
		return nil, nil
	}

	messageParts := make([]types.ContentPart, 0, len(turnParts))

	for i, turnPart := range turnParts {
		messagePart, err := convertSinglePart(ctx, turnPart, baseDir, httpLoader, storageService, i)
		if err != nil {
			return nil, err
		}
		messageParts = append(messageParts, messagePart)
	}

	return messageParts, nil
}

// convertSinglePart converts a single turn content part to a message content part
func convertSinglePart(
	ctx context.Context,
	turnPart config.TurnContentPart,
	baseDir string,
	httpLoader *HTTPMediaLoader,
	storageService storage.MediaStorageService,
	index int,
) (types.ContentPart, error) {
	switch turnPart.Type {
	case "text":
		return convertTextPart(turnPart, index)
	case "image":
		return convertImagePart(ctx, turnPart, baseDir, httpLoader, storageService, index)
	case contentTypeAudio:
		return convertAudioPart(ctx, turnPart, baseDir, httpLoader, storageService, index)
	case contentTypeVideo:
		return convertVideoPart(ctx, turnPart, baseDir, httpLoader, storageService, index)
	case contentTypeDocument:
		return convertDocumentPart(ctx, turnPart, baseDir, httpLoader, storageService, index)
	default:
		return types.ContentPart{}, NewValidationError(
			index, "unknown", "", fmt.Sprintf("unsupported content part type: %s", turnPart.Type))
	}
}

// convertTextPart converts a text content part
func convertTextPart(turnPart config.TurnContentPart, index int) (types.ContentPart, error) {
	if turnPart.Text == "" {
		return types.ContentPart{}, NewValidationError(index, "text", "", "empty text content")
	}
	return types.NewTextPart(turnPart.Text), nil
}

// loadFromStorageReference loads media from a storage reference
func loadFromStorageReference(
	ctx context.Context,
	storageService storage.MediaStorageService,
	ref string,
	contentType string,
	index int,
) (*types.MediaContent, error) {
	if storageService == nil {
		return nil, NewValidationError(index, contentType, ref, errStorageServiceMissing)
	}

	media, err := storageService.RetrieveMedia(ctx, storage.Reference(ref))
	if err != nil {
		return nil, NewFileError(index, contentType, ref, errStorageRetrieveFailed, err)
	}

	if media.Data == nil {
		return nil, NewValidationError(index, contentType, ref, errStorageReturnedNoData)
	}

	return media, nil
}

// mediaConversionConfig holds configuration for media conversion
type mediaConversionConfig struct {
	turnPart       config.TurnContentPart
	baseDir        string
	httpLoader     *HTTPMediaLoader
	storageService storage.MediaStorageService
	index          int
	contentType    string
}

// convertMediaPart is a generic helper for converting media parts (audio/video)
// It reduces code duplication by handling the common conversion logic
func convertMediaPart(
	ctx context.Context,
	cfg *mediaConversionConfig,
	createPartFromData func(data, mimeType string) types.ContentPart,
	loadFromFile func(filePath, baseDir string, idx int) (types.ContentPart, error),
) (types.ContentPart, error) {
	if cfg.turnPart.Media == nil {
		return types.ContentPart{}, NewValidationError(cfg.index, cfg.contentType, "", errMissingMediaConfig)
	}

	// Handle storage reference (highest priority)
	if cfg.turnPart.Media.StorageReference != "" {
		media, err := loadFromStorageReference(
			ctx, cfg.storageService, cfg.turnPart.Media.StorageReference, cfg.contentType, cfg.index)
		if err != nil {
			return types.ContentPart{}, err
		}
		var mediaType string
		switch cfg.contentType {
		case contentTypeAudio:
			mediaType = types.ContentTypeAudio
		case contentTypeVideo:
			mediaType = types.ContentTypeVideo
		case contentTypeDocument:
			mediaType = types.ContentTypeDocument
		default:
			mediaType = cfg.contentType
		}
		return types.ContentPart{Type: mediaType, Media: media}, nil
	}

	// Handle URL-based media
	if cfg.turnPart.Media.URL != "" {
		if cfg.httpLoader == nil {
			return types.ContentPart{}, NewValidationError(
				cfg.index, cfg.contentType, cfg.turnPart.Media.URL, errURLNoHTTPLoader)
		}
		// Pass explicit MIME type from scenario if specified
		data, mimeType, err := cfg.httpLoader.loadMediaFromURL(
			ctx, cfg.turnPart.Media.URL, cfg.contentType, cfg.index, cfg.turnPart.Media.MIMEType)
		if err != nil {
			return types.ContentPart{}, err
		}
		return createPartFromData(data, mimeType), nil
	}

	// Handle inline base64 data
	if cfg.turnPart.Media.Data != "" {
		mimeType := cfg.turnPart.Media.MIMEType
		if mimeType == "" {
			return types.ContentPart{}, NewValidationError(cfg.index, cfg.contentType, "", errInlineDataMissingMIME)
		}
		return createPartFromData(cfg.turnPart.Media.Data, mimeType), nil
	}

	// Handle file path - load from disk
	if cfg.turnPart.Media.FilePath != "" {
		return loadFromFile(cfg.turnPart.Media.FilePath, cfg.baseDir, cfg.index)
	}

	return types.ContentPart{}, NewValidationError(cfg.index, cfg.contentType, "", errNoMediaSource)
}

// convertImagePart converts an image content part, loading from storage reference, file, or URL if needed
func convertImagePart(
	ctx context.Context,
	turnPart config.TurnContentPart,
	baseDir string,
	httpLoader *HTTPMediaLoader,
	storageService storage.MediaStorageService,
	index int,
) (types.ContentPart, error) {
	if turnPart.Media == nil {
		return types.ContentPart{}, NewValidationError(index, "image", "", errMissingMediaConfig)
	}

	detail := parseDetailLevel(turnPart.Media.Detail)

	// Handle storage reference (highest priority)
	if turnPart.Media.StorageReference != "" {
		media, err := loadFromStorageReference(
			ctx,
			storageService,
			turnPart.Media.StorageReference,
			"image",
			index,
		)
		if err != nil {
			return types.ContentPart{}, err
		}
		media.Detail = detail
		return types.ContentPart{Type: types.ContentTypeImage, Media: media}, nil
	}

	// Handle URL-based images
	if turnPart.Media.URL != "" {
		// If httpLoader is provided, fetch and encode the image
		if httpLoader != nil {
			// Pass explicit MIME type from config if specified
			data, mimeType, err := httpLoader.loadMediaFromURL(
				ctx, turnPart.Media.URL, "image", index, turnPart.Media.MIMEType)
			if err != nil {
				return types.ContentPart{}, err
			}
			return types.NewImagePartFromData(data, mimeType, detail), nil
		}
		// Otherwise use URL directly (provider will fetch)
		return types.NewImagePartFromURL(turnPart.Media.URL, detail), nil
	}

	// Handle inline base64 data
	if turnPart.Media.Data != "" {
		mimeType := turnPart.Media.MIMEType
		if mimeType == "" {
			mimeType = "image/jpeg" // Default
		}
		return types.NewImagePartFromData(turnPart.Media.Data, mimeType, detail), nil
	}

	// Handle file path - load from disk
	if turnPart.Media.FilePath != "" {
		return loadImageFromFile(turnPart.Media.FilePath, baseDir, turnPart.Media.Detail, index)
	}

	return types.ContentPart{}, NewValidationError(index, "image", "", errNoMediaSource)
}

// convertAudioPart converts an audio content part, loading from storage reference, file, or URL if needed
func convertAudioPart(
	ctx context.Context,
	turnPart config.TurnContentPart,
	baseDir string,
	httpLoader *HTTPMediaLoader,
	storageService storage.MediaStorageService,
	index int,
) (types.ContentPart, error) {
	cfg := mediaConversionConfig{
		turnPart:       turnPart,
		baseDir:        baseDir,
		httpLoader:     httpLoader,
		storageService: storageService,
		index:          index,
		contentType:    contentTypeAudio,
	}
	return convertMediaPart(ctx, &cfg, types.NewAudioPartFromData, loadAudioFromFile)
}

// convertVideoPart converts a video content part, loading from storage reference, file, or URL if needed
func convertVideoPart(
	ctx context.Context,
	turnPart config.TurnContentPart,
	baseDir string,
	httpLoader *HTTPMediaLoader,
	storageService storage.MediaStorageService,
	index int,
) (types.ContentPart, error) {
	cfg := mediaConversionConfig{
		turnPart:       turnPart,
		baseDir:        baseDir,
		httpLoader:     httpLoader,
		storageService: storageService,
		index:          index,
		contentType:    contentTypeVideo,
	}
	return convertMediaPart(ctx, &cfg, types.NewVideoPartFromData, loadVideoFromFile)
}

// loadImageFromFile loads an image from disk and returns a content part
func loadImageFromFile(filePath, baseDir, detail string, index int) (types.ContentPart, error) {
	fullPath := resolveFilePath(filePath, baseDir)

	data, mimeType, err := loadMediaFile(fullPath, "image", index)
	if err != nil {
		return types.ContentPart{}, err
	}

	detailPtr := parseDetailLevel(detail)
	part := types.NewImagePartFromData(data, mimeType, detailPtr)
	// Preserve file path for HTML report display
	part.Media.FilePath = &filePath
	return part, nil
}

// loadAudioFromFile loads audio from disk and returns a content part.
// For PCM files, it converts to WAV format for browser playability.
func loadAudioFromFile(filePath, baseDir string, index int) (types.ContentPart, error) {
	fullPath := resolveFilePath(filePath, baseDir)

	data, mimeType, err := loadMediaFile(fullPath, "audio", index)
	if err != nil {
		return types.ContentPart{}, err
	}

	// Convert PCM to WAV for browser playability
	// Input PCM files are typically 16kHz, 16-bit, mono (standard for voice input)
	if mimeType == "audio/pcm" || mimeType == "audio/L16" {
		// Decode base64 data
		pcmData, decErr := base64.StdEncoding.DecodeString(data)
		if decErr != nil {
			return types.ContentPart{}, NewFileError(index, "audio", fullPath, "failed to decode PCM data", decErr)
		}
		// Wrap in WAV header (16kHz, 16-bit, mono - standard input format)
		wavData := wrapPCMInWAV(pcmData, geminiumSampleRate, geminiumBitDepth, geminiumChannelsCount)
		data = base64.StdEncoding.EncodeToString(wavData)
		mimeType = "audio/wav"
	}

	part := types.NewAudioPartFromData(data, mimeType)
	// Preserve file path for HTML report display
	part.Media.FilePath = &filePath
	return part, nil
}

// wrapPCMInWAV wraps raw PCM audio data in a WAV header for playability.
//
//nolint:gosec // G115: Integer conversions are safe for audio parameters
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
	copy(wav[0:44], header)
	copy(wav[44:], pcmData)

	return wav
}

// loadVideoFromFile loads video from disk and returns a content part
func loadVideoFromFile(filePath, baseDir string, index int) (types.ContentPart, error) {
	fullPath := resolveFilePath(filePath, baseDir)

	data, mimeType, err := loadMediaFile(fullPath, "video", index)
	if err != nil {
		return types.ContentPart{}, err
	}

	return types.NewVideoPartFromData(data, mimeType), nil
}

// convertDocumentPart converts a document content part, loading from storage reference, file, or URL if needed
func convertDocumentPart(
	ctx context.Context,
	turnPart config.TurnContentPart,
	baseDir string,
	httpLoader *HTTPMediaLoader,
	storageService storage.MediaStorageService,
	index int,
) (types.ContentPart, error) {
	cfg := mediaConversionConfig{
		turnPart:       turnPart,
		baseDir:        baseDir,
		httpLoader:     httpLoader,
		storageService: storageService,
		index:          index,
		contentType:    contentTypeDocument,
	}
	return convertMediaPart(ctx, &cfg, types.NewDocumentPartFromData, loadDocumentFromFile)
}

// loadDocumentFromFile loads a document from disk and returns a content part
func loadDocumentFromFile(filePath, baseDir string, index int) (types.ContentPart, error) {
	fullPath := resolveFilePath(filePath, baseDir)

	data, mimeType, err := loadMediaFile(fullPath, "document", index)
	if err != nil {
		return types.ContentPart{}, err
	}

	part := types.NewDocumentPartFromData(data, mimeType)
	// Preserve file path for HTML report display
	part.Media.FilePath = &filePath
	return part, nil
}

// loadMediaFromURL fetches media from an HTTP/HTTPS URL and returns base64-encoded data and MIME type.
// If explicitMIMEType is provided, it takes precedence over auto-detection.
func (h *HTTPMediaLoader) loadMediaFromURL(
	ctx context.Context, url, contentType string, index int, explicitMIMEType string,
) (data, mimeType string, err error) {
	// Validate URL scheme
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return "", "", NewValidationError(
			index, contentType, url, "unsupported URL scheme (only http:// and https:// are supported)")
	}

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", "", NewNetworkError(index, contentType, url, "failed to create HTTP request", err)
	}

	// Execute request
	resp, err := h.client.Do(req)
	if err != nil {
		// Check if context was canceled
		if ctx.Err() != nil {
			return "", "", NewNetworkError(index, contentType, url, "request canceled or timed out", ctx.Err())
		}
		return "", "", NewNetworkError(index, contentType, url, "failed to fetch from URL", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("HTTP %d response", resp.StatusCode)
		return "", "", NewNetworkError(index, contentType, url, msg, nil)
	}

	// Check content length against max file size
	if resp.ContentLength > 0 && resp.ContentLength > h.maxFileSize {
		msg := fmt.Sprintf("content-length %d bytes exceeds maximum %d bytes", resp.ContentLength, h.maxFileSize)
		return "", "", NewSizeError(index, contentType, url, msg)
	}

	// Read response body with size limit
	limitReader := io.LimitReader(resp.Body, h.maxFileSize+1)
	fileData, err := io.ReadAll(limitReader)
	if err != nil {
		return "", "", NewNetworkError(index, contentType, url, "failed to read response body", err)
	}

	// Check if we hit the size limit
	if int64(len(fileData)) > h.maxFileSize {
		msg := fmt.Sprintf("response body %d bytes exceeds maximum %d bytes", len(fileData), h.maxFileSize)
		return "", "", NewSizeError(index, contentType, url, msg)
	}

	// Get MIME type: explicit > Content-Type header > URL detection
	if explicitMIMEType != "" {
		mimeType = explicitMIMEType
	} else {
		mimeType = resp.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = detectMIMEType(url)
		}
	}

	// Base64 encode
	data = base64.StdEncoding.EncodeToString(fileData)

	return data, mimeType, nil
}

// loadMediaFile reads a media file and returns base64-encoded data and MIME type
func loadMediaFile(
	fullPath, contentType string, index int,
) (data, mimeType string, err error) {
	// Check if file exists
	if _, statErr := os.Stat(fullPath); os.IsNotExist(statErr) {
		return "", "", NewFileError(index, contentType, fullPath, "file does not exist", statErr)
	}

	// Read file - fullPath is validated by resolveFilePath and media_validator.go
	fileData, err := os.ReadFile(fullPath) //nolint:gosec // Path validated before reaching here
	if err != nil {
		return "", "", NewFileError(index, contentType, fullPath, "failed to read file", err)
	}

	// Detect MIME type from file extension
	mimeType = detectMIMEType(fullPath)

	// Base64 encode
	data = base64.StdEncoding.EncodeToString(fileData)

	return data, mimeType, nil
}

// resolveFilePath resolves a file path relative to the base directory
func resolveFilePath(filePath, baseDir string) string {
	if filepath.IsAbs(filePath) {
		return filePath
	}
	result := filepath.Join(baseDir, filePath)
	return result
}

// detectMIMEType detects MIME type from file extension
func detectMIMEType(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))

	mimeTypes := map[string]string{
		// Images
		".jpg":  types.MIMETypeImageJPEG,
		".jpeg": types.MIMETypeImageJPEG,
		".png":  types.MIMETypeImagePNG,
		".gif":  types.MIMETypeImageGIF,
		".webp": types.MIMETypeImageWebP,

		// Audio
		".mp3": types.MIMETypeAudioMP3,
		".wav": types.MIMETypeAudioWAV,
		".ogg": types.MIMETypeAudioOgg,
		".m4a": "audio/mp4", // Not defined in types package
		".pcm": "audio/pcm", // Raw PCM audio

		// Video
		".mp4":  types.MIMETypeVideoMP4,
		".webm": types.MIMETypeVideoWebM,
		".mov":  "video/quicktime", // Not defined in types package

		// Documents
		".pdf": "application/pdf",
	}

	if mimeType, ok := mimeTypes[ext]; ok {
		return mimeType
	}

	// Default fallback
	return "application/octet-stream"
}

// parseDetailLevel converts string detail level to pointer for images
func parseDetailLevel(detail string) *string {
	if detail == "" {
		return nil
	}
	return &detail
}
