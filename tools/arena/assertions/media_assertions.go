package assertions

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// Error messages
const (
	errMessageRequired   = "message parameter required"
	errNoImagesFound     = "no images found in message"
	errNoAudioFound      = "no audio found in message"
	errNoVideoFound      = "no video found in message"
	errMissingDimensions = "image missing width/height metadata"
	errMissingDuration   = "missing duration metadata"
	errAtLeastOneFormat  = "at least one format must be specified"
)

// ImageFormatValidator checks that media content has one of the allowed image formats
type ImageFormatValidator struct {
	formats []string // e.g., ["jpeg", "png", "webp"]
}

// NewImageFormatValidator creates a new image_format validator from params
func NewImageFormatValidator(params map[string]interface{}) runtimeValidators.Validator {
	formats := extractStringSlice(params, "formats")
	return &ImageFormatValidator{formats: formats}
}

// Validate checks if the message contains images with allowed formats
func (v *ImageFormatValidator) Validate(content string, params map[string]interface{}) runtimeValidators.ValidationResult {
	// Extract message from params
	message, ok := params["message"].(types.Message)
	if !ok {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error": errMessageRequired,
			},
		}
	}

	if len(v.formats) == 0 {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error": errAtLeastOneFormat,
			},
		}
	}

	// Find all image parts
	var imageFormats []string
	var invalidFormats []string

	for _, part := range message.Parts {
		if part.Type == types.ContentTypeImage && part.Media != nil {
			format := extractFormatFromMIMEType(part.Media.MIMEType)
			imageFormats = append(imageFormats, format)

			if !v.isAllowedFormat(format) {
				invalidFormats = append(invalidFormats, format)
			}
		}
	}

	if len(imageFormats) == 0 {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error":           errNoImagesFound,
				"allowed_formats": v.formats,
			},
		}
	}

	return runtimeValidators.ValidationResult{
		Passed: len(invalidFormats) == 0,
		Details: map[string]interface{}{
			"found_formats":   imageFormats,
			"invalid_formats": invalidFormats,
			"allowed_formats": v.formats,
		},
	}
}

func (v *ImageFormatValidator) isAllowedFormat(format string) bool {
	format = strings.ToLower(format)
	for _, allowed := range v.formats {
		if strings.ToLower(allowed) == format {
			return true
		}
	}
	return false
}

// ImageDimensionsValidator checks that images meet dimension requirements
type ImageDimensionsValidator struct {
	minWidth    *int
	maxWidth    *int
	minHeight   *int
	maxHeight   *int
	exactWidth  *int
	exactHeight *int
}

// NewImageDimensionsValidator creates a new image_dimensions validator from params
func NewImageDimensionsValidator(params map[string]interface{}) runtimeValidators.Validator {
	validator := &ImageDimensionsValidator{}

	if minWidth, ok := params["min_width"].(int); ok {
		validator.minWidth = &minWidth
	}
	if maxWidth, ok := params["max_width"].(int); ok {
		validator.maxWidth = &maxWidth
	}
	if minHeight, ok := params["min_height"].(int); ok {
		validator.minHeight = &minHeight
	}
	if maxHeight, ok := params["max_height"].(int); ok {
		validator.maxHeight = &maxHeight
	}
	if exactWidth, ok := params["width"].(int); ok {
		validator.exactWidth = &exactWidth
	}
	if exactHeight, ok := params["height"].(int); ok {
		validator.exactHeight = &exactHeight
	}

	return validator
}

// Validate checks if images meet dimension requirements
func (v *ImageDimensionsValidator) Validate(content string, params map[string]interface{}) runtimeValidators.ValidationResult {
	message, ok := params["message"].(types.Message)
	if !ok {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error": errMessageRequired,
			},
		}
	}

	var imageCount int
	var violations []string

	for _, part := range message.Parts {
		if part.Type == types.ContentTypeImage && part.Media != nil {
			imageCount++
			violations = append(violations, v.checkImageDimensions(part.Media)...)
		}
	}

	if imageCount == 0 {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error": errNoImagesFound,
			},
		}
	}

	return runtimeValidators.ValidationResult{
		Passed: len(violations) == 0,
		Details: map[string]interface{}{
			"image_count": imageCount,
			"violations":  violations,
		},
	}
}

func (v *ImageDimensionsValidator) checkImageDimensions(media *types.MediaContent) []string {
	if media.Width == nil || media.Height == nil {
		return []string{"image missing width/height metadata"}
	}

	width := *media.Width
	height := *media.Height
	var violations []string

	// Check exact dimensions
	if v.exactWidth != nil && width != *v.exactWidth {
		violations = append(violations, fmt.Sprintf("width %d does not match required %d", width, *v.exactWidth))
	}
	if v.exactHeight != nil && height != *v.exactHeight {
		violations = append(violations, fmt.Sprintf("height %d does not match required %d", height, *v.exactHeight))
	}

	// Check min/max
	violations = append(violations, v.checkWidthRange(width)...)
	violations = append(violations, v.checkHeightRange(height)...)

	return violations
}

func (v *ImageDimensionsValidator) checkWidthRange(width int) []string {
	var violations []string
	if v.minWidth != nil && width < *v.minWidth {
		violations = append(violations, fmt.Sprintf("width %d below minimum %d", width, *v.minWidth))
	}
	if v.maxWidth != nil && width > *v.maxWidth {
		violations = append(violations, fmt.Sprintf("width %d exceeds maximum %d", width, *v.maxWidth))
	}
	return violations
}

func (v *ImageDimensionsValidator) checkHeightRange(height int) []string {
	var violations []string
	if v.minHeight != nil && height < *v.minHeight {
		violations = append(violations, fmt.Sprintf("height %d below minimum %d", height, *v.minHeight))
	}
	if v.maxHeight != nil && height > *v.maxHeight {
		violations = append(violations, fmt.Sprintf("height %d exceeds maximum %d", height, *v.maxHeight))
	}
	return violations
}

// AudioDurationValidator checks that audio duration is within range
type AudioDurationValidator struct {
	minSeconds *float64
	maxSeconds *float64
}

// NewAudioDurationValidator creates a new audio_duration validator from params
func NewAudioDurationValidator(params map[string]interface{}) runtimeValidators.Validator {
	validator := &AudioDurationValidator{}

	if minSec, ok := params["min_seconds"].(float64); ok {
		validator.minSeconds = &minSec
	} else if minSecInt, ok := params["min_seconds"].(int); ok {
		minSec := float64(minSecInt)
		validator.minSeconds = &minSec
	}

	if maxSec, ok := params["max_seconds"].(float64); ok {
		validator.maxSeconds = &maxSec
	} else if maxSecInt, ok := params["max_seconds"].(int); ok {
		maxSec := float64(maxSecInt)
		validator.maxSeconds = &maxSec
	}

	return validator
}

// Validate checks if audio duration is within allowed range
func (v *AudioDurationValidator) Validate(content string, params map[string]interface{}) runtimeValidators.ValidationResult {
	message, ok := params["message"].(types.Message)
	if !ok {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error": errMessageRequired,
			},
		}
	}

	var audioCount int
	var violations []string

	for _, part := range message.Parts {
		if part.Type == types.ContentTypeAudio && part.Media != nil {
			audioCount++
			violations = append(violations, v.checkAudioDuration(part.Media)...)
		}
	}

	if audioCount == 0 {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error": errNoAudioFound,
			},
		}
	}

	return runtimeValidators.ValidationResult{
		Passed: len(violations) == 0,
		Details: map[string]interface{}{
			"audio_count": audioCount,
			"violations":  violations,
		},
	}
}

func (v *AudioDurationValidator) checkAudioDuration(media *types.MediaContent) []string {
	if media.Duration == nil {
		return []string{"audio missing duration metadata"}
	}

	duration := float64(*media.Duration)
	var violations []string

	if v.minSeconds != nil && duration < *v.minSeconds {
		violations = append(violations, fmt.Sprintf("duration %.1fs below minimum %.1fs", duration, *v.minSeconds))
	}
	if v.maxSeconds != nil && duration > *v.maxSeconds {
		violations = append(violations, fmt.Sprintf("duration %.1fs exceeds maximum %.1fs", duration, *v.maxSeconds))
	}

	return violations
}

// AudioFormatValidator checks that audio content has one of the allowed formats
type AudioFormatValidator struct {
	formats []string // e.g., ["mp3", "wav", "opus"]
}

// NewAudioFormatValidator creates a new audio_format validator from params
func NewAudioFormatValidator(params map[string]interface{}) runtimeValidators.Validator {
	formats := extractStringSlice(params, "formats")
	return &AudioFormatValidator{formats: formats}
}

// Validate checks if the message contains audio with allowed formats
func (v *AudioFormatValidator) Validate(content string, params map[string]interface{}) runtimeValidators.ValidationResult {
	message, ok := params["message"].(types.Message)
	if !ok {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error": errMessageRequired,
			},
		}
	}

	if len(v.formats) == 0 {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error": errAtLeastOneFormat,
			},
		}
	}

	var audioFormats []string
	var invalidFormats []string

	for _, part := range message.Parts {
		if part.Type == types.ContentTypeAudio && part.Media != nil {
			format := extractFormatFromMIMEType(part.Media.MIMEType)
			audioFormats = append(audioFormats, format)

			if !v.isAllowedFormat(format) {
				invalidFormats = append(invalidFormats, format)
			}
		}
	}

	if len(audioFormats) == 0 {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error":           errNoAudioFound,
				"allowed_formats": v.formats,
			},
		}
	}

	return runtimeValidators.ValidationResult{
		Passed: len(invalidFormats) == 0,
		Details: map[string]interface{}{
			"found_formats":   audioFormats,
			"invalid_formats": invalidFormats,
			"allowed_formats": v.formats,
		},
	}
}

func (v *AudioFormatValidator) isAllowedFormat(format string) bool {
	format = strings.ToLower(format)
	for _, allowed := range v.formats {
		if strings.ToLower(allowed) == format {
			return true
		}
	}
	return false
}

// VideoDurationValidator checks that video duration is within range
type VideoDurationValidator struct {
	minSeconds *float64
	maxSeconds *float64
}

// NewVideoDurationValidator creates a new video_duration validator from params
func NewVideoDurationValidator(params map[string]interface{}) runtimeValidators.Validator {
	validator := &VideoDurationValidator{}

	if minSec, ok := params["min_seconds"].(float64); ok {
		validator.minSeconds = &minSec
	} else if minSecInt, ok := params["min_seconds"].(int); ok {
		minSec := float64(minSecInt)
		validator.minSeconds = &minSec
	}

	if maxSec, ok := params["max_seconds"].(float64); ok {
		validator.maxSeconds = &maxSec
	} else if maxSecInt, ok := params["max_seconds"].(int); ok {
		maxSec := float64(maxSecInt)
		validator.maxSeconds = &maxSec
	}

	return validator
}

// Validate checks if video duration is within allowed range
func (v *VideoDurationValidator) Validate(content string, params map[string]interface{}) runtimeValidators.ValidationResult {
	message, ok := params["message"].(types.Message)
	if !ok {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error": errMessageRequired,
			},
		}
	}

	var videoCount int
	var violations []string

	for _, part := range message.Parts {
		if part.Type == types.ContentTypeVideo && part.Media != nil {
			videoCount++
			violations = append(violations, v.checkVideoDuration(part.Media)...)
		}
	}

	if videoCount == 0 {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error": errNoVideoFound,
			},
		}
	}

	return runtimeValidators.ValidationResult{
		Passed: len(violations) == 0,
		Details: map[string]interface{}{
			"video_count": videoCount,
			"violations":  violations,
		},
	}
}

func (v *VideoDurationValidator) checkVideoDuration(media *types.MediaContent) []string {
	if media.Duration == nil {
		return []string{"video missing duration metadata"}
	}

	duration := float64(*media.Duration)
	var violations []string

	if v.minSeconds != nil && duration < *v.minSeconds {
		violations = append(violations, fmt.Sprintf("duration %.1fs below minimum %.1fs", duration, *v.minSeconds))
	}
	if v.maxSeconds != nil && duration > *v.maxSeconds {
		violations = append(violations, fmt.Sprintf("duration %.1fs exceeds maximum %.1fs", duration, *v.maxSeconds))
	}

	return violations
}

// VideoResolutionValidator checks that video resolution meets requirements
type VideoResolutionValidator struct {
	minWidth  *int
	maxWidth  *int
	minHeight *int
	maxHeight *int
	presets   []string // e.g., ["720p", "1080p", "4k"]
}

// NewVideoResolutionValidator creates a new video_resolution validator from params
func NewVideoResolutionValidator(params map[string]interface{}) runtimeValidators.Validator {
	validator := &VideoResolutionValidator{}

	if minWidth, ok := params["min_width"].(int); ok {
		validator.minWidth = &minWidth
	}
	if maxWidth, ok := params["max_width"].(int); ok {
		validator.maxWidth = &maxWidth
	}
	if minHeight, ok := params["min_height"].(int); ok {
		validator.minHeight = &minHeight
	}
	if maxHeight, ok := params["max_height"].(int); ok {
		validator.maxHeight = &maxHeight
	}

	validator.presets = extractStringSlice(params, "presets")

	return validator
}

// Validate checks if video resolution meets requirements
func (v *VideoResolutionValidator) Validate(content string, params map[string]interface{}) runtimeValidators.ValidationResult {
	message, ok := params["message"].(types.Message)
	if !ok {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error": errMessageRequired,
			},
		}
	}

	var videoCount int
	var violations []string

	for _, part := range message.Parts {
		if part.Type == types.ContentTypeVideo && part.Media != nil {
			videoCount++
			violations = append(violations, v.checkVideoResolution(part.Media)...)
		}
	}

	if videoCount == 0 {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error": errNoVideoFound,
			},
		}
	}

	return runtimeValidators.ValidationResult{
		Passed: len(violations) == 0,
		Details: map[string]interface{}{
			"video_count": videoCount,
			"violations":  violations,
		},
	}
}

func (v *VideoResolutionValidator) checkVideoResolution(media *types.MediaContent) []string {
	if media.Width == nil || media.Height == nil {
		return []string{"video missing width/height metadata"}
	}

	width := *media.Width
	height := *media.Height
	var violations []string

	// Check presets first
	if len(v.presets) > 0 {
		if !v.matchesAnyPreset(width, height) {
			return []string{fmt.Sprintf("resolution %dx%d does not match any preset: %v", width, height, v.presets)}
		}
	}

	// Check min/max ranges
	violations = append(violations, v.checkVideoWidthRange(width)...)
	violations = append(violations, v.checkVideoHeightRange(height)...)

	return violations
}

func (v *VideoResolutionValidator) matchesAnyPreset(width, height int) bool {
	for _, preset := range v.presets {
		if v.matchesResolutionPreset(width, height, preset) {
			return true
		}
	}
	return false
}

func (v *VideoResolutionValidator) checkVideoWidthRange(width int) []string {
	var violations []string
	if v.minWidth != nil && width < *v.minWidth {
		violations = append(violations, fmt.Sprintf("width %d below minimum %d", width, *v.minWidth))
	}
	if v.maxWidth != nil && width > *v.maxWidth {
		violations = append(violations, fmt.Sprintf("width %d exceeds maximum %d", width, *v.maxWidth))
	}
	return violations
}

func (v *VideoResolutionValidator) checkVideoHeightRange(height int) []string {
	var violations []string
	if v.minHeight != nil && height < *v.minHeight {
		violations = append(violations, fmt.Sprintf("height %d below minimum %d", height, *v.minHeight))
	}
	if v.maxHeight != nil && height > *v.maxHeight {
		violations = append(violations, fmt.Sprintf("height %d exceeds maximum %d", height, *v.maxHeight))
	}
	return violations
}

func (v *VideoResolutionValidator) matchesResolutionPreset(width, height int, preset string) bool {
	preset = strings.ToLower(preset)
	switch preset {
	case "480p", "sd":
		return height == 480
	case "720p", "hd":
		return height == 720
	case "1080p", "fhd", "full_hd":
		return height == 1080
	case "1440p", "2k", "qhd":
		return height == 1440
	case "2160p", "4k", "uhd":
		return height == 2160
	case "4320p", "8k":
		return height == 4320
	default:
		return false
	}
}

// extractFormatFromMIMEType extracts the format from a MIME type string
// e.g., "image/jpeg" -> "jpeg", "audio/mpeg" -> "mp3"
func extractFormatFromMIMEType(mimeType string) string {
	parts := strings.Split(mimeType, "/")
	if len(parts) != 2 {
		return mimeType
	}

	format := parts[1]

	// Special cases
	switch format {
	case "mpeg":
		if strings.HasPrefix(mimeType, "audio/") {
			return "mp3"
		}
	}

	return format
}
