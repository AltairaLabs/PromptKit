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

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Tool choice constants
const (
	toolChoiceRequired = "required"
	toolChoiceNone     = "none"
	toolChoiceAuto     = "auto"
	sseDoneMessage     = "[DONE]"

	// OpenAI-compatible chat completions endpoint — vLLM uses this path.
	chatCompletionsPath = "/v1/chat/completions"
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

// toolTypeFunction is the only tool type the OpenAI-compatible API defines.
const toolTypeFunction = "function"

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
				Parameters:  types.NormalizeRawMessage(desc.InputSchema),
			},
		}
	}

	return tools, nil
}

// PredictWithTools performs a prediction request with tool support
//
//nolint:gocritic,gocognit // req size matches ToolSupport interface, complexity from tool call extraction
func (p *Provider) PredictWithTools( // NOSONAR
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
	vllmReq := p.buildToolRequest(&req, messages, toolRequestParams{
		temperature: temperature,
		topP:        topP,
		maxTokens:   maxTokens,
		stream:      false,
		tools:       tools,
		toolChoice:  toolChoice,
	})

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
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+chatCompletionsPath, bytes.NewReader(reqBody))
	if err != nil {
		return providers.PredictionResponse{}, nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	if hdrErr := p.ApplyCustomHeaders(httpReq); hdrErr != nil {
		return providers.PredictionResponse{}, nil, hdrErr
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
		url := p.baseURL + chatCompletionsPath
		body := string(respBody)
		var apiErr vllmErrorResponse
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
			body = apiErr.Error.Message
		}
		return providers.PredictionResponse{}, nil,
			&providers.ProviderHTTPError{StatusCode: httpResp.StatusCode, URL: url, Body: body, Provider: p.ID()}
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
	predictResp.Reasoning = reasoningFromContent(choice.Message.ReasoningContent)

	// Calculate cost from the full wire usage.
	costInfo := p.costFromUsage(vllmResp.Usage)
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
	vllmReq := p.buildToolRequest(&req, messages, toolRequestParams{
		temperature: temperature,
		topP:        topP,
		maxTokens:   maxTokens,
		stream:      true,
		tools:       tools,
		toolChoice:  toolChoice,
	})

	// Serialize request
	reqBody, err := json.Marshal(vllmReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+chatCompletionsPath, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	if hdrErr := p.ApplyCustomHeaders(httpReq); hdrErr != nil {
		return nil, hdrErr
	}

	// Send request
	httpResp, err := p.GetStreamingHTTPClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Check for HTTP errors
	if httpResp.StatusCode != http.StatusOK {
		defer httpResp.Body.Close()
		respBody, _ := io.ReadAll(httpResp.Body)
		url := p.baseURL + chatCompletionsPath
		body := string(respBody)
		var apiErr vllmErrorResponse
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
			body = apiErr.Error.Message
		}
		return nil, &providers.ProviderHTTPError{StatusCode: httpResp.StatusCode, URL: url, Body: body, Provider: p.ID()}
	}

	// Create output channel
	chunks := make(chan providers.StreamChunk, providers.DefaultStreamBufferSize)

	// Start goroutine to process SSE stream
	go p.streamToolResponse(ctx, httpResp.Body, chunks)

	return chunks, nil
}

// toolRequestParams groups parameters for building tool requests
type toolRequestParams struct {
	temperature float32
	topP        float32
	maxTokens   int
	stream      bool
	tools       any
	toolChoice  string
}

// buildToolRequest builds a vLLM API request with tools
func (p *Provider) buildToolRequest(
	req *providers.PredictionRequest,
	messages []vllmMessage,
	params toolRequestParams,
) map[string]any {
	// Build base request using the existing buildRequest method
	vllmReq := p.buildRequest(req, messages, params.temperature, params.topP, params.maxTokens, params.stream)

	// Convert struct to map for tool additions
	reqMap := make(map[string]any)
	reqMap["model"] = vllmReq.Model
	reqMap["messages"] = vllmReq.Messages
	reqMap["temperature"] = vllmReq.Temperature
	reqMap["top_p"] = vllmReq.TopP
	reqMap["max_tokens"] = vllmReq.MaxTokens
	reqMap["stream"] = vllmReq.Stream
	if vllmReq.StreamOptions != nil {
		reqMap["stream_options"] = vllmReq.StreamOptions
	}

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
	if params.tools != nil {
		reqMap["tools"] = params.tools

		// Add tool choice if specified
		if params.toolChoice != "" {
			switch params.toolChoice {
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
						"name": params.toolChoice,
					},
				}
			}
		}
	}

	return reqMap
}

// streamToolResponse processes the SSE stream for tool calls. A stream that
// ends without [DONE] or a finish_reason — canceled, a read error, or a
// server-side truncation — ends on an error chunk carrying the accumulated
// content and tool calls, never on a clean close.
func (p *Provider) streamToolResponse(ctx context.Context, body io.ReadCloser, chunks chan<- providers.StreamChunk) {
	defer close(chunks)
	defer body.Close()

	// Close the response body when context is canceled to unblock scanner.Scan()
	go func() {
		<-ctx.Done()
		_ = body.Close()
	}()

	scanner := bufio.NewScanner(body)
	st := &toolStreamState{}

	for scanner.Scan() {
		if ctx.Err() != nil {
			logger.Debug("Context canceled, stopping vLLM stream", "component", "vllm")
			chunks <- st.errorChunk(ctx.Err())
			return
		}

		data, ok := sseDataLine(scanner.Text())
		if !ok {
			continue
		}
		if data == sseDoneMessage {
			return
		}

		var chunk vllmStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			logger.Debug("Failed to parse stream chunk", "component", "vllm", "error", err, "data", data)
			continue
		}
		p.handleToolStreamChunk(&chunk, st, chunks)
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		chunks <- st.errorChunk(ctxErr)
		return
	}
	if err := scanner.Err(); err != nil {
		chunks <- st.errorChunk(fmt.Errorf("stream scan error: %w", err))
		return
	}
	if !st.finished {
		chunks <- st.errorChunk(fmt.Errorf("vllm stream ended before completion: %w", io.ErrUnexpectedEOF))
	}
}

// toolStreamState accumulates a tool stream's content and tool calls.
type toolStreamState struct {
	accumulated strings.Builder
	toolCalls   []types.MessageToolCall
	finished    bool
}

func (st *toolStreamState) errorChunk(err error) providers.StreamChunk {
	return providers.StreamChunk{
		Content:   st.accumulated.String(),
		ToolCalls: st.toolCalls,
		Error:     err,
	}
}

// applyToolCallDelta merges one streamed tool-call fragment into its slot.
func (st *toolStreamState) applyToolCallDelta(tc *vllmStreamToolCall) {
	if tc.Index == nil {
		return
	}
	idx := *tc.Index
	for len(st.toolCalls) <= idx {
		st.toolCalls = append(st.toolCalls, types.MessageToolCall{})
	}
	if tc.ID != "" {
		st.toolCalls[idx].ID = tc.ID
	}
	if tc.Function.Name != "" {
		st.toolCalls[idx].Name = tc.Function.Name
	}
	if tc.Function.Arguments != "" {
		st.toolCalls[idx].Args = append(st.toolCalls[idx].Args, []byte(tc.Function.Arguments)...)
	}
}

// sseDataLine returns the payload of an SSE "data: " line.
func sseDataLine(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "data: ") {
		return "", false
	}
	return strings.TrimPrefix(line, "data: "), true
}

// handleToolStreamChunk emits the content delta, accumulates tool-call deltas
// and emits the terminal chunk for one parsed stream chunk.
func (p *Provider) handleToolStreamChunk(
	chunk *vllmStreamChunk, st *toolStreamState, chunks chan<- providers.StreamChunk,
) {
	if len(chunk.Choices) == 0 {
		return
	}
	choice := chunk.Choices[0]

	if choice.Delta.Content != "" {
		st.accumulated.WriteString(choice.Delta.Content)
		chunks <- providers.StreamChunk{
			Content:     st.accumulated.String(),
			Delta:       choice.Delta.Content,
			DeltaTokens: 1,
		}
	}

	for i := range choice.Delta.ToolCalls {
		st.applyToolCallDelta(&choice.Delta.ToolCalls[i])
	}

	// Check for finish reason. Normalize to the canonical vocabulary (matching
	// the non-tool streaming and non-streaming paths) and, when the terminal
	// chunk carries usage (requires stream_options.include_usage on the
	// request, see buildRequest), attach the priced cost breakdown.
	if choice.FinishReason != "" {
		st.finished = true
		finishReason := providers.NormalizeOpenAIFinishReason(choice.FinishReason)
		terminal := providers.StreamChunk{
			Content:      st.accumulated.String(),
			FinishReason: &finishReason,
			ToolCalls:    st.toolCalls,
		}
		if chunk.Usage != nil {
			costInfo := p.costFromUsage(*chunk.Usage)
			terminal.CostInfo = &costInfo
		}
		chunks <- terminal
	}
}
