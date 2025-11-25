package types

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ContentPart represents a single piece of content in a multimodal message.
// A message can contain multiple parts: text, images, audio, video, etc.
type ContentPart struct {
	Type string `json:"type"` // "text", "image", "audio", "video"

	// For text content
	Text *string `json:"text,omitempty"`

	// For media content (image, audio, video)
	Media *MediaContent `json:"media,omitempty"`
}

// MediaContent represents media data (image, audio, video) in a message.
// Supports both inline base64 data and external file/URL references.
type MediaContent struct {
	// Data source - exactly one should be set
	Data     *string `json:"data,omitempty"`      // Base64-encoded media data
	FilePath *string `json:"file_path,omitempty"` // Local file path
	URL      *string `json:"url,omitempty"`       // External URL (http/https)

	// Storage backend reference (used when media is externalized)
	StorageReference *string `json:"storage_reference,omitempty"` // Backend-specific storage reference

	// Media metadata
	MIMEType   string  `json:"mime_type"`             // e.g., "image/jpeg", "audio/mp3", "video/mp4"
	Format     *string `json:"format,omitempty"`      // Optional format hint (e.g., "png", "mp3", "mp4")
	SizeKB     *int64  `json:"size_kb,omitempty"`     // Optional size in kilobytes
	Detail     *string `json:"detail,omitempty"`      // Optional detail level for images: "low", "high", "auto"
	Caption    *string `json:"caption,omitempty"`     // Optional caption/description
	Duration   *int    `json:"duration,omitempty"`    // Optional duration in seconds (for audio/video)
	BitRate    *int    `json:"bit_rate,omitempty"`    // Optional bit rate in kbps (for audio/video)
	Channels   *int    `json:"channels,omitempty"`    // Optional number of channels (for audio)
	Width      *int    `json:"width,omitempty"`       // Optional width in pixels (for image/video)
	Height     *int    `json:"height,omitempty"`      // Optional height in pixels (for image/video)
	FPS        *int    `json:"fps,omitempty"`         // Optional frames per second (for video)
	PolicyName *string `json:"policy_name,omitempty"` // Retention policy name
}

// ContentType constants for different content part types
const (
	ContentTypeText  = "text"
	ContentTypeImage = "image"
	ContentTypeAudio = "audio"
	ContentTypeVideo = "video"
)

// Common MIME types
const (
	MIMETypeImageJPEG = "image/jpeg"
	MIMETypeImagePNG  = "image/png"
	MIMETypeImageGIF  = "image/gif"
	MIMETypeImageWebP = "image/webp"

	MIMETypeAudioMP3  = "audio/mpeg"
	MIMETypeAudioWAV  = "audio/wav"
	MIMETypeAudioOgg  = "audio/ogg"
	MIMETypeAudioWebM = "audio/webm"

	MIMETypeVideoMP4  = "video/mp4"
	MIMETypeVideoWebM = "video/webm"
	MIMETypeVideoOgg  = "video/ogg"
)

// NewTextPart creates a ContentPart with text content
func NewTextPart(text string) ContentPart {
	return ContentPart{
		Type: ContentTypeText,
		Text: &text,
	}
}

// NewImagePart creates a ContentPart with image content from a file path
func NewImagePart(filePath string, detail *string) (ContentPart, error) {
	mimeType, err := inferMIMEType(filePath)
	if err != nil {
		return ContentPart{}, err
	}

	return ContentPart{
		Type: ContentTypeImage,
		Media: &MediaContent{
			FilePath: &filePath,
			MIMEType: mimeType,
			Detail:   detail,
		},
	}, nil
}

// NewImagePartFromURL creates a ContentPart with image content from a URL
func NewImagePartFromURL(url string, detail *string) ContentPart {
	mimeType := inferMIMETypeFromURL(url)
	return ContentPart{
		Type: ContentTypeImage,
		Media: &MediaContent{
			URL:      &url,
			MIMEType: mimeType,
			Detail:   detail,
		},
	}
}

// NewImagePartFromData creates a ContentPart with base64-encoded image data
func NewImagePartFromData(base64Data, mimeType string, detail *string) ContentPart {
	return ContentPart{
		Type: ContentTypeImage,
		Media: &MediaContent{
			Data:     &base64Data,
			MIMEType: mimeType,
			Detail:   detail,
		},
	}
}

// NewAudioPart creates a ContentPart with audio content from a file path
func NewAudioPart(filePath string) (ContentPart, error) {
	mimeType, err := inferMIMEType(filePath)
	if err != nil {
		return ContentPart{}, err
	}

	return ContentPart{
		Type: ContentTypeAudio,
		Media: &MediaContent{
			FilePath: &filePath,
			MIMEType: mimeType,
		},
	}, nil
}

// NewAudioPartFromData creates a ContentPart with base64-encoded audio data
func NewAudioPartFromData(base64Data, mimeType string) ContentPart {
	return ContentPart{
		Type: ContentTypeAudio,
		Media: &MediaContent{
			Data:     &base64Data,
			MIMEType: mimeType,
		},
	}
}

// NewVideoPart creates a ContentPart with video content from a file path
func NewVideoPart(filePath string) (ContentPart, error) {
	mimeType, err := inferMIMEType(filePath)
	if err != nil {
		return ContentPart{}, err
	}

	return ContentPart{
		Type: ContentTypeVideo,
		Media: &MediaContent{
			FilePath: &filePath,
			MIMEType: mimeType,
		},
	}, nil
}

// NewVideoPartFromData creates a ContentPart with base64-encoded video data
func NewVideoPartFromData(base64Data, mimeType string) ContentPart {
	return ContentPart{
		Type: ContentTypeVideo,
		Media: &MediaContent{
			Data:     &base64Data,
			MIMEType: mimeType,
		},
	}
}

// Validate checks if the ContentPart is valid
func (cp *ContentPart) Validate() error {
	switch cp.Type {
	case ContentTypeText:
		if cp.Text == nil || *cp.Text == "" {
			return fmt.Errorf("text content part must have non-empty text")
		}
	case ContentTypeImage, ContentTypeAudio, ContentTypeVideo:
		if cp.Media == nil {
			return fmt.Errorf("%s content part must have media content", cp.Type)
		}
		return cp.Media.Validate()
	default:
		return fmt.Errorf("invalid content type: %s", cp.Type)
	}
	return nil
}

// Validate checks if the MediaContent is valid
func (mc *MediaContent) Validate() error {
	// Exactly one data source must be set
	sourceCount := 0
	if mc.Data != nil && *mc.Data != "" {
		sourceCount++
	}
	if mc.FilePath != nil && *mc.FilePath != "" {
		sourceCount++
	}
	if mc.URL != nil && *mc.URL != "" {
		sourceCount++
	}

	if sourceCount == 0 {
		return fmt.Errorf("media content must have exactly one data source (data, file_path, or url)")
	}
	if sourceCount > 1 {
		return fmt.Errorf("media content must have exactly one data source, found %d", sourceCount)
	}

	// MIME type is required
	if mc.MIMEType == "" {
		return fmt.Errorf("media content must have mime_type")
	}

	return nil
}

// GetBase64Data returns the base64-encoded data for this media content.
// If the data is already base64-encoded, it returns it directly.
// If the data is from a file, it reads and encodes the file.
// If the data is from a URL or StorageReference, it returns an error (caller should use MediaLoader).
//
// Deprecated: For new code, use providers.MediaLoader.GetBase64Data which supports all sources
// including storage references and URLs with proper context handling.
func (mc *MediaContent) GetBase64Data() (string, error) {
	if mc.Data != nil {
		return *mc.Data, nil
	}

	if mc.StorageReference != nil {
		return "", fmt.Errorf("cannot get base64 data from storage reference %s: use MediaLoader with storage service", *mc.StorageReference)
	}

	if mc.FilePath != nil {
		data, err := os.ReadFile(*mc.FilePath)
		if err != nil {
			return "", fmt.Errorf("failed to read file %s: %w", *mc.FilePath, err)
		}
		return base64.StdEncoding.EncodeToString(data), nil
	}

	if mc.URL != nil {
		return "", fmt.Errorf("cannot get base64 data from URL %s: use MediaLoader with HTTP support", *mc.URL)
	}

	return "", fmt.Errorf("no data source available")
}

// ReadData returns an io.Reader for the media content.
// For base64 data, it decodes and returns a reader.
// For file paths, it opens and returns the file.
// For URLs, it returns an error (caller should fetch separately).
func (mc *MediaContent) ReadData() (io.ReadCloser, error) {
	if mc.Data != nil {
		decoded, err := base64.StdEncoding.DecodeString(*mc.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 data: %w", err)
		}
		return io.NopCloser(strings.NewReader(string(decoded))), nil
	}

	if mc.FilePath != nil {
		file, err := os.Open(*mc.FilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to open file %s: %w", *mc.FilePath, err)
		}
		return file, nil
	}

	if mc.URL != nil {
		return nil, fmt.Errorf("cannot read data from URL %s: caller must fetch URL separately", *mc.URL)
	}

	return nil, fmt.Errorf("no data source available")
}

// inferMIMEType infers the MIME type from a file path based on extension
func inferMIMEType(filePath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	// Images
	case ".jpg", ".jpeg":
		return MIMETypeImageJPEG, nil
	case ".png":
		return MIMETypeImagePNG, nil
	case ".gif":
		return MIMETypeImageGIF, nil
	case ".webp":
		return MIMETypeImageWebP, nil

	// Audio
	case ".mp3":
		return MIMETypeAudioMP3, nil
	case ".wav":
		return MIMETypeAudioWAV, nil
	case ".ogg", ".oga":
		return MIMETypeAudioOgg, nil
	case ".weba":
		return MIMETypeAudioWebM, nil

	// Video
	case ".mp4":
		return MIMETypeVideoMP4, nil
	case ".webm":
		return MIMETypeVideoWebM, nil
	case ".ogv":
		return MIMETypeVideoOgg, nil

	default:
		return "", fmt.Errorf("unsupported file extension: %s", ext)
	}
}

// inferMIMETypeFromURL infers the MIME type from a URL based on extension
func inferMIMETypeFromURL(url string) string {
	ext := strings.ToLower(filepath.Ext(url))
	mimeType, err := inferMIMEType("file" + ext)
	if err != nil {
		// Default to JPEG for images if we can't determine
		return MIMETypeImageJPEG
	}
	return mimeType
}
