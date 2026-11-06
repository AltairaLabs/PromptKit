package gemini

import (
	"context"
	"fmt"
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

// geminiVideoFormats are the video MIME types the Gemini API accepts
// (ai.google.dev/gemini-api/docs/video-understanding). "video/flv" is kept
// alongside the documented "video/x-flv" because earlier releases declared it.
var geminiVideoFormats = []string{
	types.MIMETypeVideoMP4,
	"video/mpeg",
	"video/mov",
	"video/avi",
	"video/x-flv",
	"video/flv",
	"video/mpg",
	types.MIMETypeVideoWebM,
	"video/wmv",
	"video/3gpp",
}

// geminiDocumentFormats are the document MIME types the Gemini API accepts
// (ai.google.dev/gemini-api/docs/document-processing): PDF, Microsoft Office
// and OpenDocument files, and text formats, which it reads as plain text.
var geminiDocumentFormats = []string{
	types.MIMETypePDF,
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
	"text/*",
	types.MIMETypeJSON,
	types.MIMETypeXML,
	"application/rtf",
}

// GetMultimodalCapabilities returns Gemini's multimodal support capabilities.
// When the provider config declares its capabilities, the declaration is
// authoritative for which modalities are enabled; otherwise Gemini's built-in
// defaults apply (images + audio + video + documents).
func (p *Provider) GetMultimodalCapabilities() providers.MultimodalCapabilities {
	caps := p.SendableMultimodalCapabilities()
	return providers.ApplyDeclaredModalities(caps, p.capabilities)
}

// SendableMultimodalCapabilities returns what the adapter sends whatever the
// config declares: every image, audio, video and document part, as inlineData.
func (p *Provider) SendableMultimodalCapabilities() providers.MultimodalCapabilities {
	return providers.MultimodalCapabilities{
		SupportsImages:    true,
		SupportsAudio:     true,
		SupportsVideo:     true,
		SupportsDocuments: true,
		ImageFormats: []string{
			types.MIMETypeImageJPEG,
			types.MIMETypeImagePNG,
			types.MIMETypeImageWebP,
			types.MIMETypeImageGIF,
			"image/heic",
			"image/heif",
		},
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

// convertMessagesToGemini converts PromptKit messages to Gemini format
// Handles both legacy text-only and new multimodal messages.
//
// Exercised only by tests (the live path uses convertMessagesToGeminiContents);
// the unused linter runs with tests:false so it cannot see those callers.
//
//nolint:unused // covered by unit tests; linter is configured with tests:false
func (p *Provider) convertMessagesToGemini(
	ctx context.Context, messages []types.Message, systemPrompt string,
) (contents []geminiContent, systemInstruction *geminiContent, err error) {
	// Handle system message
	if systemPrompt != "" {
		systemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: systemPrompt}},
		}
	}

	for i := range messages {
		content, err := p.convertMessageToGemini(ctx, messages[i])
		if err != nil {
			return nil, nil, err
		}
		contents = append(contents, content)
	}

	return contents, systemInstruction, nil
}

// convertMessageToGemini converts a single PromptKit message to Gemini format.
//
//nolint:unused // covered by unit tests; linter is configured with tests:false
func (p *Provider) convertMessageToGemini(ctx context.Context, msg types.Message) (geminiContent, error) {
	// Handle legacy text-only messages
	if !msg.IsMultimodal() {
		role := msg.Role
		// Gemini uses "user" and "model" roles
		if role == roleAssistant {
			role = roleModel
		}
		return geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: msg.GetContent()}},
		}, nil
	}

	// Handle multimodal messages with parts
	role := msg.Role
	if role == roleAssistant {
		role = roleModel
	}

	var parts []geminiPart
	for _, part := range msg.Parts {
		gPart, err := p.convertPartToGemini(ctx, part)
		if err != nil {
			return geminiContent{}, err
		}
		parts = append(parts, gPart)
	}

	return geminiContent{
		Role:  role,
		Parts: parts,
	}, nil
}

// convertPartToGemini converts a ContentPart to Gemini's format
func (p *Provider) convertPartToGemini(ctx context.Context, part types.ContentPart) (geminiPart, error) {
	switch part.Type {
	case types.ContentTypeText:
		if part.Text == nil || *part.Text == "" {
			return geminiPart{}, fmt.Errorf("text part has empty text")
		}
		return geminiPart{Text: *part.Text}, nil

	case types.ContentTypeImage, types.ContentTypeAudio, types.ContentTypeVideo, types.ContentTypeDocument:
		return p.convertMediaPartToGemini(ctx, part)

	default:
		return geminiPart{}, fmt.Errorf("unsupported part type: %s", part.Type)
	}
}

// convertMediaPartToGemini converts image/audio/video/document parts to Gemini format
func (p *Provider) convertMediaPartToGemini(ctx context.Context, part types.ContentPart) (geminiPart, error) {
	if part.Media == nil {
		return geminiPart{}, fmt.Errorf("%s part missing media field", part.Type)
	}

	// Get MIME type
	mimeType := part.Media.MIMEType
	if mimeType == "" {
		return geminiPart{}, fmt.Errorf("%s part missing mime_type", part.Type)
	}

	// Use the provider's store-aware MediaLoader to get base64 data from any
	// source (Data, FilePath, URL, StorageReference).
	base64Data, err := p.MediaLoader().GetBase64Data(ctx, part.Media)
	if err != nil {
		return geminiPart{}, fmt.Errorf("failed to load %s data: %w", part.Type, err)
	}

	// Create Gemini inline data part
	return geminiPart{
		InlineData: &geminiInlineData{
			MimeType: mimeType,
			Data:     base64Data,
		},
	}, nil
}
