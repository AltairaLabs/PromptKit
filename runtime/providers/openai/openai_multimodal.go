package openai

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// GetMultimodalCapabilities returns OpenAI's multimodal capabilities
func (p *OpenAIProvider) GetMultimodalCapabilities() providers.MultimodalCapabilities {
	// OpenAI Vision API supports images
	// Audio/video are not directly supported in the chat API (use Whisper separately)
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
		MaxImageSizeMB: 20, // OpenAI limit
		MaxAudioSizeMB: 0,
		MaxVideoSizeMB: 0,
	}
}

// ChatMultimodal performs a chat request with multimodal content
func (p *OpenAIProvider) ChatMultimodal(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	// Validate that messages are compatible with OpenAI's capabilities
	if err := providers.ValidateMultimodalRequest(p, req); err != nil {
		return providers.ChatResponse{}, err
	}

	// Convert messages to OpenAI format (handles both legacy and multimodal)
	messages, err := p.convertMessagesToOpenAI(req)
	if err != nil {
		return providers.ChatResponse{}, fmt.Errorf("failed to convert messages: %w", err)
	}

	// Use the existing Chat implementation but with converted messages
	return p.chatWithMessages(ctx, req, messages)
}

// ChatMultimodalStream performs a streaming chat request with multimodal content
func (p *OpenAIProvider) ChatMultimodalStream(ctx context.Context, req providers.ChatRequest) (<-chan providers.StreamChunk, error) {
	// Validate that messages are compatible with OpenAI's capabilities
	if err := providers.ValidateMultimodalRequest(p, req); err != nil {
		return nil, err
	}

	// Convert messages to OpenAI format (handles both legacy and multimodal)
	messages, err := p.convertMessagesToOpenAI(req)
	if err != nil {
		return nil, fmt.Errorf("failed to convert messages: %w", err)
	}

	// Use the existing ChatStream implementation but with converted messages
	return p.chatStreamWithMessages(ctx, req, messages)
}

// convertMessagesToOpenAI converts PromptKit messages to OpenAI format
// Handles both legacy text-only and new multimodal messages
func (p *OpenAIProvider) convertMessagesToOpenAI(req providers.ChatRequest) ([]openAIMessage, error) {
	messages := make([]openAIMessage, 0, len(req.Messages)+1)

	// Add system message if present
	if req.System != "" {
		messages = append(messages, openAIMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	// Convert each message
	for _, msg := range req.Messages {
		converted, err := p.convertMessageToOpenAI(msg)
		if err != nil {
			return nil, err
		}
		messages = append(messages, converted)
	}

	return messages, nil
}

// convertMessageToOpenAI converts a single PromptKit message to OpenAI format
func (p *OpenAIProvider) convertMessageToOpenAI(msg types.Message) (openAIMessage, error) {
	// Handle legacy text-only messages
	if !msg.IsMultimodal() {
		return openAIMessage{
			Role:    msg.Role,
			Content: msg.GetContent(),
		}, nil
	}

	// Handle multimodal messages - OpenAI uses a different structure
	// Content becomes an array of content parts
	var contentParts []interface{}

	for _, part := range msg.Parts {
		switch part.Type {
		case types.ContentTypeText:
			if part.Text != nil && *part.Text != "" {
				contentParts = append(contentParts, map[string]interface{}{
					"type": "text",
					"text": *part.Text,
				})
			}

		case types.ContentTypeImage:
			if part.Media == nil {
				return openAIMessage{}, fmt.Errorf("image part missing media content")
			}

			imagePart, err := p.convertImagePartToOpenAI(part)
			if err != nil {
				return openAIMessage{}, err
			}
			contentParts = append(contentParts, imagePart)

		case types.ContentTypeAudio, types.ContentTypeVideo:
			return openAIMessage{}, fmt.Errorf("audio and video content not supported by OpenAI chat API")

		default:
			return openAIMessage{}, fmt.Errorf("unknown content type: %s", part.Type)
		}
	}

	return openAIMessage{
		Role:    msg.Role,
		Content: contentParts,
	}, nil
}

// convertImagePartToOpenAI converts an image ContentPart to OpenAI's format
func (p *OpenAIProvider) convertImagePartToOpenAI(part types.ContentPart) (map[string]interface{}, error) {
	if part.Media == nil {
		return nil, fmt.Errorf("image part missing media content")
	}

	imagePart := map[string]interface{}{
		"type": "image_url",
	}

	imageURL := make(map[string]interface{})

	// Handle different data sources
	if part.Media.URL != nil && *part.Media.URL != "" {
		// External URL
		imageURL["url"] = *part.Media.URL
	} else if part.Media.Data != nil && *part.Media.Data != "" {
		// Base64-encoded data - format as data URL
		dataURL := fmt.Sprintf("data:%s;base64,%s", part.Media.MIMEType, *part.Media.Data)
		imageURL["url"] = dataURL
	} else if part.Media.FilePath != nil && *part.Media.FilePath != "" {
		// File path - need to read and encode
		data, err := part.Media.GetBase64Data()
		if err != nil {
			return nil, fmt.Errorf("failed to read image file: %w", err)
		}
		dataURL := fmt.Sprintf("data:%s;base64,%s", part.Media.MIMEType, data)
		imageURL["url"] = dataURL
	} else {
		return nil, fmt.Errorf("image part has no data source (url, data, or file_path)")
	}

	// Add detail level if specified
	if part.Media.Detail != nil {
		imageURL["detail"] = *part.Media.Detail
	}

	imagePart["image_url"] = imageURL

	return imagePart, nil
}

// ChatMultimodalWithTools implements providers.MultimodalToolSupport interface for OpenAIToolProvider
// This allows combining multimodal content (images) with tool calls in a single request
func (p *OpenAIToolProvider) ChatMultimodalWithTools(ctx context.Context, req providers.ChatRequest, tools interface{}, toolChoice string) (providers.ChatResponse, []types.MessageToolCall, error) {
	// Validate that all messages are compatible with OpenAI's capabilities
	for i := range req.Messages {
		if err := providers.ValidateMultimodalMessage(p, req.Messages[i]); err != nil {
			return providers.ChatResponse{}, nil, err
		}
	}

	// Use the existing ChatWithTools which now handles multimodal via updated buildToolRequest
	return p.ChatWithTools(ctx, req, tools, toolChoice)
}
