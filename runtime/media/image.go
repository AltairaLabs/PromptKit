// Package media provides utilities for processing media content (images, video).
package media

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"

	_ "image/gif" // Register GIF decoder

	_ "golang.org/x/image/webp" // Register WebP decoder
)

// Image format constants.
const (
	FormatJPEG = "jpeg"
	FormatJPG  = "jpg"
	FormatPNG  = "png"
	FormatGIF  = "gif"
	FormatWebP = "webp"
)

// MIME type constants.
const (
	MIMETypeJPEG = "image/jpeg"
	MIMETypePNG  = "image/png"
	MIMETypeGIF  = "image/gif"
	MIMETypeWebP = "image/webp"
)

// Default configuration values.
const (
	DefaultMaxWidth  = 1024
	DefaultMaxHeight = 1024
	DefaultQuality   = 85
	MinQuality       = 10
	QualityDecay     = 0.9
)

// ImageResizeConfig configures image resizing behavior.
type ImageResizeConfig struct {
	// MaxWidth is the maximum width in pixels (0 = no limit).
	MaxWidth int

	// MaxHeight is the maximum height in pixels (0 = no limit).
	MaxHeight int

	// MaxSizeBytes is the maximum encoded size in bytes (0 = no limit).
	// If exceeded after resize, quality is reduced iteratively.
	MaxSizeBytes int64

	// Quality is the encoding quality (1-100). Used for JPEG and WebP.
	// Default: 85.
	Quality int

	// Format is the output format ("jpeg", "png", "" = preserve original).
	Format string

	// PreserveAspectRatio maintains the original aspect ratio when resizing.
	// Default: true.
	PreserveAspectRatio bool

	// SkipIfSmaller skips processing if the image is already within limits.
	// Default: true.
	SkipIfSmaller bool
}

// DefaultImageResizeConfig returns sensible defaults for image resizing.
func DefaultImageResizeConfig() ImageResizeConfig {
	return ImageResizeConfig{
		MaxWidth:            DefaultMaxWidth,
		MaxHeight:           DefaultMaxHeight,
		Quality:             DefaultQuality,
		Format:              "", // Preserve original
		PreserveAspectRatio: true,
		SkipIfSmaller:       true,
	}
}

// ResizeResult contains the result of an image resize operation.
type ResizeResult struct {
	Data         []byte
	Format       string
	MIMEType     string
	Width        int
	Height       int
	OriginalSize int64
	NewSize      int64
	WasResized   bool
}

// ResizeImage resizes an image to fit within the configured dimensions.
// Returns the resized image data, format, and any error.
func ResizeImage(data []byte, config ImageResizeConfig) (*ResizeResult, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty image data")
	}

	// Decode the image
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	bounds := img.Bounds()
	origWidth := bounds.Dx()
	origHeight := bounds.Dy()

	// Determine target dimensions
	targetWidth, targetHeight := calculateTargetDimensions(
		origWidth, origHeight,
		config.MaxWidth, config.MaxHeight,
		config.PreserveAspectRatio,
	)

	// Check if resize is needed
	needsResize := targetWidth < origWidth || targetHeight < origHeight
	if config.SkipIfSmaller && !needsResize {
		// Return original data unchanged
		return &ResizeResult{
			Data:         data,
			Format:       format,
			MIMEType:     formatToMIMEType(format),
			Width:        origWidth,
			Height:       origHeight,
			OriginalSize: int64(len(data)),
			NewSize:      int64(len(data)),
			WasResized:   false,
		}, nil
	}

	// Resize the image
	var resizedImg image.Image
	if needsResize {
		resizedImg = resizeImageInternal(img, targetWidth, targetHeight)
	} else {
		resizedImg = img
	}

	// Determine output format
	outputFormat := config.Format
	if outputFormat == "" {
		outputFormat = format
	}

	// Encode the image
	quality := config.Quality
	if quality <= 0 {
		quality = DefaultQuality
	}

	encoded, err := encodeImage(resizedImg, outputFormat, quality)
	if err != nil {
		return nil, fmt.Errorf("failed to encode image: %w", err)
	}

	// Check size limit and reduce quality if needed
	if config.MaxSizeBytes > 0 && int64(len(encoded)) > config.MaxSizeBytes {
		encoded, _, err = reduceToFitSize(resizedImg, outputFormat, quality, config.MaxSizeBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to reduce image size: %w", err)
		}
	}

	finalBounds := resizedImg.Bounds()
	return &ResizeResult{
		Data:         encoded,
		Format:       outputFormat,
		MIMEType:     formatToMIMEType(outputFormat),
		Width:        finalBounds.Dx(),
		Height:       finalBounds.Dy(),
		OriginalSize: int64(len(data)),
		NewSize:      int64(len(encoded)),
		WasResized:   needsResize || outputFormat != format,
	}, nil
}

// calculateTargetDimensions calculates the target dimensions for resizing.
func calculateTargetDimensions(
	origWidth, origHeight, maxWidth, maxHeight int,
	preserveAspect bool,
) (targetWidth, targetHeight int) {
	targetWidth = origWidth
	targetHeight = origHeight

	// Apply max width constraint
	if maxWidth > 0 && targetWidth > maxWidth {
		if preserveAspect {
			ratio := float64(maxWidth) / float64(targetWidth)
			targetWidth = maxWidth
			targetHeight = int(float64(targetHeight) * ratio)
		} else {
			targetWidth = maxWidth
		}
	}

	// Apply max height constraint
	if maxHeight > 0 && targetHeight > maxHeight {
		if preserveAspect {
			ratio := float64(maxHeight) / float64(targetHeight)
			targetHeight = maxHeight
			targetWidth = int(float64(targetWidth) * ratio)
		} else {
			targetHeight = maxHeight
		}
	}

	// Ensure minimum dimensions of 1
	if targetWidth < 1 {
		targetWidth = 1
	}
	if targetHeight < 1 {
		targetHeight = 1
	}

	return targetWidth, targetHeight
}

// resizeImageInternal performs the actual image resize using high-quality scaling.
func resizeImageInternal(src image.Image, width, height int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	// Use CatmullRom for high-quality downscaling (similar to Lanczos)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

// encodeImage encodes an image to the specified format.
func encodeImage(img image.Image, format string, quality int) ([]byte, error) {
	var buf bytes.Buffer
	var err error

	switch format {
	case FormatPNG:
		err = png.Encode(&buf, img)
	default:
		// JPEG, JPG, or unknown formats default to JPEG
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
	}

	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// reduceToFitSize iteratively reduces quality to fit within size limit.
func reduceToFitSize(
	img image.Image,
	format string,
	startQuality int,
	maxSize int64,
) (data []byte, finalQuality int, err error) {
	quality := startQuality

	for quality >= MinQuality {
		encoded, encErr := encodeImage(img, format, quality)
		if encErr != nil {
			return nil, quality, encErr
		}

		if int64(len(encoded)) <= maxSize {
			return encoded, quality, nil
		}

		// Reduce quality by 10% for next iteration
		quality = int(float64(quality) * QualityDecay)
	}

	// Return at minimum quality even if still over size
	encoded, encErr := encodeImage(img, format, MinQuality)
	return encoded, MinQuality, encErr
}

// formatToMIMEType converts a format string to MIME type.
func formatToMIMEType(format string) string {
	switch format {
	case FormatJPEG, FormatJPG:
		return MIMETypeJPEG
	case FormatPNG:
		return MIMETypePNG
	case FormatGIF:
		return MIMETypeGIF
	case FormatWebP:
		return MIMETypeWebP
	default:
		return MIMETypeJPEG
	}
}

// MIMETypeToFormat converts a MIME type to format string.
func MIMETypeToFormat(mimeType string) string {
	switch mimeType {
	case MIMETypeJPEG:
		return FormatJPEG
	case MIMETypePNG:
		return FormatPNG
	case MIMETypeGIF:
		return FormatGIF
	case MIMETypeWebP:
		return FormatWebP
	default:
		return FormatJPEG
	}
}
