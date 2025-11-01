package providers

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
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// OpenAIProvider implements the Provider interface for OpenAI
type OpenAIProvider struct {
	id               string
	model            string
	baseURL          string
	apiKey           string
	defaults         ProviderDefaults
	includeRawOutput bool
	client           *http.Client
}

// ProviderDefaults holds default parameters for providers
type ProviderDefaults struct {
	Temperature float32
	TopP        float32
	MaxTokens   int
	Pricing     Pricing
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider(id, model, baseURL string, defaults ProviderDefaults, includeRawOutput bool) *OpenAIProvider {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_TOKEN")
	}

	return &OpenAIProvider{
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
func (p *OpenAIProvider) ID() string {
	return p.id
}

// ShouldIncludeRawOutput returns whether to include raw API requests in output
func (p *OpenAIProvider) ShouldIncludeRawOutput() bool {
	return p.includeRawOutput
}

// Close closes the HTTP client and cleans up idle connections
func (p *OpenAIProvider) Close() error {
	p.client.CloseIdleConnections()
	return nil
}

// OpenAI API request/response structures
type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature float32         `json:"temperature"`
	TopP        float32         `json:"top_p"`
	MaxTokens   int             `json:"max_tokens"`
	Seed        *int            `json:"seed,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
	Error   *openAIError   `json:"error,omitempty"`
}

type openAIChoice struct {
	Index        int           `json:"index"`
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openAIUsage struct {
	PromptTokens        int                  `json:"prompt_tokens"`
	CompletionTokens    int                  `json:"completion_tokens"`
	TotalTokens         int                  `json:"total_tokens"`
	PromptTokensDetails *openAIPromptDetails `json:"prompt_tokens_details,omitempty"`
}

type openAIPromptDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type openAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// Chat sends a chat request to OpenAI
func (p *OpenAIProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	start := time.Now()

	// Convert messages
	messages := make([]openAIMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, openAIMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	for _, msg := range req.Messages {
		messages = append(messages, openAIMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
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
		return ChatResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Prepare response with raw request if configured (set early to preserve on error)
	chatResp := ChatResponse{
		Latency: time.Since(start), // Will be updated at the end
	}
	if p.ShouldIncludeRawOutput() {
		chatResp.RawRequest = openAIReq
	}

	// Make HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return chatResp, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	client := &http.Client{Timeout: 30 * time.Second}

	logger.APIRequest("OpenAI", "POST", p.baseURL+"/chat/completions", map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + p.apiKey,
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

	chatResp.Content = openAIResp.Choices[0].Message.Content
	chatResp.CostInfo = &costBreakdown
	chatResp.Latency = latency
	chatResp.Raw = respBody

	return chatResp, nil
}

// CalculateCost calculates detailed cost breakdown including optional cached tokens
func (p *OpenAIProvider) CalculateCost(tokensIn, tokensOut, cachedTokens int) types.CostInfo {
	var inputCostPer1K, outputCostPer1K, cachedCostPer1K float64

	// Use configured pricing if available
	if p.defaults.Pricing.InputCostPer1K > 0 && p.defaults.Pricing.OutputCostPer1K > 0 {
		inputCostPer1K = p.defaults.Pricing.InputCostPer1K
		outputCostPer1K = p.defaults.Pricing.OutputCostPer1K
		// Assume cached tokens cost 50% of input tokens
		cachedCostPer1K = inputCostPer1K * 0.5
	} else {
		// Fallback to hardcoded pricing with warning
		fmt.Printf("WARNING: No pricing configured for provider %s (model: %s), using fallback pricing\n", p.id, p.model)

		switch p.model {
		case "gpt-4":
			inputCostPer1K = 0.03   // $0.03 per 1K input tokens
			outputCostPer1K = 0.06  // $0.06 per 1K output tokens
			cachedCostPer1K = 0.015 // $0.015 per 1K cached tokens (50% discount)
		case "gpt-4o":
			inputCostPer1K = 0.0025   // $0.0025 per 1K input tokens
			outputCostPer1K = 0.01    // $0.01 per 1K output tokens
			cachedCostPer1K = 0.00125 // $0.00125 per 1K cached tokens (50% discount)
		case "gpt-4o-mini":
			inputCostPer1K = 0.00015   // $0.00015 per 1K input tokens
			outputCostPer1K = 0.0006   // $0.0006 per 1K output tokens
			cachedCostPer1K = 0.000075 // $0.000075 per 1K cached tokens (50% discount)
		case "gpt-3.5-turbo":
			inputCostPer1K = 0.0015   // $0.0015 per 1K input tokens
			outputCostPer1K = 0.002   // $0.002 per 1K output tokens
			cachedCostPer1K = 0.00075 // $0.00075 per 1K cached tokens (50% discount)
		default:
			// Default to GPT-4o pricing for unknown models
			inputCostPer1K = 0.0025
			outputCostPer1K = 0.01
			cachedCostPer1K = 0.00125
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

// ChatStream streams a chat response from OpenAI
func (p *OpenAIProvider) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	// Convert messages
	messages := make([]openAIMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, openAIMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	for _, msg := range req.Messages {
		messages = append(messages, openAIMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
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
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
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

	outChan := make(chan StreamChunk)

	go p.streamResponse(ctx, resp.Body, outChan)

	return outChan, nil
}

// streamResponse reads SSE stream from OpenAI and sends chunks
func (p *OpenAIProvider) streamResponse(ctx context.Context, body io.ReadCloser, outChan chan<- StreamChunk) {
	defer close(outChan)
	defer body.Close()

	scanner := NewSSEScanner(body)
	accumulated := ""
	totalTokens := 0
	var accumulatedToolCalls []types.MessageToolCall

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			outChan <- StreamChunk{
				Content:      accumulated,
				ToolCalls:    accumulatedToolCalls,
				Error:        ctx.Err(),
				FinishReason: ptr("cancelled"),
			}
			return
		default:
		}

		data := scanner.Data()
		if data == "[DONE]" {
			outChan <- StreamChunk{
				Content:      accumulated,
				ToolCalls:    accumulatedToolCalls,
				TokenCount:   totalTokens,
				FinishReason: ptr("stop"),
			}
			return
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id,omitempty"`
						Type     string `json:"type,omitempty"`
						Function struct {
							Name      string `json:"name,omitempty"`
							Arguments string `json:"arguments,omitempty"`
						} `json:"function,omitempty"`
					} `json:"tool_calls,omitempty"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *openAIUsage `json:"usage,omitempty"` // Present in final chunk
		}

		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // Skip malformed chunks
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		// Handle content delta
		delta := chunk.Choices[0].Delta.Content
		if delta != "" {
			accumulated += delta
			totalTokens++ // Approximate

			outChan <- StreamChunk{
				Content:     accumulated,
				Delta:       delta,
				ToolCalls:   accumulatedToolCalls,
				TokenCount:  totalTokens,
				DeltaTokens: 1,
			}
		}

		// Handle tool call deltas
		if len(chunk.Choices[0].Delta.ToolCalls) > 0 {
			for _, tcDelta := range chunk.Choices[0].Delta.ToolCalls {
				// Ensure we have enough slots in accumulated tool calls
				for len(accumulatedToolCalls) <= tcDelta.Index {
					accumulatedToolCalls = append(accumulatedToolCalls, types.MessageToolCall{})
				}

				tc := &accumulatedToolCalls[tcDelta.Index]

				// Accumulate tool call data
				if tcDelta.ID != "" {
					tc.ID = tcDelta.ID
				}
				if tcDelta.Function.Name != "" {
					tc.Name = tcDelta.Function.Name
				}
				if tcDelta.Function.Arguments != "" {
					// Append arguments (they come in chunks)
					tc.Args = append(tc.Args, []byte(tcDelta.Function.Arguments)...)
				}
			}

			// Send chunk with updated tool calls
			outChan <- StreamChunk{
				Content:     accumulated,
				ToolCalls:   accumulatedToolCalls,
				TokenCount:  totalTokens,
				DeltaTokens: 0,
			}
		}

		if chunk.Choices[0].FinishReason != nil {
			finalChunk := StreamChunk{
				Content:      accumulated,
				ToolCalls:    accumulatedToolCalls,
				TokenCount:   totalTokens,
				FinishReason: chunk.Choices[0].FinishReason,
			}

			// Extract cost from usage if available
			if chunk.Usage != nil {
				tokensIn := chunk.Usage.PromptTokens
				tokensOut := chunk.Usage.CompletionTokens
				cachedTokens := 0
				if chunk.Usage.PromptTokensDetails != nil {
					cachedTokens = chunk.Usage.PromptTokensDetails.CachedTokens
				}

				costBreakdown := p.CalculateCost(tokensIn, tokensOut, cachedTokens)
				finalChunk.CostInfo = &costBreakdown
			}

			outChan <- finalChunk
			return
		}
	}

	if err := scanner.Err(); err != nil {
		outChan <- StreamChunk{
			Content:      accumulated,
			ToolCalls:    accumulatedToolCalls,
			Error:        err,
			FinishReason: ptr("error"),
		}
	}
}

// SupportsStreaming returns true for OpenAI
func (p *OpenAIProvider) SupportsStreaming() bool {
	return true
}
