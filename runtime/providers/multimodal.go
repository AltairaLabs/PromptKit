package providers

import (
	"strconv"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// roleAssistant is the role of a model reply, which request validation skips.
const roleAssistant = "assistant"

// Declared capability names used in a provider's Capabilities list. These are
// the source of truth for multimodal support when a provider config declares
// them, replacing per-model hardcoding inside providers.
const (
	CapabilityText      = "text"
	CapabilityStreaming = "streaming"
	CapabilityVision    = "vision"
	CapabilityAudio     = "audio"
	CapabilityVideo     = "video"
	CapabilityTools     = "tools"
	CapabilityJSON      = "json"
	CapabilityDocuments = "documents"
)

// CapabilitySet converts a declared capability list into a membership set for
// O(1) lookup. It returns nil for an empty list so callers can distinguish
// "not declared" (nil, fall back to built-in defaults) from a non-empty
// declaration that is authoritative.
func CapabilitySet(capabilities []string) map[string]bool {
	if len(capabilities) == 0 {
		return nil
	}
	set := make(map[string]bool, len(capabilities))
	for _, c := range capabilities {
		set[c] = true
	}
	return set
}

// MultimodalCapabilities describes what types of multimodal content a provider supports
type MultimodalCapabilities struct {
	SupportsImages    bool     // Provider can process image inputs
	SupportsAudio     bool     // Provider can process audio inputs
	SupportsVideo     bool     // Provider can process video inputs
	SupportsDocuments bool     // Provider can process document inputs (PDF, etc.)
	ImageFormats      []string // Supported image MIME types (e.g., "image/jpeg", "image/png")
	AudioFormats      []string // Supported audio MIME types (e.g., "audio/mpeg", "audio/wav")
	VideoFormats      []string // Supported video MIME types (e.g., "video/mp4")
	DocumentFormats   []string // Supported document MIME types (e.g., "application/pdf")
	MaxImageSizeMB    int      // Maximum image size in megabytes (0 = unlimited/unknown)
	MaxAudioSizeMB    int      // Maximum audio size in megabytes (0 = unlimited/unknown)
	MaxVideoSizeMB    int      // Maximum video size in megabytes (0 = unlimited/unknown)
	MaxDocumentSizeMB int      // Maximum document size in megabytes (0 = unlimited/unknown)
}

// ImageDetail specifies the level of detail for image processing
type ImageDetail string

// Image detail levels for multimodal processing.
const (
	ImageDetailLow  ImageDetail = "low"  // Faster, less detailed analysis
	ImageDetailHigh ImageDetail = "high" // Slower, more detailed analysis
	ImageDetailAuto ImageDetail = "auto" // Provider chooses automatically
)

// MultimodalCapabilityProvider is implemented by providers that support multimodal content.
type MultimodalCapabilityProvider interface {
	GetMultimodalCapabilities() MultimodalCapabilities
}

// SendableContentProvider is implemented by providers whose
// GetMultimodalCapabilities applies a config's declared capabilities list.
// SendableMultimodalCapabilities reports what the adapter itself can send,
// whatever the config declares. Request validation (ValidateMultimodalMessage)
// checks against it, so a partial capabilities list, often written for
// scenario selection, never rejects a part the adapter would send.
type SendableContentProvider interface {
	SendableMultimodalCapabilities() MultimodalCapabilities
}

// SupportsMultimodal checks if a provider implements multimodal support
func SupportsMultimodal(p Provider) bool {
	_, ok := p.(MultimodalCapabilityProvider)
	return ok
}

// GetMultimodalProvider safely casts a provider to MultimodalCapabilityProvider
// Returns nil if the provider doesn't support multimodal
func GetMultimodalProvider(p Provider) MultimodalCapabilityProvider {
	if mp, ok := p.(MultimodalCapabilityProvider); ok {
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

// HasDocumentSupport checks if a provider supports document inputs
func HasDocumentSupport(p Provider) bool {
	mp := GetMultimodalProvider(p)
	if mp == nil {
		return false
	}
	return mp.GetMultimodalCapabilities().SupportsDocuments
}

// IsFormatSupported checks if a provider supports a specific media format (MIME type)
func IsFormatSupported(p Provider, contentType, mimeType string) bool {
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
	case types.ContentTypeDocument:
		if !caps.SupportsDocuments {
			return false
		}
		formats = caps.DocumentFormats
	default:
		return false
	}

	return formatListed(formats, mimeType)
}

// formatListed reports whether mimeType appears in formats. An empty list
// admits every format. An entry ending in "/*" (for example "text/*") admits
// every subtype of that type.
func formatListed(formats []string, mimeType string) bool {
	if len(formats) == 0 {
		return true
	}
	for _, format := range formats {
		if format == mimeType {
			return true
		}
		if prefix, ok := strings.CutSuffix(format, "*"); ok && strings.HasSuffix(prefix, "/") &&
			strings.HasPrefix(mimeType, prefix) {
			return true
		}
	}
	return false
}

// ValidateMultimodalMessage checks that the provider supports the image, audio,
// video and document parts of a message and their MIME types. It returns an
// *UnsupportedContentError for the first part that fails. A message with no
// media parts is always valid, even when its text is carried in Parts. A part
// with no MIME type is checked for its modality only. A provider that
// implements SendableContentProvider is checked against what its adapter can
// send, so a config's capabilities list never narrows the check.
func ValidateMultimodalMessage(p Provider, msg types.Message) error {
	if !msg.HasMediaContent() {
		return nil // Text-only message, no validation needed
	}

	mp := GetMultimodalProvider(p)
	if mp == nil {
		return &UnsupportedContentError{
			Provider:    p.ID(),
			ContentType: "multimodal",
			Message:     "provider does not support multimodal content",
			PartIndex:   -1,
		}
	}

	caps := mp.GetMultimodalCapabilities()
	if sp, ok := p.(SendableContentProvider); ok {
		caps = sp.SendableMultimodalCapabilities()
	}

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
	case types.ContentTypeImage:
		return validateMediaPart(p, part, index, caps.SupportsImages, caps.ImageFormats, "image")

	case types.ContentTypeAudio:
		return validateMediaPart(p, part, index, caps.SupportsAudio, caps.AudioFormats, "audio")

	case types.ContentTypeVideo:
		return validateMediaPart(p, part, index, caps.SupportsVideo, caps.VideoFormats, "video")

	case types.ContentTypeDocument:
		return validateMediaPart(p, part, index, caps.SupportsDocuments, caps.DocumentFormats, "document")

	default:
		return nil // Text and other non-media parts are always supported
	}
}

// validateMediaPart validates a media content part (image, audio, video or document)
func validateMediaPart(
	p Provider, part types.ContentPart, index int, supported bool, formats []string, contentType string,
) error {
	if !supported {
		return &UnsupportedContentError{
			Provider:    p.ID(),
			ContentType: contentType,
			Message:     "provider does not support " + contentType + " inputs",
			PartIndex:   index,
		}
	}

	if part.Media != nil && part.Media.MIMEType != "" && !formatListed(formats, part.Media.MIMEType) {
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
	ContentType string // "image", "audio", "video", "document", or "multimodal"
	Message     string // Human-readable error message
	PartIndex   int    // Index of the unsupported content part; -1 when the error is not about one part
	MIMEType    string // Specific MIME type that's unsupported (if applicable)
}

// Error returns the message, naming the part when PartIndex is not negative.
func (e *UnsupportedContentError) Error() string {
	if e.PartIndex >= 0 {
		return e.Provider + ": " + e.Message + " (part " + strconv.Itoa(e.PartIndex) + ")"
	}
	return e.Provider + ": " + e.Message
}

// ValidateMultimodalRequest checks the input messages of a predict request
// with ValidateMultimodalMessage and returns the first *UnsupportedContentError.
// Assistant messages are not checked: their parts are what a model produced,
// not input the provider's capabilities describe, and each provider replays
// them in its own way.
//
// The provider stage calls it before every predict and stream call to a
// provider that implements MultimodalCapabilityProvider, unless
// stage.ProviderConfig.SkipContentValidation is set.
func ValidateMultimodalRequest(p Provider, req *PredictionRequest) error {
	for i := range req.Messages {
		if req.Messages[i].Role == roleAssistant {
			continue
		}
		if err := ValidateMultimodalMessage(p, req.Messages[i]); err != nil {
			return err
		}
	}
	return nil
}
