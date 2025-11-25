package providers

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// MultimodalCapabilities describes what types of multimodal content a provider supports
type MultimodalCapabilities struct {
	SupportsImages bool     // Provider can process image inputs
	SupportsAudio  bool     // Provider can process audio inputs
	SupportsVideo  bool     // Provider can process video inputs
	ImageFormats   []string // Supported image MIME types (e.g., "image/jpeg", "image/png")
	AudioFormats   []string // Supported audio MIME types (e.g., "audio/mpeg", "audio/wav")
	VideoFormats   []string // Supported video MIME types (e.g., "video/mp4")
	MaxImageSizeMB int      // Maximum image size in megabytes (0 = unlimited/unknown)
	MaxAudioSizeMB int      // Maximum audio size in megabytes (0 = unlimited/unknown)
	MaxVideoSizeMB int      // Maximum video size in megabytes (0 = unlimited/unknown)
}

// ImageDetail specifies the level of detail for image processing
type ImageDetail string

const (
	ImageDetailLow  ImageDetail = "low"  // Faster, less detailed analysis
	ImageDetailHigh ImageDetail = "high" // Slower, more detailed analysis
	ImageDetailAuto ImageDetail = "auto" // Provider chooses automatically
)

// MultimodalSupport interface for providers that support multimodal inputs
type MultimodalSupport interface {
	Provider // Extends the base Provider interface

	// GetMultimodalCapabilities returns what types of multimodal content this provider supports
	GetMultimodalCapabilities() MultimodalCapabilities

	// PredictMultimodal performs a predict request with multimodal message content
	// Messages in the request can contain Parts with images, audio, or video
	PredictMultimodal(ctx context.Context, req PredictionRequest) (PredictionResponse, error)

	// PredictMultimodalStream performs a streaming predict request with multimodal content
	PredictMultimodalStream(ctx context.Context, req PredictionRequest) (<-chan StreamChunk, error)
}

// MultimodalToolSupport interface for providers that support both multimodal and tools
type MultimodalToolSupport interface {
	MultimodalSupport // Extends multimodal support
	ToolSupport       // Extends tool support

	// PredictMultimodalWithTools performs a predict request with both multimodal content and tools
	PredictMultimodalWithTools(ctx context.Context, req PredictionRequest, tools interface{}, toolChoice string) (PredictionResponse, []types.MessageToolCall, error)
}

// SupportsMultimodal checks if a provider implements multimodal support
func SupportsMultimodal(p Provider) bool {
	_, ok := p.(MultimodalSupport)
	return ok
}

// GetMultimodalProvider safely casts a provider to MultimodalSupport
// Returns nil if the provider doesn't support multimodal
func GetMultimodalProvider(p Provider) MultimodalSupport {
	if mp, ok := p.(MultimodalSupport); ok {
		return mp
	}
	return nil
}

// HasImageSupport checks if a provider supports image inputs
func HasImageSupport(p Provider) bool {
	mp := GetMultimodalProvider(p)
	if mp == nil {
		return false
	}
	return mp.GetMultimodalCapabilities().SupportsImages
}

// HasAudioSupport checks if a provider supports audio inputs
func HasAudioSupport(p Provider) bool {
	mp := GetMultimodalProvider(p)
	if mp == nil {
		return false
	}
	return mp.GetMultimodalCapabilities().SupportsAudio
}

// HasVideoSupport checks if a provider supports video inputs
func HasVideoSupport(p Provider) bool {
	mp := GetMultimodalProvider(p)
	if mp == nil {
		return false
	}
	return mp.GetMultimodalCapabilities().SupportsVideo
}

// IsFormatSupported checks if a provider supports a specific media format (MIME type)
func IsFormatSupported(p Provider, contentType string, mimeType string) bool {
	mp := GetMultimodalProvider(p)
	if mp == nil {
		return false
	}

	caps := mp.GetMultimodalCapabilities()
	var formats []string

	switch contentType {
	case types.ContentTypeImage:
		if !caps.SupportsImages {
			return false
		}
		formats = caps.ImageFormats
	case types.ContentTypeAudio:
		if !caps.SupportsAudio {
			return false
		}
		formats = caps.AudioFormats
	case types.ContentTypeVideo:
		if !caps.SupportsVideo {
			return false
		}
		formats = caps.VideoFormats
	default:
		return false
	}

	// If no formats specified, assume all formats are supported
	if len(formats) == 0 {
		return true
	}

	// Check if the MIME type is in the supported formats list
	for _, format := range formats {
		if format == mimeType {
			return true
		}
	}
	return false
}

// ValidateMultimodalMessage checks if a message's multimodal content is supported by the provider
func ValidateMultimodalMessage(p Provider, msg types.Message) error {
	if !msg.IsMultimodal() {
		return nil // Text-only message, no validation needed
	}

	mp := GetMultimodalProvider(p)
	if mp == nil {
		return &UnsupportedContentError{
			Provider:    p.ID(),
			ContentType: "multimodal",
			Message:     "provider does not support multimodal content",
		}
	}

	caps := mp.GetMultimodalCapabilities()

	// Check each content part
	for i, part := range msg.Parts {
		if err := validateContentPart(p, part, i, caps); err != nil {
			return err
		}
	}

	return nil
}

// validateContentPart validates a single content part against provider capabilities
func validateContentPart(p Provider, part types.ContentPart, index int, caps MultimodalCapabilities) error {
	switch part.Type {
	case types.ContentTypeText:
		return nil // Text is always supported

	case types.ContentTypeImage:
		return validateMediaPart(p, part, index, caps.SupportsImages, "image")

	case types.ContentTypeAudio:
		return validateMediaPart(p, part, index, caps.SupportsAudio, "audio")

	case types.ContentTypeVideo:
		return validateMediaPart(p, part, index, caps.SupportsVideo, "video")

	default:
		return nil
	}
}

// validateMediaPart validates a media content part (image, audio, or video)
func validateMediaPart(p Provider, part types.ContentPart, index int, supported bool, contentType string) error {
	if !supported {
		return &UnsupportedContentError{
			Provider:    p.ID(),
			ContentType: contentType,
			Message:     "provider does not support " + contentType + " inputs",
			PartIndex:   index,
		}
	}

	if part.Media != nil && !IsFormatSupported(p, part.Type, part.Media.MIMEType) {
		return &UnsupportedContentError{
			Provider:    p.ID(),
			ContentType: contentType,
			Message:     "unsupported " + contentType + " format: " + part.Media.MIMEType,
			PartIndex:   index,
			MIMEType:    part.Media.MIMEType,
		}
	}

	return nil
}

// UnsupportedContentError is returned when a provider doesn't support certain content types
type UnsupportedContentError struct {
	Provider    string // Provider ID
	ContentType string // "image", "audio", "video", or "multimodal"
	Message     string // Human-readable error message
	PartIndex   int    // Index of the unsupported content part (if applicable)
	MIMEType    string // Specific MIME type that's unsupported (if applicable)
}

func (e *UnsupportedContentError) Error() string {
	if e.PartIndex >= 0 {
		return e.Provider + ": " + e.Message + " (part " + string(rune(e.PartIndex)) + ")"
	}
	return e.Provider + ": " + e.Message
}

// ValidateMultimodalRequest validates all messages in a predict request for multimodal compatibility
// This is a helper function to reduce duplication across provider implementations
func ValidateMultimodalRequest(p MultimodalSupport, req PredictionRequest) error {
	for i := range req.Messages {
		if err := ValidateMultimodalMessage(p, req.Messages[i]); err != nil {
			return err
		}
	}
	return nil
}
