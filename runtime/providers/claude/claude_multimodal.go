package claude

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

// GetMultimodalCapabilities returns Claude's multimodal support capabilities
func (p *ClaudeProvider) GetMultimodalCapabilities() providers.MultimodalCapabilities {
	// Claude supports images and PDFs, but not audio or video
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
		MaxImageSizeMB: 5, // 5MB per image
		MaxAudioSizeMB: 0, // Not supported
		MaxVideoSizeMB: 0, // Not supported
	}
}

// claudeImageSource represents an image in Claude's format
type claudeImageSource struct {
	Type      string `json:"type"`       // "base64" or "url"
	MediaType string `json:"media_type"` // MIME type
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

// claudeContentBlockMultimodal extends claudeContentBlock with image support
type claudeContentBlockMultimodal struct {
	Type         string              `json:"type"` // "text", "image", "document"
	Text         string              `json:"text,omitempty"`
	Source       *claudeImageSource  `json:"source,omitempty"`
	CacheControl *claudeCacheControl `json:"cache_control,omitempty"`
}

// PredictMultimodal sends a multimodal predict request to Claude
func (p *ClaudeProvider) PredictMultimodal(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	// Validate multimodal messages
	if err := providers.ValidateMultimodalRequest(p, req); err != nil {
		return providers.PredictionResponse{}, err
	}

	// Convert to Claude format with multimodal support
	messages, system, err := p.convertMessagesToClaudeMultimodal(req.Messages)
	if err != nil {
		return providers.PredictionResponse{}, fmt.Errorf("failed to convert messages: %w", err)
	}

	return p.predictWithContentsMultimodal(ctx, messages, system, req.Temperature, req.TopP, req.MaxTokens, req.Seed)
}

// PredictMultimodalStream sends a streaming multimodal predict request to Claude
func (p *ClaudeProvider) PredictMultimodalStream(ctx context.Context, req providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	// Validate multimodal messages
	if err := providers.ValidateMultimodalRequest(p, req); err != nil {
		return nil, err
	}

	// Convert to Claude format with multimodal support
	messages, system, err := p.convertMessagesToClaudeMultimodal(req.Messages)
	if err != nil {
		return nil, fmt.Errorf("failed to convert messages: %w", err)
	}

	return p.predictStreamWithContentsMultimodal(ctx, messages, system, req.Temperature, req.TopP, req.MaxTokens, req.Seed)
}

// convertMessagesToClaudeMultimodal converts PromptKit messages to Claude's multimodal format
func (p *ClaudeProvider) convertMessagesToClaudeMultimodal(messages []types.Message) ([]claudeMessage, []claudeContentBlockMultimodal, error) {
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

		// Convert multimodal message
		claudeMsg, err := p.convertMessageToClaudeMultimodal(*msg)
		if err != nil {
			return nil, nil, err
		}

		claudeMessages = append(claudeMessages, claudeMsg)
	}

	return claudeMessages, systemBlocks, nil
}

// convertMessageToClaudeMultimodal converts a single PromptKit message to Claude format
func (p *ClaudeProvider) convertMessageToClaudeMultimodal(msg types.Message) (claudeMessage, error) {
	var contentBlocks []interface{}

	// Handle legacy string content
	if msg.Content != "" && len(msg.Parts) == 0 {
		contentBlocks = append(contentBlocks, claudeContentBlockMultimodal{
			Type: "text",
			Text: msg.Content,
		})
	} else {
		// Handle multimodal parts
		blocks, err := p.convertPartsToClaudeBlocks(msg.Parts)
		if err != nil {
			return claudeMessage{}, err
		}
		contentBlocks = blocks
	}

	// Marshal and unmarshal to handle mixed type array
	return p.buildClaudeMessage(msg.Role, contentBlocks)
}

// convertPartsToClaudeBlocks converts content parts to Claude content blocks
func (p *ClaudeProvider) convertPartsToClaudeBlocks(parts []types.ContentPart) ([]interface{}, error) {
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
			block, err := p.convertImagePartToClaude(part)
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
func (p *ClaudeProvider) buildClaudeMessage(role string, contentBlocks []interface{}) (claudeMessage, error) {
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
func (p *ClaudeProvider) convertImagePartToClaude(part types.ContentPart) (claudeContentBlockMultimodal, error) {
	if part.Media == nil {
		return claudeContentBlockMultimodal{}, fmt.Errorf("image part missing media data")
	}

	block := claudeContentBlockMultimodal{
		Type: "image",
		Source: &claudeImageSource{
			MediaType: part.Media.MIMEType,
		},
	}

	// Determine which data source is set
	if part.Media.URL != nil && *part.Media.URL != "" {
		// External URL - use directly
		block.Source.Type = "url"
		block.Source.URL = *part.Media.URL
	} else {
		// Use MediaLoader for all other sources (Data, FilePath, StorageReference)
		loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})
		data, err := loader.GetBase64Data(context.Background(), part.Media)
		if err != nil {
			return claudeContentBlockMultimodal{}, fmt.Errorf("failed to load image data: %w", err)
		}
		block.Source.Type = "base64"
		block.Source.Data = data
	}

	return block, nil
}

// predictWithContentsMultimodal handles the actual API call with multimodal content
func (p *ClaudeProvider) predictWithContentsMultimodal(ctx context.Context, messages []claudeMessage, system []claudeContentBlockMultimodal, temperature, topP float32, maxTokens int, seed *int) (providers.PredictionResponse, error) {
	start := time.Now()

	// Apply provider defaults for zero values
	if temperature == 0 {
		temperature = p.defaults.Temperature
	}
	if topP == 0 {
		topP = p.defaults.TopP
	}
	if maxTokens == 0 {
		maxTokens = p.defaults.MaxTokens
	}

	// Build request
	claudeReq := map[string]interface{}{
		"model":       p.model,
		"max_tokens":  maxTokens,
		"messages":    messages,
		"temperature": temperature,
	}

	if topP > 0 && topP < 1 {
		claudeReq["top_p"] = topP
	}

	if len(system) > 0 {
		claudeReq["system"] = system
	}

	// Note: Claude doesn't support seed parameter

	reqBody, err := json.Marshal(claudeReq)
	if err != nil {
		return providers.PredictionResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Prepare response
	predictResp := providers.PredictionResponse{
		Latency: time.Since(start),
	}
	if p.ShouldIncludeRawOutput() {
		predictResp.RawRequest = claudeReq
	}

	// Build URL
	url := fmt.Sprintf("%s/messages", p.baseURL)

	// Debug log the request
	headers := map[string]string{
		contentTypeHeader: applicationJSON,
	}
	logger.APIRequest("Claude", "POST", url, headers, claudeReq)

	// Make HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		predictResp.Latency = time.Since(start)
		return predictResp, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		logger.APIResponse("Claude", 0, "", err)
		predictResp.Latency = time.Since(start)
		return predictResp, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.APIResponse("Claude", resp.StatusCode, "", err)
		predictResp.Latency = time.Since(start)
		return predictResp, fmt.Errorf("failed to read response: %w", err)
	}

	// Debug log the response
	logger.APIResponse("Claude", resp.StatusCode, string(respBody), nil)

	if resp.StatusCode != http.StatusOK {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return predictResp, fmt.Errorf("claude api error (status %d): %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	claudeResp, err := parseClaudeResponse(respBody)
	if err != nil {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return predictResp, err
	}

	// Extract content
	if len(claudeResp.Content) == 0 {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return predictResp, fmt.Errorf("no content in claude response")
	}

	// Calculate latency and cost
	latency := time.Since(start)
	costBreakdown := p.CalculateCost(
		claudeResp.Usage.InputTokens,
		claudeResp.Usage.OutputTokens,
		claudeResp.Usage.CacheReadInputTokens,
	)

	predictResp.Content = claudeResp.Content[0].Text
	predictResp.CostInfo = &costBreakdown
	predictResp.Latency = latency

	if p.ShouldIncludeRawOutput() {
		predictResp.Raw = respBody
	}

	return predictResp, nil
}

// parseClaudeResponse parses and validates a Claude API response
func parseClaudeResponse(respBody []byte) (*claudeResponse, error) {
	var claudeResp claudeResponse
	if err := json.Unmarshal(respBody, &claudeResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Check for API errors
	if claudeResp.Error != nil {
		return nil, fmt.Errorf("claude api error: %s (%s)", claudeResp.Error.Message, claudeResp.Error.Type)
	}

	// Check for content
	if len(claudeResp.Content) == 0 {
		return nil, fmt.Errorf("no content in response")
	}

	return &claudeResp, nil
}

// predictStreamWithContentsMultimodal handles streaming API calls with multimodal content
func (p *ClaudeProvider) predictStreamWithContentsMultimodal(ctx context.Context, messages []claudeMessage, system []claudeContentBlockMultimodal, temperature, topP float32, maxTokens int, seed *int) (<-chan providers.StreamChunk, error) {
	// Apply provider defaults
	if temperature == 0 {
		temperature = p.defaults.Temperature
	}
	if topP == 0 {
		topP = p.defaults.TopP
	}
	if maxTokens == 0 {
		maxTokens = p.defaults.MaxTokens
	}

	// Build request
	claudeReq := map[string]interface{}{
		"model":       p.model,
		"max_tokens":  maxTokens,
		"messages":    messages,
		"temperature": temperature,
		"stream":      true,
	}

	if topP > 0 && topP < 1 {
		claudeReq["top_p"] = topP
	}

	if len(system) > 0 {
		claudeReq["system"] = system
	}

	reqBody, err := json.Marshal(claudeReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Build URL
	url := fmt.Sprintf("%s/messages", p.baseURL)

	// Make HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("claude api error (status %d): %s", resp.StatusCode, string(body))
	}

	outChan := make(chan providers.StreamChunk)

	go p.streamResponseMultimodal(ctx, resp.Body, outChan)

	return outChan, nil
}

// streamResponseMultimodal processes the streaming response
func (p *ClaudeProvider) streamResponseMultimodal(ctx context.Context, body io.ReadCloser, outChan chan<- providers.StreamChunk) {
	// Don't defer close here since streamResponse already closes the channel
	defer body.Close()

	// Use the existing streaming logic from claude.go
	// This is the same as the base provider's streaming
	p.streamResponse(ctx, body, outChan)
}
