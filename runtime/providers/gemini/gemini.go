package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
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
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}

	return &GeminiProvider{
		BaseProvider: providers.NewBaseProvider(id, includeRawOutput, &http.Client{Timeout: 60 * time.Second}),
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

// Chat sends a chat request to Gemini
func (p *GeminiProvider) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	start := time.Now()

	// Convert messages to Gemini format
	var contents []geminiContent
	var systemInstruction *geminiContent

	// Handle system message
	if req.System != "" {
		systemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: req.System}},
		}
	}

	// Convert conversation messages
	for _, msg := range req.Messages {
		role := msg.Role
		// Gemini uses "user" and "model" roles
		if role == "assistant" {
			role = "model"
		}

		contents = append(contents, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: msg.Content}},
		})
	}

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

	// Create request
	geminiReq := geminiRequest{
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

	reqBody, err := json.Marshal(geminiReq)
	if err != nil {
		return providers.ChatResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Prepare response with raw request if configured (set early to preserve on error)
	chatResp := providers.ChatResponse{
		Latency: time.Since(start), // Will be updated at the end
	}
	if p.ShouldIncludeRawOutput() {
		chatResp.RawRequest = geminiReq
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
		chatResp.Latency = time.Since(start)
		return chatResp, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)

	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		logger.APIResponse("Gemini", 0, "", err)
		chatResp.Latency = time.Since(start)
		return chatResp, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.APIResponse("Gemini", resp.StatusCode, "", err)
		chatResp.Latency = time.Since(start)
		return chatResp, fmt.Errorf("failed to read response: %w", err)
	}

	// Debug log the response
	logger.APIResponse("Gemini", resp.StatusCode, string(respBody), nil)

	if resp.StatusCode != http.StatusOK {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBody
		return chatResp, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var geminiResp geminiResponse
	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBody
		return chatResp, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 {
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
			chatResp.Latency = time.Since(start)
			chatResp.Raw = respBody
			return chatResp, errors.New(errorMsg)
		}
		// No candidates but also no explicit block reason - could be model issue or invalid request
		errorMsg := "no candidates in response (prompt consumed tokens but generated no output)"
		if geminiResp.UsageMetadata != nil {
			errorMsg += fmt.Sprintf(" - used %d prompt tokens", geminiResp.UsageMetadata.PromptTokenCount)
		}
		errorMsg += " - this may indicate the model refused to generate content, try rephrasing the prompt or checking system instructions"
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBody
		return chatResp, errors.New(errorMsg)
	}

	candidate := geminiResp.Candidates[0]

	// Check for safety or other blocking
	if candidate.FinishReason == "SAFETY" {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBody
		return chatResp, fmt.Errorf("response blocked by safety filters")
	}

	if len(candidate.Content.Parts) == 0 {
		// Handle different finish reasons
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBody
		switch candidate.FinishReason {
		case "MAX_TOKENS":
			// Don't use fallback - return error to see when this happens
			return chatResp, fmt.Errorf("gemini returned MAX_TOKENS error (this should not happen with reasonable limits)")
		case "SAFETY":
			return chatResp, fmt.Errorf("response blocked by Gemini safety filters")
		case "RECITATION":
			return chatResp, fmt.Errorf("response blocked due to recitation concerns")
		default:
			return chatResp, fmt.Errorf("no content parts in response (finish reason: %s)", candidate.FinishReason)
		}
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

	chatResp.Content = candidate.Content.Parts[0].Text
	chatResp.CostInfo = &costBreakdown
	chatResp.Latency = latency
	chatResp.Raw = respBody

	return chatResp, nil
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

		switch p.Model {
		case "gemini-1.5-pro", "gemini-2.5-pro":
			inputCostPer1K = 0.00125   // $0.00125 per 1K input tokens
			outputCostPer1K = 0.005    // $0.005 per 1K output tokens
			cachedCostPer1K = 0.000625 // 50% of input cost
		case "gemini-1.5-flash", "gemini-2.5-flash":
			inputCostPer1K = 0.000075   // $0.000075 per 1K input tokens
			outputCostPer1K = 0.0003    // $0.0003 per 1K output tokens
			cachedCostPer1K = 0.0000375 // 50% of input cost
		case "gemini-pro":
			inputCostPer1K = 0.0005   // $0.0005 per 1K input tokens
			outputCostPer1K = 0.0015  // $0.0015 per 1K output tokens
			cachedCostPer1K = 0.00025 // 50% of input cost
		default:
			// Default to Gemini 1.5 Pro pricing for unknown models
			inputCostPer1K = 0.00125
			outputCostPer1K = 0.005
			cachedCostPer1K = 0.000625
		}
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

// ChatStream streams a chat response from Gemini
func (p *GeminiProvider) ChatStream(ctx context.Context, req providers.ChatRequest) (<-chan providers.StreamChunk, error) {
	// Convert messages to Gemini format
	var contents []geminiContent
	var systemInstruction *geminiContent

	if req.System != "" {
		systemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: req.System}},
		}
	}

	for _, msg := range req.Messages {
		role := msg.Role
		if role == "assistant" {
			role = "model"
		}

		contents = append(contents, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: msg.Content}},
		})
	}

	// Apply provider defaults
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

	// Create streaming request
	geminiReq := geminiRequest{
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

	httpReq.Header.Set(contentTypeHeader, applicationJSON)

	resp, err := p.GetHTTPClient().Do(httpReq)
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

		if len(chunk.Candidates) == 0 {
			continue
		}

		candidate := chunk.Candidates[0]
		if len(candidate.Content.Parts) == 0 {
			continue
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
			finishReason := candidate.FinishReason
			finalChunk := providers.StreamChunk{
				Content:      accumulated,
				TokenCount:   totalTokens,
				FinishReason: &finishReason,
			}

			// Extract cost from usage metadata if available
			if chunk.UsageMetadata != nil {
				tokensIn := chunk.UsageMetadata.PromptTokenCount
				tokensOut := chunk.UsageMetadata.CandidatesTokenCount
				cachedTokens := chunk.UsageMetadata.CachedContentTokenCount

				costBreakdown := p.CalculateCost(tokensIn, tokensOut, cachedTokens)
				finalChunk.CostInfo = &costBreakdown
			}

			outChan <- finalChunk
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

// SupportsStreaming is provided by BaseProvider (returns true)
