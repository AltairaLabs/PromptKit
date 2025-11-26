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

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	roleUser      = "user"
	roleAssistant = "assistant"
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
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

type claudeToolMessage struct {
	Role         string              `json:"role"`
	Content      []interface{}       `json:"content"`
	CacheControl *claudeCacheControl `json:"cache_control,omitempty"`
}

type claudeTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// BuildTooling converts tool descriptors to Claude format
func (p *ToolProvider) BuildTooling(descriptors []*providers.ToolDescriptor) (interface{}, error) {
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
func (p *ToolProvider) PredictWithTools(ctx context.Context, req providers.PredictionRequest, tools interface{}, toolChoice string) (providers.PredictionResponse, []types.MessageToolCall, error) {
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
		return predictResp, nil, err
	}

	// Parse response and extract tool calls
	return p.parseToolResponse(respBytes, predictResp)
}

// processClaudeToolResult converts a tool message to Claude's tool_result format
func processClaudeToolResult(msg types.Message) claudeToolResult {
	return claudeToolResult{
		Type:      "tool_result",
		ToolUseID: msg.ToolResult.ID,
		Content:   msg.Content,
	}
}

// buildClaudeMessageContent creates content array for a message including text and tool calls
func buildClaudeMessageContent(msg types.Message, pendingToolResults []claudeToolResult) []interface{} {
	content := make([]interface{}, 0)

	// Add pending tool results first if this is a user message
	if msg.Role == roleUser && len(pendingToolResults) > 0 {
		for _, tr := range pendingToolResults {
			content = append(content, tr)
		}
	}

	// Add the message text content if present
	if msg.Content != "" {
		content = append(content, claudeTextContent{
			Type: "text",
			Text: msg.Content,
		})
	}

	// Add tool calls if this is an assistant message
	if msg.Role == roleAssistant && len(msg.ToolCalls) > 0 {
		for _, toolCall := range msg.ToolCalls {
			content = append(content, claudeToolUse{
				Type:  "tool_use",
				ID:    toolCall.ID,
				Name:  toolCall.Name,
				Input: toolCall.Args,
			})
		}
	}

	return content
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

	content := buildClaudeMessageContent(msg, pendingToolResults)
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

	request := map[string]interface{}{
		"model":       p.model,
		"max_tokens":  req.MaxTokens,
		"messages":    messages,
		"temperature": req.Temperature,
		"top_p":       req.TopP,
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

func (p *ToolProvider) parseToolResponse(respBytes []byte, predictResp providers.PredictionResponse) (providers.PredictionResponse, []types.MessageToolCall, error) {
	start := time.Now()

	var resp claudeResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBytes
		return predictResp, nil, fmt.Errorf("failed to parse Claude response: %w", err)
	}

	if resp.Error != nil {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBytes
		return predictResp, nil, fmt.Errorf("claude API error: %s", resp.Error.Message)
	}

	if len(resp.Content) == 0 {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBytes
		return predictResp, nil, fmt.Errorf("no content in Claude response")
	}

	// Extract text content
	textContent := extractTextContentFromResponse(resp.Content)

	// Parse tool calls from raw response
	toolCalls := parseToolCallsFromRawResponse(respBytes)

	// Calculate cost breakdown
	costBreakdown := p.Provider.CalculateCost(resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.Usage.CacheReadInputTokens)

	predictResp.Content = textContent
	predictResp.CostInfo = &costBreakdown
	predictResp.Latency = time.Since(start)
	predictResp.Raw = respBytes
	predictResp.ToolCalls = toolCalls

	return predictResp, toolCalls, nil
}

func (p *ToolProvider) makeRequest(ctx context.Context, request interface{}) ([]byte, error) {
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Construct the full messages endpoint URL
	url := p.baseURL + "/messages"

	logger.APIRequest("Claude-Tools", "POST", url, map[string]string{
		"Content-Type":      "application/json",
		"x-api-key":         "***",
		"anthropic-version": "2023-06-01",
	}, request)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(requestBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.GetHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	logger.APIResponse("Claude-Tools", resp.StatusCode, string(respBytes), nil)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBytes))
	}

	return respBytes, nil
}

func init() {
	providers.RegisterProviderFactory("claude", func(spec providers.ProviderSpec) (providers.Provider, error) {
		return NewToolProvider(spec.ID, spec.Model, spec.BaseURL, spec.Defaults, spec.IncludeRawOutput), nil
	})
}
