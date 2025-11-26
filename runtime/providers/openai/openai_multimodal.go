package openai

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// GetMultimodalCapabilities returns OpenAI's multimodal capabilities
func (p *Provider) GetMultimodalCapabilities() providers.MultimodalCapabilities {
	// OpenAI Vision API supports images
	// Audio/video are not directly supported in the predict API (use Whisper separately)
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

// PredictMultimodal performs a predict request with multimodal content
func (p *Provider) PredictMultimodal(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	// Validate that messages are compatible with OpenAI's capabilities
	if err := providers.ValidateMultimodalRequest(p, req); err != nil {
		return providers.PredictionResponse{}, err
	}

	// Convert messages to OpenAI format (handles both legacy and multimodal)
	messages, err := p.convertMessagesToOpenAI(req)
	if err != nil {
		return providers.PredictionResponse{}, fmt.Errorf("failed to convert messages: %w", err)
	}

	// Use the existing Predict implementation but with converted messages
	return p.predictWithMessages(ctx, req, messages)
}

// PredictMultimodalStream performs a streaming predict request with multimodal content
func (p *Provider) PredictMultimodalStream(ctx context.Context, req providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	// Validate that messages are compatible with OpenAI's capabilities
	if err := providers.ValidateMultimodalRequest(p, req); err != nil {
		return nil, err
	}

	// Convert messages to OpenAI format (handles both legacy and multimodal)
	messages, err := p.convertMessagesToOpenAI(req)
	if err != nil {
		return nil, fmt.Errorf("failed to convert messages: %w", err)
	}

	// Use the existing PredictStream implementation but with converted messages
	return p.predictStreamWithMessages(ctx, req, messages)
}

// convertMessagesToOpenAI converts PromptKit messages to OpenAI format
// Handles both legacy text-only and new multimodal messages
func (p *Provider) convertMessagesToOpenAI(req providers.PredictionRequest) ([]openAIMessage, error) {
	messages := make([]openAIMessage, 0, len(req.Messages)+1)

	// Add system message if present
	if req.System != "" {
		messages = append(messages, openAIMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	// Convert each message
	for i := range req.Messages {
		converted, err := p.convertMessageToOpenAI(req.Messages[i])
		if err != nil {
			return nil, err
		}
		messages = append(messages, converted)
	}

	return messages, nil
}

// convertMessageToOpenAI converts a single PromptKit message to OpenAI format
func (p *Provider) convertMessageToOpenAI(msg types.Message) (openAIMessage, error) {
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
			return openAIMessage{}, fmt.Errorf("audio and video content not supported by OpenAI predict API")

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
func (p *Provider) convertImagePartToOpenAI(part types.ContentPart) (map[string]interface{}, error) {
	if part.Media == nil {
		return nil, fmt.Errorf("image part missing media content")
	}

	imagePart := map[string]interface{}{
		"type": "image_url",
	}

	imageURL := make(map[string]interface{})

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

// PredictMultimodalWithTools implements providers.MultimodalToolSupport interface for ToolProvider
// This allows combining multimodal content (images) with tool calls in a single request
func (p *ToolProvider) PredictMultimodalWithTools(ctx context.Context, req providers.PredictionRequest, tools interface{}, toolChoice string) (providers.PredictionResponse, []types.MessageToolCall, error) {
	// Validate that all messages are compatible with OpenAI's capabilities
	for i := range req.Messages {
		if err := providers.ValidateMultimodalMessage(p, req.Messages[i]); err != nil {
			return providers.PredictionResponse{}, nil, err
		}
	}

	// Use the existing PredictWithTools which now handles multimodal via updated buildToolRequest
	return p.PredictWithTools(ctx, req, tools, toolChoice)
}
