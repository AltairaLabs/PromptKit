// Package stage provides pipeline stages for media processing.
package stage

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/media"
)

// ImageResizeStageConfig is an alias for media.ImageResizeConfig.
// This provides stage-specific naming while avoiding code duplication.
type ImageResizeStageConfig = media.ImageResizeConfig

// DefaultImageResizeStageConfig returns sensible defaults for image resizing.
func DefaultImageResizeStageConfig() ImageResizeStageConfig {
	return media.DefaultImageResizeConfig()
}

// ImageResizeStage resizes images to fit within configured dimensions.
// This is useful for reducing image sizes before sending to providers
// or for normalizing images from different sources.
//
// This is a Transform stage: image element → resized image element (1:1)
type ImageResizeStage struct {
	BaseStage
	config ImageResizeStageConfig

	// Log deduplication: track what we've logged to avoid flooding
	loggedPassthrough bool
	loggedResizeKey   string // "WxH->WxH"
}

// NewImageResizeStage creates a new image resizing stage.
func NewImageResizeStage(config ImageResizeStageConfig) *ImageResizeStage {
	return &ImageResizeStage{
		BaseStage: NewBaseStage("image-resize", StageTypeTransform),
		config:    config,
	}
}

// Process implements the Stage interface.
// Resizes images in each element to fit within the configured dimensions.
func (s *ImageResizeStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		// Process image if present
		if elem.Image != nil && len(elem.Image.Data) > 0 {
			if err := s.resizeElement(&elem); err != nil {
				logger.Error("Image resizing failed", "error", err)
				elem.Error = err
			}
		}

		// Forward element (with or without resizing)
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// resizeElement resizes the image in an element if needed.
func (s *ImageResizeStage) resizeElement(elem *StreamElement) error {
	imageData := elem.Image
	if imageData == nil {
		return nil
	}

	// Check if processing is needed based on size constraints
	origWidth := imageData.Width
	origHeight := imageData.Height
	needsResize := (s.config.MaxWidth > 0 && origWidth > s.config.MaxWidth) ||
		(s.config.MaxHeight > 0 && origHeight > s.config.MaxHeight) ||
		(s.config.MaxSizeBytes > 0 && int64(len(imageData.Data)) > s.config.MaxSizeBytes)

	if !needsResize && s.config.SkipIfSmaller {
		if !s.loggedPassthrough {
			logger.Debug("ImageResizeStage: image within limits, passthrough",
				"width", origWidth,
				"height", origHeight,
				"size", len(imageData.Data),
			)
			s.loggedPassthrough = true
		}
		return nil
	}

	// Perform resize using the config directly (no conversion needed since it's an alias)
	result, err := media.ResizeImage(imageData.Data, s.config)
	if err != nil {
		return fmt.Errorf("resize failed: %w", err)
	}

	// Log resize operation only once per unique dimension combination
	if result.WasResized {
		resizeKey := fmt.Sprintf("%dx%d->%dx%d", origWidth, origHeight, result.Width, result.Height)
		if s.loggedResizeKey != resizeKey {
			logger.Debug("ImageResizeStage: resized image",
				"from", fmt.Sprintf("%dx%d", origWidth, origHeight),
				"to", fmt.Sprintf("%dx%d", result.Width, result.Height),
				"original_size", result.OriginalSize,
				"new_size", result.NewSize,
			)
			s.loggedResizeKey = resizeKey
		}
	}

	// Update the element with resized image
	elem.Image.Data = result.Data
	elem.Image.Width = result.Width
	elem.Image.Height = result.Height
	elem.Image.MIMEType = result.MIMEType
	elem.Image.Format = result.Format

	return nil
}

// GetConfig returns the stage configuration.
func (s *ImageResizeStage) GetConfig() ImageResizeStageConfig {
	return s.config
}
