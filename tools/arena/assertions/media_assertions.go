package assertions

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// Error messages
const (
	errMessageRequired   = "assistant message not found in validation context"
	errNoImagesFound     = "no images found in message"
	errNoAudioFound      = "no audio found in message"
	errNoVideoFound      = "no video found in message"
	errMissingDimensions = "image missing width/height metadata"
	errMissingDuration   = "missing duration metadata"
	errAtLeastOneFormat  = "at least one format must be specified"
)

// Violation message templates
const (
	msgDurationBelowMin = "duration %.1fs below minimum %.1fs"
	msgDurationAboveMax = "duration %.1fs exceeds maximum %.1fs"
	msgWidthBelowMin    = "width %d below minimum %d"
	msgWidthAboveMax    = "width %d exceeds maximum %d"
	msgHeightBelowMin   = "height %d below minimum %d"
	msgHeightAboveMax   = "height %d exceeds maximum %d"
)

// baseMediaValidator provides common functionality for all media validators
type baseMediaValidator struct{}

// extractAssistantMessage safely extracts the assistant message from params
func (b *baseMediaValidator) extractAssistantMessage(params map[string]interface{}) (types.Message, bool) {
	message, ok := params["_assistant_message"].(types.Message)
	return message, ok
}

// createErrorResult creates a validation result with an error
func (b *baseMediaValidator) createErrorResult(errorMsg string) runtimeValidators.ValidationResult {
	return runtimeValidators.ValidationResult{
		Passed: false,
		Details: map[string]interface{}{
			"error": errorMsg,
		},
	}
}

// formatValidator provides common format validation logic
type formatValidator struct {
	baseMediaValidator
	formats     []string
	contentType string
	noMediaErr  string
}

// validateFormats checks if media parts have allowed formats
func (v *formatValidator) validateFormats(message types.Message) runtimeValidators.ValidationResult {
	if len(v.formats) == 0 {
		return v.createErrorResult(errAtLeastOneFormat)
	}

	var foundFormats []string
	var invalidFormats []string

	for _, part := range message.Parts {
		if part.Type == v.contentType && part.Media != nil {
			format := extractFormatFromMIMEType(part.Media.MIMEType)
			foundFormats = append(foundFormats, format)

			if !v.isAllowedFormat(format) {
				invalidFormats = append(invalidFormats, format)
			}
		}
	}

	if len(foundFormats) == 0 {
		return runtimeValidators.ValidationResult{
			Passed: false,
			Details: map[string]interface{}{
				"error":           v.noMediaErr,
				"allowed_formats": v.formats,
			},
		}
	}

	return runtimeValidators.ValidationResult{
		Passed: len(invalidFormats) == 0,
		Details: map[string]interface{}{
			"found_formats":   foundFormats,
			"invalid_formats": invalidFormats,
			"allowed_formats": v.formats,
		},
	}
}

func (v *formatValidator) isAllowedFormat(format string) bool {
	format = strings.ToLower(format)
	for _, allowed := range v.formats {
		if strings.ToLower(allowed) == format {
			return true
		}
	}
	return false
}

// durationValidator provides common duration validation logic
type durationValidator struct {
	baseMediaValidator
	minSeconds  *float64
	maxSeconds  *float64
	contentType string
	noMediaErr  string
	countKey    string
}

func (v *durationValidator) checkDuration(media *types.MediaContent) []string {
	if media.Duration == nil {
		return []string{"missing duration metadata"}
	}

	duration := float64(*media.Duration)
	var violations []string

	if v.minSeconds != nil && duration < *v.minSeconds {
		violations = append(violations, fmt.Sprintf(msgDurationBelowMin, duration, *v.minSeconds))
	}
	if v.maxSeconds != nil && duration > *v.maxSeconds {
		violations = append(violations, fmt.Sprintf(msgDurationAboveMax, duration, *v.maxSeconds))
	}

	return violations
}

// dimensionValidator provides common dimension validation logic
type dimensionValidator struct {
	baseMediaValidator
	minWidth  *int
	maxWidth  *int
	minHeight *int
	maxHeight *int
}

func (v *dimensionValidator) checkWidthRange(width int) []string {
	var violations []string
	if v.minWidth != nil && width < *v.minWidth {
		violations = append(violations, fmt.Sprintf(msgWidthBelowMin, width, *v.minWidth))
	}
	if v.maxWidth != nil && width > *v.maxWidth {
		violations = append(violations, fmt.Sprintf(msgWidthAboveMax, width, *v.maxWidth))
	}
	return violations
}

func (v *dimensionValidator) checkHeightRange(height int) []string {
	var violations []string
	if v.minHeight != nil && height < *v.minHeight {
		violations = append(violations, fmt.Sprintf(msgHeightBelowMin, height, *v.minHeight))
	}
	if v.maxHeight != nil && height > *v.maxHeight {
		violations = append(violations, fmt.Sprintf(msgHeightAboveMax, height, *v.maxHeight))
	}
	return violations
}

// extractDurationParams extracts min/max duration parameters from params map
func extractDurationParams(params map[string]interface{}) (minSeconds, maxSeconds *float64) {
	if minSec, ok := params["min_seconds"].(float64); ok {
		minSeconds = &minSec
	} else if minSecInt, ok := params["min_seconds"].(int); ok {
		minSec := float64(minSecInt)
		minSeconds = &minSec
	}

	if maxSec, ok := params["max_seconds"].(float64); ok {
		maxSeconds = &maxSec
	} else if maxSecInt, ok := params["max_seconds"].(int); ok {
		maxSec := float64(maxSecInt)
		maxSeconds = &maxSec
	}

	return minSeconds, maxSeconds
}

// ImageFormatValidator checks that media content has one of the allowed image formats
type ImageFormatValidator struct {
	formatValidator
}

// NewImageFormatValidator creates a new image_format validator from params
func NewImageFormatValidator(params map[string]interface{}) runtimeValidators.Validator {
	return &ImageFormatValidator{
		formatValidator: formatValidator{
			formats:     extractStringSlice(params, "formats"),
			contentType: types.ContentTypeImage,
			noMediaErr:  errNoImagesFound,
		},
	}
}

// Validate checks if the message contains images with allowed formats
func (v *ImageFormatValidator) Validate(content string, params map[string]interface{}) runtimeValidators.ValidationResult {
	message, ok := v.extractAssistantMessage(params)
	if !ok {
		return v.createErrorResult(errMessageRequired)
	}
	return v.validateFormats(message)
}

// ImageDimensionsValidator checks that images meet dimension requirements
type ImageDimensionsValidator struct {
	dimensionValidator
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
	message, ok := v.extractAssistantMessage(params)
	if !ok {
		return v.createErrorResult(errMessageRequired)
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
		return v.createErrorResult(errNoImagesFound)
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

	// Check min/max using base validator methods
	violations = append(violations, v.checkWidthRange(width)...)
	violations = append(violations, v.checkHeightRange(height)...)

	return violations
}

// AudioDurationValidator checks that audio duration is within range
type AudioDurationValidator struct {
	durationValidator
}

// NewAudioDurationValidator creates a new audio_duration validator from params
func NewAudioDurationValidator(params map[string]interface{}) runtimeValidators.Validator {
	validator := &AudioDurationValidator{
		durationValidator: durationValidator{
			contentType: types.ContentTypeAudio,
			noMediaErr:  errNoAudioFound,
			countKey:    "audio_count",
		},
	}

	validator.minSeconds, validator.maxSeconds = extractDurationParams(params)

	return validator
}

// Validate checks if audio duration is within allowed range
func (v *AudioDurationValidator) Validate(content string, params map[string]interface{}) runtimeValidators.ValidationResult {
	message, ok := v.extractAssistantMessage(params)
	if !ok {
		return v.createErrorResult(errMessageRequired)
	}

	var audioCount int
	var violations []string
	var foundDurations []float64

	for _, part := range message.Parts {
		if part.Type == v.contentType && part.Media != nil {
			audioCount++
			if part.Media.Duration != nil {
				foundDurations = append(foundDurations, float64(*part.Media.Duration))
			}
			violations = append(violations, v.checkDuration(part.Media)...)
		}
	}

	if audioCount == 0 {
		return v.createErrorResult(v.noMediaErr)
	}

	details := map[string]interface{}{
		"audio_count":     audioCount,
		"found_durations": foundDurations,
		"violations":      violations,
	}

	if v.minSeconds != nil {
		details["min_seconds"] = *v.minSeconds
	}
	if v.maxSeconds != nil {
		details["max_seconds"] = *v.maxSeconds
	}

	return runtimeValidators.ValidationResult{
		Passed:  len(violations) == 0,
		Details: details,
	}
}

// AudioFormatValidator checks that audio content has one of the allowed formats
type AudioFormatValidator struct {
	formatValidator
}

// NewAudioFormatValidator creates a new audio_format validator from params
func NewAudioFormatValidator(params map[string]interface{}) runtimeValidators.Validator {
	return &AudioFormatValidator{
		formatValidator: formatValidator{
			formats:     extractStringSlice(params, "formats"),
			contentType: types.ContentTypeAudio,
			noMediaErr:  errNoAudioFound,
		},
	}
}

// Validate checks if the message contains audio with allowed formats
func (v *AudioFormatValidator) Validate(content string, params map[string]interface{}) runtimeValidators.ValidationResult {
	message, ok := v.extractAssistantMessage(params)
	if !ok {
		return v.createErrorResult(errMessageRequired)
	}
	return v.validateFormats(message)
}

// VideoDurationValidator checks that video duration is within range
type VideoDurationValidator struct {
	durationValidator
}

// NewVideoDurationValidator creates a new video_duration validator from params
func NewVideoDurationValidator(params map[string]interface{}) runtimeValidators.Validator {
	validator := &VideoDurationValidator{
		durationValidator: durationValidator{
			contentType: types.ContentTypeVideo,
			noMediaErr:  errNoVideoFound,
			countKey:    "video_count",
		},
	}

	validator.minSeconds, validator.maxSeconds = extractDurationParams(params)

	return validator
}

// Validate checks if video duration is within allowed range
func (v *VideoDurationValidator) Validate(content string, params map[string]interface{}) runtimeValidators.ValidationResult {
	message, ok := v.extractAssistantMessage(params)
	if !ok {
		return v.createErrorResult(errMessageRequired)
	}

	var videoCount int
	var violations []string
	var foundDurations []float64

	for _, part := range message.Parts {
		if part.Type == v.contentType && part.Media != nil {
			videoCount++
			if part.Media.Duration != nil {
				foundDurations = append(foundDurations, float64(*part.Media.Duration))
			}
			violations = append(violations, v.checkDuration(part.Media)...)
		}
	}

	if videoCount == 0 {
		return v.createErrorResult(v.noMediaErr)
	}

	details := map[string]interface{}{
		"video_count":     videoCount,
		"found_durations": foundDurations,
		"violations":      violations,
	}

	if v.minSeconds != nil {
		details["min_seconds"] = *v.minSeconds
	}
	if v.maxSeconds != nil {
		details["max_seconds"] = *v.maxSeconds
	}

	return runtimeValidators.ValidationResult{
		Passed:  len(violations) == 0,
		Details: details,
	}
}

// VideoResolutionValidator checks that video resolution meets requirements
type VideoResolutionValidator struct {
	dimensionValidator
	presets []string // e.g., ["720p", "1080p", "4k"]
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
	message, ok := v.extractAssistantMessage(params)
	if !ok {
		return v.createErrorResult(errMessageRequired)
	}

	var videoCount int
	var violations []string
	var foundResolutions []string

	for _, part := range message.Parts {
		if part.Type == types.ContentTypeVideo && part.Media != nil {
			videoCount++
			if part.Media.Width != nil && part.Media.Height != nil {
				foundResolutions = append(foundResolutions,
					fmt.Sprintf("%dx%d", *part.Media.Width, *part.Media.Height))
			}
			violations = append(violations, v.checkVideoResolution(part.Media)...)
		}
	}

	if videoCount == 0 {
		return v.createErrorResult(errNoVideoFound)
	}

	details := map[string]interface{}{
		"video_count":       videoCount,
		"found_resolutions": foundResolutions,
		"violations":        violations,
	}

	// Add constraints to details
	if len(v.presets) > 0 {
		details["allowed_presets"] = v.presets
	}
	if v.minWidth != nil {
		details["min_width"] = *v.minWidth
	}
	if v.maxWidth != nil {
		details["max_width"] = *v.maxWidth
	}
	if v.minHeight != nil {
		details["min_height"] = *v.minHeight
	}
	if v.maxHeight != nil {
		details["max_height"] = *v.maxHeight
	}

	return runtimeValidators.ValidationResult{
		Passed:  len(violations) == 0,
		Details: details,
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

	// Check min/max ranges using base validator methods
	violations = append(violations, v.checkWidthRange(width)...)
	violations = append(violations, v.checkHeightRange(height)...)

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
