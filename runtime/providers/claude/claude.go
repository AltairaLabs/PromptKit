package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// HTTP constants
const (
	contentTypeHeader = "Content-Type"
	applicationJSON   = "application/json"
)

// ClaudeProvider implements the Provider interface for Anthropic Claude
type ClaudeProvider struct {
	id               string
	model            string
	baseURL          string
	apiKey           string
	defaults         providers.ProviderDefaults
	includeRawOutput bool
	client           *http.Client
}

// NewClaudeProvider creates a new Claude provider
func NewClaudeProvider(id, model, baseURL string, defaults providers.ProviderDefaults, includeRawOutput bool) *ClaudeProvider {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("CLAUDE_API_KEY")
	}

	return &ClaudeProvider{
		id:               id,
		model:            model,
		baseURL:          baseURL,
		apiKey:           apiKey,
		defaults:         defaults,
		includeRawOutput: includeRawOutput,
		client:           &http.Client{Timeout: 60 * time.Second},
	}
}

// ID returns the provider ID
func (p *ClaudeProvider) ID() string {
	return p.id
}

// ShouldIncludeRawOutput returns whether to include raw API requests in output
func (p *ClaudeProvider) ShouldIncludeRawOutput() bool {
	return p.includeRawOutput
}

// Close closes the HTTP client and cleans up idle connections
func (p *ClaudeProvider) Close() error {
	p.client.CloseIdleConnections()
	return nil
}

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
func (p *ClaudeProvider) supportsCaching() bool {
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

// Chat sends a chat request to Claude
func (p *ClaudeProvider) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	start := time.Now()

	// Convert messages to Claude format
	messages := make([]claudeMessage, 0, len(req.Messages))

	for i, msg := range req.Messages {
		// Create content block for the message
		contentBlock := claudeContentBlock{
			Type: "text",
			Text: msg.Content,
		}

		// Enable cache control only if model supports it and content is long enough
		// Claude caching requires 2048 tokens minimum
		// Estimate ~4 characters per token for rough threshold
		minCharsForCaching := 2048 * 4 // ~8192 characters

		// Only cache the last message with sufficient content to maximize cache hits
		if p.supportsCaching() && i == len(req.Messages)-1 && len(msg.Content) >= minCharsForCaching {
			contentBlock.CacheControl = &claudeCacheControl{Type: "ephemeral"}
		}

		claudeMsg := claudeMessage{
			Role:    msg.Role,
			Content: []claudeContentBlock{contentBlock},
		}

		messages = append(messages, claudeMsg)
	}

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

	// Create system content blocks if system prompt exists
	var systemBlocks []claudeContentBlock
	if req.System != "" {
		// Enable cache control for system prompt only if model supports it and prompt is long enough
		systemBlock := claudeContentBlock{
			Type: "text",
			Text: req.System,
		}

		// Estimate ~4 characters per token for rough threshold
		minCharsForCaching := 1024 * 4 // ~4096 characters for system prompt
		if p.supportsCaching() && len(req.System) >= minCharsForCaching {
			systemBlock.CacheControl = &claudeCacheControl{Type: "ephemeral"}
		}

		systemBlocks = []claudeContentBlock{systemBlock}
	}

	// Create request
	claudeReq := claudeRequest{
		Model:       p.model,
		MaxTokens:   maxTokens,
		Messages:    messages,
		System:      systemBlocks,
		Temperature: temperature,
		TopP:        topP,
	}

	reqBody, err := json.Marshal(claudeReq)
	if err != nil {
		return providers.ChatResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Prepare response with raw request if configured (set early to preserve on error)
	chatResp := providers.ChatResponse{
		Latency: time.Since(start), // Will be updated at the end
	}
	if p.includeRawOutput {
		chatResp.RawRequest = claudeReq
	}

	// Construct URL
	url := p.baseURL + "/messages"
	logger.Debug("Claude API request",
		"base_url", p.baseURL,
		"full_url", url,
		"model", p.model,
		"has_api_key", p.apiKey != "")

	// Make HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		chatResp.Latency = time.Since(start)
		return chatResp, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-API-Key", p.apiKey)
	httpReq.Header.Set("Anthropic-Version", "2023-06-01")

	logger.APIRequest("Claude", "POST", url, map[string]string{
		"Content-Type":      "application/json",
		"X-API-Key":         "***",
		"Anthropic-Version": "2023-06-01",
	}, claudeReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		chatResp.Latency = time.Since(start)
		return chatResp, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		chatResp.Latency = time.Since(start)
		return chatResp, fmt.Errorf("failed to read response: %w", err)
	}

	logger.APIResponse("Claude", resp.StatusCode, string(respBody), nil)

	if resp.StatusCode != http.StatusOK {
		logger.Error("Claude API request failed",
			"status", resp.StatusCode,
			"url", url,
			"model", p.model,
			"response", string(respBody))
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBody
		return chatResp, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var claudeResp claudeResponse
	if err := json.Unmarshal(respBody, &claudeResp); err != nil {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBody
		return chatResp, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if claudeResp.Error != nil {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBody
		return chatResp, fmt.Errorf("claude API error: %s", claudeResp.Error.Message)
	}

	if len(claudeResp.Content) == 0 {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBody
		return chatResp, fmt.Errorf("no content in response")
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
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBody
		return chatResp, fmt.Errorf("no text content found in response")
	}

	latency := time.Since(start)

	// Calculate cost breakdown
	costBreakdown := p.CalculateCost(claudeResp.Usage.InputTokens, claudeResp.Usage.OutputTokens, claudeResp.Usage.CacheReadInputTokens)

	chatResp.Content = responseText
	chatResp.CostInfo = &costBreakdown
	chatResp.Latency = latency
	chatResp.Raw = respBody

	return chatResp, nil
}

// CalculateCost calculates detailed cost breakdown including optional cached tokens
func (p *ClaudeProvider) CalculateCost(tokensIn, tokensOut, cachedTokens int) types.CostInfo {
	var inputCostPer1K, outputCostPer1K, cachedCostPer1K float64

	// Use configured pricing if available
	if p.defaults.Pricing.InputCostPer1K > 0 && p.defaults.Pricing.OutputCostPer1K > 0 {
		inputCostPer1K = p.defaults.Pricing.InputCostPer1K
		outputCostPer1K = p.defaults.Pricing.OutputCostPer1K
		// Cached tokens cost 10% of input tokens according to Anthropic pricing
		cachedCostPer1K = inputCostPer1K * 0.1
	} else {
		// Fallback to hardcoded pricing with warning
		fmt.Printf("WARNING: No pricing configured for provider %s (model: %s), using fallback pricing\n", p.id, p.model)

		switch p.model {
		case "claude-3-5-sonnet-20241022", "claude-3-5-sonnet-20240620":
			inputCostPer1K = 0.003   // $0.003 per 1K input tokens
			outputCostPer1K = 0.015  // $0.015 per 1K output tokens
			cachedCostPer1K = 0.0003 // $0.0003 per 1K cached tokens (10% of input cost)
		case "claude-3-5-haiku-20241022":
			inputCostPer1K = 0.001   // $0.001 per 1K input tokens
			outputCostPer1K = 0.005  // $0.005 per 1K output tokens
			cachedCostPer1K = 0.0001 // $0.0001 per 1K cached tokens (10% of input cost)
		case "claude-3-opus-20240229":
			inputCostPer1K = 0.015   // $0.015 per 1K input tokens
			outputCostPer1K = 0.075  // $0.075 per 1K output tokens
			cachedCostPer1K = 0.0015 // $0.0015 per 1K cached tokens (10% of input cost)
		case "claude-3-sonnet-20240229":
			inputCostPer1K = 0.003   // $0.003 per 1K input tokens
			outputCostPer1K = 0.015  // $0.015 per 1K output tokens
			cachedCostPer1K = 0.0003 // $0.0003 per 1K cached tokens (10% of input cost)
		case "claude-3-haiku-20240307":
			inputCostPer1K = 0.00025   // $0.00025 per 1K input tokens
			outputCostPer1K = 0.00125  // $0.00125 per 1K output tokens
			cachedCostPer1K = 0.000025 // $0.000025 per 1K cached tokens (10% of input cost)
		default:
			// Default to Claude 3.5 Sonnet pricing for unknown models
			inputCostPer1K = 0.003
			outputCostPer1K = 0.015
			cachedCostPer1K = 0.0003
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

// ChatStream streams a chat response from Claude
func (p *ClaudeProvider) ChatStream(ctx context.Context, req providers.ChatRequest) (<-chan providers.StreamChunk, error) {
	// Convert messages to Claude format
	messages := make([]claudeMessage, 0, len(req.Messages))

	for _, msg := range req.Messages {
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

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
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

// streamResponse reads SSE stream from Claude and sends chunks
func (p *ClaudeProvider) streamResponse(ctx context.Context, body io.ReadCloser, outChan chan<- providers.StreamChunk) {
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
				FinishReason: ptr("cancelled"),
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
			if event.Delta != nil && event.Delta.Type == "text_delta" {
				delta := event.Delta.Text
				accumulated += delta
				totalTokens++ // Approximate

				outChan <- providers.StreamChunk{
					Content:     accumulated,
					Delta:       delta,
					TokenCount:  totalTokens,
					DeltaTokens: 1,
				}
			}

		case "message_stop":
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
			return
		}
	}

	if err := scanner.Err(); err != nil {
		outChan <- providers.StreamChunk{
			Content:      accumulated,
			Error:        err,
			FinishReason: ptr("error"),
		}
	}
}

// SupportsStreaming returns true for Claude
func (p *ClaudeProvider) SupportsStreaming() bool {
	return true
}

// ptr is a helper function that returns a pointer to a string
func ptr(s string) *string {
	return &s
}
