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

// ClaudeToolProvider extends ClaudeProvider with tool support
type ClaudeToolProvider struct {
	*ClaudeProvider
}

// NewClaudeToolProvider creates a new Claude provider with tool support
func NewClaudeToolProvider(id, model, baseURL string, defaults providers.ProviderDefaults, includeRawOutput bool) *ClaudeToolProvider {
	return &ClaudeToolProvider{
		ClaudeProvider: NewClaudeProvider(id, model, baseURL, defaults, includeRawOutput),
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
func (p *ClaudeToolProvider) BuildTooling(descriptors []*providers.ToolDescriptor) (interface{}, error) {
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

// ChatWithTools performs a chat request with tool support
func (p *ClaudeToolProvider) ChatWithTools(ctx context.Context, req providers.ChatRequest, tools interface{}, toolChoice string) (providers.ChatResponse, []types.MessageToolCall, error) {
	// Build Claude request with tools
	claudeReq := p.buildToolRequest(req, tools, toolChoice)

	// Prepare response with raw request if configured (set early to preserve on error)
	chatResp := providers.ChatResponse{}
	if p.ShouldIncludeRawOutput() {
		chatResp.RawRequest = claudeReq
	}

	// Make the API call
	respBytes, err := p.makeRequest(ctx, claudeReq)
	if err != nil {
		return chatResp, nil, err
	}

	// Parse response and extract tool calls
	return p.parseToolResponse(respBytes, chatResp)
}

func (p *ClaudeToolProvider) buildToolRequest(req providers.ChatRequest, tools interface{}, toolChoice string) map[string]interface{} {
	// Convert messages to Claude format
	// Claude requires tool_result blocks to be in a user message immediately after the assistant message with tool_use
	messages := make([]claudeToolMessage, 0, len(req.Messages))

	// Collect any pending tool results to group together
	var pendingToolResults []claudeToolResult

	for _, msg := range req.Messages {
		if msg.Role == "tool" {
			// Collect tool result to be added to the next user message or grouped together
			toolResult := claudeToolResult{
				Type:      "tool_result",
				ToolUseID: msg.ToolResult.ID,
				Content:   msg.Content,
			}
			pendingToolResults = append(pendingToolResults, toolResult)
			continue
		}

		// If we have pending tool results and this is a user message, add them to this message
		// Or if this is not a user message but we have pending results, create a user message for them
		if len(pendingToolResults) > 0 {
			if msg.Role != "user" {
				// Create a user message with the tool results before adding this message
				toolResultContent := make([]interface{}, len(pendingToolResults))
				for i, tr := range pendingToolResults {
					toolResultContent[i] = tr
				}
				messages = append(messages, claudeToolMessage{
					Role:    "user",
					Content: toolResultContent,
				})
				pendingToolResults = nil
			}
		}

		content := []interface{}{}

		// Add pending tool results first if this is a user message
		if msg.Role == "user" && len(pendingToolResults) > 0 {
			for _, tr := range pendingToolResults {
				content = append(content, tr)
			}
			pendingToolResults = nil
		}

		// Add the message text content if present
		if msg.Content != "" {
			content = append(content, claudeTextContent{
				Type: "text",
				Text: msg.Content,
			})
		}

		// Add tool calls if this is an assistant message
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, toolCall := range msg.ToolCalls {
				content = append(content, claudeToolUse{
					Type:  "tool_use",
					ID:    toolCall.ID,
					Name:  toolCall.Name,
					Input: toolCall.Args,
				})
			}
		}

		// Skip empty messages (e.g., assistant message with only tool calls and no text)
		if len(content) == 0 {
			continue
		}

		claudeMsg := claudeToolMessage{
			Role:    msg.Role,
			Content: content,
		}

		// For caching: cache the first user message if tools are present and model supports caching
		// This ensures tool definitions are cached along with the initial context
		if p.supportsCaching() && msg.Role == "user" && len(messages) == 0 && tools != nil {
			claudeMsg.CacheControl = &claudeCacheControl{Type: "ephemeral"}
		}

		messages = append(messages, claudeMsg)
	}

	// If there are still pending tool results at the end, add them as a final user message
	if len(pendingToolResults) > 0 {
		toolResultContent := make([]interface{}, len(pendingToolResults))
		for i, tr := range pendingToolResults {
			toolResultContent[i] = tr
		}
		messages = append(messages, claudeToolMessage{
			Role:    "user",
			Content: toolResultContent,
		})
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
		request["tools"] = tools
		// Claude uses "tool_choice" parameter like: {"type": "auto"} or {"type": "tool", "name": "tool_name"}
		if toolChoice != "" {
			switch toolChoice {
			case "auto":
				request["tool_choice"] = map[string]interface{}{"type": "auto"}
			case "required", "any":
				request["tool_choice"] = map[string]interface{}{"type": "any"}
			default:
				// Assume it's a specific tool name
				request["tool_choice"] = map[string]interface{}{
					"type": "tool",
					"name": toolChoice,
				}
			}
		}
	}

	return request
}

func (p *ClaudeToolProvider) parseToolResponse(respBytes []byte, chatResp providers.ChatResponse) (providers.ChatResponse, []types.MessageToolCall, error) {
	start := time.Now()

	var resp claudeResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBytes
		return chatResp, nil, fmt.Errorf("failed to parse Claude response: %w", err)
	}

	if resp.Error != nil {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBytes
		return chatResp, nil, fmt.Errorf("claude API error: %s", resp.Error.Message)
	}

	if len(resp.Content) == 0 {
		chatResp.Latency = time.Since(start)
		chatResp.Raw = respBytes
		return chatResp, nil, fmt.Errorf("no content in Claude response")
	}

	// Extract text content and tool calls
	var textContent string
	var toolCalls []types.MessageToolCall

	for _, content := range resp.Content {
		if content.Type == "text" {
			textContent = content.Text
		}
		// Claude may also have tool_use content, but we need to handle that differently
		// For now, focusing on text responses
	}

	// Check if response contains tool calls in raw format
	var rawResp map[string]interface{}
	if err := json.Unmarshal(respBytes, &rawResp); err == nil {
		if content, ok := rawResp["content"].([]interface{}); ok {
			for _, item := range content {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if itemMap["type"] == "tool_use" {
						toolCall := types.MessageToolCall{
							ID:   itemMap["id"].(string),
							Name: itemMap["name"].(string),
						}

						if input, ok := itemMap["input"]; ok {
							inputBytes, _ := json.Marshal(input)
							toolCall.Args = json.RawMessage(inputBytes)
						}

						toolCalls = append(toolCalls, toolCall)
					}
				}
			}
		}
	}

	// Calculate cost breakdown
	costBreakdown := p.ClaudeProvider.CalculateCost(resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.Usage.CacheReadInputTokens)

	chatResp.Content = textContent
	chatResp.CostInfo = &costBreakdown
	chatResp.Latency = time.Since(start)
	chatResp.Raw = respBytes
	chatResp.ToolCalls = toolCalls

	return chatResp, toolCalls, nil
}

func (p *ClaudeToolProvider) makeRequest(ctx context.Context, request interface{}) ([]byte, error) {
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

	resp, err := p.client.Do(req)
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
		return NewClaudeToolProvider(spec.ID, spec.Model, spec.BaseURL, spec.Defaults, spec.IncludeRawOutput), nil
	})
}
