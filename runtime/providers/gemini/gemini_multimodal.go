package gemini

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

// GetMultimodalCapabilities returns Gemini's multimodal support capabilities
func (p *GeminiProvider) GetMultimodalCapabilities() providers.MultimodalCapabilities {
	// Gemini supports images, audio, and video
	return providers.MultimodalCapabilities{
		SupportsImages: true,
		SupportsAudio:  true,
		SupportsVideo:  true,
		ImageFormats: []string{
			types.MIMETypeImageJPEG,
			types.MIMETypeImagePNG,
			types.MIMETypeImageWebP,
			types.MIMETypeImageGIF,
			"image/heic",
			"image/heif",
		},
		AudioFormats: []string{
			"audio/wav",
			"audio/mp3",
			"audio/aiff",
			"audio/aac",
			"audio/ogg",
			"audio/flac",
		},
		VideoFormats: []string{
			"video/mp4",
			"video/mpeg",
			"video/mov",
			"video/avi",
			"video/flv",
			"video/mpg",
			"video/webm",
			"video/wmv",
			"video/3gpp",
		},
		MaxImageSizeMB: 20,
		MaxAudioSizeMB: 20,
		MaxVideoSizeMB: 20,
	}
}

// PredictMultimodal performs a predict request with multimodal content
func (p *GeminiProvider) PredictMultimodal(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	// Validate that messages are compatible with Gemini's capabilities
	if err := providers.ValidateMultimodalRequest(p, req); err != nil {
		return providers.PredictionResponse{}, err
	}

	// Convert messages to Gemini format (handles both legacy and multimodal)
	contents, systemInstruction, err := convertMessagesToGemini(req.Messages, req.System)
	if err != nil {
		return providers.PredictionResponse{}, fmt.Errorf("failed to convert messages: %w", err)
	}

	// Use the common predict implementation
	return p.predictWithContents(ctx, contents, systemInstruction, req.Temperature, req.TopP, req.MaxTokens, req.Seed)
}

// PredictMultimodalStream performs a streaming predict request with multimodal content
func (p *GeminiProvider) PredictMultimodalStream(ctx context.Context, req providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	// Validate that messages are compatible with Gemini's capabilities
	if err := providers.ValidateMultimodalRequest(p, req); err != nil {
		return nil, err
	}

	// Convert messages to Gemini format (handles both legacy and multimodal)
	contents, systemInstruction, err := convertMessagesToGemini(req.Messages, req.System)
	if err != nil {
		return nil, fmt.Errorf("failed to convert messages: %w", err)
	}

	// Use the common streaming implementation
	return p.predictStreamWithContents(ctx, contents, systemInstruction, req.Temperature, req.TopP, req.MaxTokens, req.Seed)
}

// convertMessagesToGemini converts PromptKit messages to Gemini format
// Handles both legacy text-only and new multimodal messages
func convertMessagesToGemini(messages []types.Message, systemPrompt string) ([]geminiContent, *geminiContent, error) {
	var contents []geminiContent
	var systemInstruction *geminiContent

	// Handle system message
	if systemPrompt != "" {
		systemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: systemPrompt}},
		}
	}

	// Convert each message
	for _, msg := range messages {
		content, err := convertMessageToGemini(msg)
		if err != nil {
			return nil, nil, err
		}
		contents = append(contents, content)
	}

	return contents, systemInstruction, nil
}

// convertMessageToGemini converts a single PromptKit message to Gemini format
func convertMessageToGemini(msg types.Message) (geminiContent, error) {
	// Handle legacy text-only messages
	if !msg.IsMultimodal() {
		role := msg.Role
		// Gemini uses "user" and "model" roles
		if role == "assistant" {
			role = "model"
		}
		return geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: msg.GetContent()}},
		}, nil
	}

	// Handle multimodal messages with parts
	role := msg.Role
	if role == "assistant" {
		role = "model"
	}

	var parts []geminiPart
	for _, part := range msg.Parts {
		gPart, err := convertPartToGemini(part)
		if err != nil {
			return geminiContent{}, err
		}
		parts = append(parts, gPart)
	}

	return geminiContent{
		Role:  role,
		Parts: parts,
	}, nil
}

// convertPartToGemini converts a ContentPart to Gemini's format
func convertPartToGemini(part types.ContentPart) (geminiPart, error) {
	switch part.Type {
	case types.ContentTypeText:
		if part.Text == nil || *part.Text == "" {
			return geminiPart{}, fmt.Errorf("text part has empty text")
		}
		return geminiPart{Text: *part.Text}, nil

	case types.ContentTypeImage, types.ContentTypeAudio, types.ContentTypeVideo:
		return convertMediaPartToGemini(part)

	default:
		return geminiPart{}, fmt.Errorf("unsupported part type: %s", part.Type)
	}
}

// convertMediaPartToGemini converts image/audio/video parts to Gemini format
func convertMediaPartToGemini(part types.ContentPart) (geminiPart, error) {
	if part.Media == nil {
		return geminiPart{}, fmt.Errorf("%s part missing media field", part.Type)
	}

	// Get MIME type
	mimeType := part.Media.MIMEType
	if mimeType == "" {
		return geminiPart{}, fmt.Errorf("%s part missing mime_type", part.Type)
	}

	// Use MediaLoader to get base64 data from any source (Data, FilePath, URL, StorageReference)
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})
	base64Data, err := loader.GetBase64Data(context.Background(), part.Media)
	if err != nil {
		return geminiPart{}, fmt.Errorf("failed to load %s data: %w", part.Type, err)
	}

	// Create Gemini inline data part
	return geminiPart{
		InlineData: &geminiInlineData{
			MimeType: mimeType,
			Data:     base64Data,
		},
	}, nil
}

// predictWithContents is a helper method for both regular and multimodal predict
// It's similar to Predict() but accepts pre-converted contents
func (p *GeminiProvider) predictWithContents(ctx context.Context, contents []geminiContent, systemInstruction *geminiContent, temperature, topP float32, maxTokens int, seed *int) (providers.PredictionResponse, error) {
	start := time.Now()

	// Apply provider defaults for zero values
	if temperature == 0 {
		temperature = p.Defaults.Temperature
	}

	if topP == 0 {
		topP = p.Defaults.TopP
	}

	if maxTokens == 0 {
		maxTokens = p.Defaults.MaxTokens
	}

	// Create request
	geminiReq := p.buildGeminiRequest(contents, systemInstruction, temperature, topP, maxTokens)

	// Note: Gemini doesn't support seed parameter like OpenAI does

	reqBody, err := json.Marshal(geminiReq)
	if err != nil {
		return providers.PredictionResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Prepare response with raw request if configured
	predictResp := providers.PredictionResponse{
		Latency: time.Since(start),
	}
	if p.ShouldIncludeRawOutput() {
		predictResp.RawRequest = geminiReq
	}

	// Build URL with API key
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", p.BaseURL, p.Model, p.ApiKey)

	// Debug log the request
	headers := map[string]string{
		contentTypeHeader: applicationJSON,
	}
	logger.APIRequest("Gemini", "POST", url, headers, geminiReq)

	// Make HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		predictResp.Latency = time.Since(start)
		return predictResp, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)

	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		logger.APIResponse("Gemini", 0, "", err)
		predictResp.Latency = time.Since(start)
		return predictResp, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.APIResponse("Gemini", resp.StatusCode, "", err)
		predictResp.Latency = time.Since(start)
		return predictResp, fmt.Errorf("failed to read response: %w", err)
	}

	// Debug log the response
	logger.APIResponse("Gemini", resp.StatusCode, string(respBody), nil)

	if resp.StatusCode != http.StatusOK {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return predictResp, fmt.Errorf("gemini api error (status %d): %s", resp.StatusCode, string(respBody))
	}

	// Parse and validate response
	geminiResp, err := p.parseGeminiResponse(respBody)
	if err != nil {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return predictResp, err
	}

	// Extract token counts
	var tokensIn, tokensOut, cachedTokens int
	if geminiResp.UsageMetadata != nil {
		tokensIn = geminiResp.UsageMetadata.PromptTokenCount
		tokensOut = geminiResp.UsageMetadata.CandidatesTokenCount
		cachedTokens = geminiResp.UsageMetadata.CachedContentTokenCount
	}

	latency := time.Since(start)

	// Calculate cost breakdown
	costBreakdown := p.CalculateCost(tokensIn, tokensOut, cachedTokens)

	candidate := geminiResp.Candidates[0]
	predictResp.Content = candidate.Content.Parts[0].Text
	predictResp.CostInfo = &costBreakdown
	predictResp.Latency = latency
	predictResp.Raw = respBody

	return predictResp, nil
}

// parseGeminiResponse parses and validates a Gemini API response
func (p *GeminiProvider) parseGeminiResponse(respBody []byte) (*geminiResponse, error) {
	var geminiResp geminiResponse
	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Check for prompt feedback errors
	if geminiResp.PromptFeedback != nil && geminiResp.PromptFeedback.BlockReason != "" {
		return nil, fmt.Errorf("prompt blocked: %s", geminiResp.PromptFeedback.BlockReason)
	}

	// Check for candidates
	if len(geminiResp.Candidates) == 0 {
		return nil, fmt.Errorf("no candidates in response")
	}

	candidate := geminiResp.Candidates[0]

	// Check for content parts
	if len(candidate.Content.Parts) == 0 {
		// Handle different finish reasons
		switch candidate.FinishReason {
		case "MAX_TOKENS":
			return nil, fmt.Errorf("max tokens limit reached")
		case "SAFETY":
			return nil, fmt.Errorf("response blocked by safety filters")
		case "RECITATION":
			return nil, fmt.Errorf("response blocked due to recitation concerns")
		default:
			return nil, fmt.Errorf("no content parts in response (finish reason: %s)", candidate.FinishReason)
		}
	}

	return &geminiResp, nil
}

// predictStreamWithContents is a helper method for both regular and multimodal streaming
func (p *GeminiProvider) predictStreamWithContents(ctx context.Context, contents []geminiContent, systemInstruction *geminiContent, temperature, topP float32, maxTokens int, seed *int) (<-chan providers.StreamChunk, error) {
	// Apply provider defaults for zero values
	if temperature == 0 {
		temperature = p.Defaults.Temperature
	}

	if topP == 0 {
		topP = p.Defaults.TopP
	}

	if maxTokens == 0 {
		maxTokens = p.Defaults.MaxTokens
	}

	// Create streaming request
	geminiReq := p.buildGeminiRequest(contents, systemInstruction, temperature, topP, maxTokens)

	// Note: Gemini doesn't support seed parameter

	reqBody, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Build URL for streaming
	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse&key=%s", p.BaseURL, p.Model, p.ApiKey)

	// Make HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)

	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini api error (status %d): %s", resp.StatusCode, string(body))
	}

	outChan := make(chan providers.StreamChunk)

	go p.streamResponse(ctx, resp.Body, outChan)

	return outChan, nil
}
