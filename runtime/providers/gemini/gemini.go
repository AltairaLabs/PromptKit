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

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// HTTP constants
const (
	contentTypeHeader = "Content-Type"
	applicationJSON   = "application/json"
	httpClientTimeout = 60 * time.Second
)

// Provider implements the Provider interface for Google Gemini
type Provider struct {
	providers.BaseProvider
	model          string
	baseURL        string
	apiKey         string
	credential     providers.Credential
	defaults       providers.ProviderDefaults
	platform       string
	platformConfig *providers.PlatformConfig
}

// NewProvider creates a new Gemini provider
func NewProvider(id, model, baseURL string, defaults providers.ProviderDefaults, includeRawOutput bool) *Provider {
	base, apiKey := providers.NewBaseProviderWithAPIKey(id, includeRawOutput, "GEMINI_API_KEY", "GOOGLE_API_KEY")

	return &Provider{
		BaseProvider: base,
		model:        model,
		baseURL:      baseURL,
		apiKey:       apiKey,
		defaults:     defaults,
	}
}

// NewProviderWithCredential creates a new Gemini provider with explicit credential.
func NewProviderWithCredential(
	id, model, baseURL string, defaults providers.ProviderDefaults,
	includeRawOutput bool, cred providers.Credential,
	platform string, platformConfig *providers.PlatformConfig,
) *Provider {
	base, apiKey := providers.NewBaseProviderWithCredential(id, includeRawOutput, httpClientTimeout, cred)

	return &Provider{
		BaseProvider:   base,
		model:          model,
		baseURL:        baseURL,
		apiKey:         apiKey,
		credential:     cred,
		defaults:       defaults,
		platform:       platform,
		platformConfig: platformConfig,
	}
}

// Model returns the model name/identifier used by this provider.
func (p *Provider) Model() string {
	return p.model
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
	Text         string              `json:"text,omitempty"`
	InlineData   *geminiInlineData   `json:"inlineData,omitempty"`
	FunctionCall *geminiPartFuncCall `json:"functionCall,omitempty"`
}

// geminiPartFuncCall represents a function call in a streaming response part
type geminiPartFuncCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type geminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"` // base64 encoded
}

type geminiGenConfig struct {
	Temperature      float32     `json:"temperature"`
	TopP             float32     `json:"topP"`
	MaxOutputTokens  int         `json:"maxOutputTokens"`
	ResponseMimeType string      `json:"responseMimeType,omitempty"` // "text/plain" or "application/json"
	ResponseSchema   interface{} `json:"responseSchema,omitempty"`   // JSON Schema for structured output
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

// convertMessagesToGeminiContents converts provider messages to Gemini format.
// This handles both legacy text-only messages and multimodal messages with audio/image/video.
//
// For text-only messages (including SDK-style messages with multiple text parts),
// content is combined into a single text part via GetContent() to preserve backward
// compatibility with existing tests and behavior.
//
// For messages with actual media content (images, audio, video), each part is
// converted separately using the multimodal conversion functions.
func convertMessagesToGeminiContents(messages []types.Message) []geminiContent {
	contents := make([]geminiContent, 0, len(messages))
	for i := range messages {
		// Skip system role messages - they should be in systemPrompt parameter
		if messages[i].Role == roleSystem {
			continue
		}

		role := messages[i].Role
		// Gemini uses "user" and "model" roles
		if role == roleAssistant {
			role = roleModel
		}

		// Check if this message has actual media content (images, audio, video).
		// We use HasMediaContent() rather than IsMultimodal() because IsMultimodal()
		// returns true even for text-only messages that use Parts (SDK-style).
		// For text-only messages, we want to combine all text into a single part.
		if messages[i].HasMediaContent() {
			// Convert multimodal parts using the shared conversion functions
			var parts []geminiPart
			conversionFailed := false
			for _, part := range messages[i].Parts {
				gPart, err := convertPartToGemini(part)
				if err != nil {
					// Fall back to text-only on conversion error
					conversionFailed = true
					break
				}
				parts = append(parts, gPart)
			}
			if conversionFailed {
				contents = append(contents, geminiContent{
					Role:  role,
					Parts: []geminiPart{{Text: messages[i].GetContent()}},
				})
			} else {
				contents = append(contents, geminiContent{
					Role:  role,
					Parts: parts,
				})
			}
		} else {
			// Text-only message (legacy or SDK-style with only text parts)
			contents = append(contents, geminiContent{
				Role:  role,
				Parts: []geminiPart{{Text: messages[i].GetContent()}},
			})
		}
	}
	return contents
}

// applyRequestDefaults applies provider defaults to zero-valued request parameters
func (p *Provider) applyRequestDefaults(req providers.PredictionRequest) (temperature, topP float32, maxTokens int) {
	temperature = req.Temperature
	if temperature == 0 {
		temperature = p.defaults.Temperature
	}

	topP = req.TopP
	if topP == 0 {
		topP = p.defaults.TopP
	}

	maxTokens = req.MaxTokens
	if maxTokens == 0 {
		maxTokens = p.defaults.MaxTokens
	}

	return temperature, topP, maxTokens
}

// prepareGeminiRequest converts a predict request to Gemini format with defaults applied
func (p *Provider) prepareGeminiRequest(req providers.PredictionRequest) (
	contents []geminiContent,
	systemInstruction *geminiContent,
	temperature, topP float32,
	maxTokens int,
) {
	// Handle system message
	if req.System != "" {
		systemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: req.System}},
		}
	}

	// Convert conversation messages
	contents = convertMessagesToGeminiContents(req.Messages)

	// Apply defaults using the shared method
	temperature, topP, maxTokens = p.applyRequestDefaults(req)

	return contents, systemInstruction, temperature, topP, maxTokens
}

// buildGeminiRequest creates a Gemini API request with standard safety settings
func (p *Provider) buildGeminiRequest(contents []geminiContent, systemInstruction *geminiContent, temperature, topP float32, maxTokens int) geminiRequest {
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

// applyResponseFormat applies response format settings to a Gemini request
func (p *Provider) applyResponseFormat(req *geminiRequest, rf *providers.ResponseFormat) {
	if rf == nil {
		return
	}

	switch rf.Type {
	case providers.ResponseFormatJSON:
		// Simple JSON mode - just set the MIME type
		req.GenerationConfig.ResponseMimeType = applicationJSON
	case providers.ResponseFormatJSONSchema:
		// JSON schema mode - set MIME type and schema
		req.GenerationConfig.ResponseMimeType = applicationJSON
		if len(rf.JSONSchema) > 0 {
			var schema interface{}
			if err := json.Unmarshal(rf.JSONSchema, &schema); err == nil {
				req.GenerationConfig.ResponseSchema = schema
			}
		}
	case providers.ResponseFormatText:
		// Text is default, no changes needed
	}
}

// handleGeminiFinishReason processes error finish reasons from Gemini responses
func (p *Provider) handleGeminiFinishReason(finishReason string, predictResp providers.PredictionResponse, respBody []byte, start time.Time) (providers.PredictionResponse, error) {
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
func (p *Provider) handleNoCandidatesError(geminiResp geminiResponse, predictResp providers.PredictionResponse, respBody []byte, start time.Time) (providers.PredictionResponse, error) {
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
func (p *Provider) makeGeminiHTTPRequest(ctx context.Context, geminiReq geminiRequest, predictResp providers.PredictionResponse, start time.Time) ([]byte, providers.PredictionResponse, error) {
	reqBody, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, predictResp, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Build URL with API key
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", p.baseURL, p.model, p.apiKey)

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
		if p.platform != "" {
			return nil, predictResp, providers.ParsePlatformHTTPError(p.platform, resp.StatusCode, respBody)
		}
		return nil, predictResp, fmt.Errorf("API request to %s failed with status %d: %s",
			logger.RedactSensitiveData(url), resp.StatusCode, string(respBody))
	}

	return respBody, predictResp, nil
}

// parseAndValidateGeminiResponse parses and validates the Gemini API response
func (p *Provider) parseAndValidateGeminiResponse(respBody []byte, predictResp providers.PredictionResponse, start time.Time) (geminiResponse, geminiCandidate, providers.PredictionResponse, error) {
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
func (p *Provider) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	// Enrich context with provider and model info for logging
	ctx = logger.WithLoggingContext(ctx, &logger.LoggingFields{
		Provider: p.ID(),
		Model:    p.model,
	})

	start := time.Now()

	// Convert messages to Gemini format and apply defaults
	contents, systemInstruction, temperature, topP, maxTokens := p.prepareGeminiRequest(req)

	// Create request
	geminiReq := p.buildGeminiRequest(contents, systemInstruction, temperature, topP, maxTokens)

	// Apply response format if specified
	p.applyResponseFormat(&geminiReq, req.ResponseFormat)

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

// geminiPricing returns pricing for Gemini models (input, output, cached per 1K tokens)
func geminiPricing(model string) (inputPrice, outputPrice, cachedPrice float64) {
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
func (p *Provider) CalculateCost(tokensIn, tokensOut, cachedTokens int) types.CostInfo {
	var inputCostPer1K, outputCostPer1K, cachedCostPer1K float64

	// Use configured pricing if available
	if p.defaults.Pricing.InputCostPer1K > 0 && p.defaults.Pricing.OutputCostPer1K > 0 {
		inputCostPer1K = p.defaults.Pricing.InputCostPer1K
		outputCostPer1K = p.defaults.Pricing.OutputCostPer1K
		// Cached tokens cost 50% of input tokens for Gemini
		cachedCostPer1K = inputCostPer1K * 0.5
	} else {
		// Fallback to hardcoded pricing with warning
		logger.Warn("No pricing configured, using fallback pricing", "provider", p.ID(), "model", p.model)
		inputCostPer1K, outputCostPer1K, cachedCostPer1K = geminiPricing(p.model)
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
