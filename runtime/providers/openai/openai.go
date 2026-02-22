// Package openai provides OpenAI LLM provider integration.
package openai

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
	openAIPredictCompletionsPath = "/chat/completions"
	contentTypeHeader            = "Content-Type"
	applicationJSON              = "application/json"
	authorizationHeader          = "Authorization"
	bearerPrefix                 = "Bearer "
	httpClientTimeout            = 60 * time.Second
)

// Default pricing constants (GPT-4o pricing used as fallback for unknown models)
const (
	defaultInputCostPer1K  = 0.0025
	defaultOutputCostPer1K = 0.01
	defaultCachedCostPer1K = 0.00125
)

// isOSeriesModel checks if a model is an OpenAI o-series reasoning model.
// O-series models (o1, o3, o4, etc.) require max_completion_tokens instead of max_tokens.
func isOSeriesModel(model string) bool {
	// Check for o-series model prefixes: o1, o3, o4, etc.
	if len(model) >= 2 && model[0] == 'o' && model[1] >= '0' && model[1] <= '9' {
		return true
	}
	return false
}

// addMaxTokensToRequest adds the appropriate max tokens parameter to the request.
// O-series models use max_completion_tokens, others use max_tokens.
func addMaxTokensToRequest(req map[string]interface{}, model string, maxTokens int) {
	if isOSeriesModel(model) {
		req["max_completion_tokens"] = maxTokens
	} else {
		req["max_tokens"] = maxTokens
	}
}

// addSamplingParamsToRequest adds temperature and top_p to the request if the model supports them.
// O-series models don't support temperature or top_p parameters.
func addSamplingParamsToRequest(req map[string]interface{}, model string, temperature, topP float32) {
	if isOSeriesModel(model) {
		// O-series models don't support temperature or top_p
		return
	}
	req["temperature"] = temperature
	req["top_p"] = topP
}

// OpenAIProvider implements the Provider interface for OpenAI
type Provider struct {
	providers.BaseProvider
	model            string
	baseURL          string
	apiKey           string
	credential       providers.Credential
	defaults         providers.ProviderDefaults
	apiMode          APIMode
	additionalConfig map[string]any
	platform         string
	platformConfig   *providers.PlatformConfig
}

// NewProvider creates a new OpenAI provider
func NewProvider(id, model, baseURL string, defaults providers.ProviderDefaults, includeRawOutput bool) *Provider {
	return NewProviderWithConfig(id, model, baseURL, defaults, includeRawOutput, nil)
}

// NewProviderWithConfig creates a new OpenAI provider with additional configuration
func NewProviderWithConfig(
	id, model, baseURL string,
	defaults providers.ProviderDefaults,
	includeRawOutput bool,
	additionalConfig map[string]any,
) *Provider {
	base, apiKey := providers.NewBaseProviderWithAPIKey(id, includeRawOutput, "OPENAI_API_KEY", "OPENAI_TOKEN")

	return &Provider{
		BaseProvider:     base,
		model:            model,
		baseURL:          baseURL,
		apiKey:           apiKey,
		defaults:         defaults,
		apiMode:          getAPIMode(model, additionalConfig),
		additionalConfig: additionalConfig,
	}
}

// NewProviderWithCredential creates a new OpenAI provider with explicit credential.
func NewProviderWithCredential(
	id, model, baseURL string, defaults providers.ProviderDefaults,
	includeRawOutput bool, cred providers.Credential,
	platform string, platformConfig *providers.PlatformConfig,
) *Provider {
	return NewProviderWithCredentialAndConfig(
		id, model, baseURL, defaults, includeRawOutput, cred, nil, platform, platformConfig,
	)
}

// NewProviderWithCredentialAndConfig creates a new OpenAI provider with explicit credential and config.
func NewProviderWithCredentialAndConfig(
	id, model, baseURL string, defaults providers.ProviderDefaults,
	includeRawOutput bool, cred providers.Credential, additionalConfig map[string]any,
	platform string, platformConfig *providers.PlatformConfig,
) *Provider {
	client := &http.Client{Timeout: httpClientTimeout}
	base := providers.NewBaseProvider(id, includeRawOutput, client)

	// Extract API key from credential if it's an APIKeyCredential
	var apiKey string
	if cred != nil && cred.Type() == "api_key" {
		if akc, ok := cred.(interface{ APIKey() string }); ok {
			apiKey = akc.APIKey()
		}
	}

	return &Provider{
		BaseProvider:     base,
		model:            model,
		baseURL:          baseURL,
		apiKey:           apiKey,
		credential:       cred,
		defaults:         defaults,
		apiMode:          getAPIMode(model, additionalConfig),
		additionalConfig: additionalConfig,
		platform:         platform,
		platformConfig:   platformConfig,
	}
}

// applyAuth applies authentication to an HTTP request.
func (p *Provider) applyAuth(ctx context.Context, req *http.Request) error {
	if p.credential != nil {
		return p.credential.Apply(ctx, req)
	}
	// Legacy behavior: use apiKey directly
	if p.apiKey != "" {
		req.Header.Set(authorizationHeader, bearerPrefix+p.apiKey)
	}
	return nil
}

// Model returns the model name/identifier used by this provider.
func (p *Provider) Model() string {
	return p.model
}

// OpenAI API request/response structures
type openAIRequest struct {
	Model          string                `json:"model"`
	Messages       []openAIMessage       `json:"messages"`
	Temperature    float32               `json:"temperature"`
	TopP           float32               `json:"top_p"`
	MaxTokens      int                   `json:"max_tokens"`
	Seed           *int                  `json:"seed,omitempty"`
	ResponseFormat *openAIResponseFormat `json:"response_format,omitempty"`
}

// openAIResponseFormat specifies the response format for OpenAI API
type openAIResponseFormat struct {
	Type       string            `json:"type"` // "text", "json_object", or "json_schema"
	JSONSchema *openAIJSONSchema `json:"json_schema,omitempty"`
}

// openAIJSONSchema specifies a JSON schema for structured output
type openAIJSONSchema struct {
	Name   string      `json:"name"`
	Schema interface{} `json:"schema"`
	Strict bool        `json:"strict,omitempty"`
}

type openAIMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // Can be string or []interface{} for multimodal
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

// prepareOpenAIMessages converts predict request messages to OpenAI format with system message
func (p *Provider) prepareOpenAIMessages(req providers.PredictionRequest) ([]openAIMessage, error) {
	messages := make([]openAIMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, openAIMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	// Convert each message, handling both legacy text and multimodal Parts
	for i := range req.Messages {
		converted, err := p.convertMessageToOpenAI(req.Messages[i])
		if err != nil {
			return nil, fmt.Errorf("failed to convert message: %w", err)
		}
		messages = append(messages, converted)
	}

	return messages, nil
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

// convertResponseFormat converts provider ResponseFormat to OpenAI format
func (p *Provider) convertResponseFormat(rf *providers.ResponseFormat) *openAIResponseFormat {
	if rf == nil {
		return nil
	}

	result := &openAIResponseFormat{
		Type: string(rf.Type),
	}

	// Handle JSON schema format
	if rf.Type == providers.ResponseFormatJSONSchema && len(rf.JSONSchema) > 0 {
		// Parse the schema from raw JSON
		var schema interface{}
		if err := json.Unmarshal(rf.JSONSchema, &schema); err != nil {
			// If parsing fails, just use the raw JSON
			schema = rf.JSONSchema
		}

		schemaName := rf.SchemaName
		if schemaName == "" {
			schemaName = "response_schema"
		}

		result.JSONSchema = &openAIJSONSchema{
			Name:   schemaName,
			Schema: schema,
			Strict: rf.Strict,
		}
	}

	return result
}

// Predict sends a predict request to OpenAI
func (p *Provider) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	// Convert messages to OpenAI format
	messages, err := p.prepareOpenAIMessages(req)
	if err != nil {
		return providers.PredictionResponse{}, fmt.Errorf("failed to prepare messages: %w", err)
	}

	// Delegate to the common implementation
	return p.predictWithMessages(ctx, req, messages)
}

// CalculateCost calculates detailed cost breakdown including optional cached tokens
func (p *Provider) CalculateCost(tokensIn, tokensOut, cachedTokens int) types.CostInfo {
	var inputCostPer1K, outputCostPer1K, cachedCostPer1K float64

	// Use configured pricing if available
	if p.defaults.Pricing.InputCostPer1K > 0 && p.defaults.Pricing.OutputCostPer1K > 0 {
		inputCostPer1K = p.defaults.Pricing.InputCostPer1K
		outputCostPer1K = p.defaults.Pricing.OutputCostPer1K
		// Assume cached tokens cost 50% of input tokens
		cachedCostPer1K = inputCostPer1K * 0.5
	} else {
		// Fallback to hardcoded pricing with warning
		logger.Warn("No pricing configured, using fallback pricing", "provider", p.ID(), "model", p.model)

		switch p.model {
		case "gpt-4":
			inputCostPer1K = 0.03   // $0.03 per 1K input tokens
			outputCostPer1K = 0.06  // $0.06 per 1K output tokens
			cachedCostPer1K = 0.015 // $0.015 per 1K cached tokens (50% discount)
		case "gpt-4o-mini":
			inputCostPer1K = 0.00015   // $0.00015 per 1K input tokens
			outputCostPer1K = 0.0006   // $0.0006 per 1K output tokens
			cachedCostPer1K = 0.000075 // $0.000075 per 1K cached tokens (50% discount)
		case "gpt-3.5-turbo":
			inputCostPer1K = 0.0015   // $0.0015 per 1K input tokens
			outputCostPer1K = 0.002   // $0.002 per 1K output tokens
			cachedCostPer1K = 0.00075 // $0.00075 per 1K cached tokens (50% discount)
		case "gpt-4o":
			fallthrough // Use default GPT-4o pricing
		default:
			// Default to GPT-4o pricing for unknown models
			inputCostPer1K = defaultInputCostPer1K
			outputCostPer1K = defaultOutputCostPer1K
			cachedCostPer1K = defaultCachedCostPer1K
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

// PredictStream streams a predict response from OpenAI
func (p *Provider) PredictStream(ctx context.Context, req providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	// Convert messages to OpenAI format
	messages, err := p.prepareOpenAIMessages(req)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare messages: %w", err)
	}

	// Delegate to the common implementation
	return p.predictStreamWithMessages(ctx, req, messages)
}

// openAIStreamChunk represents the structure of OpenAI streaming response chunks
type openAIStreamChunk struct {
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
	Usage *openAIUsage `json:"usage,omitempty"`
}

// processToolCallDeltas accumulates tool call data from streaming deltas
func processToolCallDeltas(accumulatedToolCalls *[]types.MessageToolCall, toolCallDeltas []struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function,omitempty"`
}) {
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
func (p *Provider) createFinalStreamChunk(accumulated string, accumulatedToolCalls []types.MessageToolCall, totalTokens int, finishReason *string, usage *openAIUsage) providers.StreamChunk {
	finalChunk := providers.StreamChunk{
		Content:      accumulated,
		ToolCalls:    accumulatedToolCalls,
		TokenCount:   totalTokens,
		FinishReason: finishReason,
	}

	if usage != nil {
		tokensIn := usage.PromptTokens
		tokensOut := usage.CompletionTokens
		cachedTokens := 0
		if usage.PromptTokensDetails != nil {
			cachedTokens = usage.PromptTokensDetails.CachedTokens
		}

		costBreakdown := p.CalculateCost(tokensIn, tokensOut, cachedTokens)
		finalChunk.CostInfo = &costBreakdown
	}

	return finalChunk
}

// streamResponse reads SSE stream from OpenAI and sends chunks
func (p *Provider) streamResponse(ctx context.Context, body io.ReadCloser, outChan chan<- providers.StreamChunk) {
	defer close(outChan)
	defer body.Close()

	scanner := providers.NewSSEScanner(body)
	accumulated := ""
	totalTokens := 0
	var accumulatedToolCalls []types.MessageToolCall

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			outChan <- providers.StreamChunk{
				Content:      accumulated,
				ToolCalls:    accumulatedToolCalls,
				Error:        ctx.Err(),
				FinishReason: providers.StringPtr("cancelled"),
			}
			return
		default:
		}

		data := scanner.Data()
		if data == "[DONE]" {
			outChan <- providers.StreamChunk{
				Content:      accumulated,
				ToolCalls:    accumulatedToolCalls,
				TokenCount:   totalTokens,
				FinishReason: providers.StringPtr("stop"),
			}
			return
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // Skip malformed chunks
		}

		// Handle usage-only chunk (sent when stream_options.include_usage is true)
		// This chunk has no choices but contains the final token counts
		if len(chunk.Choices) == 0 {
			if chunk.Usage != nil {
				// Send final chunk with usage data
				stopReason := providers.StringPtr("stop")
				finalChunk := p.createFinalStreamChunk(
					accumulated, accumulatedToolCalls, totalTokens, stopReason, chunk.Usage)
				outChan <- finalChunk
			}
			continue
		}

		choice := chunk.Choices[0]

		// Handle content delta
		if choice.Delta.Content != "" {
			accumulated += choice.Delta.Content
			totalTokens++

			outChan <- providers.StreamChunk{
				Content:     accumulated,
				Delta:       choice.Delta.Content,
				ToolCalls:   accumulatedToolCalls,
				TokenCount:  totalTokens,
				DeltaTokens: 1,
			}
		}

		// Handle tool call deltas
		if len(choice.Delta.ToolCalls) > 0 {
			processToolCallDeltas(&accumulatedToolCalls, choice.Delta.ToolCalls)
			outChan <- providers.StreamChunk{
				Content:     accumulated,
				ToolCalls:   accumulatedToolCalls,
				TokenCount:  totalTokens,
				DeltaTokens: 0,
			}
		}

		// Handle finish reason - don't return yet, wait for usage-only chunk
		// When stream_options.include_usage is true, usage comes in a separate chunk
		if choice.FinishReason != nil {
			// If usage is included in this chunk, send final chunk now
			if chunk.Usage != nil {
				finalChunk := p.createFinalStreamChunk(
					accumulated, accumulatedToolCalls, totalTokens, choice.FinishReason, chunk.Usage)
				outChan <- finalChunk
				return
			}
			// Otherwise, continue to wait for the usage-only chunk
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

// extractContentString extracts text content from OpenAI's response content
// which can be either a string or an array of content parts
func extractContentString(content interface{}) string {
	if str, ok := content.(string); ok {
		return str
	}

	if parts, ok := content.([]interface{}); ok {
		return extractTextFromParts(parts)
	}

	return ""
}

// extractTextFromParts extracts text from an array of content parts
func extractTextFromParts(parts []interface{}) string {
	var text string
	for _, part := range parts {
		if textVal := getTextFromPart(part); textVal != "" {
			text += textVal
		}
	}
	return text
}

// getTextFromPart extracts text from a single content part
func getTextFromPart(part interface{}) string {
	partMap, ok := part.(map[string]interface{})
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

// predictWithMessages is a refactored version of Predict that accepts pre-converted messages
func (p *Provider) predictWithMessages(ctx context.Context, req providers.PredictionRequest, messages []openAIMessage) (providers.PredictionResponse, error) {
	// Enrich context with provider and model info for logging
	ctx = logger.WithLoggingContext(ctx, &logger.LoggingFields{
		Provider: p.ID(),
		Model:    p.model,
	})

	start := time.Now()

	// Apply provider defaults for zero values
	temperature, topP, maxTokens := p.applyRequestDefaults(req)

	// Create request as a map for flexibility with o-series models
	openAIReq := map[string]interface{}{
		"model":    p.model,
		"messages": messages,
	}

	// Add modalities for audio models when audio content is present
	if p.apiMode == APIModeCompletions && isAudioModel(p.model) && requestContainsAudio(&req) {
		openAIReq["modalities"] = []string{"text", "audio"}
		// Audio models require audio output configuration
		openAIReq["audio"] = map[string]interface{}{
			"voice":  "alloy",
			"format": "wav",
		}
	}

	// Add max tokens with the correct parameter name for the model type
	addMaxTokensToRequest(openAIReq, p.model, maxTokens)
	// Add sampling parameters (temperature, top_p) if model supports them
	addSamplingParamsToRequest(openAIReq, p.model, temperature, topP)

	if req.Seed != nil {
		openAIReq["seed"] = *req.Seed
	}

	// Add response format if specified
	if req.ResponseFormat != nil {
		openAIReq["response_format"] = p.convertResponseFormat(req.ResponseFormat)
	}

	reqBody, err := json.Marshal(openAIReq)
	if err != nil {
		return providers.PredictionResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Prepare response with raw request if configured (set early to preserve on error)
	predictResp := providers.PredictionResponse{
		Latency: time.Since(start), // Will be updated at the end
	}
	if p.ShouldIncludeRawOutput() {
		predictResp.RawRequest = openAIReq
	}

	// Make HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+openAIPredictCompletionsPath, bytes.NewReader(reqBody))
	if err != nil {
		return predictResp, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	if authErr := p.applyAuth(ctx, httpReq); authErr != nil {
		return predictResp, fmt.Errorf("failed to apply authentication: %w", authErr)
	}

	client := &http.Client{Timeout: 30 * time.Second}

	logger.APIRequest("OpenAI", "POST", p.baseURL+openAIPredictCompletionsPath, map[string]string{
		contentTypeHeader:   applicationJSON,
		authorizationHeader: "***",
	}, openAIReq)

	resp, err := client.Do(httpReq)
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

	logger.APIResponse("OpenAI", resp.StatusCode, string(respBody), nil)

	if resp.StatusCode != http.StatusOK {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		if p.platform != "" {
			return predictResp, providers.ParsePlatformHTTPError(p.platform, resp.StatusCode, respBody)
		}
		return predictResp, fmt.Errorf("API request to %s failed with status %d: %s",
			p.baseURL+openAIPredictCompletionsPath, resp.StatusCode, string(respBody))
	}

	var openAIResp openAIResponse
	if err := json.Unmarshal(respBody, &openAIResp); err != nil {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return predictResp, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if openAIResp.Error != nil {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return predictResp, fmt.Errorf("OpenAI API error: %s", openAIResp.Error.Message)
	}

	if len(openAIResp.Choices) == 0 {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return predictResp, fmt.Errorf("no choices in response")
	}

	latency := time.Since(start)

	cachedTokens := 0
	if openAIResp.Usage.PromptTokensDetails != nil {
		cachedTokens = openAIResp.Usage.PromptTokensDetails.CachedTokens
	}

	// Calculate cost breakdown
	costBreakdown := p.CalculateCost(openAIResp.Usage.PromptTokens, openAIResp.Usage.CompletionTokens, cachedTokens)

	// Extract content - can be string or array of content parts
	content := extractContentString(openAIResp.Choices[0].Message.Content)

	predictResp.Content = content
	predictResp.CostInfo = &costBreakdown
	predictResp.Latency = latency
	predictResp.Raw = respBody

	return predictResp, nil
}

// predictStreamWithMessages is a refactored version of PredictStream that accepts pre-converted messages
func (p *Provider) predictStreamWithMessages(ctx context.Context, req providers.PredictionRequest, messages []openAIMessage) (<-chan providers.StreamChunk, error) {
	// Enrich context with provider and model info for logging
	ctx = logger.WithLoggingContext(ctx, &logger.LoggingFields{
		Provider: p.ID(),
		Model:    p.model,
	})

	// Apply provider defaults for zero values
	temperature, topP, maxTokens := p.applyRequestDefaults(req)

	// Create streaming request
	openAIReq := map[string]interface{}{
		"model":    p.model,
		"messages": messages,
		"stream":   true,
		"stream_options": map[string]interface{}{
			"include_usage": true,
		},
	}

	// Add modalities for audio models when audio content is present
	if p.apiMode == APIModeCompletions && isAudioModel(p.model) && requestContainsAudio(&req) {
		openAIReq["modalities"] = []string{"text", "audio"}
		// Audio models require audio output configuration
		openAIReq["audio"] = map[string]interface{}{
			"voice":  "alloy",
			"format": "wav",
		}
	}

	// Add max tokens with the correct parameter name for the model type
	addMaxTokensToRequest(openAIReq, p.model, maxTokens)
	// Add sampling parameters (temperature, top_p) if model supports them
	addSamplingParamsToRequest(openAIReq, p.model, temperature, topP)
	if req.Seed != nil {
		openAIReq["seed"] = *req.Seed
	}
	// Add response format if specified
	if req.ResponseFormat != nil {
		openAIReq["response_format"] = p.convertResponseFormat(req.ResponseFormat)
	}

	reqBody, err := json.Marshal(openAIReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+openAIPredictCompletionsPath, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	httpReq.Header.Set("Accept", "text/event-stream")
	if authErr := p.applyAuth(ctx, httpReq); authErr != nil {
		return nil, fmt.Errorf("failed to apply authentication: %w", authErr)
	}

	//nolint:bodyclose // body is closed in streamResponse goroutine
	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if err := providers.CheckHTTPError(resp, p.baseURL+openAIPredictCompletionsPath); err != nil {
		return nil, err
	}

	outChan := make(chan providers.StreamChunk)

	go p.streamResponse(ctx, resp.Body, outChan)

	return outChan, nil
}

// SupportsStreaming is provided by BaseProvider (returns true)
