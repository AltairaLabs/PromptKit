package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
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
	for i := range req.Messages {
		if err := providers.ValidateMultimodalMessage(p, req.Messages[i]); err != nil {
			return providers.ChatResponse{}, err
		}
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
	for i := range req.Messages {
		if err := providers.ValidateMultimodalMessage(p, req.Messages[i]); err != nil {
			return nil, err
		}
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

// extractContentString extracts text content from OpenAI's response content
// which can be either a string or an array of content parts
func extractContentString(content interface{}) string {
	if str, ok := content.(string); ok {
		return str
	}

	if parts, ok := content.([]interface{}); ok {
		return extractTextFromParts(parts)
	}

	return ""
}

// extractTextFromParts extracts text from an array of content parts
func extractTextFromParts(parts []interface{}) string {
	var text string
	for _, part := range parts {
		if textVal := getTextFromPart(part); textVal != "" {
			text += textVal
		}
	}
	return text
}

// getTextFromPart extracts text from a single content part
func getTextFromPart(part interface{}) string {
	partMap, ok := part.(map[string]interface{})
	if !ok {
		return ""
	}

	partType, ok := partMap["type"].(string)
	if !ok || partType != "text" {
		return ""
	}

	textVal, ok := partMap["text"].(string)
	if !ok {
		return ""
	}

	return textVal
}

// chatWithMessages is a refactored version of Chat that accepts pre-converted messages
func (p *OpenAIProvider) chatWithMessages(ctx context.Context, req providers.ChatRequest, messages []openAIMessage) (providers.ChatResponse, error) {
	start := time.Now()

	// Apply provider defaults for zero values
	temperature := req.Temperature
	if temperature == 0 {
		temperature = p.defaults.Temperature
	}

	topP := req.TopP
	if topP == 0 {
		topP = p.defaults.TopP
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = p.defaults.MaxTokens
	}

	// Create request
	openAIReq := openAIRequest{
		Model:       p.model,
		Messages:    messages,
		Temperature: temperature,
		TopP:        topP,
		MaxTokens:   maxTokens,
		Seed:        req.Seed,
	}

	reqBody, err := json.Marshal(openAIReq)
	if err != nil {
		return providers.ChatResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Prepare response with raw request if configured (set early to preserve on error)
	chatResp := providers.ChatResponse{
		Latency: time.Since(start), // Will be updated at the end
	}
	if p.ShouldIncludeRawOutput() {
		chatResp.RawRequest = openAIReq
	}

	// Make HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+openAIChatCompletionsPath, bytes.NewReader(reqBody))
	if err != nil {
		return chatResp, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	httpReq.Header.Set(authorizationHeader, bearerPrefix+p.apiKey)

	client := &http.Client{Timeout: 30 * time.Second}

	logger.APIRequest("OpenAI", "POST", p.baseURL+openAIChatCompletionsPath, map[string]string{
		contentTypeHeader:   applicationJSON,
		authorizationHeader: bearerPrefix + p.apiKey,
	}, openAIReq)

	resp, err := client.Do(httpReq)
	if err != nil {
		chatResp.Latency = time.Since(start)
		return chatResp, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		chatResp.Latency = time.Since(start)
		return chatResp, fmt.Errorf("failed to read response body: %w", err)
	}

	logger.APIResponse("OpenAI", resp.StatusCode, string(respBody), nil)

	if resp.StatusCode != http.StatusOK {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBody
		return chatResp, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var openAIResp openAIResponse
	if err := json.Unmarshal(respBody, &openAIResp); err != nil {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBody
		return chatResp, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if openAIResp.Error != nil {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBody
		return chatResp, fmt.Errorf("OpenAI API error: %s", openAIResp.Error.Message)
	}

	if len(openAIResp.Choices) == 0 {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBody
		return chatResp, fmt.Errorf("no choices in response")
	}

	latency := time.Since(start)

	cachedTokens := 0
	if openAIResp.Usage.PromptTokensDetails != nil {
		cachedTokens = openAIResp.Usage.PromptTokensDetails.CachedTokens
	}

	// Calculate cost breakdown
	costBreakdown := p.CalculateCost(openAIResp.Usage.PromptTokens, openAIResp.Usage.CompletionTokens, cachedTokens)

	// Extract content - can be string or array of content parts
	content := extractContentString(openAIResp.Choices[0].Message.Content)

	chatResp.Content = content
	chatResp.CostInfo = &costBreakdown
	chatResp.Latency = latency
	chatResp.Raw = respBody

	return chatResp, nil
}

// chatStreamWithMessages is a refactored version of ChatStream that accepts pre-converted messages
func (p *OpenAIProvider) chatStreamWithMessages(ctx context.Context, req providers.ChatRequest, messages []openAIMessage) (<-chan providers.StreamChunk, error) {
	// Apply provider defaults for zero values
	temperature := req.Temperature
	if temperature == 0 {
		temperature = p.defaults.Temperature
	}

	topP := req.TopP
	if topP == 0 {
		topP = p.defaults.TopP
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = p.defaults.MaxTokens
	}

	// Create streaming request
	openAIReq := map[string]interface{}{
		"model":       p.model,
		"messages":    messages,
		"temperature": temperature,
		"top_p":       topP,
		"max_tokens":  maxTokens,
		"stream":      true,
		"stream_options": map[string]interface{}{
			"include_usage": true,
		},
	}
	if req.Seed != nil {
		openAIReq["seed"] = *req.Seed
	}

	reqBody, err := json.Marshal(openAIReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+openAIChatCompletionsPath, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	httpReq.Header.Set(authorizationHeader, bearerPrefix+p.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	outChan := make(chan providers.StreamChunk)

	go p.streamResponse(ctx, resp.Body, outChan)

	return outChan, nil
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
