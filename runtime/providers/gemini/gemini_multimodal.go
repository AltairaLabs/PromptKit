package gemini

import (
	"slices"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const (
	roleAssistant          = "assistant"
	roleModel              = "model"
	finishReasonMaxTokens  = "MAX_TOKENS"
	finishReasonSafety     = "SAFETY"
	finishReasonRecitation = "RECITATION"
)

// geminiAudioFormats are the audio MIME types the Gemini API accepts
// (ai.google.dev/gemini-api/docs/audio).
var geminiAudioFormats = []string{
	types.MIMETypeAudioWAV,
	"audio/mp3",
	"audio/aiff",
	"audio/aac",
	types.MIMETypeAudioOgg,
	"audio/flac",
	types.MIMETypeAudioMP3, // audio/mpeg
	"audio/m4a",
	"audio/l16",
	"audio/opus",
	"audio/alaw",
	"audio/mulaw",
	types.MIMETypeAudioWebM,
}

// geminiImageFormats are the image MIME types the Gemini API accepts
// (ai.google.dev/gemini-api/docs/image-understanding).
var geminiImageFormats = []string{
	types.MIMETypeImageJPEG,
	types.MIMETypeImagePNG,
	types.MIMETypeImageWebP,
	types.MIMETypeImageGIF,
	"image/heic",
	"image/heif",
}

// geminiDocumentedVideoFormats are the video MIME types the Gemini API
// documents accepting (ai.google.dev/gemini-api/docs/video-understanding).
var geminiDocumentedVideoFormats = []string{
	types.MIMETypeVideoMP4,
	"video/mpeg",
	"video/mov",
	"video/avi",
	"video/x-flv",
	"video/mpg",
	types.MIMETypeVideoWebM,
	"video/wmv",
	"video/3gpp",
}

// geminiVideoFormats are the video MIME types the adapter sends on
// generateContent: the documented ones, and "video/flv" alongside
// "video/x-flv" because earlier releases declared it.
var geminiVideoFormats = append(slices.Clone(geminiDocumentedVideoFormats), "video/flv")

// geminiOfficeDocumentFormats are the Microsoft Office and OpenDocument MIME
// types the Gemini API reads natively, on generateContent and Interactions.
var geminiOfficeDocumentFormats = []string{
	types.MIMETypeDocx,
	types.MIMETypeDoc,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.template",
	"application/vnd.oasis.opendocument.text",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation",
	"application/vnd.ms-powerpoint",
	"application/vnd.oasis.opendocument.presentation",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"application/vnd.ms-excel",
	"application/vnd.oasis.opendocument.spreadsheet",
}

// geminiDocumentFormats are the document MIME types the Gemini API accepts
// (ai.google.dev/gemini-api/docs/document-processing): PDF, Microsoft Office
// and OpenDocument files, and text formats, which it reads as plain text.
var geminiDocumentFormats = slices.Concat(
	[]string{types.MIMETypePDF},
	geminiOfficeDocumentFormats,
	[]string{"text/*", types.MIMETypeJSON, types.MIMETypeXML, "application/rtf"},
)

// GetMultimodalCapabilities returns Gemini's multimodal support capabilities.
// When the provider config declares its capabilities, the declaration is
// authoritative for which modalities are enabled; otherwise Gemini's built-in
// defaults apply (images + audio + video + documents).
func (p *Provider) GetMultimodalCapabilities() providers.MultimodalCapabilities {
	caps := p.SendableMultimodalCapabilities()
	return providers.ApplyDeclaredModalities(caps, p.capabilities)
}

// SendableMultimodalCapabilities returns what the adapter sends whatever the
// config declares: every image, audio, video and document part, inline. The
// MIME types are those of the API the provider sends every turn to: the
// Interactions API's lists with api_mode interactions, else generateContent's.
func (p *Provider) SendableMultimodalCapabilities() providers.MultimodalCapabilities {
	caps := p.generateContentSendable()
	if p.resolveAPIMode() == APIModeInteractions {
		caps.ImageFormats = slices.Clone(interactionsMIMETypes[types.ContentTypeImage])
		caps.AudioFormats = slices.Clone(interactionsMIMETypes[types.ContentTypeAudio])
		caps.VideoFormats = slices.Clone(interactionsMIMETypes[types.ContentTypeVideo])
		caps.DocumentFormats = slices.Clone(interactionsMIMETypes[types.ContentTypeDocument])
	}
	return caps
}

// generateContentSendable is what the adapter sends on generateContent.
func (p *Provider) generateContentSendable() providers.MultimodalCapabilities {
	return providers.MultimodalCapabilities{
		SupportsImages:    true,
		SupportsAudio:     true,
		SupportsVideo:     true,
		SupportsDocuments: true,
		ImageFormats:      slices.Clone(geminiImageFormats),
		// The adapter sends every media part as inlineData with its own MIME
		// type, so these lists are what the Gemini API documents accepting.
		AudioFormats: slices.Clone(geminiAudioFormats),
		VideoFormats: slices.Clone(geminiVideoFormats),
		// PDF and Office files are understood natively; text formats are read
		// as plain text.
		DocumentFormats:   slices.Clone(geminiDocumentFormats),
		MaxImageSizeMB:    20, //nolint:mnd // Gemini's documented media size limit
		MaxAudioSizeMB:    20, //nolint:mnd // Gemini's documented media size limit
		MaxVideoSizeMB:    20, //nolint:mnd // Gemini's documented media size limit
		MaxDocumentSizeMB: 20, //nolint:mnd // Gemini's documented media size limit
	}
}
