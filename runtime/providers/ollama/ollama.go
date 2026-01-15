// Package ollama provides Ollama LLM provider integration for local development.
package ollama

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
	ollamaChatCompletionsPath = "/v1/chat/completions"
	contentTypeHeader         = "Content-Type"
	applicationJSON           = "application/json"
)

// Provider implements the Provider interface for Ollama
type Provider struct {
	providers.BaseProvider
	model     string
	baseURL   string
	keepAlive string // Ollama-specific: how long to keep model loaded (e.g., "5m")
	defaults  providers.ProviderDefaults
}

// Default timeout for Ollama requests (longer for local inference)
const ollamaHTTPTimeout = 120 * time.Second

// NewProvider creates a new Ollama provider
func NewProvider(
	id, model, baseURL string,
	defaults providers.ProviderDefaults,
	includeRawOutput bool,
	additionalConfig map[string]any,
) *Provider {
	// Ollama doesn't require API keys - create base provider without key lookup
	client := &http.Client{Timeout: ollamaHTTPTimeout}
	base := providers.NewBaseProvider(id, includeRawOutput, client)

	// Extract keep_alive from additional config
	keepAlive := ""
	if additionalConfig != nil {
		if ka, ok := additionalConfig["keep_alive"].(string); ok {
			keepAlive = ka
		}
	}

	return &Provider{
		BaseProvider: base,
		model:        model,
		baseURL:      baseURL,
		keepAlive:    keepAlive,
		defaults:     defaults,
	}
}

// Model returns the model name/identifier used by this provider.
func (p *Provider) Model() string {
	return p.model
}

// Ollama API request/response structures (OpenAI-compatible format)
type ollamaRequest struct {
	Model       string          `json:"model"`
	Messages    []ollamaMessage `json:"messages"`
	Temperature float32         `json:"temperature"`
	TopP        float32         `json:"top_p"`
	MaxTokens   int             `json:"max_tokens"`
	Seed        *int            `json:"seed,omitempty"`
	Stream      bool            `json:"stream"`
	KeepAlive   string          `json:"keep_alive,omitempty"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // Can be string or []any for multimodal
}

type ollamaResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []ollamaChoice `json:"choices"`
	Usage   ollamaUsage    `json:"usage"`
	Error   *ollamaError   `json:"error,omitempty"`
}

type ollamaChoice struct {
	Index        int           `json:"index"`
	Message      ollamaMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type ollamaUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ollamaError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// prepareMessages converts predict request messages to Ollama format with system message
func (p *Provider) prepareMessages(req providers.PredictionRequest) ([]ollamaMessage, error) {
	messages := make([]ollamaMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, ollamaMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	// Convert each message, handling both legacy text and multimodal Parts
	for i := range req.Messages {
		converted, err := p.convertMessageToOllama(&req.Messages[i])
		if err != nil {
			return nil, fmt.Errorf("failed to convert message: %w", err)
		}
		messages = append(messages, converted)
	}

	return messages, nil
}

// applyRequestDefaults applies provider defaults to zero-valued request parameters
func (p *Provider) applyRequestDefaults(
	req providers.PredictionRequest,
) (temperature, topP float32, maxTokens int) {
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

// Predict sends a predict request to Ollama
func (p *Provider) Predict(
	ctx context.Context,
	req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	// Convert messages to Ollama format
	messages, err := p.prepareMessages(req)
	if err != nil {
		return providers.PredictionResponse{}, fmt.Errorf("failed to prepare messages: %w", err)
	}

	// Delegate to the common implementation
	return p.predictWithMessages(ctx, req, messages)
}

// CalculateCost calculates cost breakdown - Ollama is free (local inference)
func (p *Provider) CalculateCost(tokensIn, tokensOut, cachedTokens int) types.CostInfo {
	// Ollama is free local inference - no cost
	return types.CostInfo{
		InputTokens:   tokensIn - cachedTokens,
		OutputTokens:  tokensOut,
		CachedTokens:  cachedTokens,
		InputCostUSD:  0,
		OutputCostUSD: 0,
		CachedCostUSD: 0,
		TotalCost:     0,
	}
}

// PredictStream streams a predict response from Ollama
func (p *Provider) PredictStream(
	ctx context.Context,
	req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	// Convert messages to Ollama format
	messages, err := p.prepareMessages(req)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare messages: %w", err)
	}

	// Delegate to the common implementation
	return p.predictStreamWithMessages(ctx, req, messages)
}

// ollamaStreamChunk represents the structure of Ollama streaming response chunks
type ollamaStreamChunk struct {
	Choices []ollamaStreamChoice `json:"choices"`
	Usage   *ollamaUsage         `json:"usage,omitempty"`
}

type ollamaStreamChoice struct {
	Delta        ollamaStreamDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason"`
}

type ollamaStreamDelta struct {
	Content   string                 `json:"content"`
	ToolCalls []ollamaStreamToolCall `json:"tool_calls,omitempty"`
}

type ollamaStreamToolCall struct {
	Index    int                      `json:"index"`
	ID       string                   `json:"id,omitempty"`
	Type     string                   `json:"type,omitempty"`
	Function ollamaStreamToolFunction `json:"function,omitempty"`
}

type ollamaStreamToolFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// processToolCallDeltas accumulates tool call data from streaming deltas
func processToolCallDeltas(
	accumulatedToolCalls *[]types.MessageToolCall,
	toolCallDeltas []ollamaStreamToolCall,
) {
	for _, tcDelta := range toolCallDeltas {
		// Ensure we have enough slots
		for len(*accumulatedToolCalls) <= tcDelta.Index {
			*accumulatedToolCalls = append(*accumulatedToolCalls, types.MessageToolCall{})
		}

		tc := &(*accumulatedToolCalls)[tcDelta.Index]

		if tcDelta.ID != "" {
			tc.ID = tcDelta.ID
		}
		if tcDelta.Function.Name != "" {
			tc.Name = tcDelta.Function.Name
		}
		if tcDelta.Function.Arguments != "" {
			tc.Args = append(tc.Args, []byte(tcDelta.Function.Arguments)...)
		}
	}
}

// createFinalStreamChunk creates the final chunk with usage and cost information
func (p *Provider) createFinalStreamChunk(
	accumulated string,
	accumulatedToolCalls []types.MessageToolCall,
	totalTokens int,
	finishReason *string,
	usage *ollamaUsage,
) providers.StreamChunk {
	finalChunk := providers.StreamChunk{
		Content:      accumulated,
		ToolCalls:    accumulatedToolCalls,
		TokenCount:   totalTokens,
		FinishReason: finishReason,
	}

	if usage != nil {
		costBreakdown := p.CalculateCost(usage.PromptTokens, usage.CompletionTokens, 0)
		finalChunk.CostInfo = &costBreakdown
	}

	return finalChunk
}

// streamResponse reads SSE stream from Ollama and sends chunks
func (p *Provider) streamResponse(
	ctx context.Context,
	body io.ReadCloser,
	outChan chan<- providers.StreamChunk,
) {
	defer close(outChan)
	defer body.Close()

	scanner := providers.NewSSEScanner(body)
	accumulated := ""
	totalTokens := 0
	var accumulatedToolCalls []types.MessageToolCall

	for scanner.Scan() {
		if p.handleContextCancellation(ctx, accumulated, accumulatedToolCalls, outChan) {
			return
		}

		data := scanner.Data()
		if data == "[DONE]" {
			p.sendDoneChunk(accumulated, accumulatedToolCalls, totalTokens, outChan)
			return
		}

		chunk, ok := p.parseStreamChunk(data)
		if !ok || len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		accumulated, totalTokens = p.processStreamChoice(
			choice, accumulated, totalTokens,
			&accumulatedToolCalls, chunk.Usage, outChan,
		)
		if choice.FinishReason != nil {
			return
		}
	}

	if err := scanner.Err(); err != nil {
		outChan <- providers.StreamChunk{
			Content:      accumulated,
			ToolCalls:    accumulatedToolCalls,
			Error:        err,
			FinishReason: providers.StringPtr("error"),
		}
	}
}

// handleContextCancellation checks for context cancellation and sends appropriate chunk
func (p *Provider) handleContextCancellation(
	ctx context.Context,
	accumulated string,
	accumulatedToolCalls []types.MessageToolCall,
	outChan chan<- providers.StreamChunk,
) bool {
	select {
	case <-ctx.Done():
		outChan <- providers.StreamChunk{
			Content:      accumulated,
			ToolCalls:    accumulatedToolCalls,
			Error:        ctx.Err(),
			FinishReason: providers.StringPtr("canceled"),
		}
		return true
	default:
		return false
	}
}

// sendDoneChunk sends the final done chunk
func (p *Provider) sendDoneChunk(
	accumulated string,
	accumulatedToolCalls []types.MessageToolCall,
	totalTokens int,
	outChan chan<- providers.StreamChunk,
) {
	outChan <- providers.StreamChunk{
		Content:      accumulated,
		ToolCalls:    accumulatedToolCalls,
		TokenCount:   totalTokens,
		FinishReason: providers.StringPtr("stop"),
	}
}

// parseStreamChunk parses a stream chunk from JSON
func (p *Provider) parseStreamChunk(data string) (ollamaStreamChunk, bool) {
	var chunk ollamaStreamChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return chunk, false
	}
	return chunk, true
}

// processStreamChoice processes a single stream choice and returns updated state
func (p *Provider) processStreamChoice(
	choice ollamaStreamChoice,
	accumulated string,
	totalTokens int,
	accumulatedToolCalls *[]types.MessageToolCall,
	usage *ollamaUsage,
	outChan chan<- providers.StreamChunk,
) (newAccumulated string, newTotalTokens int) {
	// Handle content delta
	if choice.Delta.Content != "" {
		accumulated += choice.Delta.Content
		totalTokens++

		outChan <- providers.StreamChunk{
			Content:     accumulated,
			Delta:       choice.Delta.Content,
			ToolCalls:   *accumulatedToolCalls,
			TokenCount:  totalTokens,
			DeltaTokens: 1,
		}
	}

	// Handle tool call deltas
	if len(choice.Delta.ToolCalls) > 0 {
		processToolCallDeltas(accumulatedToolCalls, choice.Delta.ToolCalls)
		outChan <- providers.StreamChunk{
			Content:     accumulated,
			ToolCalls:   *accumulatedToolCalls,
			TokenCount:  totalTokens,
			DeltaTokens: 0,
		}
	}

	// Handle finish reason
	if choice.FinishReason != nil {
		finalChunk := p.createFinalStreamChunk(
			accumulated, *accumulatedToolCalls, totalTokens, choice.FinishReason, usage,
		)
		outChan <- finalChunk
	}

	return accumulated, totalTokens
}

// extractContentString extracts text content from Ollama's response content
// which can be either a string or an array of content parts
func extractContentString(content any) string {
	if str, ok := content.(string); ok {
		return str
	}

	if parts, ok := content.([]any); ok {
		return extractTextFromParts(parts)
	}

	return ""
}

// extractTextFromParts extracts text from an array of content parts
func extractTextFromParts(parts []any) string {
	var text string
	for _, part := range parts {
		if textVal := getTextFromPart(part); textVal != "" {
			text += textVal
		}
	}
	return text
}

// getTextFromPart extracts text from a single content part
func getTextFromPart(part any) string {
	partMap, ok := part.(map[string]any)
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

// predictWithMessages is the core implementation that accepts pre-converted messages
func (p *Provider) predictWithMessages(
	ctx context.Context,
	req providers.PredictionRequest,
	messages []ollamaMessage,
) (providers.PredictionResponse, error) {
	// Enrich context with provider and model info for logging
	ctx = logger.WithLoggingContext(ctx, &logger.LoggingFields{
		Provider: p.ID(),
		Model:    p.model,
	})

	start := time.Now()

	// Apply provider defaults for zero values
	temperature, topP, maxTokens := p.applyRequestDefaults(req)

	// Create request
	ollamaReq := ollamaRequest{
		Model:       p.model,
		Messages:    messages,
		Temperature: temperature,
		TopP:        topP,
		MaxTokens:   maxTokens,
		Seed:        req.Seed,
		Stream:      false,
		KeepAlive:   p.keepAlive,
	}

	reqBody, err := json.Marshal(ollamaReq)
	if err != nil {
		return providers.PredictionResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Prepare response with raw request if configured (set early to preserve on error)
	predictResp := providers.PredictionResponse{
		Latency: time.Since(start), // Will be updated at the end
	}
	if p.ShouldIncludeRawOutput() {
		predictResp.RawRequest = ollamaReq
	}

	// Make HTTP request - Ollama doesn't require Authorization header
	url := p.baseURL + ollamaChatCompletionsPath
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return predictResp, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)

	logger.APIRequest("Ollama", "POST", url, map[string]string{
		contentTypeHeader: applicationJSON,
	}, ollamaReq)

	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		predictResp.Latency = time.Since(start)
		return predictResp, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		predictResp.Latency = time.Since(start)
		return predictResp, fmt.Errorf("failed to read response body: %w", err)
	}

	logger.APIResponse("Ollama", resp.StatusCode, string(respBody), nil)

	if resp.StatusCode != http.StatusOK {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return predictResp, fmt.Errorf(
			"ollama API request to %s failed with status %d: %s", url, resp.StatusCode, string(respBody),
		)
	}

	var ollamaResp ollamaResponse
	if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return predictResp, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if ollamaResp.Error != nil {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return predictResp, fmt.Errorf("ollama API error: %s", ollamaResp.Error.Message)
	}

	if len(ollamaResp.Choices) == 0 {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return predictResp, fmt.Errorf("no choices in response")
	}

	latency := time.Since(start)

	// Calculate cost breakdown (free for Ollama)
	costBreakdown := p.CalculateCost(
		ollamaResp.Usage.PromptTokens, ollamaResp.Usage.CompletionTokens, 0,
	)

	// Extract content - can be string or array of content parts
	content := extractContentString(ollamaResp.Choices[0].Message.Content)

	predictResp.Content = content
	predictResp.CostInfo = &costBreakdown
	predictResp.Latency = latency
	predictResp.Raw = respBody

	return predictResp, nil
}

// predictStreamWithMessages is the streaming implementation that accepts pre-converted messages
func (p *Provider) predictStreamWithMessages(
	ctx context.Context,
	req providers.PredictionRequest,
	messages []ollamaMessage,
) (<-chan providers.StreamChunk, error) {
	// Enrich context with provider and model info for logging
	ctx = logger.WithLoggingContext(ctx, &logger.LoggingFields{
		Provider: p.ID(),
		Model:    p.model,
	})

	// Apply provider defaults for zero values
	temperature, topP, maxTokens := p.applyRequestDefaults(req)

	// Create streaming request
	ollamaReq := map[string]any{
		"model":       p.model,
		"messages":    messages,
		"temperature": temperature,
		"top_p":       topP,
		"max_tokens":  maxTokens,
		"stream":      true,
	}
	if req.Seed != nil {
		ollamaReq["seed"] = *req.Seed
	}
	if p.keepAlive != "" {
		ollamaReq["keep_alive"] = p.keepAlive
	}

	reqBody, err := json.Marshal(ollamaReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request - Ollama doesn't require Authorization header
	url := p.baseURL + ollamaChatCompletionsPath
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	httpReq.Header.Set("Accept", "text/event-stream")

	//nolint:bodyclose // body is closed in streamResponse goroutine
	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if err := providers.CheckHTTPError(resp, url); err != nil {
		return nil, err
	}

	outChan := make(chan providers.StreamChunk)

	go p.streamResponse(ctx, resp.Body, outChan)

	return outChan, nil
}

// SupportsStreaming is provided by BaseProvider (returns true)
