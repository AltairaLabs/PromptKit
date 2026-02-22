// Package openai provides OpenAI LLM provider integration.
package openai

import (
	"bufio"
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

// API mode constants
const (
	APIModeResponses   = "responses"   // New Responses API (v1/responses)
	APIModeCompletions = "completions" // Legacy Chat Completions API (v1/chat/completions)

	responsesAPIPath = "/responses"

	// Event types for Responses API streaming
	eventTypeTextDelta     = "response.output_text.delta"
	eventTypeFuncArgsDelta = "response.function_call_arguments.delta"
	eventTypeOutputAdded   = "response.output_item.added"
	eventTypeCompleted     = "response.completed"
	eventTypeError         = "error"

	// Common string constants
	toolChoiceAuto        = "auto"
	roleToolResult        = "tool"
	typeFunctionCall      = "function_call"
	finishStop            = "stop"
	finishCanceled        = "canceled"
	finishError           = "error"
	partTypeText          = "text"
	defaultResponseSchema = "response_schema"
	sseStreamDone         = "[DONE]"
)

// APIMode represents the OpenAI API mode to use
type APIMode string

// requiresResponsesAPI returns true if the model only works with the Responses API.
// Some models like o1-pro are only available via v1/responses.
func requiresResponsesAPI(model string) bool {
	// o1-pro requires the Responses API
	return model == "o1-pro"
}

// transformToResponsesCallID converts a call ID to Responses API format.
// The Responses API requires function call IDs to start with 'fc_'.
// Chat Completions API uses 'call_' prefix which must be transformed.
func transformToResponsesCallID(callID string) string {
	// If already in Responses format, return as-is
	if strings.HasPrefix(callID, "fc_") {
		return callID
	}
	// Transform call_ prefix to fc_ prefix
	if strings.HasPrefix(callID, "call_") {
		return "fc_" + strings.TrimPrefix(callID, "call_")
	}
	// For any other format, add fc_ prefix
	return "fc_" + callID
}

// getAPIMode determines which API to use based on config and model.
// Priority:
//  1. Model requirement (o1-pro requires responses)
//  2. Explicit config setting
//  3. Default to Responses API
func getAPIMode(model string, additionalConfig map[string]any) APIMode {
	// If model requires Responses API, use it regardless of config
	if requiresResponsesAPI(model) {
		return APIModeResponses
	}

	// Check explicit config for legacy API
	if additionalConfig != nil {
		if mode, ok := additionalConfig["api_mode"].(string); ok {
			switch strings.ToLower(mode) {
			case "completions", "chat_completions", "legacy":
				return APIModeCompletions
			case "responses":
				return APIModeResponses
			}
		}
	}

	// Default to Responses API
	return APIModeResponses
}

// Responses API response structures

// responsesResponse represents the response from the Responses API
type responsesResponse struct {
	ID        string            `json:"id"`
	Object    string            `json:"object"`
	CreatedAt int64             `json:"created_at"`
	Status    string            `json:"status"` // "completed", "failed", "in_progress"
	Model     string            `json:"model"`
	Output    []responsesOutput `json:"output"`
	Usage     *responsesUsage   `json:"usage,omitempty"`
	Error     *responsesError   `json:"error,omitempty"`
}

// responsesOutput represents an output item in the response
type responsesOutput struct {
	Type    string             `json:"type"` // "message", "function_call", etc.
	ID      string             `json:"id,omitempty"`
	Role    string             `json:"role,omitempty"`
	Content []responsesContent `json:"content,omitempty"`
	Name    string             `json:"name,omitempty"`      // For function calls
	CallID  string             `json:"call_id,omitempty"`   // For function calls
	Args    string             `json:"arguments,omitempty"` // For function calls
}

// responsesContent represents content within an output
type responsesContent struct {
	Type string `json:"type"` // "output_text", "refusal", etc.
	Text string `json:"text,omitempty"`
}

// responsesUsage represents token usage in Responses API
type responsesUsage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	InputTokensCached  int `json:"input_tokens_cached,omitempty"`
	OutputTokensCached int `json:"output_tokens_cached,omitempty"`
}

// responsesError represents an error in the Responses API
type responsesError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Streaming event structures

// responsesStreamEvent represents a streaming event from the Responses API
type responsesStreamEvent struct {
	Type     string          `json:"type"`
	Sequence int             `json:"sequence,omitempty"`
	Response json.RawMessage `json:"response,omitempty"`
	Delta    json.RawMessage `json:"delta,omitempty"`
	Item     json.RawMessage `json:"item,omitempty"`
	Part     json.RawMessage `json:"part,omitempty"`
}

// buildResponsesRequest constructs a Responses API request from a PredictionRequest
//
//nolint:gocritic // hugeParam: interface requires value receiver for compatibility
func (p *Provider) buildResponsesRequest(req providers.PredictionRequest, tools any, toolChoice string) map[string]any {
	// Convert messages to Responses API input format
	input := p.convertMessagesToResponsesInput(req.Messages)

	// Apply defaults
	temperature, topP, maxTokens := p.applyRequestDefaults(req)

	// Build request map for flexibility
	responsesReq := map[string]any{
		"model": p.model,
		"input": input,
	}

	// Add system instructions if present
	if req.System != "" {
		responsesReq["instructions"] = req.System
	}

	// Add max_output_tokens
	if maxTokens > 0 {
		responsesReq["max_output_tokens"] = maxTokens
	}

	// Add sampling parameters (o-series models still don't support these in Responses API)
	if !isOSeriesModel(p.model) {
		if temperature > 0 {
			responsesReq["temperature"] = temperature
		}
		if topP > 0 {
			responsesReq["top_p"] = topP
		}
	}

	// Add tools if provided
	if tools != nil {
		responsesReq["tools"] = p.convertToolsToResponsesFormat(tools)
		if toolChoice != "" && toolChoice != toolChoiceAuto {
			responsesReq["tool_choice"] = toolChoice
		}
	}

	// Add response format if specified
	if req.ResponseFormat != nil {
		responsesReq["text"] = p.convertResponseFormatToResponses(req.ResponseFormat)
	}

	return responsesReq
}

// convertMessagesToResponsesInput converts messages to Responses API input format
// The Responses API expects a flat list where tool calls are separate function_call items
func (p *Provider) convertMessagesToResponsesInput(messages []types.Message) []any {
	// Allocate extra capacity for tool calls which become separate items
	const toolCallCapacityMultiplier = 2
	input := make([]any, 0, len(messages)*toolCallCapacityMultiplier)
	for i := range messages {
		items := p.convertSingleMessageToResponsesInput(&messages[i])
		for _, item := range items {
			input = append(input, item)
		}
	}
	return input
}

// convertSingleMessageToResponsesInput converts a single message to Responses API format
// Returns a slice because assistant messages with tool calls become multiple items
func (p *Provider) convertSingleMessageToResponsesInput(msg *types.Message) []map[string]any {
	// Handle tool results - these are function_call_output items
	if msg.Role == roleToolResult && msg.ToolResult != nil {
		// Transform call ID to Responses API format (must start with 'fc_')
		callID := transformToResponsesCallID(msg.ToolResult.ID)
		return []map[string]any{{
			"type":    "function_call_output",
			"call_id": callID,
			"output":  msg.ToolResult.Content,
		}}
	}

	// Handle assistant messages with tool calls
	// In Responses API, tool calls become separate function_call items
	if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
		items := make([]map[string]any, 0, len(msg.ToolCalls)+1)

		// Add text content as a message if present
		content := msg.GetContent()
		if content != "" {
			items = append(items, map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{{
					"type": "output_text",
					"text": content,
				}},
			})
		}

		// Add each tool call as a separate function_call item
		for _, tc := range msg.ToolCalls {
			// Parse the args JSON to get actual object
			var args any
			if err := json.Unmarshal(tc.Args, &args); err != nil {
				args = string(tc.Args) // Fallback to string if not valid JSON
			}
			// Transform call ID to Responses API format (must start with 'fc_')
			callID := transformToResponsesCallID(tc.ID)
			items = append(items, map[string]any{
				"type":      typeFunctionCall,
				"id":        callID,
				"call_id":   callID,
				"name":      tc.Name,
				"arguments": args,
			})
		}
		return items
	}

	// Regular message (user or assistant without tool calls)
	inputMsg := map[string]any{
		"role": msg.Role,
	}

	// Handle content (multimodal or simple text)
	inputMsg["content"] = p.getMessageContent(msg)

	return []map[string]any{inputMsg}
}

// getMessageContent extracts content from a message in Responses API format
func (p *Provider) getMessageContent(msg *types.Message) any {
	if len(msg.Parts) == 0 {
		return msg.GetContent()
	}

	parts := make([]map[string]any, 0, len(msg.Parts))
	for i := range msg.Parts {
		if part := p.convertPartToResponsesFormat(&msg.Parts[i]); part != nil {
			parts = append(parts, part)
		}
	}
	return parts
}

// convertPartToResponsesFormat converts a single message part to Responses API format
func (p *Provider) convertPartToResponsesFormat(part *types.ContentPart) map[string]any {
	switch part.Type {
	case partTypeText:
		return map[string]any{
			"type": "input_text",
			"text": part.Text,
		}
	case "image":
		if part.Media != nil {
			var imageURL string

			// Check for URL first
			if part.Media.URL != nil && *part.Media.URL != "" {
				imageURL = *part.Media.URL
			} else if part.Media.Data != nil && *part.Media.Data != "" {
				// Handle base64 data - construct data URL
				mimeType := part.Media.MIMEType
				if mimeType == "" {
					mimeType = "image/png" // Default mime type
				}
				imageURL = "data:" + mimeType + ";base64," + *part.Media.Data
			}

			if imageURL != "" {
				// Responses API expects image_url as a string (the URL directly)
				return map[string]any{
					"type":      "input_image",
					"image_url": imageURL,
				}
			}
		}
	}
	return nil
}

// convertToolsToResponsesFormat converts tools to Responses API format
func (p *Provider) convertToolsToResponsesFormat(tools any) []any {
	// Tools format is similar but uses "function" type with slightly different structure
	openAITools, ok := tools.([]openAITool)
	if !ok {
		return nil
	}

	result := make([]any, len(openAITools))
	for i, tool := range openAITools {
		result[i] = map[string]any{
			"type":        "function",
			"name":        tool.Function.Name,
			"description": tool.Function.Description,
			"parameters":  tool.Function.Parameters,
		}
	}
	return result
}

// convertResponseFormatToResponses converts response format to Responses API format
func (p *Provider) convertResponseFormatToResponses(rf *providers.ResponseFormat) map[string]any {
	if rf == nil {
		return nil
	}

	result := map[string]any{
		"format": map[string]any{
			"type": string(rf.Type),
		},
	}

	if rf.Type == providers.ResponseFormatJSONSchema && len(rf.JSONSchema) > 0 {
		var schema any
		if err := json.Unmarshal(rf.JSONSchema, &schema); err == nil {
			schemaName := rf.SchemaName
			if schemaName == "" {
				schemaName = defaultResponseSchema
			}
			result["format"] = map[string]any{
				"type": "json_schema",
				"json_schema": map[string]any{
					"name":   schemaName,
					"schema": schema,
					"strict": rf.Strict,
				},
			}
		}
	}

	return result
}

// predictWithResponses performs a prediction using the Responses API
//
//nolint:gocritic // hugeParam: interface signature requires value receiver for compatibility
func (p *Provider) predictWithResponses(
	ctx context.Context,
	req providers.PredictionRequest,
	tools any,
	toolChoice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	ctx = logger.WithLoggingContext(ctx, &logger.LoggingFields{
		Provider: p.ID(),
		Model:    p.model,
	})

	start := time.Now()

	// Build request
	responsesReq := p.buildResponsesRequest(req, tools, toolChoice)

	// Prepare response with raw request if configured
	predictResp := providers.PredictionResponse{}
	if p.ShouldIncludeRawOutput() {
		predictResp.RawRequest = responsesReq
	}

	reqBody, err := json.Marshal(responsesReq)
	if err != nil {
		return predictResp, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request
	url := p.baseURL + responsesAPIPath
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return predictResp, nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	if authErr := p.applyAuth(ctx, httpReq); authErr != nil {
		return predictResp, nil, fmt.Errorf("failed to apply authentication: %w", authErr)
	}

	logger.APIRequest("OpenAI Responses", "POST", url, map[string]string{
		contentTypeHeader:   applicationJSON,
		authorizationHeader: "***",
	}, responsesReq)

	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		predictResp.Latency = time.Since(start)
		return predictResp, nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		predictResp.Latency = time.Since(start)
		return predictResp, nil, fmt.Errorf("failed to read response body: %w", err)
	}

	logger.APIResponse("OpenAI Responses", resp.StatusCode, string(respBody), nil)

	if resp.StatusCode != http.StatusOK {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		if p.platform != "" {
			return predictResp, nil, providers.ParsePlatformHTTPError(p.platform, resp.StatusCode, respBody)
		}
		return predictResp, nil, fmt.Errorf("API request to %s failed with status %d: %s",
			url, resp.StatusCode, string(respBody))
	}

	// Parse response
	var responsesResp responsesResponse
	if err := json.Unmarshal(respBody, &responsesResp); err != nil {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return predictResp, nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if responsesResp.Error != nil {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return predictResp, nil, fmt.Errorf("OpenAI API error: %s", responsesResp.Error.Message)
	}

	latency := time.Since(start)

	// Extract content and tool calls from output
	content, toolCalls := p.extractResponsesOutput(responsesResp.Output)

	// Calculate cost
	var costInfo *types.CostInfo
	if responsesResp.Usage != nil {
		cost := p.CalculateCost(
			responsesResp.Usage.InputTokens,
			responsesResp.Usage.OutputTokens,
			responsesResp.Usage.InputTokensCached,
		)
		costInfo = &cost
	}

	predictResp.Content = content
	predictResp.CostInfo = costInfo
	predictResp.Latency = latency
	predictResp.Raw = respBody

	return predictResp, toolCalls, nil
}

// extractResponsesOutput extracts text content and tool calls from Responses API output
func (p *Provider) extractResponsesOutput(outputs []responsesOutput) (string, []types.MessageToolCall) {
	var content strings.Builder
	var toolCalls []types.MessageToolCall

	for _, output := range outputs {
		switch output.Type {
		case "message":
			for _, c := range output.Content {
				if c.Type == "output_text" {
					content.WriteString(c.Text)
				}
			}
		case "function_call":
			toolCalls = append(toolCalls, types.MessageToolCall{
				ID:   output.CallID,
				Name: output.Name,
				Args: json.RawMessage(output.Args),
			})
		}
	}

	return content.String(), toolCalls
}

// predictStreamWithResponses performs a streaming prediction using the Responses API
//
//nolint:gocritic // hugeParam: interface signature requires value receiver for compatibility
func (p *Provider) predictStreamWithResponses(
	ctx context.Context,
	req providers.PredictionRequest,
	tools any,
	toolChoice string,
) (<-chan providers.StreamChunk, error) {
	ctx = logger.WithLoggingContext(ctx, &logger.LoggingFields{
		Provider: p.ID(),
		Model:    p.model,
	})

	// Build request with streaming enabled
	responsesReq := p.buildResponsesRequest(req, tools, toolChoice)
	responsesReq["stream"] = true

	reqBody, err := json.Marshal(responsesReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request
	url := p.baseURL + responsesAPIPath
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	httpReq.Header.Set("Accept", "text/event-stream")
	if authErr := p.applyAuth(ctx, httpReq); authErr != nil {
		return nil, fmt.Errorf("failed to apply authentication: %w", authErr)
	}

	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if err := providers.CheckHTTPError(resp, url); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}

	outChan := make(chan providers.StreamChunk)
	go p.streamResponsesResponse(ctx, resp.Body, outChan)

	return outChan, nil
}

// sendFinalChunk sends the final stream chunk with accumulated content
func (p *Provider) sendFinalChunk(
	outChan chan<- providers.StreamChunk,
	accumulated string,
	toolCalls []types.MessageToolCall,
	totalTokens int,
	usage *responsesUsage,
) {
	reason := finishStop
	finalChunk := providers.StreamChunk{
		Content:      accumulated,
		ToolCalls:    toolCalls,
		TokenCount:   totalTokens,
		FinishReason: &reason,
	}
	if usage != nil {
		cost := p.CalculateCost(usage.InputTokens, usage.OutputTokens, usage.InputTokensCached)
		finalChunk.CostInfo = &cost
	}
	outChan <- finalChunk
}

// handleStreamEvent processes a single streaming event and returns updated state
//
//nolint:gocritic // hugeParam: event passed by value for simplicity in switch dispatch
func (p *Provider) handleStreamEvent(
	event responsesStreamEvent,
	data string,
	accumulated string,
	totalTokens int,
	toolCalls []types.MessageToolCall,
	usage *responsesUsage,
	outChan chan<- providers.StreamChunk,
) (newAccumulated string, newTokens int, newToolCalls []types.MessageToolCall, newUsage *responsesUsage) {
	switch event.Type {
	case eventTypeTextDelta:
		return p.handleTextDelta(data, accumulated, totalTokens, toolCalls, outChan)

	case eventTypeFuncArgsDelta:
		return accumulated, totalTokens, p.handleFuncArgsDelta(event, toolCalls), usage

	case eventTypeOutputAdded:
		return accumulated, totalTokens, p.handleOutputAdded(data, toolCalls), usage

	case eventTypeCompleted:
		usage = p.handleCompleted(data, accumulated, toolCalls, totalTokens, outChan)
		return accumulated, totalTokens, toolCalls, usage

	case eventTypeError:
		p.handleErrorEvent(data, accumulated, toolCalls, outChan)
		return accumulated, totalTokens, toolCalls, usage
	}

	return accumulated, totalTokens, toolCalls, usage
}

// handleTextDelta processes text delta events
func (p *Provider) handleTextDelta(
	data string,
	accumulated string,
	totalTokens int,
	toolCalls []types.MessageToolCall,
	outChan chan<- providers.StreamChunk,
) (newAccumulated string, newTokens int, newToolCalls []types.MessageToolCall, newUsage *responsesUsage) {
	var textDelta struct {
		Type  string `json:"type"`
		Delta string `json:"delta"`
	}
	if err := json.Unmarshal([]byte(data), &textDelta); err == nil && textDelta.Delta != "" {
		accumulated += textDelta.Delta
		totalTokens++
		outChan <- providers.StreamChunk{
			Content:     accumulated,
			Delta:       textDelta.Delta,
			ToolCalls:   toolCalls,
			TokenCount:  totalTokens,
			DeltaTokens: 1,
		}
	}
	return accumulated, totalTokens, toolCalls, nil
}

// handleFuncArgsDelta processes function call arguments delta events
//
//nolint:gocritic // hugeParam: event passed by value for simplicity
func (p *Provider) handleFuncArgsDelta(
	event responsesStreamEvent,
	toolCalls []types.MessageToolCall,
) []types.MessageToolCall {
	var delta struct {
		CallID string `json:"call_id"`
		Delta  string `json:"delta"`
	}
	if err := json.Unmarshal(event.Delta, &delta); err == nil {
		found := false
		for i := range toolCalls {
			if toolCalls[i].ID == delta.CallID {
				// Replace placeholder {} with actual content, or append to existing
				currentArgs := string(toolCalls[i].Args)
				if currentArgs == "{}" || currentArgs == "" {
					toolCalls[i].Args = json.RawMessage(delta.Delta)
				} else {
					toolCalls[i].Args = append(toolCalls[i].Args, []byte(delta.Delta)...)
				}
				found = true
				break
			}
		}
		if !found {
			toolCalls = append(toolCalls, types.MessageToolCall{
				ID:   delta.CallID,
				Args: json.RawMessage(delta.Delta),
			})
		}
	}
	return toolCalls
}

// handleOutputAdded processes output item added events
func (p *Provider) handleOutputAdded(data string, toolCalls []types.MessageToolCall) []types.MessageToolCall {
	var item struct {
		Item struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
			Name   string `json:"name"`
		} `json:"item"`
	}
	if err := json.Unmarshal([]byte(data), &item); err == nil {
		if item.Item.Type == typeFunctionCall {
			toolCalls = append(toolCalls, types.MessageToolCall{
				ID:   item.Item.CallID,
				Name: item.Item.Name,
				Args: json.RawMessage(""), // Will be populated by delta events
			})
		}
	}
	return toolCalls
}

// handleCompleted processes response completed events
func (p *Provider) handleCompleted(
	data string,
	accumulated string,
	toolCalls []types.MessageToolCall,
	totalTokens int,
	outChan chan<- providers.StreamChunk,
) *responsesUsage {
	var completed struct {
		Response struct {
			Usage *responsesUsage `json:"usage"`
		} `json:"response"`
	}
	var usage *responsesUsage
	if err := json.Unmarshal([]byte(data), &completed); err == nil {
		usage = completed.Response.Usage
	}
	p.sendFinalChunk(outChan, accumulated, toolCalls, totalTokens, usage)
	return usage
}

// handleErrorEvent processes error events
func (p *Provider) handleErrorEvent(
	data string,
	accumulated string,
	toolCalls []types.MessageToolCall,
	outChan chan<- providers.StreamChunk,
) {
	var errEvent struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(data), &errEvent); err == nil {
		outChan <- providers.StreamChunk{
			Content:      accumulated,
			ToolCalls:    toolCalls,
			Error:        fmt.Errorf("stream error: %s", errEvent.Error.Message),
			FinishReason: providers.StringPtr(finishError),
		}
	}
}

// streamResponsesResponse handles streaming from the Responses API
//
//nolint:gocognit // Complexity 16 is acceptable for SSE stream parsing with context cancellation
func (p *Provider) streamResponsesResponse(
	ctx context.Context,
	body io.ReadCloser,
	outChan chan<- providers.StreamChunk,
) {
	defer close(outChan)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	accumulated := ""
	totalTokens := 0
	var accumulatedToolCalls []types.MessageToolCall
	var usage *responsesUsage

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			outChan <- providers.StreamChunk{
				Content:      accumulated,
				ToolCalls:    accumulatedToolCalls,
				Error:        ctx.Err(),
				FinishReason: providers.StringPtr(finishCanceled),
			}
			return
		default:
		}

		line := scanner.Text()

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// Parse SSE data
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == sseStreamDone {
			p.sendFinalChunk(outChan, accumulated, accumulatedToolCalls, totalTokens, usage)
			return
		}

		// Parse event
		var event responsesStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		// Handle different event types
		accumulated, totalTokens, accumulatedToolCalls, usage = p.handleStreamEvent(
			event, data, accumulated, totalTokens, accumulatedToolCalls, usage, outChan,
		)

		// Check if we should return (completed or error events signal this via usage being set and returned)
		if event.Type == eventTypeCompleted || event.Type == eventTypeError {
			return
		}
	}

	if err := scanner.Err(); err != nil {
		outChan <- providers.StreamChunk{
			Content:      accumulated,
			ToolCalls:    accumulatedToolCalls,
			Error:        err,
			FinishReason: providers.StringPtr(finishError),
		}
	}
}
