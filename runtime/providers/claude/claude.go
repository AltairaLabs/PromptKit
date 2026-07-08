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
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
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

// Platform constants
const (
	bedrockPlatform       = "bedrock"
	bedrockVersionValue   = "bedrock-2023-05-31"
	vertexPlatform        = "vertex"
	vertexVersionValue    = "vertex-2023-10-16"
	azurePlatform         = "azure"
	bedrockVersionBodyKey = "anthropic_version"

	// defaultAzureAPIVersion is the api-version query parameter used when
	// the caller has not supplied one via PlatformConfig.AdditionalConfig.
	// Tracks the openai+azure default for consistency.
	defaultAzureAPIVersion = "2024-12-01-preview"
)

// vertexAnthropicEndpoint returns the Vertex AI base URL for Anthropic
// publisher models. The result ends in `/publishers/anthropic/models` (no
// trailing slash); per-call code appends `/{model}:rawPredict` (or
// `:streamRawPredict`) to address a specific model.
func vertexAnthropicEndpoint(region, project string) string {
	return fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/anthropic/models",
		region, project, region,
	)
}

// bedrockAnthropicEndpoint returns the AWS Bedrock Runtime base URL for the
// given region. The result has no trailing slash; per-call code appends
// `/model/{model}/invoke` (or `/invoke-with-response-stream`) to address a
// specific model. Returns an empty string if region is empty so callers
// can fall back to an explicit BaseURL.
func bedrockAnthropicEndpoint(region string) string {
	if region == "" {
		return ""
	}
	return fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", region)
}

// azureAnthropicDeploymentEndpoint returns the Azure AI Foundry base URL
// for an Anthropic deployment on an AIServices account. The result ends
// in `/openai/deployments/{deployment}` (no trailing slash); per-call
// code appends `/messages?api-version={v}` to address the Anthropic
// Messages API for that deployment.
//
// The path layout mirrors Azure OpenAI deployments
// (`{endpoint}/openai/deployments/{deployment}/chat/completions?api-version=v`)
// — Microsoft routes Foundry partner models through the same
// deployment-name surface, swapping only the trailing API path. The
// {deployment} segment is the deployment name the user assigned at
// deploy time (passed through via spec.Model, mirroring openai+azure).
//
// Note: this is a best-endeavors URL pattern; the AI Foundry
// deployment-style surface is the most-Microsoft-native shape but has
// not been verified end-to-end against a live deployment in this PR.
// The Anthropic-on-Foundry quota SKU is named
// `AIServices.GlobalStandard.claude-*`, strongly suggesting deployment
// via the AIServices account model surface (i.e. this layout) rather
// than the older Marketplace + Foundry-hub serverless-API path. First
// live run should validate; switching to a vendor-prefixed pattern
// (`{endpoint}/anthropic/v1/messages?...`) is a one-line change to
// `messagesURL`/`messagesStreamURL` if needed.
func azureAnthropicDeploymentEndpoint(endpoint, deployment string) string {
	return strings.TrimRight(endpoint, "/") + "/openai/deployments/" + deployment
}

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
	// unsupportedParams holds model parameters the configured model rejects
	// (e.g. Claude 4.7+ deprecated "temperature"). Populated from the spec.
	unsupportedParams map[string]bool
	// capabilities holds the declared capability set from the provider config.
	// When non-nil it is authoritative for multimodal support; nil falls back
	// to Claude's built-in defaults (images + documents, no audio/video).
	capabilities map[string]bool
	// thinkingBudget, when non-nil, enables Claude extended thinking with this
	// token budget. Set from additional_config.thinking_budget. nil = off.
	thinkingBudget *int
}

// setCapabilities records the declared capability set on the provider.
func (p *Provider) setCapabilities(capabilities []string) {
	p.capabilities = providers.CapabilitySet(capabilities)
}

// setUnsupportedParams records the model parameters that must be omitted from
// requests. A no-op for an empty list so the common case stays nil.
func (p *Provider) setUnsupportedParams(params []string) {
	if len(params) == 0 {
		return
	}
	p.unsupportedParams = make(map[string]bool, len(params))
	for _, name := range params {
		p.unsupportedParams[name] = true
	}
}

// paramSupported reports whether the named request parameter may be sent to the
// model. Parameters listed in the provider's UnsupportedParams are not.
func (p *Provider) paramSupported(name string) bool {
	return !p.unsupportedParams[name]
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
//
// When baseURL is empty the URL is derived from platformConfig for the
// platforms that support it: Vertex (`publishers/anthropic/models`) and
// Azure (`openai/deployments/{deployment}`). Callers that pass an
// explicit baseURL always win.
func NewProviderWithCredential(
	id, model, baseURL string, defaults providers.ProviderDefaults,
	includeRawOutput bool, cred providers.Credential,
	platform string, platformConfig *providers.PlatformConfig,
) *Provider {
	base, apiKey := providers.NewBaseProviderWithCredential(id, includeRawOutput, httpClientTimeout, cred)

	if baseURL == "" && platformConfig != nil {
		switch {
		case platform == vertexPlatform &&
			platformConfig.Region != "" && platformConfig.Project != "":
			baseURL = vertexAnthropicEndpoint(platformConfig.Region, platformConfig.Project)
		case platform == azurePlatform && platformConfig.Endpoint != "":
			baseURL = azureAnthropicDeploymentEndpoint(platformConfig.Endpoint, model)
		case platform == bedrockPlatform && platformConfig.Region != "":
			baseURL = bedrockAnthropicEndpoint(platformConfig.Region)
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

// isVertex returns true if this provider is hosted on Google Vertex AI's
// Anthropic partner endpoint.
func (p *Provider) isVertex() bool {
	return p.platform == vertexPlatform
}

// isAzure returns true if this provider is hosted on Azure AI Foundry's
// Anthropic deployment surface.
func (p *Provider) isAzure() bool {
	return p.platform == azurePlatform
}

// isPartnerHosted returns true when the provider sits behind a hyperscaler
// partner endpoint that uses the partner body shape (Bedrock or Vertex).
// These share three traits that distinguish them from the direct Anthropic
// API: the model identifier appears in the URL path (not the request body);
// the request body must include `anthropic_version` set to a platform-
// specific value; and authentication uses the resolved Credential (SigV4 /
// Bearer) rather than the x-api-key header.
//
// Azure is intentionally NOT in this set — Azure AI Foundry passes the
// Anthropic Messages API through verbatim (model field stays in the body,
// no anthropic_version), so only the URL and auth differ from the direct
// path. Azure shares the auth half via usesCredentialAuth().
func (p *Provider) isPartnerHosted() bool {
	return p.isBedrock() || p.isVertex()
}

// usesCredentialAuth returns true when authentication goes through the
// Credential interface (SigV4 for Bedrock, Bearer for Vertex/Azure)
// rather than the direct API's x-api-key header. This is the broader
// auth-side counterpart to isPartnerHosted().
func (p *Provider) usesCredentialAuth() bool {
	return p.isPartnerHosted() || p.isAzure()
}

// azureAPIVersion returns the api-version query parameter for Azure AI
// Foundry calls. Configurable via PlatformConfig.AdditionalConfig
// ("api_version"); falls back to defaultAzureAPIVersion.
func (p *Provider) azureAPIVersion() string {
	if p.platformConfig != nil {
		if v, ok := p.platformConfig.AdditionalConfig["api_version"].(string); ok && v != "" {
			return v
		}
	}
	return defaultAzureAPIVersion
}

// platformAnthropicVersion returns the anthropic_version body value for the
// current partner platform. Returns an empty string for the direct API
// path, where the version is sent as a header instead.
func (p *Provider) platformAnthropicVersion() string {
	switch {
	case p.isBedrock():
		return bedrockVersionValue
	case p.isVertex():
		return vertexVersionValue
	default:
		return ""
	}
}

// messagesURL returns the appropriate API endpoint URL.
// Bedrock: {baseURL}/model/{model}/invoke
// Vertex:  {baseURL}/{model}:rawPredict
// Azure:   {baseURL}/messages?api-version={v}        (baseURL is the
//
//	deployment URL `…/openai/deployments/{deployment}` built by
//	azureAnthropicDeploymentEndpoint; streaming uses the same path)
//
// Direct:  {baseURL}/messages
func (p *Provider) messagesURL() string {
	switch {
	case p.isBedrock():
		return p.baseURL + "/model/" + p.model + "/invoke"
	case p.isVertex():
		return p.baseURL + "/" + p.model + ":rawPredict"
	case p.isAzure():
		return p.baseURL + "/messages?api-version=" + p.azureAPIVersion()
	default:
		return p.baseURL + "/messages"
	}
}

// messagesStreamURL returns the appropriate streaming API endpoint URL.
// Bedrock: {baseURL}/model/{model}/invoke-with-response-stream  (binary event-stream)
// Vertex:  {baseURL}/{model}:streamRawPredict                   (SSE)
// Azure:   {baseURL}/messages?api-version={v}                   (SSE; same as messagesURL)
// Direct:  {baseURL}/messages                                   (SSE; same path as messagesURL)
func (p *Provider) messagesStreamURL() string {
	switch {
	case p.isBedrock():
		return p.baseURL + "/model/" + p.model + "/invoke-with-response-stream"
	case p.isVertex():
		return p.baseURL + "/" + p.model + ":streamRawPredict"
	case p.isAzure():
		return p.baseURL + "/messages?api-version=" + p.azureAPIVersion()
	default:
		return p.baseURL + "/messages"
	}
}

// marshalPartnerRequest produces the partner-platform (Bedrock/Vertex) body
// from the canonical request struct. It is the single partner marshaler for
// EVERY partner path — plain Predict, streaming, and the tool paths (streaming
// and non-streaming): the model and stream fields are dropped (the model lives
// in the URL path; the URL action signals streaming) and anthropic_version is
// injected. Every other field the builder set (system, temperature,
// output_config, tools, tool_choice) passes through unchanged, so partner
// requests can no longer silently drop a field the direct API honors (#1379).
func (p *Provider) marshalPartnerRequest(cr *claudeRequest) ([]byte, error) {
	raw, err := json.Marshal(cr)
	if err != nil {
		return nil, err
	}
	var reqMap map[string]any
	if err := json.Unmarshal(raw, &reqMap); err != nil {
		return nil, err
	}
	reqMap[bedrockVersionBodyKey] = p.platformAnthropicVersion()
	delete(reqMap, "model")
	delete(reqMap, "stream")
	return json.Marshal(reqMap)
}

// makeBedrockStreamingRequest sends a streaming HTTP request to Bedrock and returns the
// response body and a BedrockEventScanner. The caller owns the response body.
func (p *Provider) makeBedrockStreamingRequest(
	ctx context.Context, reqBody []byte,
) (io.ReadCloser, providers.StreamScanner, error) {
	url := p.messagesStreamURL()
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	httpReq.Header.Set("Accept", "application/vnd.amazon.eventstream")

	if authErr := p.applyAuth(ctx, httpReq); authErr != nil {
		return nil, nil, fmt.Errorf("failed to apply authentication: %w", authErr)
	}

	if hdrErr := p.ApplyCustomHeaders(httpReq); hdrErr != nil {
		return nil, nil, hdrErr
	}

	resp, err := p.GetStreamingHTTPClient().Do(httpReq)
	if err != nil {
		return nil, nil, &providers.ProviderTransportError{Cause: err, Provider: p.ID()}
	}

	if resp.StatusCode != http.StatusOK {
		body := providers.ReadErrorBody(resp.Body)
		_ = resp.Body.Close()
		return nil, nil, parseBedrockHTTPError(resp.StatusCode, body)
	}

	scanner := providers.NewBedrockEventScanner(resp.Body)
	return resp.Body, scanner, nil
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

// claudeRequest is the single canonical representation of an Anthropic
// /v1/messages body. Every path — non-streaming Predict, streaming
// PredictStream, and the tool builders — produces this one struct via
// buildBaseRequest and layers on its own deltas (Stream, Tools, ToolChoice).
// Messages is an interface because the no-tools paths use []claudeMessage
// (text/image content blocks) while the tool paths use []claudeToolMessage
// (interleaved tool_use/tool_result blocks); both marshal to {role, content}.
// Optional fields use omitempty so an unset delta is absent from the wire body.
type claudeRequest struct {
	Model        string               `json:"model"`
	MaxTokens    int                  `json:"max_tokens"`
	Messages     any                  `json:"messages"`
	System       []claudeContentBlock `json:"system,omitempty"`
	Temperature  float32              `json:"temperature,omitempty"`
	TopP         float32              `json:"top_p,omitempty"`
	OutputConfig *claudeOutputConfig  `json:"output_config,omitempty"`
	Thinking     *claudeThinking      `json:"thinking,omitempty"`
	Stream       bool                 `json:"stream,omitempty"`
	Tools        any                  `json:"tools,omitempty"`
	ToolChoice   any                  `json:"tool_choice,omitempty"`
}

// claudeThinking enables Claude extended thinking. BudgetTokens caps reasoning
// tokens (>= 1024) and must be below max_tokens (the answer needs headroom). With
// thinking enabled the API rejects a custom temperature, so callers omit it.
type claudeThinking struct {
	Type         string `json:"type"` // "enabled"
	BudgetTokens int    `json:"budget_tokens"`
}

// thinkingAnswerHeadroom is added above budget_tokens when the configured
// max_tokens leaves no room for an answer after reasoning.
const thinkingAnswerHeadroom = 1024

// claudeThinkingFor returns the thinking block to attach, or nil when extended
// thinking is not configured for this provider.
func (p *Provider) claudeThinkingFor() *claudeThinking {
	if p.thinkingBudget == nil {
		return nil
	}
	return &claudeThinking{Type: "enabled", BudgetTokens: *p.thinkingBudget}
}

// buildBaseRequest assembles the fields every Claude request shares: model,
// max_tokens (defaulted), gated temperature, and system blocks. Callers supply
// the already-converted messages and layer on output_config / stream / tools.
// This is the single source of truth #1379 consolidates the three former
// builders onto.
//
// Temperature uses the struct's omitempty: a resolved temperature of 0 is
// omitted on every path (the API then applies its own default). This unifies
// the previously divergent behavior where the map-based stream/tool builders
// sent temperature:0 while Predict omitted it.
//
// output_config is intentionally NOT set here: the no-tools callers
// (Predict/PredictStream) apply outputConfigFor themselves, and the tool paths
// deliberately omit structured outputs. Wiring structured outputs into the tool
// paths is a separate change, not a no-op refactor.
//
//nolint:gocritic // hugeParam: providers.PredictionRequest is passed by value across the provider interface
func (p *Provider) buildBaseRequest(req providers.PredictionRequest, messages any) claudeRequest {
	temperature, _, maxTokens := p.applyDefaults(req.Temperature, req.TopP, req.MaxTokens)
	cr := claudeRequest{
		Model:     p.model,
		MaxTokens: maxTokens,
		Messages:  messages,
		System:    p.createSystemBlocks(req.System),
	}
	if thinking := p.claudeThinkingFor(); thinking != nil {
		// Extended thinking: reasoning tokens count toward max_tokens, so ensure
		// headroom for an answer; and the API rejects a custom temperature, so we
		// leave it omitted (omitempty).
		cr.Thinking = thinking
		if cr.MaxTokens <= thinking.BudgetTokens {
			cr.MaxTokens = thinking.BudgetTokens + thinkingAnswerHeadroom
		}
	} else if p.paramSupported("temperature") {
		// Claude 4.7+ models reject temperature; only send it when supported.
		cr.Temperature = temperature
	}
	return cr
}

// claudeOutputConfig requests native structured outputs (GA, no beta header):
// the response is guaranteed valid JSON conforming to the schema, returned in
// content[0].text. https://platform.claude.com/docs/en/docs/build-with-claude/structured-outputs
type claudeOutputConfig struct {
	Format claudeOutputFormat `json:"format"`
}

type claudeOutputFormat struct {
	Type   string          `json:"type"`   // "json_schema"
	Schema json.RawMessage `json:"schema"` // the raw JSON schema
}

// outputConfigFor maps a provider ResponseFormat to Anthropic's output_config.
// Returns nil unless a JSON-schema response format with a schema is set.
func outputConfigFor(rf *providers.ResponseFormat) *claudeOutputConfig {
	if rf == nil || rf.Type != providers.ResponseFormatJSONSchema || len(rf.JSONSchema) == 0 {
		return nil
	}
	return &claudeOutputConfig{
		Format: claudeOutputFormat{Type: "json_schema", Schema: rf.JSONSchema},
	}
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

// cacheTypeEphemeral is the only cache_control type the Anthropic API supports.
const cacheTypeEphemeral = "ephemeral"

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
	// Thinking carries the reasoning text of a "thinking" block — Claude puts it in
	// a "thinking" field, NOT "text". Captured onto Message.Reasoning.
	Thinking string `json:"thinking,omitempty"`
	// Signature accompanies a thinking block; Data carries a redacted_thinking
	// payload. Both are opaque reasoning tokens captured for round-trip, never
	// displayed. See extractReasoning.
	Signature string `json:"signature,omitempty"`
	Data      string `json:"data,omitempty"`
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

// supportsCaching returns true if prompt caching should be enabled.
// All Anthropic models support caching by default; set DisablePromptCaching in
// ProviderDefaults to opt out (e.g. when the provider config sets prompt_caching: false).
func (p *Provider) supportsCaching() bool {
	return !p.defaults.DisablePromptCaching
}

// SupportsPromptCaching reports whether prompt caching is active for this
// provider (supported and not disabled by config). The pipeline uses this to
// decide whether a zero-cache-read tool loop is worth warning about, so
// providers without caching don't produce false "caching not engaging" warnings.
func (p *Provider) SupportsPromptCaching() bool {
	return p.supportsCaching()
}

// convertMessagesToClaudeFormat converts provider messages to Claude format with cache control.
// Handles both text-only and multimodal (image) messages inline.
func (p *Provider) convertMessagesToClaudeFormat(messages []types.Message) []claudeMessage {
	claudeMessages := make([]claudeMessage, 0, len(messages))
	minCharsForCaching := 2048 * 4 // ~8192 characters (Claude requires 2048 tokens minimum)

	for i := range messages {
		msg := &messages[i]

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
			contentBlock.CacheControl = &claudeCacheControl{Type: cacheTypeEphemeral}
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
		systemBlock.CacheControl = &claudeCacheControl{Type: cacheTypeEphemeral}
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
	// Partner-hosted (Bedrock/Vertex): inject anthropic_version and drop the
	// model field via the shared partner marshaler — the same transform every
	// other partner path uses, so output_config and any future field stay in.
	var reqBody []byte
	var err error
	if p.isPartnerHosted() {
		reqBody, err = p.marshalPartnerRequest(&claudeReq)
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
	// Partner-hosted (Bedrock/Vertex) puts anthropic_version in the body.
	// Azure pins the API contract via the api-version query parameter.
	// Only the direct API path uses the anthropic-version request header.
	if !p.isPartnerHosted() && !p.isAzure() {
		httpReq.Header.Set(anthropicVersionKey, anthropicVersionValue)
	}

	// Apply authentication
	if authErr := p.applyAuth(ctx, httpReq); authErr != nil {
		predictResp.Latency = time.Since(start)
		return nil, predictResp, fmt.Errorf("failed to apply authentication: %w", authErr)
	}

	if hdrErr := p.ApplyCustomHeaders(httpReq); hdrErr != nil {
		predictResp.Latency = time.Since(start)
		return nil, predictResp, hdrErr
	}

	logger.APIRequest("Claude", "POST", url, map[string]string{
		contentTypeHeader:   applicationJSON,
		"X-API-Key":         "***",
		anthropicVersionKey: anthropicVersionValue,
	}, claudeReq)

	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		predictResp.Latency = time.Since(start)
		return nil, predictResp, &providers.ProviderTransportError{Cause: err, Provider: p.ID()}
	}
	defer resp.Body.Close()

	respBody, err := providers.ReadResponseBody(resp.Body)
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
		switch {
		case p.isBedrock():
			return nil, predictResp, parseBedrockHTTPError(resp.StatusCode, respBody)
		case p.isVertex(), p.isAzure():
			return nil, predictResp, providers.ParsePlatformHTTPError(p.platform, resp.StatusCode, respBody)
		default:
			return nil, predictResp, &providers.ProviderHTTPError{
				StatusCode: resp.StatusCode, URL: url,
				Body: string(respBody), Provider: p.ID(),
			}
		}
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

	// Extract text content from response blocks; reasoning goes on Reasoning.
	var responseText string
	var parts []types.ContentPart
	for _, content := range claudeResp.Content {
		if content.Type == "text" {
			responseText = content.Text
			parts = append(parts, types.NewTextPart(content.Text))
		}
	}
	if len(parts) > 0 {
		predictResp.Parts = parts
	}
	predictResp.Reasoning = extractReasoning(claudeResp.Content)

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

	// Build the canonical request via the shared base builder, then layer on
	// structured outputs (the no-tools paths honor output_config).
	messages := p.convertMessagesToClaudeFormat(req.Messages)
	claudeReq := p.buildBaseRequest(req, messages)
	claudeReq.OutputConfig = outputConfigFor(req.ResponseFormat)

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

	// Calculate cost breakdown. claudeResp.Usage already carries
	// CacheCreationInputTokens (cache WRITE) alongside the read/input/output
	// counts, so route the full wire usage straight through costFromUsage
	// rather than the CalculateCost wrapper (which only accepts cache reads).
	costBreakdown := p.costFromUsage(claudeResp.Usage)

	predictResp.Content = responseText
	predictResp.CostInfo = &costBreakdown
	predictResp.Latency = latency
	predictResp.Raw = respBody
	predictResp.FinishReason = normalizeFinishReason(claudeResp.StopReason)

	return predictResp, nil
}

// claudePricing returns pricing for Claude models (input, output, cached per 1K tokens)
func claudePricing(model string) (inputPrice, outputPrice, cachedPrice float64) {
	// Define pricing constants (USD per 1K tokens). Cached = 10% of input.
	const (
		sonnetInput  = 0.003
		sonnetOutput = 0.015
		sonnetCached = 0.0003
		haikuInput   = 0.001 // Haiku 4.5
		haikuOutput  = 0.005
		haikuCached  = 0.0001
		opus3Input   = 0.015 // Claude 3 Opus (legacy)
		opus3Output  = 0.075
		opus3Cached  = 0.0015
		opus4Input   = 0.005 // Opus 4.6/4.7/4.8
		opus4Output  = 0.025
		opus4Cached  = 0.0005
		fableInput   = 0.010 // Fable 5 / Mythos 5
		fableOutput  = 0.050
		fableCached  = 0.001
		haiku3Input  = 0.00025
		haiku3Output = 0.00125
		haiku3Cached = 0.000025
	)

	switch model {
	case idClaude35Sonnet20241022, idClaude35Sonnet20240620, idClaude3Sonnet20240229:
		return sonnetInput, sonnetOutput, sonnetCached
	case "claude-sonnet-4-6", idClaudeSonnet45:
		return sonnetInput, sonnetOutput, sonnetCached
	case idClaudeHaiku45:
		return haikuInput, haikuOutput, haikuCached
	case idClaude35Haiku20241022:
		return haikuInput, haikuOutput, haikuCached
	case "claude-opus-4-8", "claude-opus-4-7", "claude-opus-4-6":
		return opus4Input, opus4Output, opus4Cached
	case "claude-fable-5", "claude-mythos-5", "claude-mythos-preview":
		return fableInput, fableOutput, fableCached
	case idClaude3Opus20240229:
		return opus3Input, opus3Output, opus3Cached
	case idClaude3Haiku20240307:
		return haiku3Input, haiku3Output, haiku3Cached
	default:
		// Heuristic fallback for unlisted IDs (e.g. a future dated snapshot) so a
		// new model name doesn't silently bill at the wrong tier. Order matters:
		// check the most specific family token first.
		switch {
		case strings.Contains(model, "haiku"):
			return haikuInput, haikuOutput, haikuCached
		case strings.Contains(model, "opus"):
			return opus4Input, opus4Output, opus4Cached
		case strings.Contains(model, "fable"), strings.Contains(model, "mythos"):
			return fableInput, fableOutput, fableCached
		default: // sonnet and everything else
			return sonnetInput, sonnetOutput, sonnetCached
		}
	}
}

// claudeUsageToTokens maps Anthropic's cache-EXCLUSIVE input accounting into
// canonical units: input_tokens is already full-price, uncached input (do NOT
// subtract cache reads/writes from it — cache_read_input_tokens and
// cache_creation_input_tokens are separate wire fields, not a subset of
// input_tokens).
func claudeUsageToTokens(u claudeUsage) base.TokenUsage {
	return base.TokenUsage{
		Input:      u.InputTokens,
		CacheRead:  u.CacheReadInputTokens,
		CacheWrite: u.CacheCreationInputTokens,
		Output:     u.OutputTokens,
	}
}

// costFromUsage prices a claudeUsage through the unit-keyed pricing engine:
// an explicit config descriptor wins, then legacy flat per-1K config, then
// the built-in per-model table (claudePricingTable), else nil (priced units
// are surfaced loudly by PriceUsage rather than silently reported as $0
// without warning — see ResolveLLMPricing/PriceUsage doc comments).
func (p *Provider) costFromUsage(u claudeUsage) types.CostInfo {
	desc := base.ResolveLLMPricing(
		p.Pricing(),
		base.FlatPricing{Input: p.defaults.Pricing.InputCostPer1K, Output: p.defaults.Pricing.OutputCostPer1K},
		claudePricingTable, p.model)
	return base.PriceUsage(desc, p.ID(), base.ProviderTypeInference, claudeUsageToTokens(u), nil, 0)
}

// CalculateCost calculates detailed cost breakdown including optional cached
// tokens. Thin wrapper over costFromUsage, kept for the Provider interface's
// legacy signature (cachedTokens here means cache READS only — callers that
// also have cache-write counts, e.g. wire claudeUsage, should call
// costFromUsage directly instead so cache writes aren't dropped).
func (p *Provider) CalculateCost(tokensIn, tokensOut, cachedTokens int) types.CostInfo {
	return p.costFromUsage(claudeUsage{
		InputTokens: tokensIn, OutputTokens: tokensOut, CacheReadInputTokens: cachedTokens,
	})
}

// SupportsStreaming is provided by BaseProvider (returns true)
