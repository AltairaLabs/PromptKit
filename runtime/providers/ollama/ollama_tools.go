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

// Tool choice constants
const (
	toolChoiceRequired = "required"
	toolChoiceNone     = "none"
	toolChoiceAuto     = "auto"
)

// ToolProvider extends Provider with tool support
type ToolProvider struct {
	*Provider
}

// NewToolProvider creates a new Ollama provider with tool support
func NewToolProvider(
	id, model, baseURL string,
	defaults providers.ProviderDefaults,
	includeRawOutput bool,
	additionalConfig map[string]any,
) *ToolProvider {
	return &ToolProvider{
		Provider: NewProvider(id, model, baseURL, defaults, includeRawOutput, additionalConfig),
	}
}

// Ollama-specific tool structures (OpenAI-compatible format)
type ollamaTool struct {
	Type     string             `json:"type"`
	Function ollamaToolFunction `json:"function"`
}

type ollamaToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ollamaToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function ollamaFunctionCall `json:"function"`
}

type ollamaFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // Ollama returns this as a string
}

// BuildTooling converts tool descriptors to Ollama format
func (p *ToolProvider) BuildTooling(descriptors []*providers.ToolDescriptor) (any, error) {
	if len(descriptors) == 0 {
		return nil, nil
	}

	tools := make([]ollamaTool, len(descriptors))
	for i, desc := range descriptors {
		tools[i] = ollamaTool{
			Type: "function",
			Function: ollamaToolFunction{
				Name:        desc.Name,
				Description: desc.Description,
				Parameters:  desc.InputSchema,
			},
		}
	}

	return tools, nil
}

// PredictWithTools performs a prediction request with tool support
func (p *ToolProvider) PredictWithTools(
	ctx context.Context,
	req providers.PredictionRequest,
	tools any,
	toolChoice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	// Track latency - START timing
	start := time.Now()

	// Build Ollama request with tools
	ollamaReq := p.buildToolRequest(req, tools, toolChoice)

	// Prepare response with raw request if configured (set early to preserve on error)
	predictResp := providers.PredictionResponse{}
	if p.ShouldIncludeRawOutput() {
		predictResp.RawRequest = ollamaReq
	}

	// Make the API call
	respBytes, err := p.makeRequest(ctx, ollamaReq)
	if err != nil {
		predictResp.Latency = time.Since(start)
		return predictResp, nil, err
	}

	// Calculate latency immediately after API call completes
	latency := time.Since(start)

	// Parse response
	resp, toolCalls, err := p.parseToolResponse(respBytes)
	if err != nil {
		resp.Latency = latency
		resp.RawRequest = predictResp.RawRequest
		return resp, nil, err
	}

	// Set latency on response
	resp.Latency = latency
	resp.RawRequest = predictResp.RawRequest

	return resp, toolCalls, nil
}

// buildToolRequest constructs the Ollama API request with tools
func (p *ToolProvider) buildToolRequest(
	req providers.PredictionRequest,
	tools any,
	toolChoice string,
) map[string]any {
	messages := p.convertRequestMessagesToOllama(req)

	// Apply defaults to zero-valued request parameters
	temperature, topP, maxTokens := p.applyRequestDefaults(req)

	// Build request
	ollamaReq := map[string]any{
		"model":       p.model,
		"messages":    messages,
		"temperature": temperature,
		"top_p":       topP,
		"max_tokens":  maxTokens,
	}

	if req.Seed != nil {
		ollamaReq["seed"] = *req.Seed
	}

	if p.keepAlive != "" {
		ollamaReq["keep_alive"] = p.keepAlive
	}

	// Add tools if provided
	if tools != nil {
		ollamaReq["tools"] = tools
		p.addToolChoiceToRequest(ollamaReq, toolChoice)
	}

	return ollamaReq
}

// convertRequestMessagesToOllama converts all messages in a request to Ollama format
func (p *ToolProvider) convertRequestMessagesToOllama(
	req providers.PredictionRequest,
) []map[string]any {
	messages := make([]map[string]any, 0, len(req.Messages)+1)

	// Add system message if present
	if req.System != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": req.System,
		})
	}

	// Add conversation messages
	for i := range req.Messages {
		ollamaMsg := p.convertSingleMessageForTools(&req.Messages[i])
		messages = append(messages, ollamaMsg)
	}

	return messages
}

// convertSingleMessageForTools converts a single message to Ollama format including tool metadata
func (p *ToolProvider) convertSingleMessageForTools(msg *types.Message) map[string]any {
	// Convert message to Ollama format (handles both legacy and multimodal)
	convertedMsg, err := p.convertMessageToOllama(msg)
	if err != nil {
		// Log error but continue with best effort (use Content as fallback)
		convertedMsg = ollamaMessage{
			Role:    msg.Role,
			Content: msg.GetContent(),
		}
	}

	// Build the message map with content
	ollamaMsg := map[string]any{
		"role":    convertedMsg.Role,
		"content": convertedMsg.Content,
	}

	// Add tool-related fields if present
	if len(msg.ToolCalls) > 0 {
		ollamaMsg["tool_calls"] = p.convertToolCallsToOllama(msg.ToolCalls)
	}

	// Handle tool result messages
	if msg.Role == "tool" && msg.ToolResult != nil {
		ollamaMsg["tool_call_id"] = msg.ToolResult.ID
		ollamaMsg["name"] = msg.ToolResult.Name
	}

	return ollamaMsg
}

// convertToolCallsToOllama converts ToolCalls to Ollama format
func (p *ToolProvider) convertToolCallsToOllama(toolCalls []types.MessageToolCall) []map[string]any {
	result := make([]map[string]any, len(toolCalls))
	for i, tc := range toolCalls {
		result[i] = map[string]any{
			"id":   tc.ID,
			"type": "function",
			"function": map[string]any{
				"name":      tc.Name,
				"arguments": string(tc.Args),
			},
		}
	}
	return result
}

// addToolChoiceToRequest adds tool_choice parameter based on the choice string
func (p *ToolProvider) addToolChoiceToRequest(ollamaReq map[string]any, toolChoice string) {
	if toolChoice == "" || toolChoice == toolChoiceAuto {
		return
	}

	switch toolChoice {
	case toolChoiceRequired:
		ollamaReq["tool_choice"] = toolChoiceRequired
	case toolChoiceNone:
		ollamaReq["tool_choice"] = toolChoiceNone
	default:
		// Specific function name
		ollamaReq["tool_choice"] = map[string]any{
			"type": "function",
			"function": map[string]string{
				"name": toolChoice,
			},
		}
	}
}

// parseToolResponse parses the Ollama response and extracts tool calls
func (p *ToolProvider) parseToolResponse(
	respBytes []byte,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	var ollamaResp struct {
		Choices []struct {
			Message struct {
				Content   string           `json:"content"`
				ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(respBytes, &ollamaResp); err != nil {
		return providers.PredictionResponse{}, nil, fmt.Errorf("failed to parse Ollama response: %w", err)
	}

	if len(ollamaResp.Choices) == 0 {
		return providers.PredictionResponse{}, nil, fmt.Errorf("no choices in Ollama response")
	}

	choice := ollamaResp.Choices[0]

	// Calculate cost breakdown (free for Ollama)
	costBreakdown := p.CalculateCost(
		ollamaResp.Usage.PromptTokens, ollamaResp.Usage.CompletionTokens, 0,
	)

	resp := providers.PredictionResponse{
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

// makeRequest makes an HTTP request to the Ollama API
func (p *ToolProvider) makeRequest(ctx context.Context, request any) ([]byte, error) {
	reqBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := p.baseURL + ollamaChatCompletionsPath
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Ollama doesn't require Authorization header
	req.Header.Set(contentTypeHeader, applicationJSON)

	logger.APIRequest("Ollama", "POST", url, map[string]string{
		contentTypeHeader: applicationJSON,
	}, request)

	resp, err := p.GetHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	logger.APIResponse("Ollama", resp.StatusCode, string(respBytes), nil)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	return respBytes, nil
}

// PredictStreamWithTools performs a streaming predict request with tool support
func (p *ToolProvider) PredictStreamWithTools(
	ctx context.Context,
	req providers.PredictionRequest,
	tools any,
	toolChoice string,
) (<-chan providers.StreamChunk, error) {
	// Build Ollama request with tools (same as non-streaming)
	ollamaReq := p.buildToolRequest(req, tools, toolChoice)

	// Add streaming options
	ollamaReq["stream"] = true

	reqBody, err := json.Marshal(ollamaReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request
	url := p.baseURL + ollamaChatCompletionsPath
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Ollama doesn't require Authorization header
	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if err := providers.CheckHTTPError(resp); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}

	outChan := make(chan providers.StreamChunk)

	go p.streamResponse(ctx, resp.Body, outChan)

	return outChan, nil
}

//nolint:gochecknoinits // Factory registration requires init
func init() {
	providers.RegisterProviderFactory("ollama", func(spec providers.ProviderSpec) (providers.Provider, error) {
		return NewToolProvider(
			spec.ID, spec.Model, spec.BaseURL, spec.Defaults,
			spec.IncludeRawOutput, spec.AdditionalConfig,
		), nil
	})
}
