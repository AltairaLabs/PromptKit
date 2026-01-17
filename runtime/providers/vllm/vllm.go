// Package vllm provides vLLM LLM provider integration for high-performance inference.
package vllm

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
	vllmChatCompletionsPath = "/v1/chat/completions"
	contentTypeHeader       = "Content-Type"
	applicationJSON         = "application/json"
	tokensPerThousand       = 1000.0 // divisor for per-1K pricing
)

// Provider implements the Provider interface for vLLM
type Provider struct {
	providers.BaseProvider
	model            string
	baseURL          string
	apiKey           string // Optional - vLLM supports both auth and no-auth
	defaults         providers.ProviderDefaults
	additionalConfig map[string]any
}

// Default timeout for vLLM requests (configurable via additional_config)
const vllmHTTPTimeout = 120 * time.Second

// NewProvider creates a new vLLM provider
func NewProvider(
	id, model, baseURL string,
	defaults providers.ProviderDefaults,
	includeRawOutput bool,
	additionalConfig map[string]any,
) *Provider {
	// vLLM can optionally require API keys - check for it
	client := &http.Client{Timeout: vllmHTTPTimeout}
	base := providers.NewBaseProvider(id, includeRawOutput, client)

	// Extract optional API key from additional config
	apiKey := ""
	if additionalConfig != nil {
		if key, ok := additionalConfig["api_key"].(string); ok {
			apiKey = key
		}
	}

	return &Provider{
		BaseProvider:     base,
		model:            model,
		baseURL:          baseURL,
		apiKey:           apiKey,
		defaults:         defaults,
		additionalConfig: additionalConfig,
	}
}

// Model returns the model name/identifier used by this provider.
func (p *Provider) Model() string {
	return p.model
}

// vLLM API request/response structures (OpenAI-compatible format)
type vllmRequest struct {
	Model       string        `json:"model"`
	Messages    []vllmMessage `json:"messages"`
	Temperature float32       `json:"temperature"`
	TopP        float32       `json:"top_p"`
	MaxTokens   int           `json:"max_tokens"`
	Seed        *int          `json:"seed,omitempty"`
	Stream      bool          `json:"stream"`

	// vLLM-specific parameters
	UseBeamSearch     bool                   `json:"use_beam_search,omitempty"`
	BestOf            int                    `json:"best_of,omitempty"`
	IgnoreEOS         bool                   `json:"ignore_eos,omitempty"`
	SkipSpecialTokens bool                   `json:"skip_special_tokens,omitempty"`
	GuidedJSON        map[string]interface{} `json:"guided_json,omitempty"`
	GuidedRegex       string                 `json:"guided_regex,omitempty"`
	GuidedGrammar     string                 `json:"guided_grammar,omitempty"`
	GuidedChoice      []string               `json:"guided_choice,omitempty"`
}

type vllmMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // Can be string or []any for multimodal
}

type vllmResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []vllmChoice `json:"choices"`
	Usage   vllmUsage    `json:"usage"`
	Error   *vllmError   `json:"error,omitempty"`
}

type vllmChoice struct {
	Index        int         `json:"index"`
	Message      vllmMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type vllmUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type vllmError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// prepareMessages converts prediction request messages to vLLM format with system message
//
//nolint:unparam // error return for consistency with other providers
func (p *Provider) prepareMessages(req *providers.PredictionRequest) ([]vllmMessage, error) {
	messages := make([]vllmMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, vllmMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	// Convert each message
	for i := range req.Messages {
		messages = append(messages, vllmMessage{
			Role:    req.Messages[i].Role,
			Content: req.Messages[i].GetContent(),
		})
	}

	return messages, nil
}

// applyRequestDefaults applies provider defaults to zero-valued request parameters
func (p *Provider) applyRequestDefaults(
	req *providers.PredictionRequest,
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

// buildRequest creates a vLLM request with optional vLLM-specific parameters
//
//nolint:gocognit // complexity from vLLM-specific parameter extraction
func (p *Provider) buildRequest(
	req *providers.PredictionRequest,
	messages []vllmMessage,
	temperature, topP float32,
	maxTokens int,
	stream bool,
) *vllmRequest {
	vllmReq := &vllmRequest{
		Model:       p.model,
		Messages:    messages,
		Temperature: temperature,
		TopP:        topP,
		MaxTokens:   maxTokens,
		Seed:        req.Seed,
		Stream:      stream,
	}

	// Apply vLLM-specific configuration from additional_config
	if p.additionalConfig != nil {
		if useBeamSearch, ok := p.additionalConfig["use_beam_search"].(bool); ok {
			vllmReq.UseBeamSearch = useBeamSearch
		}
		if bestOf, ok := p.additionalConfig["best_of"].(int); ok {
			vllmReq.BestOf = bestOf
		}
		if ignoreEOS, ok := p.additionalConfig["ignore_eos"].(bool); ok {
			vllmReq.IgnoreEOS = ignoreEOS
		}
		if skipSpecialTokens, ok := p.additionalConfig["skip_special_tokens"].(bool); ok {
			vllmReq.SkipSpecialTokens = skipSpecialTokens
		}
		if guidedJSON, ok := p.additionalConfig["guided_json"].(map[string]interface{}); ok {
			vllmReq.GuidedJSON = guidedJSON
		}
		if guidedRegex, ok := p.additionalConfig["guided_regex"].(string); ok {
			vllmReq.GuidedRegex = guidedRegex
		}
		if guidedGrammar, ok := p.additionalConfig["guided_grammar"].(string); ok {
			vllmReq.GuidedGrammar = guidedGrammar
		}
		if guidedChoice, ok := p.additionalConfig["guided_choice"].([]string); ok {
			vllmReq.GuidedChoice = guidedChoice
		}
	}

	return vllmReq
}

// Predict sends a prediction request to vLLM
//
//nolint:gocritic // req size matches Provider interface
func (p *Provider) Predict(
	ctx context.Context,
	req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	// Convert messages to vLLM format
	messages, err := p.prepareMessages(&req)
	if err != nil {
		return providers.PredictionResponse{}, fmt.Errorf("failed to prepare messages: %w", err)
	}

	// Delegate to the common implementation
	return p.predictWithMessages(ctx, req, messages)
}

// CalculateCost calculates cost breakdown
// vLLM is typically self-hosted, so default is $0 unless custom pricing is configured
func (p *Provider) CalculateCost(tokensIn, tokensOut, cachedTokens int) types.CostInfo {
	// Default: free for self-hosted
	inputCost := 0.0
	outputCost := 0.0

	// Use configured pricing if available
	if p.defaults.Pricing.InputCostPer1K > 0 || p.defaults.Pricing.OutputCostPer1K > 0 {
		inputCost = float64(tokensIn-cachedTokens) * p.defaults.Pricing.InputCostPer1K / tokensPerThousand
		outputCost = float64(tokensOut) * p.defaults.Pricing.OutputCostPer1K / tokensPerThousand
	}

	return types.CostInfo{
		InputTokens:   tokensIn - cachedTokens,
		OutputTokens:  tokensOut,
		CachedTokens:  cachedTokens,
		InputCostUSD:  inputCost,
		OutputCostUSD: outputCost,
		CachedCostUSD: 0,
		TotalCost:     inputCost + outputCost,
	}
}

// PredictStream streams a prediction response from vLLM
//
//nolint:gocritic // req size matches Provider interface
func (p *Provider) PredictStream(
	ctx context.Context,
	req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	// Convert messages to vLLM format
	messages, err := p.prepareMessages(&req)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare messages: %w", err)
	}

	// Delegate to the common implementation
	return p.predictStreamWithMessages(ctx, req, messages)
}

// predictWithMessages is the core implementation that accepts pre-converted messages
//
//nolint:gocritic // req size matches internal pattern, consistent with other provider methods
func (p *Provider) predictWithMessages(
	ctx context.Context,
	req providers.PredictionRequest,
	messages []vllmMessage,
) (providers.PredictionResponse, error) {
	// Enrich context with provider and model info for logging
	ctx = logger.WithLoggingContext(ctx, &logger.LoggingFields{
		Provider: p.ID(),
		Model:    p.model,
	})

	start := time.Now()

	// Apply provider defaults for zero values
	temperature, topP, maxTokens := p.applyRequestDefaults(&req)

	// Build request with vLLM-specific parameters
	vllmReq := p.buildRequest(&req, messages, temperature, topP, maxTokens, false)

	reqBody, err := json.Marshal(vllmReq)
	if err != nil {
		return providers.PredictionResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Prepare response with raw request if configured
	predictResp := providers.PredictionResponse{
		Latency: time.Since(start),
	}
	if p.ShouldIncludeRawOutput() {
		predictResp.RawRequest = vllmReq
	}

	// Make HTTP request
	url := p.baseURL + vllmChatCompletionsPath
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return predictResp, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)

	// Add optional authentication
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	logger.APIRequest("vLLM", "POST", url, map[string]string{
		contentTypeHeader: applicationJSON,
	}, vllmReq)

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

	logger.APIResponse("vLLM", resp.StatusCode, string(respBody), nil)

	if resp.StatusCode != http.StatusOK {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return predictResp, fmt.Errorf(
			"vLLM API request to %s failed with status %d: %s", url, resp.StatusCode, string(respBody),
		)
	}

	var vllmResp vllmResponse
	if err := json.Unmarshal(respBody, &vllmResp); err != nil {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return predictResp, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if vllmResp.Error != nil {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return predictResp, fmt.Errorf("vLLM API error: %s", vllmResp.Error.Message)
	}

	if len(vllmResp.Choices) == 0 {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return predictResp, fmt.Errorf("no choices in response")
	}

	latency := time.Since(start)

	// Calculate cost breakdown
	costBreakdown := p.CalculateCost(
		vllmResp.Usage.PromptTokens, vllmResp.Usage.CompletionTokens, 0,
	)

	// Extract content
	content := extractContentString(vllmResp.Choices[0].Message.Content)

	predictResp.Content = content
	predictResp.CostInfo = &costBreakdown
	predictResp.Latency = latency
	predictResp.Raw = respBody

	return predictResp, nil
}

// extractContentString extracts text content from vLLM's response
func extractContentString(content any) string {
	if str, ok := content.(string); ok {
		return str
	}
	return ""
}

// predictStreamWithMessages is the streaming implementation
//
//nolint:gocritic // req size matches internal pattern, consistent with other provider methods
func (p *Provider) predictStreamWithMessages(
	ctx context.Context,
	req providers.PredictionRequest,
	messages []vllmMessage,
) (<-chan providers.StreamChunk, error) {
	// Enrich context with provider and model info for logging
	ctx = logger.WithLoggingContext(ctx, &logger.LoggingFields{
		Provider: p.ID(),
		Model:    p.model,
	})

	// Apply provider defaults for zero values
	temperature, topP, maxTokens := p.applyRequestDefaults(&req)

	// Build request with vLLM-specific parameters
	vllmReq := p.buildRequest(&req, messages, temperature, topP, maxTokens, true)

	reqBody, err := json.Marshal(vllmReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request
	url := p.baseURL + vllmChatCompletionsPath
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	httpReq.Header.Set("Accept", "text/event-stream")

	// Add optional authentication
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

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

// streamResponse reads SSE stream from vLLM and sends chunks
func (p *Provider) streamResponse(
	ctx context.Context,
	body io.ReadCloser,
	outChan chan<- providers.StreamChunk,
) {
	defer close(outChan)
	defer body.Close()

	scanner := providers.NewSSEScanner(body)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			outChan <- providers.StreamChunk{Error: ctx.Err()}
			return
		default:
		}

		data := scanner.Data()
		if data == "[DONE]" {
			return
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *vllmUsage `json:"usage,omitempty"`
		}

		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]

		// Send content chunk
		if choice.Delta.Content != "" {
			outChan <- providers.StreamChunk{
				Content:     choice.Delta.Content,
				DeltaTokens: 1,
			}
		}

		// Send final chunk with usage info
		if choice.FinishReason != nil && chunk.Usage != nil {
			costInfo := p.CalculateCost(chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens, 0)
			outChan <- providers.StreamChunk{
				FinishReason: choice.FinishReason,
				CostInfo:     &costInfo,
			}
		}
	}

	if err := scanner.Err(); err != nil {
		outChan <- providers.StreamChunk{Error: err}
	}
}

// SupportsStreaming is provided by BaseProvider (returns true)
