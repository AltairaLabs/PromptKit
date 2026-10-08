package ollama

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// GetMultimodalCapabilities returns Ollama's multimodal capabilities
func (p *Provider) GetMultimodalCapabilities() providers.MultimodalCapabilities {
	// Ollama supports vision models like LLaVA and Llama 3.2 Vision
	// These support images but not audio/video
	return providers.MultimodalCapabilities{
		SupportsImages: true,
		SupportsAudio:  false,
		SupportsVideo:  false,
		ImageFormats: []string{
			types.MIMETypeImageJPEG,
			types.MIMETypeImagePNG,
			types.MIMETypeImageGIF,
			types.MIMETypeImageWebP,
		},
		AudioFormats:   []string{},
		VideoFormats:   []string{},
		MaxImageSizeMB: 0, // No specific limit documented
		MaxAudioSizeMB: 0,
		MaxVideoSizeMB: 0,
	}
}

// convertMessageToOllama converts a single PromptKit message to Ollama format
func (p *Provider) convertMessageToOllama(
	ctx context.Context, msg *types.Message,
) (ollamaMessage, error) {
	// Handle legacy text-only messages
	if !msg.IsMultimodal() {
		return ollamaMessage{
			Role:    msg.Role,
			Content: msg.GetContent(),
		}, nil
	}

	// Handle multimodal messages - Ollama uses OpenAI-compatible structure
	// Content becomes an array of content parts
	var contentParts []any

	for _, part := range msg.Parts {
		switch part.Type {
		case types.ContentTypeText:
			if part.Text != nil && *part.Text != "" {
				contentParts = append(contentParts, map[string]any{
					"type": "text",
					"text": *part.Text,
				})
			}

		case types.ContentTypeImage:
			if part.Media == nil {
				return ollamaMessage{}, fmt.Errorf("image part missing media content")
			}

			imagePart, err := p.convertImagePartToOllama(ctx, part)
			if err != nil {
				return ollamaMessage{}, err
			}
			contentParts = append(contentParts, imagePart)

		case types.ContentTypeAudio, types.ContentTypeVideo:
			return ollamaMessage{}, fmt.Errorf("audio and video content not supported by Ollama")

		default:
			return ollamaMessage{}, fmt.Errorf("unknown content type: %s", part.Type)
		}
	}

	return ollamaMessage{
		Role:    msg.Role,
		Content: contentParts,
	}, nil
}

// convertImagePartToOllama converts an image ContentPart to Ollama's format
func (p *Provider) convertImagePartToOllama(
	ctx context.Context, part types.ContentPart,
) (map[string]any, error) {
	if part.Media == nil {
		return nil, fmt.Errorf("image part missing media content")
	}

	imagePart := map[string]any{
		"type": "image_url",
	}

	imageURL := make(map[string]any)

	// Hand Ollama a storage reference's URL when the store gives one, else
	// inline base64 data. A message's own URL is always fetched here and
	// inlined, never passed on: an Ollama server usually sits inside the host's
	// network, so letting it fetch a caller-chosen URL would reach whatever
	// the host can reach. The loader refuses non-public addresses.
	loader := p.MediaLoader()
	if url, ok, err := resolveStorageURL(ctx, loader, part.Media); err != nil {
		return nil, fmt.Errorf("failed to resolve image: %w", err)
	} else if ok {
		imageURL["url"] = url
	} else {
		data, err := loader.GetBase64Data(ctx, part.Media)
		if err != nil {
			return nil, fmt.Errorf("failed to load image data: %w", err)
		}
		imageURL["url"] = fmt.Sprintf("data:%s;base64,%s", part.Media.MIMEType, data)
	}

	// Add detail level if specified
	if part.Media.Detail != nil {
		imageURL["detail"] = *part.Media.Detail
	}

	imagePart["image_url"] = imageURL

	return imagePart, nil
}

// resolveStorageURL resolves only a storage reference to a URL. A message's own
// URL is reported as not resolvable, so the caller fetches and inlines it.
func resolveStorageURL(
	ctx context.Context, loader *providers.MediaLoader, media *types.MediaContent,
) (string, bool, error) {
	if media.URL != nil && *media.URL != "" {
		return "", false, nil
	}
	return loader.ResolveURL(ctx, media)
}
