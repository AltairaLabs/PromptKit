package gemini

import (
	"context"
	"fmt"

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

// GetMultimodalCapabilities returns Gemini's multimodal support capabilities.
// When the provider config declares its capabilities, the declaration is
// authoritative for which modalities are enabled; otherwise Gemini's built-in
// defaults apply (images + audio + video + documents).
func (p *Provider) GetMultimodalCapabilities() providers.MultimodalCapabilities {
	caps := providers.MultimodalCapabilities{
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
		AudioFormats: []string{
			types.MIMETypeAudioWAV,
			"audio/mp3",
			"audio/aiff",
			"audio/aac",
			"audio/ogg",
			"audio/flac",
		},
		VideoFormats: []string{
			types.MIMETypeVideoMP4,
			"video/mpeg",
			"video/mov",
			"video/avi",
			"video/flv",
			"video/mpg",
			"video/webm",
			"video/wmv",
			"video/3gpp",
		},
		DocumentFormats: []string{
			types.MIMETypePDF,
		},
		MaxImageSizeMB:    20, //nolint:mnd // Gemini's documented media size limit
		MaxAudioSizeMB:    20, //nolint:mnd // Gemini's documented media size limit
		MaxVideoSizeMB:    20, //nolint:mnd // Gemini's documented media size limit
		MaxDocumentSizeMB: 20, //nolint:mnd // Gemini's documented media size limit
	}
	if p.capabilities != nil {
		caps.SupportsImages = p.capabilities[providers.CapabilityVision]
		caps.SupportsAudio = p.capabilities[providers.CapabilityAudio]
		caps.SupportsVideo = p.capabilities[providers.CapabilityVideo]
		caps.SupportsDocuments = p.capabilities[providers.CapabilityDocuments]
	}
	return caps
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
