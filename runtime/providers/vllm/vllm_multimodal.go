// Package vllm provides multimodal support for vLLM provider
package vllm

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	// vLLMMaxImageSizeMB is the maximum image size supported by vLLM
	vLLMMaxImageSizeMB = 20
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

// PredictMultimodal sends a multimodal prediction request to vLLM
// vLLM supports vision models via OpenAI-compatible API with image_url format
//
//nolint:gocritic // req size matches MultimodalProvider interface
func (p *Provider) PredictMultimodal(
	ctx context.Context,
	req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	// Convert messages to vLLM format with image support
	messages, err := p.prepareMultimodalMessages(req)
	if err != nil {
		return providers.PredictionResponse{}, fmt.Errorf("failed to prepare multimodal messages: %w", err)
	}

	// Delegate to the common prediction implementation
	return p.predictWithMessages(ctx, req, messages)
}

// PredictMultimodalStream sends a streaming multimodal prediction request to vLLM
//
//nolint:gocritic // req size matches MultimodalProvider interface
func (p *Provider) PredictMultimodalStream(
	ctx context.Context,
	req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	// Convert messages to vLLM format with image support
	messages, err := p.prepareMultimodalMessages(req)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare multimodal messages: %w", err)
	}

	// Delegate to the common streaming implementation
	return p.predictStreamWithMessages(ctx, req, messages)
}

// prepareMultimodalMessages converts prediction request messages to vLLM format with image support
//
//nolint:gocritic // req size matches internal pattern
func (p *Provider) prepareMultimodalMessages(req providers.PredictionRequest) ([]vllmMessage, error) {
	messages := make([]vllmMessage, 0, len(req.Messages)+1)

	// Add system message if present
	if req.System != "" {
		messages = append(messages, vllmMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	// Convert each message with potential multimodal content
	for i := range req.Messages {
		msg := req.Messages[i]

		// If message has multimodal parts with images, use multimodal format
		if msg.IsMultimodal() && msg.HasMediaContent() {
			content, err := p.buildMultimodalContent(msg)
			if err != nil {
				return nil, fmt.Errorf("failed to build multimodal content for message %d: %w", i, err)
			}
			messages = append(messages, vllmMessage{
				Role:    msg.Role,
				Content: content,
			})
		} else {
			// Regular text-only message
			messages = append(messages, vllmMessage{
				Role:    msg.Role,
				Content: msg.GetContent(),
			})
		}
	}

	return messages, nil
}

// buildMultimodalContent creates the multimodal content array for a message with images
// vLLM uses OpenAI-compatible format: array of content parts with type and text/image_url
//
//nolint:gocognit,gocritic // complexity from multimodal content part handling; msg contains media data
func (p *Provider) buildMultimodalContent(msg types.Message) ([]map[string]any, error) { // NOSONAR
	content := make([]map[string]any, 0)

	// Process each content part
	for _, part := range msg.Parts {
		switch part.Type {
		case types.ContentTypeText:
			if part.Text != nil && *part.Text != "" {
				content = append(content, map[string]any{
					"type": "text",
					"text": *part.Text,
				})
			}

		case types.ContentTypeImage:
			if part.Media == nil {
				return nil, fmt.Errorf("image part has no media content")
			}
			imageURL, err := p.convertMediaToURL(part.Media)
			if err != nil {
				return nil, fmt.Errorf("failed to convert image: %w", err)
			}

			imageURLPart := map[string]any{
				"type": "image_url",
				"image_url": map[string]any{
					"url": imageURL,
				},
			}

			// Add detail level if specified
			if part.Media.Detail != nil {
				imageURLPart["image_url"].(map[string]any)["detail"] = *part.Media.Detail
			}

			content = append(content, imageURLPart)

		default:
			// vLLM doesn't support audio/video yet, skip unsupported types
			continue
		}
	}

	return content, nil
}

// convertMediaToURL converts media content to a format vLLM can accept
// vLLM accepts:
// - data:image/... base64 URLs
// - http/https URLs
func (p *Provider) convertMediaToURL(media *types.MediaContent) (string, error) {
	// If URL is provided, use it directly
	if media.URL != nil {
		return *media.URL, nil
	}

	// If base64 data is provided, create a data URL
	if media.Data != nil {
		return fmt.Sprintf("data:%s;base64,%s", media.MIMEType, *media.Data), nil
	}

	// If file path is provided, we need to load and encode it
	if media.FilePath != nil {
		// For now, vLLM requires the file to be already loaded as base64
		// The pipeline should have already loaded this via media processors
		return "", fmt.Errorf("file path provided but data not loaded - ensure media is preprocessed")
	}

	return "", fmt.Errorf("no valid image source (URL, Data, or FilePath)")
}
