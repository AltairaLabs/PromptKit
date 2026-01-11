// Package stage provides pipeline stages for media processing.
package stage

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	medialib "github.com/AltairaLabs/PromptKit/runtime/media"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ImagePreprocessConfig contains configuration for the image preprocessing stage.
type ImagePreprocessConfig struct {
	// Resize configuration for image resizing.
	Resize ImageResizeStageConfig

	// EnableResize enables image resizing.
	// Default: true.
	EnableResize bool
}

// DefaultImagePreprocessConfig returns sensible defaults for image preprocessing.
func DefaultImagePreprocessConfig() ImagePreprocessConfig {
	return ImagePreprocessConfig{
		Resize:       DefaultImageResizeStageConfig(),
		EnableResize: true,
	}
}

// ImagePreprocessStage preprocesses images in messages before sending to providers.
// This stage processes images directly within Message.Parts[].Media, performing
// operations like resizing, format conversion, and size optimization.
//
// This is a Transform stage: message with images → message with processed images (1:1)
type ImagePreprocessStage struct {
	BaseStage
	config ImagePreprocessConfig

	// Log deduplication
	loggedProcessing bool
}

// NewImagePreprocessStage creates a new image preprocessing stage.
func NewImagePreprocessStage(config ImagePreprocessConfig) *ImagePreprocessStage {
	return &ImagePreprocessStage{
		BaseStage: NewBaseStage("image-preprocess", StageTypeTransform),
		config:    config,
	}
}

// Process implements the Stage interface.
// Preprocesses images in messages that flow through the stage.
func (s *ImagePreprocessStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		// Process message if it contains images
		if elem.Message != nil {
			if err := s.processMessage(ctx, elem.Message); err != nil {
				logger.Error("Image preprocessing failed", "error", err)
				elem.Error = err
			}
		}

		// Also process standalone image elements
		if elem.Image != nil && len(elem.Image.Data) > 0 {
			if err := s.processImageData(elem.Image); err != nil {
				logger.Error("Image data preprocessing failed", "error", err)
				elem.Error = err
			}
		}

		// Forward element
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// processMessage processes all images in a message.
func (s *ImagePreprocessStage) processMessage(ctx context.Context, msg *types.Message) error {
	for i := range msg.Parts {
		part := &msg.Parts[i]
		if part.Type != types.ContentTypeImage || part.Media == nil {
			continue
		}

		if err := s.processMediaContent(ctx, part.Media); err != nil {
			return fmt.Errorf("failed to process image part %d: %w", i, err)
		}
	}
	return nil
}

// extractImageData extracts raw image bytes from a MediaContent.
// Returns nil, nil if no data source is available.
func (s *ImagePreprocessStage) extractImageData(media *types.MediaContent) ([]byte, error) {
	if media.Data != nil && *media.Data != "" {
		// Decode base64 data
		return base64.StdEncoding.DecodeString(*media.Data)
	}

	if media.FilePath != nil && *media.FilePath != "" {
		return s.readImageFromFile(media)
	}

	// No data source available (URL would need fetching)
	return nil, nil
}

// readImageFromFile reads image data from the file path in MediaContent.
func (s *ImagePreprocessStage) readImageFromFile(media *types.MediaContent) ([]byte, error) {
	reader, readErr := media.ReadData()
	if readErr != nil {
		return nil, fmt.Errorf("failed to read image file: %w", readErr)
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

// buildResizeConfig converts stage config to medialib config.
func (s *ImagePreprocessStage) buildResizeConfig() medialib.ImageResizeConfig {
	return medialib.ImageResizeConfig{
		MaxWidth:            s.config.Resize.MaxWidth,
		MaxHeight:           s.config.Resize.MaxHeight,
		MaxSizeBytes:        s.config.Resize.MaxSizeBytes,
		Quality:             s.config.Resize.Quality,
		Format:              s.config.Resize.Format,
		PreserveAspectRatio: s.config.Resize.PreserveAspectRatio,
		SkipIfSmaller:       s.config.Resize.SkipIfSmaller,
	}
}

// processMediaContent processes a single media content (image).
func (s *ImagePreprocessStage) processMediaContent(ctx context.Context, media *types.MediaContent) error {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if !s.config.EnableResize {
		return nil
	}

	imageData, err := s.extractImageData(media)
	if err != nil {
		return fmt.Errorf("failed to extract image data: %w", err)
	}
	if imageData == nil {
		return nil
	}

	result, err := medialib.ResizeImage(imageData, s.buildResizeConfig())
	if err != nil {
		return fmt.Errorf("failed to resize image: %w", err)
	}

	s.logResizeIfNeeded(result)
	s.applyResizeResult(media, result)

	return nil
}

// logResizeIfNeeded logs the resize operation once.
func (s *ImagePreprocessStage) logResizeIfNeeded(result *medialib.ResizeResult) {
	if result.WasResized && !s.loggedProcessing {
		logger.Debug("ImagePreprocessStage: resized image in message",
			"original_size", result.OriginalSize,
			"new_size", result.NewSize,
			"dimensions", fmt.Sprintf("%dx%d", result.Width, result.Height),
		)
		s.loggedProcessing = true
	}
}

// applyResizeResult updates media content with the resize result.
func (s *ImagePreprocessStage) applyResizeResult(media *types.MediaContent, result *medialib.ResizeResult) {
	if result.WasResized {
		encoded := base64.StdEncoding.EncodeToString(result.Data)
		media.Data = &encoded
		media.MIMEType = result.MIMEType
		media.FilePath = nil
	}
}

// processImageData processes a standalone ImageData struct.
func (s *ImagePreprocessStage) processImageData(img *ImageData) error {
	if !s.config.EnableResize {
		return nil
	}

	result, err := medialib.ResizeImage(img.Data, s.buildResizeConfig())
	if err != nil {
		return fmt.Errorf("failed to resize image data: %w", err)
	}

	if result.WasResized {
		img.Data = result.Data
		img.Width = result.Width
		img.Height = result.Height
		img.MIMEType = result.MIMEType
		img.Format = result.Format
	}

	return nil
}

// GetConfig returns the stage configuration.
func (s *ImagePreprocessStage) GetConfig() ImagePreprocessConfig {
	return s.config
}
