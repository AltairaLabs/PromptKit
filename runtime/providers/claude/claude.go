// Package claude provides Anthropic Claude LLM provider integration.
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

// HTTP constants
const (
	contentTypeHeader     = "Content-Type"
	applicationJSON       = "application/json"
	anthropicVersionValue = "2023-06-01"
	anthropicVersionKey   = "Anthropic-Version"
)

// ClaudeProvider implements the Provider interface for Anthropic Claude
type Provider struct {
	providers.BaseProvider
	model    string
	baseURL  string
	apiKey   string
	defaults providers.ProviderDefaults
}

// NewClaudeProvider creates a new Claude provider
func NewProvider(id, model, baseURL string, defaults providers.ProviderDefaults, includeRawOutput bool) *Provider {
	base, apiKey := providers.NewBaseProviderWithAPIKey(id, includeRawOutput, "ANTHROPIC_API_KEY", "CLAUDE_API_KEY")

	return &Provider{
		BaseProvider: base,
		model:        model,
		baseURL:      baseURL,
		apiKey:       apiKey,
		defaults:     defaults,
	}
}

// Close implements provider cleanup (uses BaseProvider.Close)

// Claude API request/response structures
type claudeRequest struct {
	Model       string               `json:"model"`
	MaxTokens   int                  `json:"max_tokens"`
	Messages    []claudeMessage      `json:"messages"`
	System      []claudeContentBlock `json:"system,omitempty"`
	Temperature float32              `json:"temperature,omitempty"`
	TopP        float32              `json:"top_p,omitempty"`
}

type claudeMessage struct {
	Role    string               `json:"role"`
	Content []claudeContentBlock `json:"content"`
}

type claudeContentBlock struct {
	Type         string              `json:"type"` // "text", "image", etc.
	Text         string              `json:"text,omitempty"`
	CacheControl *claudeCacheControl `json:"cache_control,omitempty"`
}

type claudeCacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

type claudeResponse struct {
	ID           string          `json:"id"`
	Type         string          `json:"type"`
	Role         string          `json:"role"`
	Content      []claudeContent `json:"content"`
	Model        string          `json:"model"`
	StopReason   string          `json:"stop_reason"`
	StopSequence string          `json:"stop_sequence"`
	Usage        claudeUsage     `json:"usage"`
	Error        *claudeError    `json:"error,omitempty"`
}

type claudeContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type claudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

type claudeError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// supportsCaching returns true if the model supports prompt caching
func (p *Provider) supportsCaching() bool {
	// Claude 3.5 Haiku does not support prompt caching as of October 2024
	// Only Claude 3.5 Sonnet and Claude 3 Opus support it
	switch p.model {
	case "claude-3-5-haiku-20241022":
		return false
	case "claude-3-5-sonnet-20241022", "claude-3-5-sonnet-20240620":
		return true
	case "claude-3-opus-20240229":
		return true
	default:
		// For unknown models, disable caching to be safe
		return false
	}
}

// convertMessagesToClaudeFormat converts provider messages to Claude format with cache control
func (p *Provider) convertMessagesToClaudeFormat(messages []types.Message) []claudeMessage {
	claudeMessages := make([]claudeMessage, 0, len(messages))
	minCharsForCaching := 2048 * 4 // ~8192 characters (Claude requires 2048 tokens minimum)

	for i := range messages {
		msg := &messages[i]
		contentBlock := claudeContentBlock{
			Type: "text",
			Text: msg.Content,
		}

		// Only cache the last message with sufficient content to maximize cache hits
		if p.supportsCaching() && i == len(messages)-1 && len(msg.Content) >= minCharsForCaching {
			contentBlock.CacheControl = &claudeCacheControl{Type: "ephemeral"}
		}

		claudeMessages = append(claudeMessages, claudeMessage{
			Role:    msg.Role,
			Content: []claudeContentBlock{contentBlock},
		})
	}

	return claudeMessages
}

// createSystemBlocks creates system content blocks with cache control if applicable
func (p *Provider) createSystemBlocks(systemPrompt string) []claudeContentBlock {
	if systemPrompt == "" {
		return nil
	}

	systemBlock := claudeContentBlock{
		Type: "text",
		Text: systemPrompt,
	}

	// Enable cache control for system prompt only if model supports it and prompt is long enough
	minCharsForCaching := 1024 * 4 // ~4096 characters for system prompt
	if p.supportsCaching() && len(systemPrompt) >= minCharsForCaching {
		systemBlock.CacheControl = &claudeCacheControl{Type: "ephemeral"}
	}

	return []claudeContentBlock{systemBlock}
}

// applyDefaults applies provider defaults to zero values in the request
func (p *Provider) applyDefaults(temperature, topP float32, maxTokens int) (finalTemp, finalTopP float32, finalMaxTokens int) {
	if temperature == 0 {
		temperature = p.defaults.Temperature
	}
	if topP == 0 {
		topP = p.defaults.TopP
	}
	if maxTokens == 0 {
		maxTokens = p.defaults.MaxTokens
	}
	return temperature, topP, maxTokens
}

// makeClaudeHTTPRequest sends the HTTP request to Claude API
func (p *Provider) makeClaudeHTTPRequest(ctx context.Context, claudeReq claudeRequest, predictResp providers.PredictionResponse, start time.Time) ([]byte, providers.PredictionResponse, error) {
	reqBody, err := json.Marshal(claudeReq)
	if err != nil {
		return nil, predictResp, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := p.baseURL + "/messages"
	logger.Debug("Claude API request",
		"base_url", p.baseURL,
		"full_url", url,
		"model", p.model,
		"has_api_key", p.apiKey != "")

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		predictResp.Latency = time.Since(start)
		return nil, predictResp, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	httpReq.Header.Set("X-API-Key", p.apiKey)
	httpReq.Header.Set(anthropicVersionKey, anthropicVersionValue)

	logger.APIRequest("Claude", "POST", url, map[string]string{
		contentTypeHeader:   applicationJSON,
		"X-API-Key":         "***",
		anthropicVersionKey: anthropicVersionValue,
	}, claudeReq)

	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		predictResp.Latency = time.Since(start)
		return nil, predictResp, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		predictResp.Latency = time.Since(start)
		return nil, predictResp, fmt.Errorf("failed to read response: %w", err)
	}

	logger.APIResponse("Claude", resp.StatusCode, string(respBody), nil)

	if resp.StatusCode != http.StatusOK {
		logger.Error("Claude API request failed",
			"status", resp.StatusCode,
			"url", url,
			"model", p.model,
			"response", string(respBody))
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return nil, predictResp, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, predictResp, nil
}

// parseAndValidateClaudeResponse parses and validates the Claude API response
func (p *Provider) parseAndValidateClaudeResponse(respBody []byte, predictResp providers.PredictionResponse, start time.Time) (claudeResponse, string, providers.PredictionResponse, error) {
	var claudeResp claudeResponse
	if err := providers.UnmarshalJSON(respBody, &claudeResp, &predictResp, start); err != nil {
		return claudeResp, "", predictResp, err
	}

	if claudeResp.Error != nil {
		providers.SetErrorResponse(&predictResp, respBody, start)
		return claudeResp, "", predictResp, fmt.Errorf("claude API error: %s", claudeResp.Error.Message)
	}

	if len(claudeResp.Content) == 0 {
		providers.SetErrorResponse(&predictResp, respBody, start)
		return claudeResp, "", predictResp, fmt.Errorf("no content in response")
	}

	// Find text content
	var responseText string
	for _, content := range claudeResp.Content {
		if content.Type == "text" {
			responseText = content.Text
			break
		}
	}

	if responseText == "" {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return claudeResp, "", predictResp, fmt.Errorf("no text content found in response")
	}

	return claudeResp, responseText, predictResp, nil
}

// Predict sends a predict request to Claude
func (p *Provider) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	start := time.Now()

	// Convert messages to Claude format
	messages := p.convertMessagesToClaudeFormat(req.Messages)

	// Apply provider defaults
	temperature, topP, maxTokens := p.applyDefaults(req.Temperature, req.TopP, req.MaxTokens)

	// Create system content blocks
	systemBlocks := p.createSystemBlocks(req.System)

	// Create request
	claudeReq := claudeRequest{
		Model:       p.model,
		MaxTokens:   maxTokens,
		Messages:    messages,
		System:      systemBlocks,
		Temperature: temperature,
		TopP:        topP,
	}

	// Prepare response with raw request if configured (set early to preserve on error)
	predictResp := providers.PredictionResponse{
		Latency: time.Since(start), // Will be updated at the end
	}
	if p.ShouldIncludeRawOutput() {
		predictResp.RawRequest = claudeReq
	}

	// Make HTTP request
	respBody, predictResp, err := p.makeClaudeHTTPRequest(ctx, claudeReq, predictResp, start)
	if err != nil {
		return predictResp, err
	}

	// Parse and validate response
	claudeResp, responseText, predictResp, err := p.parseAndValidateClaudeResponse(respBody, predictResp, start)
	if err != nil {
		return predictResp, err
	}

	latency := time.Since(start)

	// Calculate cost breakdown
	costBreakdown := p.CalculateCost(claudeResp.Usage.InputTokens, claudeResp.Usage.OutputTokens, claudeResp.Usage.CacheReadInputTokens)

	predictResp.Content = responseText
	predictResp.CostInfo = &costBreakdown
	predictResp.Latency = latency
	predictResp.Raw = respBody

	return predictResp, nil
}

// claudePricing returns pricing for Claude models (input, output, cached per 1K tokens)
func claudePricing(model string) (inputPrice, outputPrice, cachedPrice float64) {
	// Define pricing constants
	const (
		sonnetInput  = 0.003
		sonnetOutput = 0.015
		sonnetCached = 0.0003
		haikuInput   = 0.001
		haikuOutput  = 0.005
		haikuCached  = 0.0001
		opusInput    = 0.015
		opusOutput   = 0.075
		opusCached   = 0.0015
		haiku3Input  = 0.00025
		haiku3Output = 0.00125
		haiku3Cached = 0.000025
	)

	switch model {
	case "claude-3-5-sonnet-20241022", "claude-3-5-sonnet-20240620", "claude-3-sonnet-20240229":
		return sonnetInput, sonnetOutput, sonnetCached
	case "claude-3-5-haiku-20241022":
		return haikuInput, haikuOutput, haikuCached
	case "claude-3-opus-20240229":
		return opusInput, opusOutput, opusCached
	case "claude-3-haiku-20240307":
		return haiku3Input, haiku3Output, haiku3Cached
	default:
		// Default to Claude 3.5 Sonnet pricing for unknown models
		return sonnetInput, sonnetOutput, sonnetCached
	}
}

// CalculateCost calculates detailed cost breakdown including optional cached tokens
func (p *Provider) CalculateCost(tokensIn, tokensOut, cachedTokens int) types.CostInfo {
	var inputCostPer1K, outputCostPer1K, cachedCostPer1K float64

	// Use configured pricing if available
	if p.defaults.Pricing.InputCostPer1K > 0 && p.defaults.Pricing.OutputCostPer1K > 0 {
		inputCostPer1K = p.defaults.Pricing.InputCostPer1K
		outputCostPer1K = p.defaults.Pricing.OutputCostPer1K
		// Cached tokens cost 10% of input tokens according to Anthropic pricing
		cachedCostPer1K = inputCostPer1K * 0.1
	} else {
		// Fallback to hardcoded pricing with warning
		fmt.Printf("WARNING: No pricing configured for provider %s (model: %s), using fallback pricing\n", p.ID(), p.model)
		inputCostPer1K, outputCostPer1K, cachedCostPer1K = claudePricing(p.model)
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

// PredictStream streams a predict response from Claude
func (p *Provider) PredictStream(ctx context.Context, req providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	// Convert messages to Claude format
	messages := make([]claudeMessage, 0, len(req.Messages))

	for i := range req.Messages {
		msg := &req.Messages[i]
		claudeMsg := claudeMessage{
			Role: msg.Role,
			Content: []claudeContentBlock{
				{
					Type: "text",
					Text: msg.Content,
				},
			},
		}
		messages = append(messages, claudeMsg)
	}

	// Apply provider defaults
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
	claudeReq := map[string]interface{}{
		"model":       p.model,
		"max_tokens":  maxTokens,
		"messages":    messages,
		"temperature": temperature,
		"top_p":       topP,
		"stream":      true,
	}

	if req.System != "" {
		claudeReq["system"] = []claudeContentBlock{
			{
				Type: "text",
				Text: req.System,
			},
		}
	}

	reqBody, err := json.Marshal(claudeReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set(anthropicVersionKey, anthropicVersionValue)
	httpReq.Header.Set("Accept", "text/event-stream")

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

// processClaudeContentDelta handles content_block_delta events from Claude stream
func (p *Provider) processClaudeContentDelta(event struct {
	Type  string `json:"type"`
	Delta *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta,omitempty"`
	Message *struct {
		StopReason string       `json:"stop_reason"`
		Usage      *claudeUsage `json:"usage,omitempty"`
	} `json:"message,omitempty"`
}, accumulated string, totalTokens int, outChan chan<- providers.StreamChunk) (newAccumulated string, newTotalTokens int) {
	if event.Delta == nil || event.Delta.Type != "text_delta" {
		return accumulated, totalTokens
	}

	delta := event.Delta.Text
	accumulated += delta
	totalTokens++ // Approximate

	outChan <- providers.StreamChunk{
		Content:     accumulated,
		Delta:       delta,
		TokenCount:  totalTokens,
		DeltaTokens: 1,
	}

	return accumulated, totalTokens
}

// processClaudeMessageStop handles message_stop events from Claude stream
func (p *Provider) processClaudeMessageStop(event struct {
	Type  string `json:"type"`
	Delta *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta,omitempty"`
	Message *struct {
		StopReason string       `json:"stop_reason"`
		Usage      *claudeUsage `json:"usage,omitempty"`
	} `json:"message,omitempty"`
}, accumulated string, totalTokens int, outChan chan<- providers.StreamChunk) {
	finishReason := "stop"
	finalChunk := providers.StreamChunk{
		Content:      accumulated,
		TokenCount:   totalTokens,
		FinishReason: &finishReason,
	}

	if event.Message != nil {
		if event.Message.StopReason != "" {
			finishReason = event.Message.StopReason
			finalChunk.FinishReason = &finishReason
		}

		// Extract cost from usage if available
		if event.Message.Usage != nil {
			tokensIn := event.Message.Usage.InputTokens
			tokensOut := event.Message.Usage.OutputTokens
			cachedTokens := event.Message.Usage.CacheReadInputTokens

			costBreakdown := p.CalculateCost(tokensIn, tokensOut, cachedTokens)
			finalChunk.CostInfo = &costBreakdown
		}
	}

	outChan <- finalChunk
}

// streamResponse reads SSE stream from Claude and sends chunks
func (p *Provider) streamResponse(ctx context.Context, body io.ReadCloser, outChan chan<- providers.StreamChunk) {
	defer close(outChan)
	defer body.Close()

	scanner := providers.NewSSEScanner(body)
	accumulated := ""
	totalTokens := 0

	for scanner.Scan() {
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

		data := scanner.Data()

		var event struct {
			Type  string `json:"type"`
			Delta *struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta,omitempty"`
			Message *struct {
				StopReason string       `json:"stop_reason"`
				Usage      *claudeUsage `json:"usage,omitempty"`
			} `json:"message,omitempty"`
		}

		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue // Skip malformed chunks
		}

		switch event.Type {
		case "content_block_delta":
			accumulated, totalTokens = p.processClaudeContentDelta(event, accumulated, totalTokens, outChan)

		case "message_stop":
			p.processClaudeMessageStop(event, accumulated, totalTokens, outChan)
			return
		}
	}

	if err := scanner.Err(); err != nil {
		outChan <- providers.StreamChunk{
			Content:      accumulated,
			Error:        err,
			FinishReason: providers.StringPtr("error"),
		}
	}
}

// SupportsStreaming is provided by BaseProvider (returns true)
