package ollama

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
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

// convertMessagesToOllama converts PromptKit messages to Ollama format
// Handles both legacy text-only and new multimodal messages
func (p *Provider) convertMessagesToOllama(req providers.PredictionRequest) ([]ollamaMessage, error) {
	messages := make([]ollamaMessage, 0, len(req.Messages)+1)

	// Add system message if present
	if req.System != "" {
		messages = append(messages, ollamaMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	// Convert each message
	for i := range req.Messages {
		converted, err := p.convertMessageToOllama(&req.Messages[i])
		if err != nil {
			return nil, err
		}
		messages = append(messages, converted)
	}

	return messages, nil
}

// convertMessageToOllama converts a single PromptKit message to Ollama format
func (p *Provider) convertMessageToOllama(msg *types.Message) (ollamaMessage, error) {
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

			imagePart, err := p.convertImagePartToOllama(part)
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
func (p *Provider) convertImagePartToOllama(part types.ContentPart) (map[string]any, error) {
	if part.Media == nil {
		return nil, fmt.Errorf("image part missing media content")
	}

	imagePart := map[string]any{
		"type": "image_url",
	}

	imageURL := make(map[string]any)

	// Handle different data sources
	if part.Media.URL != nil && *part.Media.URL != "" {
		// External URL - use directly
		imageURL["url"] = *part.Media.URL
	} else {
		// Use MediaLoader for all other sources (Data, FilePath, StorageReference)
		loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})
		data, err := loader.GetBase64Data(context.Background(), part.Media)
		if err != nil {
			return nil, fmt.Errorf("failed to load image data: %w", err)
		}
		dataURL := fmt.Sprintf("data:%s;base64,%s", part.Media.MIMEType, data)
		imageURL["url"] = dataURL
	}

	// Add detail level if specified
	if part.Media.Detail != nil {
		imageURL["detail"] = *part.Media.Detail
	}

	imagePart["image_url"] = imageURL

	return imagePart, nil
}
