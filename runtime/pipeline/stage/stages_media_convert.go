// Package stage provides pipeline stages for media processing.
package stage

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/media"
)

// MediaConvertConfig configures the MediaConvertStage behavior.
type MediaConvertConfig struct {
	// TargetAudioFormats lists accepted audio MIME types.
	// Audio will be converted to the first format if not already supported.
	TargetAudioFormats []string

	// TargetImageFormats lists accepted image MIME types.
	// Image conversion not yet implemented.
	TargetImageFormats []string

	// TargetVideoFormats lists accepted video MIME types.
	// Video conversion not yet implemented.
	TargetVideoFormats []string

	// AudioConverterConfig configures audio conversion.
	AudioConverterConfig media.AudioConverterConfig

	// PassthroughOnError passes through unconverted content if conversion fails.
	// If false, errors are propagated to the pipeline.
	// Default: true.
	PassthroughOnError bool
}

// DefaultMediaConvertConfig returns sensible defaults for media conversion.
func DefaultMediaConvertConfig() MediaConvertConfig {
	return MediaConvertConfig{
		AudioConverterConfig: media.DefaultAudioConverterConfig(),
		PassthroughOnError:   true,
	}
}

// MediaConvertStage converts media content to match target format requirements.
// This is useful for normalizing media from various sources to match provider capabilities.
//
// This is a Transform stage: element → converted element (1:1)
type MediaConvertStage struct {
	BaseStage
	config         MediaConvertConfig
	audioConverter *media.AudioConverter
}

// NewMediaConvertStage creates a new media conversion stage.
func NewMediaConvertStage(config *MediaConvertConfig) *MediaConvertStage {
	return &MediaConvertStage{
		BaseStage:      NewBaseStage("media-convert", StageTypeTransform),
		config:         *config,
		audioConverter: media.NewAudioConverter(config.AudioConverterConfig),
	}
}

// Process implements the Stage interface.
// Converts media elements to target formats as needed.
func (s *MediaConvertStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		converted := s.convertElement(ctx, &elem)

		select {
		case output <- converted:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// convertElement converts media in an element if needed.
func (s *MediaConvertStage) convertElement(ctx context.Context, elem *StreamElement) StreamElement {
	// Handle audio conversion
	if elem.Audio != nil && len(s.config.TargetAudioFormats) > 0 {
		s.tryConvertAudio(ctx, elem)
	}

	// Handle image conversion (future)
	if elem.Image != nil && len(s.config.TargetImageFormats) > 0 {
		s.tryConvertImage(ctx, elem)
	}

	// Handle video conversion (future)
	if elem.Video != nil && len(s.config.TargetVideoFormats) > 0 {
		s.tryConvertVideo(ctx, elem)
	}

	return *elem
}

// tryConvertAudio attempts audio conversion and handles errors.
func (s *MediaConvertStage) tryConvertAudio(ctx context.Context, elem *StreamElement) {
	if err := s.convertAudioElement(ctx, elem); err != nil {
		s.handleConversionError(elem, err, "Audio", audioFormatToMIMEType(elem.Audio.Format))
	}
}

// tryConvertImage attempts image conversion and handles errors.
func (s *MediaConvertStage) tryConvertImage(ctx context.Context, elem *StreamElement) {
	if err := s.convertImageElement(ctx, elem); err != nil {
		s.handleConversionError(elem, err, "Image", elem.Image.MIMEType)
	}
}

// tryConvertVideo attempts video conversion and handles errors.
func (s *MediaConvertStage) tryConvertVideo(ctx context.Context, elem *StreamElement) {
	if err := s.convertVideoElement(ctx, elem); err != nil {
		s.handleConversionError(elem, err, "Video", elem.Video.MIMEType)
	}
}

// handleConversionError logs or propagates conversion errors based on config.
func (s *MediaConvertStage) handleConversionError(elem *StreamElement, err error, mediaType, format string) {
	if s.config.PassthroughOnError {
		logger.Warn(mediaType+" conversion failed, passing through original",
			"error", err,
			"format", format,
		)
	} else {
		elem.Error = err
	}
}

// convertAudioElement converts audio data to a supported format.
func (s *MediaConvertStage) convertAudioElement(ctx context.Context, elem *StreamElement) error {
	audio := elem.Audio
	if audio == nil {
		return nil
	}

	// Get current MIME type from AudioFormat enum
	currentMIME := audioFormatToMIMEType(audio.Format)

	// Check if already in supported format
	if media.IsFormatSupported(currentMIME, s.config.TargetAudioFormats) {
		return nil
	}

	// Select target format
	targetMIME := media.SelectTargetFormat(s.config.TargetAudioFormats)

	// Get audio data
	if len(audio.Samples) == 0 {
		return fmt.Errorf("no audio data available")
	}

	// Convert
	result, err := s.audioConverter.ConvertAudio(ctx, audio.Samples, currentMIME, targetMIME)
	if err != nil {
		return err
	}

	// Update element with converted data
	if result.WasConverted {
		logger.Debug("Audio converted",
			"from", currentMIME,
			"to", result.MIMEType,
			"original_size", result.OriginalSize,
			"new_size", result.NewSize,
		)

		// Update audio data and format
		audio.Samples = result.Data
		audio.Format = mimeTypeToStageAudioFormat(result.MIMEType)
		audio.Encoding = result.Format
	}

	return nil
}

// convertImageElement converts image data to a supported format.
// Currently a placeholder for future implementation.
func (s *MediaConvertStage) convertImageElement(_ context.Context, elem *StreamElement) error {
	img := elem.Image
	if img == nil {
		return nil
	}

	// Check if already in supported format
	if media.IsFormatSupported(img.MIMEType, s.config.TargetImageFormats) {
		return nil
	}

	// TODO: Implement image format conversion using media.ResizeImage
	return fmt.Errorf("image format conversion not yet implemented")
}

// convertVideoElement converts video data to a supported format.
// Currently a placeholder for future implementation.
func (s *MediaConvertStage) convertVideoElement(_ context.Context, elem *StreamElement) error {
	video := elem.Video
	if video == nil {
		return nil
	}

	// Check if already in supported format
	if media.IsFormatSupported(video.MIMEType, s.config.TargetVideoFormats) {
		return nil
	}

	// TODO: Implement video format conversion using ffmpeg
	return fmt.Errorf("video format conversion not yet implemented")
}

// InputCapabilities implements FormatCapable interface.
func (s *MediaConvertStage) InputCapabilities() Capabilities {
	// Accepts any audio/image/video content
	return Capabilities{
		ContentTypes: []ContentType{ContentTypeAudio, ContentTypeImage, ContentTypeVideo, ContentTypeAny},
	}
}

// OutputCapabilities implements FormatCapable interface.
func (s *MediaConvertStage) OutputCapabilities() Capabilities {
	// Produces content in target formats
	caps := Capabilities{
		ContentTypes: []ContentType{ContentTypeAudio, ContentTypeImage, ContentTypeVideo, ContentTypeAny},
	}

	if len(s.config.TargetAudioFormats) > 0 {
		// Map string formats to AudioFormat enum
		var formats []AudioFormat
		for _, mimeType := range s.config.TargetAudioFormats {
			format := mimeTypeToAudioFormat(mimeType)
			formats = append(formats, format)
		}
		caps.Audio = &AudioCapability{
			Formats: formats,
		}
	}

	return caps
}

// mimeTypeToAudioFormat converts MIME type to stage AudioFormat.
func mimeTypeToAudioFormat(mimeType string) AudioFormat {
	switch mimeType {
	case media.MIMETypeAudioWAV, media.MIMETypeAudioXWAV:
		return AudioFormatPCM16 // WAV is typically PCM16
	case media.MIMETypeAudioMP3:
		return AudioFormatMP3
	case media.MIMETypeAudioOGG:
		return AudioFormatOpus // OGG typically contains Opus
	default:
		return AudioFormatPCM16
	}
}

// GetConfig returns the stage configuration.
func (s *MediaConvertStage) GetConfig() MediaConvertConfig {
	return s.config
}

// audioFormatToMIMEType converts stage AudioFormat enum to MIME type string.
func audioFormatToMIMEType(format AudioFormat) string {
	switch format {
	case AudioFormatPCM16:
		return media.MIMETypeAudioWAV // PCM is typically in WAV container
	case AudioFormatFloat32:
		return media.MIMETypeAudioWAV
	case AudioFormatOpus:
		return media.MIMETypeAudioOGG // Opus typically in OGG container
	case AudioFormatMP3:
		return media.MIMETypeAudioMP3
	case AudioFormatAAC:
		return media.MIMETypeAudioAAC
	default:
		return media.MIMETypeAudioWAV
	}
}

// mimeTypeToStageAudioFormat converts MIME type string to stage AudioFormat enum.
func mimeTypeToStageAudioFormat(mimeType string) AudioFormat {
	switch mimeType {
	case media.MIMETypeAudioWAV, media.MIMETypeAudioXWAV:
		return AudioFormatPCM16
	case media.MIMETypeAudioMP3:
		return AudioFormatMP3
	case media.MIMETypeAudioOGG:
		return AudioFormatOpus
	case media.MIMETypeAudioAAC, media.MIMETypeAudioM4A:
		return AudioFormatAAC
	default:
		return AudioFormatPCM16
	}
}
