package prompt

import (
	"errors"
	"fmt"
	"strings"
)

// Error messages
var (
	errMaxSizeMBNegative      = errors.New("max_size_mb cannot be negative")
	errMaxDurationSecNegative = errors.New("max_duration_sec cannot be negative")
)

// Media type constants
const (
	mediaTypeImage = "image"
	mediaTypeAudio = "audio"
	mediaTypeVideo = "video"
)

// ValidateMediaConfig validates a MediaConfig for correctness and completeness
func ValidateMediaConfig(config *MediaConfig) error {
	if config == nil || !config.Enabled {
		return nil // Media config is optional or disabled
	}

	if err := validateSupportedTypes(config.SupportedTypes); err != nil {
		return err
	}

	if err := validateTypeSpecificConfigs(config); err != nil {
		return err
	}

	if err := validateExamples(config.Examples); err != nil {
		return err
	}

	return nil
}

// validateSupportedTypes validates the list of supported media types
func validateSupportedTypes(supportedTypes []string) error {
	if len(supportedTypes) == 0 {
		return fmt.Errorf("media config enabled but no supported_types specified")
	}

	validTypes := map[string]bool{
		mediaTypeImage: true,
		mediaTypeAudio: true,
		mediaTypeVideo: true,
	}

	for _, t := range supportedTypes {
		if !validTypes[t] {
			return fmt.Errorf("invalid supported type '%s': must be one of image, audio, video", t)
		}
	}

	return nil
}

// validateTypeSpecificConfigs validates type-specific configurations
func validateTypeSpecificConfigs(config *MediaConfig) error {
	for _, mediaType := range config.SupportedTypes {
		var err error
		switch mediaType {
		case mediaTypeImage:
			if config.Image != nil {
				err = validateImageConfig(config.Image)
			}
		case mediaTypeAudio:
			if config.Audio != nil {
				err = validateAudioConfig(config.Audio)
			}
		case mediaTypeVideo:
			if config.Video != nil {
				err = validateVideoConfig(config.Video)
			}
		}
		if err != nil {
			return fmt.Errorf("invalid %s config: %w", mediaType, err)
		}
	}
	return nil
}

// validateExamples validates multimodal examples
func validateExamples(examples []MultimodalExample) error {
	for i, example := range examples {
		if err := validateMultimodalExample(&example); err != nil {
			return fmt.Errorf("invalid example at index %d: %w", i, err)
		}
	}
	return nil
}

// validateImageConfig validates image-specific configuration
func validateImageConfig(config *ImageConfig) error {
	if config.MaxSizeMB < 0 {
		return errMaxSizeMBNegative
	}

	// Validate allowed formats
	validFormats := map[string]bool{
		"jpeg": true,
		"jpg":  true,
		"png":  true,
		"webp": true,
		"gif":  true,
	}

	for _, format := range config.AllowedFormats {
		normalized := strings.ToLower(format)
		if !validFormats[normalized] {
			return fmt.Errorf("invalid image format '%s': must be one of jpeg, jpg, png, webp, gif", format)
		}
	}

	// Validate detail level
	if config.DefaultDetail != "" {
		validDetails := map[string]bool{
			"low":  true,
			"high": true,
			"auto": true,
		}
		if !validDetails[config.DefaultDetail] {
			return fmt.Errorf("invalid default_detail '%s': must be one of low, high, auto", config.DefaultDetail)
		}
	}

	if config.MaxImagesPerMsg < 0 {
		return fmt.Errorf("max_images_per_msg cannot be negative")
	}

	return nil
}

// validateAudioConfig validates audio-specific configuration
func validateAudioConfig(config *AudioConfig) error {
	if config.MaxSizeMB < 0 {
		return errMaxSizeMBNegative
	}

	// Validate allowed formats
	validFormats := map[string]bool{
		"mp3":  true,
		"wav":  true,
		"ogg":  true,
		"webm": true,
		"m4a":  true,
		"flac": true,
	}

	for _, format := range config.AllowedFormats {
		normalized := strings.ToLower(format)
		if !validFormats[normalized] {
			return fmt.Errorf("invalid audio format '%s': must be one of mp3, wav, ogg, webm, m4a, flac", format)
		}
	}

	if config.MaxDurationSec < 0 {
		return errMaxDurationSecNegative
	}

	return nil
}

// validateVideoConfig validates video-specific configuration
func validateVideoConfig(config *VideoConfig) error {
	if config.MaxSizeMB < 0 {
		return errMaxSizeMBNegative
	}

	// Validate allowed formats
	validFormats := map[string]bool{
		"mp4":  true,
		"webm": true,
		"ogg":  true,
		"mov":  true,
		"avi":  true,
	}

	for _, format := range config.AllowedFormats {
		normalized := strings.ToLower(format)
		if !validFormats[normalized] {
			return fmt.Errorf("invalid video format '%s': must be one of mp4, webm, ogg, mov, avi", format)
		}
	}

	if config.MaxDurationSec < 0 {
		return errMaxDurationSecNegative
	}

	return nil
}

// validateMultimodalExample validates a multimodal example
func validateMultimodalExample(example *MultimodalExample) error {
	if example.Name == "" {
		return fmt.Errorf("example name is required")
	}

	if example.Role == "" {
		return fmt.Errorf("example role is required")
	}

	validRoles := map[string]bool{
		"user":      true,
		"assistant": true,
		"system":    true,
	}

	if !validRoles[example.Role] {
		return fmt.Errorf("invalid role '%s': must be one of user, assistant, system", example.Role)
	}

	if len(example.Parts) == 0 {
		return fmt.Errorf("example must have at least one content part")
	}

	// Validate each content part
	for i, part := range example.Parts {
		if err := validateExampleContentPart(&part); err != nil {
			return fmt.Errorf("invalid content part at index %d: %w", i, err)
		}
	}

	return nil
}

// validateExampleContentPart validates a content part in an example
func validateExampleContentPart(part *ExampleContentPart) error {
	validTypes := map[string]bool{
		"text":         true,
		mediaTypeImage: true,
		mediaTypeAudio: true,
		mediaTypeVideo: true,
	}

	if !validTypes[part.Type] {
		return fmt.Errorf("invalid content type '%s': must be one of text, image, audio, video", part.Type)
	}

	switch part.Type {
	case "text":
		if part.Text == "" {
			return fmt.Errorf("text content part must have non-empty text")
		}
		if part.Media != nil {
			return fmt.Errorf("text content part should not have media")
		}

	case mediaTypeImage, mediaTypeAudio, mediaTypeVideo:
		if part.Media == nil {
			return fmt.Errorf("%s content part must have media", part.Type)
		}
		if err := validateExampleMedia(part.Media, part.Type); err != nil {
			return fmt.Errorf("invalid media: %w", err)
		}
		if part.Text != "" {
			return fmt.Errorf("%s content part should not have text field", part.Type)
		}
	}

	return nil
}

// validateExampleMedia validates media content in an example
func validateExampleMedia(media *ExampleMedia, contentType string) error {
	// Must have exactly one source
	sourceCount := 0
	if media.FilePath != "" {
		sourceCount++
	}
	if media.URL != "" {
		sourceCount++
	}

	if sourceCount == 0 {
		return fmt.Errorf("media must have either file_path or url")
	}
	if sourceCount > 1 {
		return fmt.Errorf("media must have exactly one source (file_path or url), found %d", sourceCount)
	}

	// MIME type is required
	if media.MIMEType == "" {
		return fmt.Errorf("media must have mime_type")
	}

	// Validate detail level for images
	if contentType == mediaTypeImage && media.Detail != "" {
		validDetails := map[string]bool{
			"low":  true,
			"high": true,
			"auto": true,
		}
		if !validDetails[media.Detail] {
			return fmt.Errorf("invalid detail level '%s': must be one of low, high, auto", media.Detail)
		}
	}

	return nil
}

// SupportsMediaType checks if a MediaConfig supports a specific media type
func SupportsMediaType(config *MediaConfig, mediaType string) bool {
	if config == nil || !config.Enabled {
		return false
	}

	for _, t := range config.SupportedTypes {
		if t == mediaType {
			return true
		}
	}
	return false
}

// GetImageConfig returns the image configuration if images are supported
func GetImageConfig(config *MediaConfig) *ImageConfig {
	if SupportsMediaType(config, mediaTypeImage) {
		return config.Image
	}
	return nil
}

// GetAudioConfig returns the audio configuration if audio is supported
func GetAudioConfig(config *MediaConfig) *AudioConfig {
	if SupportsMediaType(config, mediaTypeAudio) {
		return config.Audio
	}
	return nil
}

// GetVideoConfig returns the video configuration if video is supported
func GetVideoConfig(config *MediaConfig) *VideoConfig {
	if SupportsMediaType(config, mediaTypeVideo) {
		return config.Video
	}
	return nil
}
