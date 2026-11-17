package claude

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// GetMultimodalCapabilities returns Claude's multimodal support capabilities.
// When the provider config declares its capabilities, the declaration is
// authoritative for which modalities are enabled; otherwise Claude's built-in
// defaults apply (images + documents, no audio/video).
func (p *Provider) GetMultimodalCapabilities() providers.MultimodalCapabilities {
	caps := p.SendableMultimodalCapabilities()
	return providers.ApplyDeclaredModalities(caps, p.capabilities)
}

// SendableMultimodalCapabilities returns what the adapter sends whatever the
// config declares: images (convertImagePartToClaude) and PDF documents
// (convertDocumentPartToClaude). Audio and video parts fail conversion.
func (p *Provider) SendableMultimodalCapabilities() providers.MultimodalCapabilities {
	return providers.MultimodalCapabilities{
		SupportsImages:    true,
		SupportsAudio:     false,
		SupportsVideo:     false,
		SupportsDocuments: true,
		ImageFormats: []string{
			types.MIMETypeImageJPEG,
			types.MIMETypeImagePNG,
			types.MIMETypeImageGIF,
			types.MIMETypeImageWebP,
		},
		AudioFormats: []string{},
		VideoFormats: []string{},
		DocumentFormats: []string{
			types.MIMETypePDF,
		},
		MaxImageSizeMB:    5,  //nolint:mnd // Claude's documented image size limit
		MaxAudioSizeMB:    0,  // Not supported
		MaxVideoSizeMB:    0,  // Not supported
		MaxDocumentSizeMB: 32, //nolint:mnd // Claude's documented PDF size limit
	}
}

// claudeImageSource represents an image or document source in Claude's format
type claudeImageSource struct {
	Type string `json:"type"` // "base64" or "url"
	// MediaType is required for base64 sources but MUST be omitted for url
	// sources — Anthropic rejects "media_type" on a url source with 400
	// "Extra inputs are not permitted".
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

// claudeContentBlockMultimodal extends claudeContentBlock with multimodal support
type claudeContentBlockMultimodal struct {
	Type         string              `json:"type"` // "text", "image", "document"
	Text         string              `json:"text,omitempty"`
	Source       *claudeImageSource  `json:"source,omitempty"`
	CacheControl *claudeCacheControl `json:"cache_control,omitempty"`
}

// convertPartsToClaudeBlocks converts one message's content parts to Claude
// content blocks. The parts share one MediaLoader, so the message's media is
// held to providers.MaxMediaItems and providers.MaxAggregateMediaSize.
func (p *Provider) convertPartsToClaudeBlocks(ctx context.Context, parts []types.ContentPart) ([]interface{}, error) {
	var contentBlocks []interface{}
	loader := p.MediaLoader()

	for _, part := range parts {
		switch part.Type {
		case types.ContentTypeText:
			if part.Text != nil && *part.Text != "" {
				contentBlocks = append(contentBlocks, claudeContentBlockMultimodal{
					Type: contentTypeText,
					Text: *part.Text,
				})
			}

		case types.ContentTypeImage:
			block, err := p.convertImagePartToClaude(ctx, loader, part)
			if err != nil {
				return nil, err
			}
			contentBlocks = append(contentBlocks, block)

		case types.ContentTypeDocument:
			block, err := p.convertDocumentPartToClaude(ctx, loader, part)
			if err != nil {
				return nil, err
			}
			contentBlocks = append(contentBlocks, block)

		case types.ContentTypeAudio:
			return nil, fmt.Errorf("claude does not support audio content")

		case types.ContentTypeVideo:
			return nil, fmt.Errorf("claude does not support video content")

		default:
			return nil, fmt.Errorf("unsupported content type: %s", part.Type)
		}
	}

	return contentBlocks, nil
}

// claudeBlockTypeImage is the Claude content-block type for image sources.
const claudeBlockTypeImage = "image"

// convertImagePartToClaude converts an image part to Claude's format
func (p *Provider) convertImagePartToClaude(
	ctx context.Context, loader *providers.MediaLoader, part types.ContentPart,
) (claudeContentBlockMultimodal, error) {
	if part.Media == nil {
		return claudeContentBlockMultimodal{}, fmt.Errorf("image part missing media data")
	}

	block := claudeContentBlockMultimodal{
		Type:   claudeBlockTypeImage,
		Source: &claudeImageSource{},
	}

	// Prefer a model-fetchable URL (explicit URL or presigned StorageReference);
	// fall back to inlined base64 bytes when no URL is available. MediaType is
	// set only on the base64 branch — Anthropic rejects it on a url source.
	if url, ok, err := loader.ResolveURL(ctx, part.Media); err != nil {
		return claudeContentBlockMultimodal{}, fmt.Errorf("failed to resolve image: %w", err)
	} else if ok {
		block.Source.Type = "url"
		block.Source.URL = url
	} else {
		data, err := loader.GetBase64Data(ctx, part.Media)
		if err != nil {
			return claudeContentBlockMultimodal{}, fmt.Errorf("failed to load image data: %w", err)
		}
		block.Source.Type = sourceTypeBase64
		block.Source.MediaType = part.Media.MIMEType
		block.Source.Data = data
	}

	return block, nil
}

// convertDocumentPartToClaude converts a document part to Claude's format
func (p *Provider) convertDocumentPartToClaude(
	ctx context.Context, loader *providers.MediaLoader, part types.ContentPart,
) (claudeContentBlockMultimodal, error) {
	if part.Media == nil {
		return claudeContentBlockMultimodal{}, fmt.Errorf("document part missing media data")
	}

	// Validate that Claude supports this document type
	if part.Media.MIMEType != types.MIMETypePDF {
		return claudeContentBlockMultimodal{}, fmt.Errorf("claude only supports PDF documents, got: %s", part.Media.MIMEType)
	}

	block := claudeContentBlockMultimodal{
		Type: "document",
		Source: &claudeImageSource{
			MediaType: part.Media.MIMEType,
		},
	}

	// Claude documents only support base64 encoding (no URL support).
	// The store-aware loader resolves Data, FilePath, and StorageReference.
	data, err := loader.GetBase64Data(ctx, part.Media)
	if err != nil {
		return claudeContentBlockMultimodal{}, fmt.Errorf("failed to load document data: %w", err)
	}
	block.Source.Type = sourceTypeBase64
	block.Source.Data = data

	return block, nil
}
