// Package claude provides Anthropic Claude LLM provider integration.
package claude

import (
	"bytes"
	"context"
	"encoding/json"
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
	contentTypeHeader     = "Content-Type"
	applicationJSON       = "application/json"
	anthropicVersionValue = "2023-06-01"
	anthropicVersionKey   = "Anthropic-Version"
	anthropicAPIHost      = "api.anthropic.com"
	textDeltaType         = "text_delta"
	roleSystem            = "system"
	httpClientTimeout     = 60 * time.Second
)

// normalizeBaseURL ensures the baseURL includes the /v1 path for Anthropic's API.
// If the URL is the Anthropic API without /v1, it adds it.
// Mock server URLs (non-Anthropic hosts) are left unchanged.
func normalizeBaseURL(baseURL string) string {
	// Only modify if it's the Anthropic API host
	if strings.Contains(baseURL, anthropicAPIHost) {
		// Check if /v1 is already present
		if !strings.Contains(baseURL, "/v1") {
			return strings.TrimSuffix(baseURL, "/") + "/v1"
		}
	}
	return baseURL
}

// Bedrock constants
const (
	bedrockPlatform       = "bedrock"
	bedrockVersionValue   = "bedrock-2023-05-31"
	bedrockVersionBodyKey = "anthropic_version"
)

// Provider implements the Provider interface for Anthropic Claude
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

// NewProvider creates a new Claude provider
func NewProvider(id, model, baseURL string, defaults providers.ProviderDefaults, includeRawOutput bool) *Provider {
	base, apiKey := providers.NewBaseProviderWithAPIKey(id, includeRawOutput, "ANTHROPIC_API_KEY", "CLAUDE_API_KEY")

	return &Provider{
		BaseProvider: base,
		model:        model,
		baseURL:      normalizeBaseURL(baseURL),
		apiKey:       apiKey,
		defaults:     defaults,
	}
}

// NewProviderWithCredential creates a new Claude provider with explicit credential.
func NewProviderWithCredential(
	id, model, baseURL string, defaults providers.ProviderDefaults,
	includeRawOutput bool, cred providers.Credential,
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
		BaseProvider:   base,
		model:          model,
		baseURL:        normalizeBaseURL(baseURL),
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

// isBedrock returns true if this provider is hosted on AWS Bedrock.
func (p *Provider) isBedrock() bool {
	return p.platform == bedrockPlatform
}

// messagesURL returns the appropriate API endpoint URL.
// For Bedrock: {baseURL}/model/{model}/invoke
// For direct Anthropic API: {baseURL}/messages
func (p *Provider) messagesURL() string {
	if p.isBedrock() {
		return p.baseURL + "/model/" + p.model + "/invoke"
	}
	return p.baseURL + "/messages"
}

// marshalBedrockRequest converts a claudeRequest to JSON with Bedrock-specific fields.
// Bedrock expects anthropic_version in the body and does not use the model field in the body
// (the model is specified in the URL path).
func (p *Provider) marshalBedrockRequest(claudeReq *claudeRequest) ([]byte, error) {
	m := map[string]interface{}{
		bedrockVersionBodyKey: bedrockVersionValue,
		"max_tokens":          claudeReq.MaxTokens,
		"messages":            claudeReq.Messages,
	}
	if len(claudeReq.System) > 0 {
		m["system"] = claudeReq.System
	}
	if claudeReq.Temperature != 0 {
		m["temperature"] = claudeReq.Temperature
	}
	if claudeReq.TopP != 0 {
		m["top_p"] = claudeReq.TopP
	}
	return json.Marshal(m)
}

// applyAuth applies authentication to an HTTP request.
// Uses credential interface if available, falls back to legacy apiKey.
func (p *Provider) applyAuth(ctx context.Context, req *http.Request) error {
	if p.credential != nil {
		return p.credential.Apply(ctx, req)
	}
	// Legacy behavior: use apiKey directly
	if p.apiKey != "" {
		req.Header.Set("X-API-Key", p.apiKey)
	}
	return nil
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
	Source       *claudeImageSource  `json:"source,omitempty"` // For image content
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

// convertMessagesToClaudeFormat converts provider messages to Claude format with cache control.
// Handles both text-only and multimodal (image) messages inline.
func (p *Provider) convertMessagesToClaudeFormat(messages []types.Message) []claudeMessage {
	claudeMessages := make([]claudeMessage, 0, len(messages))
	minCharsForCaching := 2048 * 4 // ~8192 characters (Claude requires 2048 tokens minimum)

	for i := range messages {
		msg := &messages[i]

		// Skip system role messages - they should be in req.System parameter
		if msg.Role == roleSystem {
			continue
		}

		// Check if message has media content (images, audio, video)
		if msg.HasMediaContent() {
			// Use multimodal conversion path
			claudeMsg, err := p.convertMessageToClaudeMultimodal(*msg)
			if err != nil {
				// Fall back to text-only on conversion error
				logger.Warn("Failed to convert multimodal message, falling back to text", "error", err)
				textContent := msg.GetContent()
				claudeMessages = append(claudeMessages, claudeMessage{
					Role:    msg.Role,
					Content: []claudeContentBlock{{Type: "text", Text: textContent}},
				})
			} else {
				claudeMessages = append(claudeMessages, claudeMsg)
			}
			continue
		}

		// Text-only message
		textContent := msg.GetContent()
		contentBlock := claudeContentBlock{
			Type: "text",
			Text: textContent,
		}

		// Only cache the last message with sufficient content to maximize cache hits
		if p.supportsCaching() && i == len(messages)-1 && len(textContent) >= minCharsForCaching {
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
	// For Bedrock, marshal via a map so we can inject anthropic_version into the body
	var reqBody []byte
	var err error
	if p.isBedrock() {
		reqBody, err = p.marshalBedrockRequest(&claudeReq)
	} else {
		reqBody, err = json.Marshal(claudeReq)
	}
	if err != nil {
		return nil, predictResp, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := p.messagesURL()
	logger.Debug("Claude API request",
		"base_url", p.baseURL,
		"full_url", url,
		"model", p.model,
		"platform", p.platform,
		"has_api_key", p.apiKey != "")

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		predictResp.Latency = time.Since(start)
		return nil, predictResp, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	// Bedrock uses anthropic_version in the body, not as a header
	if !p.isBedrock() {
		httpReq.Header.Set(anthropicVersionKey, anthropicVersionValue)
	}

	// Apply authentication
	if authErr := p.applyAuth(ctx, httpReq); authErr != nil {
		predictResp.Latency = time.Since(start)
		return nil, predictResp, fmt.Errorf("failed to apply authentication: %w", authErr)
	}

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
		if p.isBedrock() {
			return nil, predictResp, parseBedrockHTTPError(resp.StatusCode, respBody)
		}
		return nil, predictResp, fmt.Errorf("API request to %s failed with status %d: %s",
			url, resp.StatusCode, string(respBody))
	}

	// Bedrock can return HTTP 200 with an error in the body (e.g. UnknownOperationException)
	if err := checkBedrockBodyError(respBody); err != nil {
		logger.Error("Bedrock body error on HTTP 200", "url", url, "error", err)
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return nil, predictResp, err
	}

	return respBody, predictResp, nil
}

// checkBedrockBodyError detects Bedrock errors returned with HTTP 200 status.
// Bedrock sometimes returns 200 with an error payload (e.g. UnknownOperationException).
func checkBedrockBodyError(body []byte) error {
	// Quick check: if body doesn't look like it might contain an exception, skip parsing
	if !strings.Contains(string(body), "Exception") {
		return nil
	}
	var errResp struct {
		Message string `json:"Message"`
		Type    string `json:"__type"`
	}
	if err := json.Unmarshal(body, &errResp); err != nil {
		return nil // Not a Bedrock error format
	}
	if errResp.Type != "" {
		return fmt.Errorf("bedrock error (%s): %s", errResp.Type, errResp.Message)
	}
	return nil
}

// parseBedrockHTTPError extracts a human-readable message from Bedrock HTTP error responses.
// Bedrock returns JSON like {"message":"..."} on HTTP 4xx/5xx. This extracts the message
// and returns a clear error prefixed with "bedrock:" to distinguish from direct API errors.
// Falls back to raw body if parsing fails.
func parseBedrockHTTPError(statusCode int, body []byte) error {
	return providers.ParsePlatformHTTPError("bedrock", statusCode, body)
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
	// Enrich context with provider and model info for logging
	ctx = logger.WithLoggingContext(ctx, &logger.LoggingFields{
		Provider: p.ID(),
		Model:    p.model,
	})

	start := time.Now()

	// Convert messages to Claude format
	messages := p.convertMessagesToClaudeFormat(req.Messages)

	// Apply provider defaults
	// Note: We ignore topP because Anthropic's newer models (Claude 4+) don't support both temperature and top_p
	temperature, _, maxTokens := p.applyDefaults(req.Temperature, req.TopP, req.MaxTokens)

	// Create system content blocks
	systemBlocks := p.createSystemBlocks(req.System)

	// Create request
	// Note: TopP is omitted to avoid the "cannot both be specified" error with newer Claude models
	claudeReq := claudeRequest{
		Model:       p.model,
		MaxTokens:   maxTokens,
		Messages:    messages,
		System:      systemBlocks,
		Temperature: temperature,
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
		logger.Warn("No pricing configured, using fallback pricing", "provider", p.ID(), "model", p.model)
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

// SupportsStreaming is provided by BaseProvider (returns true)
