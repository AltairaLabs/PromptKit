// Package stage provides pipeline stages for media processing.
package stage

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/media"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// MediaConvertConfig configures the MediaConvertStage behavior.
type MediaConvertConfig struct {
	// TargetAudioFormats lists accepted audio MIME types.
	// Audio will be converted to the first format if not already supported.
	TargetAudioFormats []string

	// TargetImageFormats lists accepted image MIME types.
	// Images will be converted to the first format if not already supported.
	// Supported formats: image/jpeg, image/png
	TargetImageFormats []string

	// TargetVideoFormats lists accepted video MIME types.
	// Video conversion not yet implemented.
	TargetVideoFormats []string

	// AudioConverterConfig configures audio conversion.
	AudioConverterConfig media.AudioConverterConfig

	// ImageResizeConfig configures image conversion/resizing.
	// Only the Format and Quality fields are used for format conversion.
	ImageResizeConfig media.ImageResizeConfig

	// PassthroughOnError passes through unconverted content if conversion fails.
	// If false, errors are propagated to the pipeline.
	// Default: true.
	PassthroughOnError bool
}

// DefaultMediaConvertConfig returns sensible defaults for media conversion.
func DefaultMediaConvertConfig() MediaConvertConfig {
	return MediaConvertConfig{
		AudioConverterConfig: media.DefaultAudioConverterConfig(),
		ImageResizeConfig:    media.DefaultImageResizeConfig(),
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
	// Handle standalone audio conversion
	if elem.Audio != nil && len(s.config.TargetAudioFormats) > 0 {
		s.tryConvertAudio(ctx, elem)
	}

	// Handle standalone image conversion (future)
	if elem.Image != nil && len(s.config.TargetImageFormats) > 0 {
		s.tryConvertImage(ctx, elem)
	}

	// Handle standalone video conversion (future)
	if elem.Video != nil && len(s.config.TargetVideoFormats) > 0 {
		s.tryConvertVideo(ctx, elem)
	}

	// Handle message with multimodal parts
	if elem.Message != nil && len(elem.Message.Parts) > 0 {
		s.convertMessageParts(ctx, elem)
	}

	return *elem
}

// convertMessageParts converts media content in message parts.
func (s *MediaConvertStage) convertMessageParts(ctx context.Context, elem *StreamElement) {
	for i := range elem.Message.Parts {
		part := &elem.Message.Parts[i]

		switch part.Type {
		case types.ContentTypeAudio:
			if part.Media != nil && len(s.config.TargetAudioFormats) > 0 {
				s.convertMessageAudioPart(ctx, part, elem)
			}
		case types.ContentTypeImage:
			if part.Media != nil && len(s.config.TargetImageFormats) > 0 {
				s.convertMessageImagePart(ctx, part, elem)
			}
		case types.ContentTypeVideo:
			// Video conversion not yet implemented
		}
	}
}

// convertMessageAudioPart converts audio content in a message part.
func (s *MediaConvertStage) convertMessageAudioPart(ctx context.Context, part *types.ContentPart, elem *StreamElement) {
	if part.Media == nil || part.Media.Data == nil {
		return
	}

	currentMIME := part.Media.MIMEType
	if currentMIME == "" {
		logger.Warn("Audio part missing MIME type, skipping conversion")
		return
	}

	// Check if already in supported format
	if media.IsFormatSupported(currentMIME, s.config.TargetAudioFormats) {
		logger.Debug("Audio already in supported format", "mime_type", currentMIME)
		return
	}

	// Decode base64 data
	audioData, err := base64.StdEncoding.DecodeString(*part.Media.Data)
	if err != nil {
		s.handleConversionError(elem, err, "Audio", currentMIME)
		return
	}

	// Select target format
	targetMIME := media.SelectTargetFormat(s.config.TargetAudioFormats)

	logger.Debug("Converting message audio part",
		"from", currentMIME,
		"to", targetMIME,
		"data_size", len(audioData))

	// Convert
	result, err := s.audioConverter.ConvertAudio(ctx, audioData, currentMIME, targetMIME)
	if err != nil {
		s.handleConversionError(elem, err, "Audio", currentMIME)
		return
	}

	// Update part with converted data
	if result.WasConverted {
		logger.Info("Audio converted in message part",
			"from", currentMIME,
			"to", result.MIMEType,
			"original_size", result.OriginalSize,
			"new_size", result.NewSize)

		// Encode back to base64
		convertedData := base64.StdEncoding.EncodeToString(result.Data)
		part.Media.Data = &convertedData
		part.Media.MIMEType = result.MIMEType
	}
}

// convertMessageImagePart converts image content in a message part.
func (s *MediaConvertStage) convertMessageImagePart(_ context.Context, part *types.ContentPart, elem *StreamElement) {
	if part.Media == nil || part.Media.Data == nil {
		return
	}

	currentMIME := part.Media.MIMEType
	if currentMIME == "" {
		logger.Warn("Image part missing MIME type, skipping conversion")
		return
	}

	// Check if already in supported format
	if isImageFormatSupported(currentMIME, s.config.TargetImageFormats) {
		logger.Debug("Image already in supported format", "mime_type", currentMIME)
		return
	}

	// Decode base64 data
	imageData, err := base64.StdEncoding.DecodeString(*part.Media.Data)
	if err != nil {
		s.handleConversionError(elem, err, "Image", currentMIME)
		return
	}

	// Select target format
	targetMIME := selectTargetImageFormat(s.config.TargetImageFormats)
	targetFormat := mimeTypeToImageFormat(targetMIME)

	logger.Debug("Converting message image part",
		"from", currentMIME,
		"to", targetMIME,
		"data_size", len(imageData))

	// Configure resize for format conversion only (preserve dimensions)
	config := s.config.ImageResizeConfig
	config.Format = targetFormat
	config.MaxWidth = 0          // No resize
	config.MaxHeight = 0         // No resize
	config.SkipIfSmaller = false // Force processing for format conversion

	// Convert using ResizeImage (which handles format conversion)
	result, err := media.ResizeImage(imageData, config)
	if err != nil {
		s.handleConversionError(elem, err, "Image", currentMIME)
		return
	}

	// Update part with converted data
	logger.Info("Image converted in message part",
		"from", currentMIME,
		"to", result.MIMEType,
		"original_size", result.OriginalSize,
		"new_size", result.NewSize)

	// Encode back to base64
	convertedData := base64.StdEncoding.EncodeToString(result.Data)
	part.Media.Data = &convertedData
	part.Media.MIMEType = result.MIMEType
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
func (s *MediaConvertStage) convertImageElement(_ context.Context, elem *StreamElement) error {
	img := elem.Image
	if img == nil {
		return nil
	}

	// Check if already in supported format
	if isImageFormatSupported(img.MIMEType, s.config.TargetImageFormats) {
		return nil
	}

	// Get image data
	if len(img.Data) == 0 {
		return fmt.Errorf("no image data available")
	}

	// Select target format
	targetMIME := selectTargetImageFormat(s.config.TargetImageFormats)
	targetFormat := mimeTypeToImageFormat(targetMIME)

	// Configure resize for format conversion only (preserve dimensions)
	config := s.config.ImageResizeConfig
	config.Format = targetFormat
	config.MaxWidth = 0          // No resize
	config.MaxHeight = 0         // No resize
	config.SkipIfSmaller = false // Force processing for format conversion

	// Convert using ResizeImage (which handles format conversion)
	result, err := media.ResizeImage(img.Data, config)
	if err != nil {
		return fmt.Errorf("image conversion failed: %w", err)
	}

	// Update element with converted data
	logger.Debug("Image converted",
		"from", img.MIMEType,
		"to", result.MIMEType,
		"original_size", result.OriginalSize,
		"new_size", result.NewSize,
	)

	img.Data = result.Data
	img.MIMEType = result.MIMEType
	img.Format = result.Format
	img.Width = result.Width
	img.Height = result.Height

	return nil
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

// isImageFormatSupported checks if a MIME type is in the list of supported image formats.
func isImageFormatSupported(mimeType string, supportedFormats []string) bool {
	mimeType = normalizeImageMIMEType(mimeType)
	for _, supported := range supportedFormats {
		if normalizeImageMIMEType(supported) == mimeType {
			return true
		}
	}
	return false
}

// selectTargetImageFormat selects the best target format from supported formats.
// Prefers JPEG (widely supported), then PNG.
func selectTargetImageFormat(supportedFormats []string) string {
	if len(supportedFormats) == 0 {
		return media.MIMETypeJPEG // Default fallback
	}

	// Preference order: JPEG (most compatible), then PNG
	preferences := []string{
		media.MIMETypeJPEG,
		media.MIMETypePNG,
	}

	for _, pref := range preferences {
		if isImageFormatSupported(pref, supportedFormats) {
			return normalizeImageMIMEType(pref)
		}
	}

	// Return first supported format
	return normalizeImageMIMEType(supportedFormats[0])
}

// mimeTypeToImageFormat converts a MIME type to a format string for media.ResizeImage.
func mimeTypeToImageFormat(mimeType string) string {
	mimeType = normalizeImageMIMEType(mimeType)
	switch mimeType {
	case media.MIMETypeJPEG:
		return media.FormatJPEG
	case media.MIMETypePNG:
		return media.FormatPNG
	case media.MIMETypeGIF:
		return media.FormatGIF
	case media.MIMETypeWebP:
		return media.FormatWebP
	default:
		return media.FormatJPEG
	}
}

// normalizeImageMIMEType normalizes image MIME type variations to a canonical form.
func normalizeImageMIMEType(mimeType string) string {
	switch mimeType {
	case "image/jpg":
		return media.MIMETypeJPEG
	default:
		return mimeType
	}
}
