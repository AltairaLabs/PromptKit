package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// OpenAIToolProvider extends OpenAIProvider with tool support
type OpenAIToolProvider struct {
	*OpenAIProvider
}

// NewOpenAIToolProvider creates a new OpenAI provider with tool support
func NewOpenAIToolProvider(id, model, baseURL string, defaults ProviderDefaults, includeRawOutput bool, additionalConfig map[string]interface{}) *OpenAIToolProvider {
	return &OpenAIToolProvider{
		OpenAIProvider: NewOpenAIProvider(id, model, baseURL, defaults, includeRawOutput),
	}
}

// OpenAI-specific tool structures
type openAITool struct {
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // OpenAI returns this as a string, not RawMessage
}

// BuildTooling converts tool descriptors to OpenAI format
func (p *OpenAIToolProvider) BuildTooling(descriptors []*ToolDescriptor) (interface{}, error) {
	if len(descriptors) == 0 {
		return nil, nil
	}

	tools := make([]openAITool, len(descriptors))
	for i, desc := range descriptors {
		tools[i] = openAITool{
			Type: "function",
			Function: openAIToolFunction{
				Name:        desc.Name,
				Description: desc.Description,
				Parameters:  desc.InputSchema,
			},
		}
	}

	return tools, nil
}

// ChatWithTools performs a chat request with tool support
func (p *OpenAIToolProvider) ChatWithTools(ctx context.Context, req ChatRequest, tools interface{}, toolChoice string) (ChatResponse, []types.MessageToolCall, error) {
	// Track latency - START timing
	start := time.Now()

	// Build OpenAI request with tools
	openaiReq := p.buildToolRequest(req, tools, toolChoice)

	// Prepare response with raw request if configured (set early to preserve on error)
	chatResp := ChatResponse{}
	if p.ShouldIncludeRawOutput() {
		chatResp.RawRequest = openaiReq
	}

	// Make the API call
	respBytes, err := p.makeRequest(ctx, openaiReq)
	if err != nil {
		chatResp.Latency = time.Since(start)
		return chatResp, nil, err
	}

	// Calculate latency immediately after API call completes
	latency := time.Since(start)

	// Parse response
	resp, toolCalls, err := p.parseToolResponse(respBytes)
	if err != nil {
		resp.Latency = latency
		resp.RawRequest = chatResp.RawRequest
		return resp, nil, err
	}

	// Set latency on response
	resp.Latency = latency
	resp.RawRequest = chatResp.RawRequest

	return resp, toolCalls, nil
}

// buildToolRequest constructs the OpenAI API request with tools
func (p *OpenAIToolProvider) buildToolRequest(req ChatRequest, tools interface{}, toolChoice string) map[string]interface{} {
	messages := p.convertRequestMessagesToOpenAI(req)

	// Build request
	openaiReq := map[string]interface{}{
		"model":       p.model,
		"messages":    messages,
		"temperature": req.Temperature,
		"top_p":       req.TopP,
		"max_tokens":  req.MaxTokens,
	}

	if req.Seed != nil {
		openaiReq["seed"] = *req.Seed
	}

	// Add tools if provided
	if tools != nil {
		openaiReq["tools"] = tools
		p.addToolChoiceToRequest(openaiReq, toolChoice)
	}

	return openaiReq
}

// convertRequestMessagesToOpenAI converts all messages in a request to OpenAI format
func (p *OpenAIToolProvider) convertRequestMessagesToOpenAI(req ChatRequest) []map[string]interface{} {
	messages := make([]map[string]interface{}, 0, len(req.Messages)+1)

	// Add system message if present
	if req.System != "" {
		messages = append(messages, map[string]interface{}{
			"role":    "system",
			"content": req.System,
		})
	}

	// Add conversation messages
	for _, msg := range req.Messages {
		openaiMsg := p.convertSingleMessageForTools(msg)
		messages = append(messages, openaiMsg)
	}

	return messages
}

// convertSingleMessageForTools converts a single message to OpenAI format including tool metadata
func (p *OpenAIToolProvider) convertSingleMessageForTools(msg types.Message) map[string]interface{} {
	// Convert message to OpenAI format (handles both legacy and multimodal)
	convertedMsg, err := p.convertMessageToOpenAI(msg)
	if err != nil {
		// Log error but continue with best effort (use Content as fallback)
		// This allows tool calls to work even if multimodal conversion fails
		convertedMsg = openAIMessage{
			Role:    msg.Role,
			Content: msg.GetContent(),
		}
	}

	// Build the message map with content
	openaiMsg := map[string]interface{}{
		"role":    convertedMsg.Role,
		"content": convertedMsg.Content,
	}

	// Add tool-related fields if present
	if len(msg.ToolCalls) > 0 {
		openaiMsg["tool_calls"] = p.convertToolCallsToOpenAI(msg.ToolCalls)
	}

	// Handle tool result messages
	if msg.Role == "tool" && msg.ToolResult != nil {
		openaiMsg["tool_call_id"] = msg.ToolResult.ID
		openaiMsg["name"] = msg.ToolResult.Name
	}

	return openaiMsg
}

// convertToolCallsToOpenAI converts ToolCalls to OpenAI format
func (p *OpenAIToolProvider) convertToolCallsToOpenAI(toolCalls []types.MessageToolCall) []map[string]interface{} {
	result := make([]map[string]interface{}, len(toolCalls))
	for i, tc := range toolCalls {
		result[i] = map[string]interface{}{
			"id":   tc.ID,
			"type": "function",
			"function": map[string]interface{}{
				"name":      tc.Name,
				"arguments": string(tc.Args),
			},
		}
	}
	return result
}

// addToolChoiceToRequest adds tool_choice parameter based on the choice string
func (p *OpenAIToolProvider) addToolChoiceToRequest(openaiReq map[string]interface{}, toolChoice string) {
	if toolChoice == "" || toolChoice == "auto" {
		return
	}

	switch toolChoice {
	case "required":
		openaiReq["tool_choice"] = "required"
	case "none":
		openaiReq["tool_choice"] = "none"
	default:
		// Specific function name
		openaiReq["tool_choice"] = map[string]interface{}{
			"type": "function",
			"function": map[string]string{
				"name": toolChoice,
			},
		}
	}
}

// parseToolResponse parses the OpenAI response and extracts tool calls
func (p *OpenAIToolProvider) parseToolResponse(respBytes []byte) (ChatResponse, []types.MessageToolCall, error) {
	var openaiResp struct {
		Choices []struct {
			Message struct {
				Content   string           `json:"content"`
				ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens        int `json:"prompt_tokens"`
			CompletionTokens    int `json:"completion_tokens"`
			PromptTokensDetails *struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details,omitempty"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(respBytes, &openaiResp); err != nil {
		return ChatResponse{}, nil, fmt.Errorf("failed to parse OpenAI response: %w", err)
	}

	if len(openaiResp.Choices) == 0 {
		return ChatResponse{}, nil, fmt.Errorf("no choices in OpenAI response")
	}

	choice := openaiResp.Choices[0]

	// Get cached tokens if available
	cachedTokens := 0
	if openaiResp.Usage.PromptTokensDetails != nil {
		cachedTokens = openaiResp.Usage.PromptTokensDetails.CachedTokens
	}

	// Calculate cost breakdown
	costBreakdown := p.OpenAIProvider.CalculateCost(openaiResp.Usage.PromptTokens, openaiResp.Usage.CompletionTokens, cachedTokens)

	resp := ChatResponse{
		Content:  choice.Message.Content,
		CostInfo: &costBreakdown,
		Raw:      respBytes,
	}

	// Extract tool calls
	var toolCalls []types.MessageToolCall
	for _, tc := range choice.Message.ToolCalls {
		toolCalls = append(toolCalls, types.MessageToolCall{
			Name: tc.Function.Name,
			Args: json.RawMessage(tc.Function.Arguments), // Convert string to RawMessage
			ID:   tc.ID,
		})
	}

	return resp, toolCalls, nil
}

// makeRequest makes an HTTP request to the OpenAI API
func (p *OpenAIToolProvider) makeRequest(ctx context.Context, request interface{}) ([]byte, error) {
	reqBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+openAIChatCompletionsPath, bytes.NewBuffer(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set(contentTypeHeader, applicationJSON)
	req.Header.Set(authorizationHeader, bearerPrefix+p.apiKey)

	logger.APIRequest("OpenAI", "POST", p.baseURL+openAIChatCompletionsPath, map[string]string{
		contentTypeHeader:   applicationJSON,
		authorizationHeader: bearerPrefix + p.apiKey,
	}, request)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	logger.APIResponse("OpenAI", resp.StatusCode, string(respBytes), nil)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	return respBytes, nil
}
