package claude

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// GetMultimodalCapabilities returns Claude's multimodal support capabilities.
// When the provider config declares its capabilities, the declaration is
// authoritative for which modalities are enabled; otherwise Claude's built-in
// defaults apply (images + documents, no audio/video).
func (p *Provider) GetMultimodalCapabilities() providers.MultimodalCapabilities {
	caps := providers.MultimodalCapabilities{
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
	if p.capabilities != nil {
		caps.SupportsImages = p.capabilities[providers.CapabilityVision]
		caps.SupportsAudio = p.capabilities[providers.CapabilityAudio]
		caps.SupportsVideo = p.capabilities[providers.CapabilityVideo]
		caps.SupportsDocuments = p.capabilities[providers.CapabilityDocuments]
	}
	return caps
}

// claudeImageSource represents an image or document source in Claude's format
type claudeImageSource struct {
	Type      string `json:"type"`       // "base64" or "url"
	MediaType string `json:"media_type"` // MIME type
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

// convertMessagesToClaudeMultimodal converts PromptKit messages to Claude's multimodal format
func (p *Provider) convertMessagesToClaudeMultimodal(messages []types.Message) ([]claudeMessage, []claudeContentBlockMultimodal, error) {
	claudeMessages := make([]claudeMessage, 0)
	var systemBlocks []claudeContentBlockMultimodal

	for i := range messages {
		msg := &messages[i]
		// Handle system messages separately
		if msg.Role == "system" {
			systemBlocks = append(systemBlocks, claudeContentBlockMultimodal{
				Type: "text",
				Text: msg.Content,
			})
			continue
		}

		// Convert multimodal message. This helper has no production caller (the
		// live path is convertMessagesToClaudeFormat); background context suffices.
		claudeMsg, err := p.convertMessageToClaudeMultimodal(context.Background(), *msg)
		if err != nil {
			return nil, nil, err
		}

		claudeMessages = append(claudeMessages, claudeMsg)
	}

	return claudeMessages, systemBlocks, nil
}

// convertMessageToClaudeMultimodal converts a single PromptKit message to Claude format
func (p *Provider) convertMessageToClaudeMultimodal(ctx context.Context, msg types.Message) (claudeMessage, error) {
	var contentBlocks []interface{}

	// Handle legacy string content
	if msg.Content != "" && len(msg.Parts) == 0 {
		contentBlocks = append(contentBlocks, claudeContentBlockMultimodal{
			Type: "text",
			Text: msg.Content,
		})
	} else {
		// Handle multimodal parts
		blocks, err := p.convertPartsToClaudeBlocks(ctx, msg.Parts)
		if err != nil {
			return claudeMessage{}, err
		}
		contentBlocks = blocks
	}

	// Marshal and unmarshal to handle mixed type array
	return p.buildClaudeMessage(msg.Role, contentBlocks)
}

// convertPartsToClaudeBlocks converts content parts to Claude content blocks
func (p *Provider) convertPartsToClaudeBlocks(ctx context.Context, parts []types.ContentPart) ([]interface{}, error) {
	var contentBlocks []interface{}

	for _, part := range parts {
		switch part.Type {
		case types.ContentTypeText:
			if part.Text != nil && *part.Text != "" {
				contentBlocks = append(contentBlocks, claudeContentBlockMultimodal{
					Type: "text",
					Text: *part.Text,
				})
			}

		case types.ContentTypeImage:
			block, err := p.convertImagePartToClaude(ctx, part)
			if err != nil {
				return nil, err
			}
			contentBlocks = append(contentBlocks, block)

		case types.ContentTypeDocument:
			block, err := p.convertDocumentPartToClaude(ctx, part)
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

// buildClaudeMessage creates a claudeMessage from role and content blocks
func (p *Provider) buildClaudeMessage(role string, contentBlocks []interface{}) (claudeMessage, error) {
	// Marshal to JSON to handle the mixed type array
	contentJSON, err := json.Marshal(contentBlocks)
	if err != nil {
		return claudeMessage{}, fmt.Errorf("failed to marshal content blocks: %w", err)
	}

	// Create the message
	claudeMsg := claudeMessage{
		Role: role,
	}

	// Unmarshal into the Content field
	if err := json.Unmarshal(contentJSON, &claudeMsg.Content); err != nil {
		return claudeMessage{}, fmt.Errorf("failed to unmarshal content blocks: %w", err)
	}

	return claudeMsg, nil
}

// convertImagePartToClaude converts an image part to Claude's format
func (p *Provider) convertImagePartToClaude(
	ctx context.Context, part types.ContentPart,
) (claudeContentBlockMultimodal, error) {
	if part.Media == nil {
		return claudeContentBlockMultimodal{}, fmt.Errorf("image part missing media data")
	}

	block := claudeContentBlockMultimodal{
		Type: "image",
		Source: &claudeImageSource{
			MediaType: part.Media.MIMEType,
		},
	}

	// Prefer a model-fetchable URL (explicit URL or presigned StorageReference);
	// fall back to inlined base64 bytes when no URL is available.
	loader := p.MediaLoader()
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
		block.Source.Type = "base64"
		block.Source.Data = data
	}

	return block, nil
}

// convertDocumentPartToClaude converts a document part to Claude's format
func (p *Provider) convertDocumentPartToClaude(
	ctx context.Context, part types.ContentPart,
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
	data, err := p.MediaLoader().GetBase64Data(ctx, part.Media)
	if err != nil {
		return claudeContentBlockMultimodal{}, fmt.Errorf("failed to load document data: %w", err)
	}
	block.Source.Type = "base64"
	block.Source.Data = data

	return block, nil
}
