package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	roleUser        = "user"
	roleAssistant   = "assistant"
	roleTool        = "tool"
	apiKeyHeader    = "x-api-key"
	providerNameLog = "Claude-Tools"

	// Claude content source types
	sourceTypeBase64 = "base64"
	sourceTypeURL    = "url"

	// Shared error-wrapping formats for tool-path request building.
	errMarshalRequestFailed = "failed to marshal request: %w"
	errCreateRequestFailed  = "failed to create request: %w"

	// fieldType is the "type" discriminator key used in tool_choice objects.
	fieldType = "type"
)

// ToolProvider extends ClaudeProvider with tool support
type ToolProvider struct {
	*Provider
}

// NewToolProvider creates a new Claude provider with tool support
func NewToolProvider(id, model, baseURL string, defaults providers.ProviderDefaults, includeRawOutput bool) *ToolProvider {
	return &ToolProvider{
		Provider: NewProvider(id, model, baseURL, defaults, includeRawOutput),
	}
}

// NewToolProviderWithCredential creates a Claude tool provider with explicit credential.
func NewToolProviderWithCredential(
	id, model, baseURL string, defaults providers.ProviderDefaults,
	includeRawOutput bool, cred providers.Credential,
	platform string, platformConfig *providers.PlatformConfig,
) *ToolProvider {
	return &ToolProvider{
		Provider: NewProviderWithCredential(id, model, baseURL, defaults, includeRawOutput, cred, platform, platformConfig),
	}
}

// Claude-specific tool structures
type claudeTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type claudeToolUse struct {
	Type         string              `json:"type"`
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Input        json.RawMessage     `json:"input"`
	CacheControl *claudeCacheControl `json:"cache_control,omitempty"`
}

type claudeToolResult struct {
	Type         string              `json:"type"`
	ToolUseID    string              `json:"tool_use_id"`
	Content      interface{}         `json:"content"` // string for text-only, []interface{} for multimodal
	CacheControl *claudeCacheControl `json:"cache_control,omitempty"`
}

type claudeToolMessage struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

type claudeTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
	// CacheControl marks a prompt-cache breakpoint. Per the Anthropic API,
	// cache_control is only valid on content blocks (and tools/system), never on
	// the message object itself.
	CacheControl *claudeCacheControl `json:"cache_control,omitempty"`
}

// BuildTooling converts tool descriptors to Claude format
func (p *ToolProvider) BuildTooling(descriptors []*providers.ToolDescriptor) (providers.ProviderTools, error) {
	if len(descriptors) == 0 {
		return nil, nil
	}

	tools := make([]claudeTool, len(descriptors))
	for i, desc := range descriptors {
		tools[i] = claudeTool{
			Name:        desc.Name,
			Description: desc.Description,
			InputSchema: types.NormalizeRawMessage(desc.InputSchema),
		}
	}

	return tools, nil
}

// PredictWithTools performs a predict request with tool support
//
//nolint:gocritic // hugeParam: interface signature requires value receiver
func (p *ToolProvider) PredictWithTools(
	ctx context.Context,
	req providers.PredictionRequest,
	tools providers.ProviderTools,
	toolChoice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	// Track total latency including API call time
	start := time.Now()

	// Build Claude request with tools
	claudeReq := p.buildToolRequest(req, tools, toolChoice)

	// Prepare response with raw request if configured (set early to preserve on error)
	predictResp := providers.PredictionResponse{}
	if p.ShouldIncludeRawOutput() {
		predictResp.RawRequest = claudeReq
	}

	// Make the API call
	respBytes, err := p.makeRequest(ctx, &claudeReq)
	if err != nil {
		predictResp.Latency = time.Since(start)
		return predictResp, nil, err
	}

	// Capture total latency
	latency := time.Since(start)

	// Parse response and extract tool calls
	return p.parseToolResponse(respBytes, &predictResp, latency)
}

// processClaudeToolResult converts a tool message to Claude's tool_result format.
// For text-only results, content is a plain string.
// For multimodal results (containing images, documents, etc.), content is an array of content blocks.
//
//nolint:gocritic // hugeParam: types.Message is part of established API
func processClaudeToolResult(msg types.Message) claudeToolResult {
	result := claudeToolResult{
		Type:      "tool_result",
		ToolUseID: msg.ToolResult.ID,
	}

	// If the tool result has media parts, serialize as an array of content blocks
	if msg.ToolResult.HasMedia() {
		result.Content = buildToolResultContentBlocks(msg.ToolResult.Parts)
		return result
	}

	// Text-only: use plain string to avoid unnecessary array wrapping
	result.Content = msg.ToolResult.GetTextContent()
	return result
}

// buildToolResultContentBlocks converts tool result parts to Claude content block array.
// Each part is converted independently via convertToolResultPart.
func buildToolResultContentBlocks(parts []types.ContentPart) []interface{} {
	blocks := make([]interface{}, 0, len(parts))
	for _, part := range parts {
		if block := convertToolResultPart(part); block != nil {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

// convertToolResultPart converts a single content part to a Claude content block.
// Returns nil if the part cannot be converted.
func convertToolResultPart(part types.ContentPart) interface{} {
	switch part.Type {
	case types.ContentTypeText:
		if part.Text != nil && *part.Text != "" {
			return claudeTextContent{Type: "text", Text: *part.Text}
		}
	case types.ContentTypeImage:
		if part.Media != nil {
			return buildToolResultMediaBlock("image", part)
		}
	case types.ContentTypeDocument:
		if part.Media != nil {
			return buildToolResultMediaBlock("document", part)
		}
	}
	return nil
}

// buildToolResultMediaBlock creates a Claude media content block (image or document).
func buildToolResultMediaBlock(blockType string, part types.ContentPart) interface{} {
	block := claudeContentBlockMultimodal{
		Type: blockType,
		Source: &claudeImageSource{
			MediaType: part.Media.MIMEType,
		},
	}

	// For images, check URL source first
	if blockType == "image" && part.Media.URL != nil && *part.Media.URL != "" {
		block.Source.Type = sourceTypeURL
		block.Source.URL = *part.Media.URL
		return block
	}

	// Use MediaLoader for base64 data (supports Data, FilePath, StorageReference)
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})
	data, err := loader.GetBase64Data(context.Background(), part.Media)
	if err != nil {
		return nil
	}
	block.Source.Type = sourceTypeBase64
	block.Source.Data = data
	return block
}

// buildClaudeMessageContent creates content array for a message including text, images, and tool calls
//
//nolint:gocritic // hugeParam: types.Message is part of established API
func (p *ToolProvider) buildClaudeMessageContent(
	msg types.Message,
	pendingToolResults []claudeToolResult,
) []interface{} {
	content := make([]interface{}, 0)

	// Add pending tool results first if this is a user message
	if msg.Role == roleUser && len(pendingToolResults) > 0 {
		for _, tr := range pendingToolResults {
			content = append(content, tr)
		}
	}

	// Add message content (text and/or media)
	content = append(content, p.buildMessageContentBlocks(msg)...)

	// Add tool calls if this is an assistant message
	if msg.Role == roleAssistant && len(msg.ToolCalls) > 0 {
		for _, toolCall := range msg.ToolCalls {
			// Normalize empty/null/truncated args to {} — a streamed tool call cut
			// off at max_tokens leaves non-empty-but-invalid JSON that would crash
			// the request marshal on replay.
			content = append(content, claudeToolUse{
				Type:  "tool_use",
				ID:    toolCall.ID,
				Name:  toolCall.Name,
				Input: types.NormalizeRawMessage(toolCall.Args),
			})
		}
	}

	return content
}

// buildMessageContentBlocks creates content blocks for text and media parts of a message.
// This is extracted to reduce cognitive complexity of buildClaudeMessageContent.
//
//nolint:gocritic // hugeParam: types.Message is part of established API
func (p *ToolProvider) buildMessageContentBlocks(msg types.Message) []interface{} {
	// Check if message has multimodal content (images, etc.)
	if msg.HasMediaContent() {
		// Use multimodal conversion path
		blocks, err := p.convertPartsToClaudeBlocks(msg.Parts)
		if err == nil {
			return blocks
		}
		// Fallback to text-only on conversion error
	}

	// Text-only path (either no media or conversion failed)
	textContent := msg.GetContent()
	if textContent != "" {
		return []interface{}{claudeTextContent{
			Type: "text",
			Text: textContent,
		}}
	}

	return nil
}

// addClaudeToolConfig sets the tools and tool_choice fields on the request based
// on toolChoice.
func addClaudeToolConfig(request *claudeRequest, tools any, toolChoice string) {
	request.Tools = tools

	switch toolChoice {
	case "", "none":
		// Empty: no forced selection. "none": Claude doesn't support
		// tool_choice:"none" directly, so omit tool_choice and let the model
		// decide not to use tools (the tools are still sent so it knows they
		// exist). Both leave ToolChoice unset.
	case "auto":
		request.ToolChoice = map[string]any{fieldType: "auto"}
	case "required", "any":
		request.ToolChoice = map[string]any{fieldType: "any"}
	default:
		request.ToolChoice = map[string]any{
			fieldType: "tool",
			"name":    toolChoice,
		}
	}
}

// flushPendingToolResults creates a user message from pending tool results
func flushPendingToolResults(pendingToolResults []claudeToolResult) claudeToolMessage {
	toolResultContent := make([]interface{}, len(pendingToolResults))
	for i, tr := range pendingToolResults {
		toolResultContent[i] = tr
	}
	return claudeToolMessage{
		Role:    "user",
		Content: toolResultContent,
	}
}

// processMessageForTools processes a single message and manages tool results
func (p *ToolProvider) processMessageForTools(
	msg types.Message,
	pendingToolResults []claudeToolResult,
	messages []claudeToolMessage,
	tools interface{},
) ([]claudeToolMessage, []claudeToolResult) {
	// If we have pending tool results and this is not a user message, create a user message for them
	if len(pendingToolResults) > 0 && msg.Role != roleUser {
		messages = append(messages, flushPendingToolResults(pendingToolResults))
		pendingToolResults = nil
	}

	content := p.buildClaudeMessageContent(msg, pendingToolResults)
	if msg.Role == roleUser {
		pendingToolResults = nil
	}

	if len(content) == 0 {
		return messages, pendingToolResults
	}

	claudeMsg := claudeToolMessage{
		Role:    msg.Role,
		Content: content,
	}

	// For caching: cache the prefix through the first user message when tools are
	// present and the model supports caching. cache_control must go on a CONTENT
	// BLOCK (the last one) — the Anthropic API rejects cache_control on a message
	// object ("messages.N.cache_control: Extra inputs are not permitted").
	if p.supportsCaching() && msg.Role == roleUser && len(messages) == 0 && tools != nil {
		last := len(claudeMsg.Content) - 1
		if tc, ok := claudeMsg.Content[last].(claudeTextContent); ok {
			tc.CacheControl = &claudeCacheControl{Type: cacheTypeEphemeral}
			claudeMsg.Content[last] = tc
		}
	}

	messages = append(messages, claudeMsg)
	return messages, pendingToolResults
}

// markLastBlockCacheable sets a cache_control breakpoint on the final content
// block of the most recent message so Anthropic caches the whole conversation
// prefix up to that point. It walks back within the last message to find a block
// type that can carry cache_control (text, tool_use, tool_result).
func markLastBlockCacheable(messages []claudeToolMessage) {
	if len(messages) == 0 {
		return
	}
	last := &messages[len(messages)-1]
	bp := &claudeCacheControl{Type: cacheTypeEphemeral}
	for i := len(last.Content) - 1; i >= 0; i-- {
		switch b := last.Content[i].(type) {
		case claudeTextContent:
			b.CacheControl = bp
			last.Content[i] = b
			return
		case claudeToolUse:
			b.CacheControl = bp
			last.Content[i] = b
			return
		case claudeToolResult:
			b.CacheControl = bp
			last.Content[i] = b
			return
		}
	}
}

//nolint:gocritic // hugeParam: providers.PredictionRequest is passed by value across the provider interface
func (p *ToolProvider) buildToolRequest(req providers.PredictionRequest, tools any, toolChoice string) claudeRequest {
	messages := make([]claudeToolMessage, 0, len(req.Messages))
	var pendingToolResults []claudeToolResult

	for i := range req.Messages {
		msg := &req.Messages[i]
		if msg.Role == roleTool {
			pendingToolResults = append(pendingToolResults, processClaudeToolResult(*msg))
			continue
		}

		messages, pendingToolResults = p.processMessageForTools(*msg, pendingToolResults, messages, tools)
	}

	// If there are still pending tool results at the end, add them as a final user message
	if len(pendingToolResults) > 0 {
		messages = append(messages, flushPendingToolResults(pendingToolResults))
	}

	// Cache the GROWING conversation prefix. A cache_control breakpoint caches the
	// entire prefix up to it, and Anthropic auto-matches earlier cached prefixes
	// within a 20-block lookback, so marking the LAST block rolls the cache forward
	// each round: round N reads round N-1's full prefix and writes the new one.
	// (The static first-message breakpoint set above only cached the base, leaving
	// the growing tool-result history re-billed at full price every round.)
	if p.supportsCaching() && tools != nil {
		markLastBlockCacheable(messages)
	}

	// Common fields (model, max_tokens, gated temperature, cache-aware system)
	// come from the shared base builder — same source of truth as
	// Predict/PredictStream. The tool paths deliberately omit output_config.
	request := p.buildBaseRequest(req, messages)
	if tools != nil {
		addClaudeToolConfig(&request, tools, toolChoice)
	}

	return request
}

// extractTextContentFromResponse extracts text content from Claude response
func extractTextContentFromResponse(content []claudeContent) string {
	for _, c := range content {
		if c.Type == "text" {
			return c.Text
		}
	}
	return ""
}

// extractContentParts converts Claude content blocks to typed ContentParts.
// Returns text and thinking parts; tool_use blocks are handled separately.
func extractContentParts(content []claudeContent) []types.ContentPart {
	var parts []types.ContentPart
	for _, c := range content {
		switch c.Type {
		case "text":
			parts = append(parts, types.NewTextPart(c.Text))
		case types.ContentTypeThinking:
			parts = append(parts, types.NewThinkingPart(c.Text))
		}
	}
	return parts
}

// parseToolCallsFromRawResponse extracts tool calls from raw JSON response
func parseToolCallsFromRawResponse(respBytes []byte) []types.MessageToolCall {
	var toolCalls []types.MessageToolCall
	var rawResp map[string]interface{}

	if err := json.Unmarshal(respBytes, &rawResp); err != nil {
		return toolCalls
	}

	content, ok := rawResp["content"].([]interface{})
	if !ok {
		return toolCalls
	}

	for _, item := range content {
		itemMap, ok := item.(map[string]interface{})
		if !ok || itemMap["type"] != "tool_use" {
			continue
		}

		toolCall := types.MessageToolCall{
			ID:   itemMap["id"].(string),
			Name: itemMap["name"].(string),
		}

		if input, ok := itemMap["input"]; ok {
			inputBytes, _ := json.Marshal(input) // NOSONAR: Marshal only errors on unsupported types, impossible with map[string]interface{}
			toolCall.Args = json.RawMessage(inputBytes)
		}

		toolCalls = append(toolCalls, toolCall)
	}

	return toolCalls
}

func (p *ToolProvider) parseToolResponse(
	respBytes []byte,
	predictResp *providers.PredictionResponse,
	latency time.Duration,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	var resp claudeResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		predictResp.Latency = latency
		predictResp.Raw = respBytes
		return *predictResp, nil, fmt.Errorf("failed to parse Claude response: %w", err)
	}

	if resp.Error != nil {
		predictResp.Latency = latency
		predictResp.Raw = respBytes
		return *predictResp, nil, fmt.Errorf("claude API error: %s", resp.Error.Message)
	}

	if len(resp.Content) == 0 {
		predictResp.Latency = latency
		predictResp.Raw = respBytes
		return *predictResp, nil, fmt.Errorf("no content in Claude response")
	}

	// Extract text content
	textContent := extractTextContentFromResponse(resp.Content)

	// Parse tool calls from raw response
	toolCalls := parseToolCallsFromRawResponse(respBytes)

	// Calculate cost breakdown
	costBreakdown := p.Provider.CalculateCost(resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.Usage.CacheReadInputTokens)

	predictResp.Content = textContent
	predictResp.Parts = extractContentParts(resp.Content)
	predictResp.CostInfo = &costBreakdown
	predictResp.Latency = latency
	predictResp.Raw = respBytes
	predictResp.ToolCalls = toolCalls

	return *predictResp, toolCalls, nil
}

// applyToolRequestHeaders sets content-type, anthropic auth, and custom
// headers on req. Credential-based auth (Bedrock SigV4 / Vertex Bearer /
// Azure Bearer) goes via the resolved Credential; the direct API uses
// x-api-key + anthropic-version headers. Azure pins the API contract via
// the api-version query parameter so it does not need the
// anthropic-version header. Centralizing this keeps the caller functions
// below cognitive-complexity limits and ensures every request path goes
// through the same custom-header collision check.
func (p *ToolProvider) applyToolRequestHeaders(ctx context.Context, req *http.Request) error {
	req.Header.Set(contentTypeHeader, applicationJSON)
	if p.usesCredentialAuth() {
		if err := p.applyAuth(ctx, req); err != nil {
			return fmt.Errorf("failed to apply authentication: %w", err)
		}
	} else {
		req.Header.Set(apiKeyHeader, p.apiKey)
		req.Header.Set(anthropicVersionKey, anthropicVersionValue)
	}
	return p.ApplyCustomHeaders(req)
}

func (p *ToolProvider) makeRequest(ctx context.Context, request *claudeRequest) ([]byte, error) {
	url := p.messagesURL()

	// Partner-hosted (Bedrock/Vertex): inject anthropic_version into the request
	// body and drop the model field — both put the model in the URL path. The
	// shared partner marshaler also drops the (absent here) stream field; the
	// tools and tool_choice fields the builder set pass through unchanged.
	var reqBytes []byte
	var err error
	if p.isPartnerHosted() {
		reqBytes, err = p.marshalBedrockStreamingRequest(request)
	} else {
		reqBytes, err = json.Marshal(request)
	}
	if err != nil {
		return nil, fmt.Errorf(errMarshalRequestFailed, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf(errCreateRequestFailed, err)
	}

	if hdrErr := p.applyToolRequestHeaders(ctx, httpReq); hdrErr != nil {
		return nil, hdrErr
	}

	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		return nil, &providers.ProviderTransportError{Cause: err, Provider: p.ID()}
	}
	defer resp.Body.Close()

	respBody, err := providers.ReadResponseBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		switch {
		case p.isBedrock():
			return nil, parseBedrockHTTPError(resp.StatusCode, respBody)
		case p.isVertex(), p.isAzure():
			return nil, providers.ParsePlatformHTTPError(p.platform, resp.StatusCode, respBody)
		default:
			return nil, &providers.ProviderHTTPError{
				StatusCode: resp.StatusCode, URL: url,
				Body: string(respBody), Provider: p.ID(),
			}
		}
	}

	// Check for Bedrock body errors (e.g. UnknownOperationException on HTTP 200)
	if bodyErr := checkBedrockBodyError(respBody); bodyErr != nil {
		return nil, bodyErr
	}

	return respBody, nil
}

// PredictStreamWithTools performs a streaming predict request with tool support.
func (p *ToolProvider) PredictStreamWithTools(
	ctx context.Context,
	req providers.PredictionRequest,
	tools interface{},
	toolChoice string,
) (<-chan providers.StreamChunk, error) {
	// Build Claude request with tools
	claudeReq := p.buildToolRequest(req, tools, toolChoice)

	// Add streaming flag
	claudeReq.Stream = true

	// Bedrock: binary event-stream format.
	if p.isBedrock() {
		return p.streamBedrockToolRequest(ctx, &claudeReq)
	}

	// Vertex: SSE via :streamRawPredict with the partner body shape.
	if p.isVertex() {
		return p.streamVertexToolRequest(ctx, &claudeReq)
	}

	return p.streamDirectToolRequest(ctx, &claudeReq)
}

// streamVertexToolRequest handles the Vertex AI Anthropic-partner
// tool-calling streaming path. URL is :streamRawPredict; body uses the
// shared partner shape (no model field, anthropic_version=vertex-2023-10-16,
// no stream flag); auth via the GCP credential. The response is plain SSE
// like the direct API, so the SSE helper is reused.
func (p *ToolProvider) streamVertexToolRequest(
	ctx context.Context,
	claudeReq *claudeRequest,
) (<-chan providers.StreamChunk, error) {
	reqBody, err := p.marshalBedrockStreamingRequest(claudeReq)
	if err != nil {
		return nil, fmt.Errorf(errMarshalRequestFailed, err)
	}
	return p.runSSEToolStream(ctx, reqBody)
}

// runSSEToolStream is the shared SSE streaming engine for tool-calling
// requests. Both the direct Anthropic API and the Vertex Anthropic-partner
// path call into this once they have produced a serialized body — they
// differ only in body encoding (direct: full claudeReq as-is; vertex: the
// partner-shape body with anthropic_version+no model+no stream). URL,
// header set, and SSE scanner are identical.
func (p *ToolProvider) runSSEToolStream(
	ctx context.Context, reqBody []byte,
) (<-chan providers.StreamChunk, error) {
	url := p.messagesStreamURL()
	requestFn := p.buildDirectStreamingRequestFn(url, reqBody)
	return p.RunStreamingRequest(ctx, &providers.StreamRetryRequest{
		Policy:       p.StreamRetryPolicy(),
		Budget:       p.StreamRetryBudget(),
		ProviderName: p.ID(),
		Host:         providers.HostFromURL(url),
		IdleTimeout:  p.StreamIdleTimeout(),
		RequestFn:    requestFn,
		Client:       p.GetStreamingHTTPClient(),
	}, func(ctx context.Context, body io.ReadCloser, outChan chan<- providers.StreamChunk) {
		idleBody := providers.NewIdleTimeoutReader(body, p.StreamIdleTimeout())
		scanner := providers.NewSSEScanner(idleBody)
		p.streamResponse(ctx, idleBody, scanner, outChan)
	})
}

// streamBedrockToolRequest handles the Bedrock tool-calling streaming path.
// Extracted from PredictStreamWithTools to keep that function under the
// cognitive-complexity threshold.
func (p *ToolProvider) streamBedrockToolRequest(
	ctx context.Context,
	claudeReq *claudeRequest,
) (<-chan providers.StreamChunk, error) {
	reqBody, err := p.marshalBedrockStreamingRequest(claudeReq)
	if err != nil {
		return nil, fmt.Errorf(errMarshalRequestFailed, err)
	}
	url := p.messagesStreamURL()
	requestFn := p.buildBedrockStreamingRequestFn(url, reqBody)
	return p.RunStreamingRequest(ctx, &providers.StreamRetryRequest{
		Policy:        p.StreamRetryPolicy(),
		Budget:        p.StreamRetryBudget(),
		ProviderName:  p.ID(),
		Host:          providers.HostFromURL(url),
		IdleTimeout:   p.StreamIdleTimeout(),
		RequestFn:     requestFn,
		Client:        p.GetStreamingHTTPClient(),
		FrameDetector: providers.BedrockEventStreamFrameDetector{},
	}, func(ctx context.Context, body io.ReadCloser, outChan chan<- providers.StreamChunk) {
		idleBody := providers.NewIdleTimeoutReader(body, p.StreamIdleTimeout())
		scanner := providers.NewBedrockEventScanner(idleBody)
		p.streamResponse(ctx, idleBody, scanner, outChan)
	})
}

// streamDirectToolRequest handles the direct Anthropic API tool-calling
// streaming path. Body is the full claudeReq (model, stream:true and all)
// serialized as-is; auth is x-api-key via applyToolRequestHeaders.
func (p *ToolProvider) streamDirectToolRequest(
	ctx context.Context,
	claudeReq *claudeRequest,
) (<-chan providers.StreamChunk, error) {
	requestBytes, err := json.Marshal(claudeReq)
	if err != nil {
		return nil, fmt.Errorf(errMarshalRequestFailed, err)
	}
	return p.runSSEToolStream(ctx, requestBytes)
}

// buildBedrockStreamingRequestFn returns a RequestFn for the Bedrock
// tool-calling streaming path. Headers (content type, SigV4 auth, custom
// headers) are applied via applyToolRequestHeaders to share the same
// centralized header logic as the non-streaming path.
func (p *ToolProvider) buildBedrockStreamingRequestFn(
	url string, reqBody []byte,
) func(context.Context) (*http.Request, error) {
	return func(ctx context.Context) (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf(errCreateRequestFailed, err)
		}
		httpReq.Header.Set("Accept", "application/vnd.amazon.eventstream")
		if err := p.applyToolRequestHeaders(ctx, httpReq); err != nil {
			return nil, err
		}
		return httpReq, nil
	}
}

// buildDirectStreamingRequestFn returns a RequestFn for the direct Anthropic
// API tool-calling streaming path.
func (p *ToolProvider) buildDirectStreamingRequestFn(
	url string, reqBody []byte,
) func(context.Context) (*http.Request, error) {
	return func(ctx context.Context) (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf(errCreateRequestFailed, err)
		}
		httpReq.Header.Set("Accept", "text/event-stream")
		if err := p.applyToolRequestHeaders(ctx, httpReq); err != nil {
			return nil, err
		}
		return httpReq, nil
	}
}

//nolint:gochecknoinits // Factory registration requires init
func init() {
	providers.RegisterProviderFactory("claude", providers.CredentialFactory(
		func(spec providers.ProviderSpec) (providers.Provider, error) {
			tp := NewToolProviderWithCredential(
				spec.ID, spec.Model, spec.BaseURL, spec.Defaults,
				spec.IncludeRawOutput, spec.Credential,
				spec.Platform, spec.PlatformConfig,
			)
			tp.setUnsupportedParams(spec.UnsupportedParams)
			tp.setCapabilities(spec.Capabilities)
			return tp, nil
		},
		func(spec providers.ProviderSpec) (providers.Provider, error) {
			tp := NewToolProvider(
				spec.ID, spec.Model, spec.BaseURL, spec.Defaults, spec.IncludeRawOutput,
			)
			tp.setUnsupportedParams(spec.UnsupportedParams)
			tp.setCapabilities(spec.Capabilities)
			return tp, nil
		},
	))
}
