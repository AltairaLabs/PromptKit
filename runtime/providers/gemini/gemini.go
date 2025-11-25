package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// HTTP constants
const (
	contentTypeHeader = "Content-Type"
	applicationJSON   = "application/json"
)

// GeminiProvider implements the Provider interface for Google Gemini
type GeminiProvider struct {
	providers.BaseProvider
	Model    string
	BaseURL  string
	ApiKey   string
	Defaults providers.ProviderDefaults
}

// NewGeminiProvider creates a new Gemini provider
func NewGeminiProvider(id, model, baseURL string, defaults providers.ProviderDefaults, includeRawOutput bool) *GeminiProvider {
	base, apiKey := providers.NewBaseProviderWithAPIKey(id, includeRawOutput, "GEMINI_API_KEY", "GOOGLE_API_KEY")

	return &GeminiProvider{
		BaseProvider: base,
		Model:        model,
		BaseURL:      baseURL,
		ApiKey:       apiKey,
		Defaults:     defaults,
	}
}

// Gemini API request/response structures
type geminiRequest struct {
	Contents          []geminiContent `json:"contents"`
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
	GenerationConfig  geminiGenConfig `json:"generationConfig"`
	SafetySettings    []geminiSafety  `json:"safetySettings,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text       string            `json:"text,omitempty"`
	InlineData *geminiInlineData `json:"inlineData,omitempty"`
}

type geminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"` // base64 encoded
}

type geminiGenConfig struct {
	Temperature     float32 `json:"temperature"`
	TopP            float32 `json:"topP"`
	MaxOutputTokens int     `json:"maxOutputTokens"`
}

type geminiSafety struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

type geminiResponse struct {
	Candidates     []geminiCandidate     `json:"candidates"`
	UsageMetadata  *geminiUsage          `json:"usageMetadata,omitempty"`
	PromptFeedback *geminiPromptFeedback `json:"promptFeedback,omitempty"`
}

type geminiCandidate struct {
	Content       geminiContent        `json:"content"`
	FinishReason  string               `json:"finishReason"`
	Index         int                  `json:"index"`
	SafetyRatings []geminiSafetyRating `json:"safetyRatings,omitempty"`
}

type geminiUsage struct {
	PromptTokenCount        int `json:"promptTokenCount"`
	CandidatesTokenCount    int `json:"candidatesTokenCount"`
	TotalTokenCount         int `json:"totalTokenCount"`
	CachedContentTokenCount int `json:"cachedContentTokenCount,omitempty"`
}

type geminiPromptFeedback struct {
	SafetyRatings []geminiSafetyRating `json:"safetyRatings,omitempty"`
	BlockReason   string               `json:"blockReason,omitempty"`
}

type geminiSafetyRating struct {
	Category    string `json:"category"`
	Probability string `json:"probability"`
}

// convertMessagesToGeminiContents converts provider messages to Gemini format
func convertMessagesToGeminiContents(messages []types.Message) []geminiContent {
	contents := make([]geminiContent, 0, len(messages))
	for _, msg := range messages {
		role := msg.Role
		// Gemini uses "user" and "model" roles
		if role == roleAssistant {
			role = roleModel
		}

		contents = append(contents, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: msg.Content}},
		})
	}
	return contents
}

// prepareGeminiRequest converts a predict request to Gemini format with defaults applied
func (p *GeminiProvider) prepareGeminiRequest(req providers.PredictionRequest) ([]geminiContent, *geminiContent, float32, float32, int) {
	// Handle system message
	var systemInstruction *geminiContent
	if req.System != "" {
		systemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: req.System}},
		}
	}

	// Convert conversation messages
	contents := convertMessagesToGeminiContents(req.Messages)

	// Apply provider defaults for zero values
	temperature := req.Temperature
	if temperature == 0 {
		temperature = p.Defaults.Temperature
	}

	topP := req.TopP
	if topP == 0 {
		topP = p.Defaults.TopP
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = p.Defaults.MaxTokens
	}

	return contents, systemInstruction, temperature, topP, maxTokens
}

// buildGeminiRequest creates a Gemini API request with standard safety settings
func (p *GeminiProvider) buildGeminiRequest(contents []geminiContent, systemInstruction *geminiContent, temperature, topP float32, maxTokens int) geminiRequest {
	return geminiRequest{
		Contents:          contents,
		SystemInstruction: systemInstruction,
		GenerationConfig: geminiGenConfig{
			Temperature:     temperature,
			TopP:            topP,
			MaxOutputTokens: maxTokens,
		},
		SafetySettings: []geminiSafety{
			{Category: "HARM_CATEGORY_HARASSMENT", Threshold: "BLOCK_NONE"},
			{Category: "HARM_CATEGORY_HATE_SPEECH", Threshold: "BLOCK_NONE"},
			{Category: "HARM_CATEGORY_SEXUALLY_EXPLICIT", Threshold: "BLOCK_NONE"},
			{Category: "HARM_CATEGORY_DANGEROUS_CONTENT", Threshold: "BLOCK_NONE"},
		},
	}
}

// handleGeminiFinishReason processes error finish reasons from Gemini responses
func (p *GeminiProvider) handleGeminiFinishReason(finishReason string, predictResp providers.PredictionResponse, respBody []byte, start time.Time) (providers.PredictionResponse, error) {
	predictResp.Latency = time.Since(start)
	predictResp.Raw = respBody

	switch finishReason {
	case finishReasonMaxTokens:
		return predictResp, fmt.Errorf("gemini returned MAX_TOKENS error (this should not happen with reasonable limits)")
	case finishReasonSafety:
		return predictResp, fmt.Errorf("response blocked by Gemini safety filters")
	case finishReasonRecitation:
		return predictResp, fmt.Errorf("response blocked due to recitation concerns")
	default:
		return predictResp, fmt.Errorf("no content parts in response (finish reason: %s)", finishReason)
	}
}

// handleNoCandidatesError creates an appropriate error when no candidates are returned
func (p *GeminiProvider) handleNoCandidatesError(geminiResp geminiResponse, predictResp providers.PredictionResponse, respBody []byte, start time.Time) (providers.PredictionResponse, error) {
	predictResp.Latency = time.Since(start)
	predictResp.Raw = respBody

	// Check if prompt was blocked
	if geminiResp.PromptFeedback != nil && geminiResp.PromptFeedback.BlockReason != "" {
		errorMsg := fmt.Sprintf("no candidates in response - prompt blocked: %s", geminiResp.PromptFeedback.BlockReason)
		if len(geminiResp.PromptFeedback.SafetyRatings) > 0 {
			errorMsg += " (safety ratings:"
			for _, rating := range geminiResp.PromptFeedback.SafetyRatings {
				errorMsg += fmt.Sprintf(" %s=%s", rating.Category, rating.Probability)
			}
			errorMsg += ")"
		}
		errorMsg += " - consider adjusting safety settings or prompt content"
		return predictResp, errors.New(errorMsg)
	}

	// No candidates but also no explicit block reason
	errorMsg := "no candidates in response (prompt consumed tokens but generated no output)"
	if geminiResp.UsageMetadata != nil {
		errorMsg += fmt.Sprintf(" - used %d prompt tokens", geminiResp.UsageMetadata.PromptTokenCount)
	}
	errorMsg += " - this may indicate the model refused to generate content, try rephrasing the prompt or checking system instructions"
	return predictResp, errors.New(errorMsg)
}

// makeGeminiHTTPRequest sends the HTTP request to Gemini API
func (p *GeminiProvider) makeGeminiHTTPRequest(ctx context.Context, geminiReq geminiRequest, predictResp providers.PredictionResponse, start time.Time) ([]byte, providers.PredictionResponse, error) {
	reqBody, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, predictResp, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Build URL with API key
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", p.BaseURL, p.Model, p.ApiKey)

	// Debug log the request
	headers := map[string]string{
		contentTypeHeader: applicationJSON,
	}
	logger.APIRequest("Gemini", "POST", url, headers, geminiReq)

	// Make HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		predictResp.Latency = time.Since(start)
		return nil, predictResp, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)

	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		logger.APIResponse("Gemini", 0, "", err)
		predictResp.Latency = time.Since(start)
		return nil, predictResp, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.APIResponse("Gemini", resp.StatusCode, "", err)
		predictResp.Latency = time.Since(start)
		return nil, predictResp, fmt.Errorf("failed to read response: %w", err)
	}

	// Debug log the response
	logger.APIResponse("Gemini", resp.StatusCode, string(respBody), nil)

	if resp.StatusCode != http.StatusOK {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return nil, predictResp, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, predictResp, nil
}

// parseAndValidateGeminiResponse parses and validates the Gemini API response
func (p *GeminiProvider) parseAndValidateGeminiResponse(respBody []byte, predictResp providers.PredictionResponse, start time.Time) (geminiResponse, geminiCandidate, providers.PredictionResponse, error) {
	var geminiResp geminiResponse
	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return geminiResp, geminiCandidate{}, predictResp, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 {
		resp, err := p.handleNoCandidatesError(geminiResp, predictResp, respBody, start)
		return geminiResp, geminiCandidate{}, resp, err
	}

	candidate := geminiResp.Candidates[0]

	// Check for safety blocking
	if candidate.FinishReason == "SAFETY" {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return geminiResp, candidate, predictResp, fmt.Errorf("response blocked by safety filters")
	}

	if len(candidate.Content.Parts) == 0 {
		resp, err := p.handleGeminiFinishReason(candidate.FinishReason, predictResp, respBody, start)
		return geminiResp, candidate, resp, err
	}

	return geminiResp, candidate, predictResp, nil
}

// Predict sends a predict request to Gemini
func (p *GeminiProvider) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	start := time.Now()

	// Convert messages to Gemini format and apply defaults
	contents, systemInstruction, temperature, topP, maxTokens := p.prepareGeminiRequest(req)

	// Create request
	geminiReq := p.buildGeminiRequest(contents, systemInstruction, temperature, topP, maxTokens)

	// Prepare response with raw request if configured (set early to preserve on error)
	predictResp := providers.PredictionResponse{
		Latency: time.Since(start), // Will be updated at the end
	}
	if p.ShouldIncludeRawOutput() {
		predictResp.RawRequest = geminiReq
	}

	// Make HTTP request
	respBody, predictResp, err := p.makeGeminiHTTPRequest(ctx, geminiReq, predictResp, start)
	if err != nil {
		return predictResp, err
	}

	// Parse and validate response
	geminiResp, candidate, predictResp, err := p.parseAndValidateGeminiResponse(respBody, predictResp, start)
	if err != nil {
		return predictResp, err
	}

	// Extract token counts
	var tokensIn, tokensOut int
	if geminiResp.UsageMetadata != nil {
		tokensIn = geminiResp.UsageMetadata.PromptTokenCount
		tokensOut = geminiResp.UsageMetadata.CandidatesTokenCount
	}

	latency := time.Since(start)

	// Calculate cost breakdown (Gemini doesn't support cached tokens yet)
	costBreakdown := p.CalculateCost(tokensIn, tokensOut, 0)

	// Extract all content parts (text + inline media + markdown images)
	var contentParts []types.ContentPart
	var textContent strings.Builder

	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			// Check if text contains markdown images and split accordingly
			textParts := processTextPartForImages(part.Text)
			contentParts = append(contentParts, textParts...)
			textContent.WriteString(part.Text)
		}

		if part.InlineData != nil {
			// Add inline media part (image, audio, video)
			mediaType := inferMediaTypeFromMIME(part.InlineData.MimeType)
			var mediaPart types.ContentPart

			switch mediaType {
			case types.ContentTypeImage:
				mediaPart = types.NewImagePartFromData(part.InlineData.Data, part.InlineData.MimeType, nil)
			case types.ContentTypeAudio:
				mediaPart = types.NewAudioPartFromData(part.InlineData.Data, part.InlineData.MimeType)
			case types.ContentTypeVideo:
				mediaPart = types.NewVideoPartFromData(part.InlineData.Data, part.InlineData.MimeType)
			default:
				// Unknown media type, skip
				continue
			}

			contentParts = append(contentParts, mediaPart)
		}
	}

	predictResp.Content = textContent.String()
	predictResp.Parts = contentParts
	predictResp.CostInfo = &costBreakdown
	predictResp.Latency = latency
	predictResp.Raw = respBody

	// Debug log the extracted parts
	logger.Debug("Extracted content parts from Gemini response",
		"total_parts", len(contentParts),
		"text_parts", countPartsByType(contentParts, types.ContentTypeText),
		"image_parts", countPartsByType(contentParts, types.ContentTypeImage),
		"audio_parts", countPartsByType(contentParts, types.ContentTypeAudio),
		"video_parts", countPartsByType(contentParts, types.ContentTypeVideo))

	return predictResp, nil
}

// countPartsByType counts how many parts match a given type
func countPartsByType(parts []types.ContentPart, partType string) int {
	count := 0
	for _, part := range parts {
		if part.Type == partType {
			count++
		}
	}
	return count
}

// getGeminiPricing returns pricing for Gemini models (input, output, cached per 1K tokens)
func getGeminiPricing(model string) (float64, float64, float64) {
	// Define pricing constants
	const (
		proInput     = 0.00125
		proOutput    = 0.005
		proCached    = 0.000625
		flashInput   = 0.000075
		flashOutput  = 0.0003
		flashCached  = 0.0000375
		geminiInput  = 0.0005
		geminiOutput = 0.0015
		geminiCached = 0.00025
	)

	switch model {
	case "gemini-1.5-pro", "gemini-2.5-pro":
		return proInput, proOutput, proCached
	case "gemini-1.5-flash", "gemini-2.5-flash":
		return flashInput, flashOutput, flashCached
	case "gemini-pro":
		return geminiInput, geminiOutput, geminiCached
	default:
		// Default to Gemini 1.5 Pro pricing for unknown models
		return proInput, proOutput, proCached
	}
}

// CalculateCost calculates detailed cost breakdown including optional cached tokens
func (p *GeminiProvider) CalculateCost(tokensIn, tokensOut, cachedTokens int) types.CostInfo {
	var inputCostPer1K, outputCostPer1K, cachedCostPer1K float64

	// Use configured pricing if available
	if p.Defaults.Pricing.InputCostPer1K > 0 && p.Defaults.Pricing.OutputCostPer1K > 0 {
		inputCostPer1K = p.Defaults.Pricing.InputCostPer1K
		outputCostPer1K = p.Defaults.Pricing.OutputCostPer1K
		// Cached tokens cost 50% of input tokens for Gemini
		cachedCostPer1K = inputCostPer1K * 0.5
	} else {
		// Fallback to hardcoded pricing with warning
		fmt.Printf("WARNING: No pricing configured for provider %s (model: %s), using fallback pricing\n", p.ID(), p.Model)
		inputCostPer1K, outputCostPer1K, cachedCostPer1K = getGeminiPricing(p.Model)
	}

	// Calculate costs
	inputCost := float64(tokensIn-cachedTokens) / 1000.0 * inputCostPer1K
	cachedCost := float64(cachedTokens) / 1000.0 * cachedCostPer1K
	outputCost := float64(tokensOut) / 1000.0 * outputCostPer1K

	return types.CostInfo{
		InputTokens:   tokensIn - cachedTokens,
		OutputTokens:  tokensOut,
		CachedTokens:  cachedTokens,
		InputCostUSD:  inputCost,
		OutputCostUSD: outputCost,
		CachedCostUSD: cachedCost,
		TotalCost:     inputCost + cachedCost + outputCost,
	}
}

// PredictStream streams a predict response from Gemini
func (p *GeminiProvider) PredictStream(ctx context.Context, req providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	// Convert messages to Gemini format and apply defaults
	contents, systemInstruction, temperature, topP, maxTokens := p.prepareGeminiRequest(req)

	// Create streaming request
	geminiReq := p.buildGeminiRequest(contents, systemInstruction, temperature, topP, maxTokens)

	reqBody, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request
	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?key=%s", p.BaseURL, p.Model, p.ApiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	//nolint:bodyclose // body is closed in streamResponse goroutine
	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if err := providers.CheckHTTPError(resp); err != nil {
		return nil, err
	}

	outChan := make(chan providers.StreamChunk)

	go p.streamResponse(ctx, resp.Body, outChan)

	return outChan, nil
}

// processGeminiStreamChunk processes a single chunk from the Gemini stream
func (p *GeminiProvider) processGeminiStreamChunk(chunk geminiResponse, accumulated string, totalTokens int, outChan chan<- providers.StreamChunk) (string, int, bool) {
	if len(chunk.Candidates) == 0 {
		return accumulated, totalTokens, false
	}

	candidate := chunk.Candidates[0]
	if len(candidate.Content.Parts) == 0 {
		return accumulated, totalTokens, false
	}

	delta := candidate.Content.Parts[0].Text
	if delta != "" {
		accumulated += delta
		totalTokens++ // Approximate

		outChan <- providers.StreamChunk{
			Content:     accumulated,
			Delta:       delta,
			TokenCount:  totalTokens,
			DeltaTokens: 1,
		}
	}

	if candidate.FinishReason != "" {
		finalChunk := providers.StreamChunk{
			Content:      accumulated,
			TokenCount:   totalTokens,
			FinishReason: &candidate.FinishReason,
		}

		// Extract cost from usage metadata if available
		if chunk.UsageMetadata != nil {
			costBreakdown := p.CalculateCost(
				chunk.UsageMetadata.PromptTokenCount,
				chunk.UsageMetadata.CandidatesTokenCount,
				chunk.UsageMetadata.CachedContentTokenCount,
			)
			finalChunk.CostInfo = &costBreakdown
		}

		outChan <- finalChunk
		return accumulated, totalTokens, true // Signal finish
	}

	return accumulated, totalTokens, false
}

// streamResponse reads JSON stream from Gemini and sends chunks
func (p *GeminiProvider) streamResponse(ctx context.Context, body io.ReadCloser, outChan chan<- providers.StreamChunk) {
	defer close(outChan)
	defer body.Close()

	// Gemini returns JSON array: [{"candidates": [...], ...}, {"candidates": [...], ...}]
	// We need to read the entire body and parse it as an array
	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		outChan <- providers.StreamChunk{
			Error:        fmt.Errorf("failed to read response body: %w", err),
			FinishReason: providers.StringPtr("error"),
		}
		return
	}

	// Parse as array of responses
	var responses []geminiResponse
	if err := json.Unmarshal(bodyBytes, &responses); err != nil {
		outChan <- providers.StreamChunk{
			Error:        fmt.Errorf("failed to parse streaming response: %w", err),
			FinishReason: providers.StringPtr("error"),
		}
		return
	}

	accumulated := ""
	totalTokens := 0

	// Process each chunk in the array
	for _, chunk := range responses {
		select {
		case <-ctx.Done():
			outChan <- providers.StreamChunk{
				Content:      accumulated,
				Error:        ctx.Err(),
				FinishReason: providers.StringPtr("cancelled"),
			}
			return
		default:
		}

		var finished bool
		accumulated, totalTokens, finished = p.processGeminiStreamChunk(chunk, accumulated, totalTokens, outChan)
		if finished {
			return
		}
	}

	// No finish reason received, send final chunk
	outChan <- providers.StreamChunk{
		Content:      accumulated,
		TokenCount:   totalTokens,
		FinishReason: providers.StringPtr("stop"),
	}
}

// inferMediaTypeFromMIME infers the content type (image/audio/video) from a MIME type
func inferMediaTypeFromMIME(mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return types.ContentTypeImage
	case strings.HasPrefix(mimeType, "audio/"):
		return types.ContentTypeAudio
	case strings.HasPrefix(mimeType, "video/"):
		return types.ContentTypeVideo
	default:
		return ""
	}
}

// SupportsStreaming is provided by BaseProvider (returns true)
