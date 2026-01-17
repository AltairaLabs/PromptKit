package vllm

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

// Tool choice constants
const (
	toolChoiceRequired = "required"
	toolChoiceNone     = "none"
	toolChoiceAuto     = "auto"
	sseDoneMessage     = "[DONE]"
	streamBufferSize   = 10
)

// vLLM-specific tool structures (OpenAI-compatible format)
type vllmTool struct {
	Type     string           `json:"type"`
	Function vllmToolFunction `json:"function"`
}

type vllmToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type vllmToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function vllmFunctionCall `json:"function"`
}

type vllmFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // vLLM returns this as a JSON string
}

// BuildTooling converts tool descriptors to vLLM format
func (p *Provider) BuildTooling(descriptors []*providers.ToolDescriptor) (any, error) {
	if len(descriptors) == 0 {
		return nil, nil
	}

	tools := make([]vllmTool, len(descriptors))
	for i, desc := range descriptors {
		tools[i] = vllmTool{
			Type: "function",
			Function: vllmToolFunction{
				Name:        desc.Name,
				Description: desc.Description,
				Parameters:  desc.InputSchema,
			},
		}
	}

	return tools, nil
}

// PredictWithTools performs a prediction request with tool support
//
//nolint:gocritic,gocognit // req size matches ToolSupport interface, complexity from tool call extraction
func (p *Provider) PredictWithTools(
	ctx context.Context,
	req providers.PredictionRequest,
	tools any,
	toolChoice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	// Track latency
	start := time.Now()

	// Prepare messages
	messages, err := p.prepareMessages(&req)
	if err != nil {
		return providers.PredictionResponse{}, nil, fmt.Errorf("failed to prepare messages: %w", err)
	}

	// Apply defaults
	temperature, topP, maxTokens := p.applyRequestDefaults(&req)

	// Build vLLM request with tools
	vllmReq := p.buildToolRequest(&req, messages, temperature, topP, maxTokens, false, tools, toolChoice)

	// Prepare response with raw request if configured
	predictResp := providers.PredictionResponse{}
	if p.ShouldIncludeRawOutput() {
		rawReq, marshalErr := json.Marshal(vllmReq)
		if marshalErr == nil {
			predictResp.RawRequest = string(rawReq)
		}
	}

	// Serialize request
	reqBody, err := json.Marshal(vllmReq)
	if err != nil {
		return providers.PredictionResponse{}, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return providers.PredictionResponse{}, nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	// Send request
	httpResp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		return providers.PredictionResponse{}, nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer httpResp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return providers.PredictionResponse{}, nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check for HTTP errors
	if httpResp.StatusCode != http.StatusOK {
		var apiErr vllmErrorResponse
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
			return providers.PredictionResponse{}, nil, fmt.Errorf("vLLM API error: %s", apiErr.Error.Message)
		}
		return providers.PredictionResponse{}, nil,
			fmt.Errorf("vLLM API error: HTTP %d: %s", httpResp.StatusCode, string(respBody))
	}

	// Parse response
	var vllmResp vllmChatResponse
	if err := json.Unmarshal(respBody, &vllmResp); err != nil {
		return providers.PredictionResponse{}, nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Include raw output if configured
	if p.ShouldIncludeRawOutput() {
		predictResp.Raw = respBody
	}

	// Extract response
	if len(vllmResp.Choices) == 0 {
		return providers.PredictionResponse{}, nil, fmt.Errorf("no choices in response")
	}

	choice := vllmResp.Choices[0]
	predictResp.Content = choice.Message.Content.(string)

	// Calculate cost
	costInfo := p.CalculateCost(vllmResp.Usage.PromptTokens, vllmResp.Usage.CompletionTokens, 0)
	predictResp.CostInfo = &costInfo

	// Set latency
	predictResp.Latency = time.Since(start)

	// Extract tool calls if present
	var toolCalls []types.MessageToolCall
	if len(choice.Message.ToolCalls) > 0 {
		toolCalls = make([]types.MessageToolCall, len(choice.Message.ToolCalls))
		for i, tc := range choice.Message.ToolCalls {
			toolCalls[i] = types.MessageToolCall{
				ID:   tc.ID,
				Name: tc.Function.Name,
				Args: json.RawMessage(tc.Function.Arguments),
			}
		}
	}

	return predictResp, toolCalls, nil
}

// PredictStreamWithTools performs a streaming prediction request with tool support
//
//nolint:gocritic // req size matches ToolSupport interface
func (p *Provider) PredictStreamWithTools(
	ctx context.Context,
	req providers.PredictionRequest,
	tools any,
	toolChoice string,
) (<-chan providers.StreamChunk, error) {
	// Prepare messages
	messages, err := p.prepareMessages(&req)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare messages: %w", err)
	}

	// Apply defaults
	temperature, topP, maxTokens := p.applyRequestDefaults(&req)

	// Build vLLM request with tools (stream=true)
	vllmReq := p.buildToolRequest(&req, messages, temperature, topP, maxTokens, true, tools, toolChoice)

	// Serialize request
	reqBody, err := json.Marshal(vllmReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	// Send request
	httpResp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Check for HTTP errors
	if httpResp.StatusCode != http.StatusOK {
		defer httpResp.Body.Close()
		respBody, _ := io.ReadAll(httpResp.Body)
		var apiErr vllmErrorResponse
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
			return nil, fmt.Errorf("vLLM API error: %s", apiErr.Error.Message)
		}
		return nil, fmt.Errorf("vLLM API error: HTTP %d: %s", httpResp.StatusCode, string(respBody))
	}

	// Create output channel
	chunks := make(chan providers.StreamChunk, streamBufferSize)

	// Start goroutine to process SSE stream
	go p.streamToolResponse(ctx, httpResp.Body, chunks)

	return chunks, nil
}

// buildToolRequest builds a vLLM API request with tools
func (p *Provider) buildToolRequest(
	req *providers.PredictionRequest,
	messages []vllmMessage,
	temperature, topP float32,
	maxTokens int,
	stream bool,
	tools any,
	toolChoice string,
) map[string]any {
	// Build base request using the existing buildRequest method
	vllmReq := p.buildRequest(req, messages, temperature, topP, maxTokens, stream)

	// Convert struct to map for tool additions
	reqMap := make(map[string]any)
	reqMap["model"] = vllmReq.Model
	reqMap["messages"] = vllmReq.Messages
	reqMap["temperature"] = vllmReq.Temperature
	reqMap["top_p"] = vllmReq.TopP
	reqMap["max_tokens"] = vllmReq.MaxTokens
	reqMap["stream"] = vllmReq.Stream

	if vllmReq.Seed != nil {
		reqMap["seed"] = vllmReq.Seed
	}
	if vllmReq.UseBeamSearch {
		reqMap["use_beam_search"] = vllmReq.UseBeamSearch
	}
	if vllmReq.BestOf > 0 {
		reqMap["best_of"] = vllmReq.BestOf
	}
	if vllmReq.IgnoreEOS {
		reqMap["ignore_eos"] = vllmReq.IgnoreEOS
	}
	if vllmReq.SkipSpecialTokens {
		reqMap["skip_special_tokens"] = vllmReq.SkipSpecialTokens
	}
	if vllmReq.GuidedJSON != nil {
		reqMap["guided_json"] = vllmReq.GuidedJSON
	}
	if vllmReq.GuidedRegex != "" {
		reqMap["guided_regex"] = vllmReq.GuidedRegex
	}
	if vllmReq.GuidedChoice != nil {
		reqMap["guided_choice"] = vllmReq.GuidedChoice
	}

	// Add tools if present
	if tools != nil {
		reqMap["tools"] = tools

		// Add tool choice if specified
		if toolChoice != "" {
			switch toolChoice {
			case toolChoiceRequired:
				reqMap["tool_choice"] = "required"
			case toolChoiceNone:
				reqMap["tool_choice"] = "none"
			case toolChoiceAuto:
				reqMap["tool_choice"] = "auto"
			default:
				// Specific tool choice (function name)
				reqMap["tool_choice"] = map[string]any{
					"type": "function",
					"function": map[string]any{
						"name": toolChoice,
					},
				}
			}
		}
	}

	return reqMap
}

// streamToolResponse processes the SSE stream for tool calls
//
//nolint:gocognit // complexity from SSE parsing and tool call accumulation
func (p *Provider) streamToolResponse(ctx context.Context, body io.ReadCloser, chunks chan<- providers.StreamChunk) {
	defer close(chunks)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	var accumulated strings.Builder
	var toolCalls []types.MessageToolCall

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			logger.Debug("Context canceled, stopping vLLM stream", "component", "vllm")
			return
		default:
		}

		line := scanner.Text()

		// Skip empty lines and comments
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// Parse SSE data line
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == sseDoneMessage {
			return
		}

		// Parse JSON chunk
		var chunk vllmStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			logger.Debug("Failed to parse stream chunk", "component", "vllm", "error", err, "data", data)
			continue
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]

		// Handle content delta
		if choice.Delta.Content != "" {
			accumulated.WriteString(choice.Delta.Content)
			chunks <- providers.StreamChunk{
				Delta: choice.Delta.Content,
			}
		}

		// Handle tool call deltas
		if len(choice.Delta.ToolCalls) > 0 {
			for _, tc := range choice.Delta.ToolCalls {
				// Accumulate tool calls
				if tc.Index == nil {
					continue
				}
				idx := *tc.Index
				// Ensure toolCalls slice is large enough
				for len(toolCalls) <= idx {
					toolCalls = append(toolCalls, types.MessageToolCall{})
				}

				// Update tool call at index
				if tc.ID != "" {
					toolCalls[idx].ID = tc.ID
				}
				if tc.Function.Name != "" {
					toolCalls[idx].Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					toolCalls[idx].Args = append(toolCalls[idx].Args, []byte(tc.Function.Arguments)...)
				}
			}
		}

		// Check for finish reason
		if choice.FinishReason != "" {
			finishReason := choice.FinishReason
			chunks <- providers.StreamChunk{
				FinishReason: &finishReason,
				ToolCalls:    toolCalls,
			}
		}
	}

	if err := scanner.Err(); err != nil {
		chunks <- providers.StreamChunk{
			Error: fmt.Errorf("stream scan error: %w", err),
		}
	}
}
