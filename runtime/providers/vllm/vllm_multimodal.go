// Package vllm provides multimodal support for vLLM provider
package vllm

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const (
	// vLLMMaxImageSizeMB is the maximum image size supported by vLLM
	vLLMMaxImageSizeMB = 20
)

// Wire values used by the image content builder.
const (
	partTypeText     = "text"
	partTypeImageURL = "image_url"
	keyImageURL      = "image_url"
)

// GetMultimodalCapabilities returns the multimodal capabilities of the vLLM provider
func (p *Provider) GetMultimodalCapabilities() providers.MultimodalCapabilities {
	return providers.MultimodalCapabilities{
		SupportsImages: true,
		SupportsAudio:  false,
		SupportsVideo:  false,
		ImageFormats:   []string{"image/jpeg", "image/png", "image/gif", "image/webp"},
		MaxImageSizeMB: vLLMMaxImageSizeMB,
	}
}

// hasImagePart reports whether msg must be sent as a content-part array.
// Tool results stay string content: their text comes from ToolResult, which
// GetContent reads and the part builder does not.
func hasImagePart(msg *types.Message) bool {
	if msg.Role == roleTool {
		return false
	}
	for i := range msg.Parts {
		if msg.Parts[i].Type == types.ContentTypeImage {
			return true
		}
	}
	return false
}

// buildMultimodalContent creates the multimodal content array for a message with images
// vLLM uses OpenAI-compatible format: array of content parts with type and text/image_url
func (p *Provider) buildMultimodalContent(
	ctx context.Context, loader *providers.MediaLoader, msg types.Message,
) ([]map[string]any, error) {
	content := make([]map[string]any, 0, len(msg.Parts))

	for _, part := range msg.Parts {
		switch part.Type {
		case types.ContentTypeText:
			if part.Text != nil && *part.Text != "" {
				content = append(content, map[string]any{
					keyType: partTypeText,
					"text":  *part.Text,
				})
			}

		case types.ContentTypeImage:
			imageURLPart, err := p.buildImageURLPart(ctx, loader, part.Media)
			if err != nil {
				return nil, err
			}
			content = append(content, imageURLPart)

		default:
			// vLLM doesn't support audio/video yet, skip unsupported types
			continue
		}
	}

	return content, nil
}

// buildImageURLPart creates the image_url content part for an image's media.
func (p *Provider) buildImageURLPart(
	ctx context.Context, loader *providers.MediaLoader, media *types.MediaContent,
) (map[string]any, error) {
	if media == nil {
		return nil, fmt.Errorf("image part has no media content")
	}
	imageURL, err := p.convertMediaToURL(ctx, loader, media)
	if err != nil {
		return nil, fmt.Errorf("failed to convert image: %w", err)
	}

	urlObj := map[string]any{"url": imageURL}
	if media.Detail != nil {
		urlObj["detail"] = *media.Detail
	}
	return map[string]any{
		keyType:     partTypeImageURL,
		keyImageURL: urlObj,
	}, nil
}

// convertMediaToURL converts media content to a URL vLLM accepts: an http(s)
// URL when the media has one (directly or via the storage service), otherwise
// a data: URI built from inline, file or stored bytes.
func (p *Provider) convertMediaToURL(
	ctx context.Context, loader *providers.MediaLoader, media *types.MediaContent,
) (string, error) {
	if url, ok, err := loader.ResolveURL(ctx, media); err != nil {
		return "", err
	} else if ok {
		return url, nil
	}

	data, err := loader.GetBase64Data(ctx, media)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("data:%s;base64,%s", media.MIMEType, data), nil
}
