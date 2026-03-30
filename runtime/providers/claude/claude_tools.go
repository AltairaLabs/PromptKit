package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	roleUser        = "user"
	roleAssistant   = "assistant"
	apiKeyHeader    = "x-api-key"
	providerNameLog = "Claude-Tools"

	// Claude content source types
	sourceTypeBase64 = "base64"
	sourceTypeURL    = "url"
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
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type claudeToolResult struct {
	Type      string      `json:"type"`
	ToolUseID string      `json:"tool_use_id"`
	Content   interface{} `json:"content"` // string for text-only, []interface{} for multimodal
}

type claudeToolMessage struct {
	Role         string              `json:"role"`
	Content      []any               `json:"content"`
	CacheControl *claudeCacheControl `json:"cache_control,omitempty"`
}

type claudeTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
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
			InputSchema: desc.InputSchema,
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
	respBytes, err := p.makeRequest(ctx, claudeReq)
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
			args := toolCall.Args
			if len(args) == 0 || string(args) == "null" {
				args = json.RawMessage("{}")
			}
			content = append(content, claudeToolUse{
				Type:  "tool_use",
				ID:    toolCall.ID,
				Name:  toolCall.Name,
				Input: args,
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

// addClaudeToolConfig adds tool configuration to the request based on toolChoice
func addClaudeToolConfig(request map[string]interface{}, tools interface{}, toolChoice string) {
	request["tools"] = tools

	if toolChoice == "" {
		return
	}

	switch toolChoice {
	case "auto":
		request["tool_choice"] = map[string]interface{}{"type": "auto"}
	case "required", "any":
		request["tool_choice"] = map[string]interface{}{"type": "any"}
	case "none":
		// Claude doesn't support tool_choice: "none" directly.
		// Omit tool_choice to let the model decide not to use tools.
		// The tools are still sent (so the model knows they exist) but
		// no forced selection is applied.
	default:
		request["tool_choice"] = map[string]interface{}{
			"type": "tool",
			"name": toolChoice,
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

	// For caching: cache the first user message if tools are present and model supports caching
	if p.supportsCaching() && msg.Role == roleUser && len(messages) == 0 && tools != nil {
		claudeMsg.CacheControl = &claudeCacheControl{Type: "ephemeral"}
	}

	messages = append(messages, claudeMsg)
	return messages, pendingToolResults
}

func (p *ToolProvider) buildToolRequest(req providers.PredictionRequest, tools interface{}, toolChoice string) map[string]interface{} {
	messages := make([]claudeToolMessage, 0, len(req.Messages))
	var pendingToolResults []claudeToolResult

	for i := range req.Messages {
		msg := &req.Messages[i]
		if msg.Role == "tool" {
			pendingToolResults = append(pendingToolResults, processClaudeToolResult(*msg))
			continue
		}

		messages, pendingToolResults = p.processMessageForTools(*msg, pendingToolResults, messages, tools)
	}

	// If there are still pending tool results at the end, add them as a final user message
	if len(pendingToolResults) > 0 {
		messages = append(messages, flushPendingToolResults(pendingToolResults))
	}

	// Apply defaults to zero-valued request parameters
	temperature, _, maxTokens := p.applyDefaults(req.Temperature, req.TopP, req.MaxTokens)

	// Note: Anthropic's newer models (Claude 4+) don't support both temperature and top_p
	// We only send temperature to avoid the "cannot both be specified" error
	request := map[string]interface{}{
		"model":       p.model,
		"max_tokens":  maxTokens,
		"messages":    messages,
		"temperature": temperature,
	}

	if req.System != "" {
		request["system"] = req.System
	}

	if tools != nil {
		addClaudeToolConfig(request, tools, toolChoice)
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

func (p *ToolProvider) makeRequest(ctx context.Context, request interface{}) ([]byte, error) {
	url := p.messagesURL()

	// For Bedrock, inject anthropic_version into the request body and omit the header
	if p.isBedrock() {
		if reqMap, ok := request.(map[string]interface{}); ok {
			reqMap[bedrockVersionBodyKey] = bedrockVersionValue
			// Remove model from body — Bedrock uses the URL path for model selection
			delete(reqMap, "model")
		}
	}

	reqBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	if p.isBedrock() {
		// Bedrock uses SigV4 signing via applyAuth — no API key or version header
		if authErr := p.applyAuth(ctx, httpReq); authErr != nil {
			return nil, fmt.Errorf("failed to apply authentication: %w", authErr)
		}
	} else {
		httpReq.Header.Set(apiKeyHeader, p.apiKey)
		httpReq.Header.Set(anthropicVersionKey, anthropicVersionValue)
	}

	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := providers.ReadResponseBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if p.isBedrock() {
			return nil, parseBedrockHTTPError(resp.StatusCode, respBody)
		}
		return nil, fmt.Errorf("API request to %s failed with status %d: %s",
			url, resp.StatusCode, string(respBody))
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
	claudeReq["stream"] = true

	// Bedrock: use binary event-stream format
	if p.isBedrock() {
		reqBody, err := p.marshalBedrockStreamingRequest(claudeReq)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		body, scanner, err := p.makeBedrockStreamingRequest(ctx, reqBody)
		if err != nil {
			return nil, err
		}
		idleBody := providers.NewIdleTimeoutReader(body, providers.DefaultStreamIdleTimeout)
		outChan := make(chan providers.StreamChunk, providers.DefaultStreamBufferSize)
		go p.streamResponse(ctx, idleBody, scanner, outChan)
		return outChan, nil
	}

	requestBytes, err := json.Marshal(claudeReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := p.messagesURL()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(requestBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	httpReq.Header.Set(apiKeyHeader, p.apiKey)
	httpReq.Header.Set(anthropicVersionKey, anthropicVersionValue)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body := providers.ReadErrorBody(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("API request to %s failed with status %d: %s", url, resp.StatusCode, string(body))
	}

	outChan := make(chan providers.StreamChunk, providers.DefaultStreamBufferSize)
	idleBody := providers.NewIdleTimeoutReader(resp.Body, providers.DefaultStreamIdleTimeout)
	scanner := providers.NewSSEScanner(idleBody)
	go p.streamResponse(ctx, idleBody, scanner, outChan)

	return outChan, nil
}

//nolint:gochecknoinits // Factory registration requires init
func init() {
	providers.RegisterProviderFactory("claude", providers.CredentialFactory(
		func(spec providers.ProviderSpec) (providers.Provider, error) {
			return NewToolProviderWithCredential(
				spec.ID, spec.Model, spec.BaseURL, spec.Defaults,
				spec.IncludeRawOutput, spec.Credential,
				spec.Platform, spec.PlatformConfig,
			), nil
		},
		func(spec providers.ProviderSpec) (providers.Provider, error) {
			return NewToolProvider(
				spec.ID, spec.Model, spec.BaseURL, spec.Defaults, spec.IncludeRawOutput,
			), nil
		},
	))
}
