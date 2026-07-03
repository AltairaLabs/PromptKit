// Package openai provides OpenAI LLM provider integration.
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
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
	eventTypeTextDelta      = "response.output_text.delta"
	eventTypeReasoningDelta = "response.reasoning_summary_text.delta"
	eventTypeFuncArgsDelta  = "response.function_call_arguments.delta"
	eventTypeOutputAdded    = "response.output_item.added"
	eventTypeCompleted      = "response.completed"
	eventTypeError          = "error"

	// Audio event types for Responses API streaming
	eventTypeAudioDelta      = "response.audio.delta"
	eventTypeAudioDone       = "response.audio.done"
	eventTypeAudioTransDelta = "response.audio_transcript.delta"
	eventTypeAudioTransDone  = "response.audio_transcript.done"

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
	Type    string             `json:"type"` // "message", "function_call", "reasoning", etc.
	ID      string             `json:"id,omitempty"`
	Role    string             `json:"role,omitempty"`
	Content []responsesContent `json:"content,omitempty"`
	Name    string             `json:"name,omitempty"`      // For function calls
	CallID  string             `json:"call_id,omitempty"`   // For function calls
	Args    string             `json:"arguments,omitempty"` // For function calls
	// Summary holds reasoning summary parts on a type:"reasoning" output item.
	Summary []responsesSummaryPart `json:"summary,omitempty"`
}

// responsesSummaryPart is one reasoning summary block ({type:"summary_text"}).
type responsesSummaryPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
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

	// Add sampling parameters (some models, e.g. o-series, don't support these)
	if !hasUnsupportedParam(p.unsupportedParams, "temperature") && temperature > 0 {
		responsesReq["temperature"] = temperature
	}
	if !hasUnsupportedParam(p.unsupportedParams, "top_p") && topP > 0 {
		responsesReq["top_p"] = topP
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

	// Add reasoning.effort when configured. Reasoning models (o-series,
	// gpt-5-pro) default to effort=high on OpenAI's side, which on simple
	// prompts can burn tens of seconds on internal reasoning. Callers set
	// this via additional_config.reasoning_effort to control it.
	if p.reasoningEffort != "" || p.reasoningSummary != "" {
		reasoning := map[string]any{}
		if p.reasoningEffort != "" {
			reasoning["effort"] = p.reasoningEffort
		}
		// reasoning_summary is opt-in: OpenAI captures these summaries onto
		// Message.Reasoning (the raw chain-of-thought is never exposed), but
		// requesting them requires a verified OpenAI org.
		if p.reasoningSummary != "" {
			reasoning["summary"] = p.reasoningSummary
		}
		responsesReq["reasoning"] = reasoning
	}

	return responsesReq
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
	url := p.responsesURL()
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
		return predictResp, nil, &providers.ProviderTransportError{Cause: err, Provider: p.ID()}
	}
	defer resp.Body.Close()

	respBody, err := providers.ReadResponseBody(resp.Body)
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
		return predictResp, nil, &providers.ProviderHTTPError{
			StatusCode: resp.StatusCode, URL: url,
			Body: string(respBody), Provider: p.ID(),
		}
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

	// Extract content, tool calls, and reasoning summary from output
	content, toolCalls, reasoning := p.extractResponsesOutput(responsesResp.Output)

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
	predictResp.Reasoning = reasoning
	predictResp.Raw = respBody

	return predictResp, toolCalls, nil
}

// extractResponsesOutput extracts text content, tool calls, and the reasoning
// summary from Responses API output. Reasoning is returned separately (never
// folded into content) for Message.Reasoning.
func (p *Provider) extractResponsesOutput(
	outputs []responsesOutput,
) (string, []types.MessageToolCall, *types.ReasoningTrace) {
	var content strings.Builder
	var toolCalls []types.MessageToolCall
	var reasoning strings.Builder

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
		case "reasoning":
			for _, s := range output.Summary {
				reasoning.WriteString(s.Text)
			}
		}
	}

	var rt *types.ReasoningTrace
	if reasoning.Len() > 0 {
		rt = &types.ReasoningTrace{Text: reasoning.String()}
	}
	return content.String(), toolCalls, rt
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

	// Build request factory for OpenStreamWithRetry. Each retry rebuilds
	// the HTTP request so that headers (especially auth) are reapplied
	// cleanly and a fresh body reader is used.
	url := p.responsesURL()
	requestFn := func(ctx context.Context) (*http.Request, error) {
		httpReq, reqErr := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
		if reqErr != nil {
			return nil, fmt.Errorf("failed to create request: %w", reqErr)
		}
		httpReq.Header.Set(contentTypeHeader, applicationJSON)
		httpReq.Header.Set("Accept", "text/event-stream")
		if authErr := p.applyAuth(ctx, httpReq); authErr != nil {
			return nil, fmt.Errorf("failed to apply authentication: %w", authErr)
		}
		return httpReq, nil
	}

	return p.RunStreamingRequest(ctx, &providers.StreamRetryRequest{
		Policy:       p.StreamRetryPolicy(),
		Budget:       p.StreamRetryBudget(),
		ProviderName: p.ID(),
		Host:         providers.HostFromURL(url),
		IdleTimeout:  p.StreamIdleTimeout(),
		RequestFn:    requestFn,
		Client:       p.GetStreamingHTTPClient(),
	}, p.streamResponsesResponse)
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

// handleStreamEvent processes a single streaming event and returns updated state.
// event is taken by pointer to avoid copying the (~120-byte) struct on every SSE
// event in the hot streaming loop.
func (p *Provider) handleStreamEvent(
	event *responsesStreamEvent,
	data string,
	sb *strings.Builder,
	totalTokens int,
	toolCalls []types.MessageToolCall,
	usage *responsesUsage,
	outChan chan<- providers.StreamChunk,
	idMap itemIDMap,
) (newTokens int, newToolCalls []types.MessageToolCall, newUsage *responsesUsage) {
	switch event.Type {
	case eventTypeTextDelta:
		newTokens = p.handleTextDelta(data, sb, totalTokens, toolCalls, outChan)
		return newTokens, toolCalls, nil

	case eventTypeReasoningDelta:
		p.handleReasoningDelta(data, outChan)
		return totalTokens, toolCalls, usage

	case eventTypeFuncArgsDelta:
		return totalTokens, p.handleFuncArgsDelta(data, toolCalls, idMap), usage

	case eventTypeOutputAdded:
		return totalTokens, p.handleOutputAdded(data, toolCalls, idMap), usage

	case eventTypeCompleted:
		usage = p.handleCompleted(data, sb.String(), toolCalls, totalTokens, outChan)
		return totalTokens, toolCalls, usage

	case eventTypeError:
		p.handleErrorEvent(data, sb.String(), toolCalls, outChan)
		return totalTokens, toolCalls, usage

	case eventTypeAudioDelta:
		p.handleAudioDelta(data, outChan)
		return totalTokens, toolCalls, usage

	case eventTypeAudioDone:
		return totalTokens, toolCalls, usage

	case eventTypeAudioTransDelta:
		newTokens = p.handleAudioTranscriptDelta(data, sb, totalTokens, toolCalls, outChan)
		return newTokens, toolCalls, usage

	case eventTypeAudioTransDone:
		return totalTokens, toolCalls, usage
	}

	return totalTokens, toolCalls, usage
}

// handleReasoningDelta streams a reasoning-summary delta on StreamChunk.Reasoning
// (non-content), so the UI can show thinking live without it leaking into the answer.
func (p *Provider) handleReasoningDelta(data string, outChan chan<- providers.StreamChunk) {
	var ev struct {
		Delta string `json:"delta"`
	}
	if err := json.Unmarshal([]byte(data), &ev); err == nil && ev.Delta != "" {
		outChan <- providers.StreamChunk{Reasoning: ev.Delta}
	}
}

// handleTextDelta processes text delta events
func (p *Provider) handleTextDelta(
	data string,
	sb *strings.Builder,
	totalTokens int,
	toolCalls []types.MessageToolCall,
	outChan chan<- providers.StreamChunk,
) int {
	var textDelta struct {
		Type  string `json:"type"`
		Delta string `json:"delta"`
	}
	if err := json.Unmarshal([]byte(data), &textDelta); err == nil && textDelta.Delta != "" {
		sb.WriteString(textDelta.Delta)
		totalTokens++
		outChan <- providers.StreamChunk{
			Content:     sb.String(),
			Delta:       textDelta.Delta,
			ToolCalls:   toolCalls,
			TokenCount:  totalTokens,
			DeltaTokens: 1,
		}
	}
	return totalTokens
}

// handleAudioDelta processes audio delta events, decoding base64 PCM data and
// emitting it as a StreamChunk with MediaData.
func (p *Provider) handleAudioDelta(data string, outChan chan<- providers.StreamChunk) {
	var delta struct {
		Delta string `json:"delta"`
	}
	if err := json.Unmarshal([]byte(data), &delta); err != nil || delta.Delta == "" {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(delta.Delta)
	if err != nil {
		return
	}
	outChan <- providers.StreamChunk{
		MediaData: &providers.StreamMediaData{
			Data:     raw,
			MIMEType: "audio/pcm",
		},
	}
}

// handleAudioTranscriptDelta processes audio transcript delta events, emitting
// transcript text as regular text deltas.
func (p *Provider) handleAudioTranscriptDelta(
	data string,
	sb *strings.Builder,
	totalTokens int,
	toolCalls []types.MessageToolCall,
	outChan chan<- providers.StreamChunk,
) int {
	var delta struct {
		Delta string `json:"delta"`
	}
	if err := json.Unmarshal([]byte(data), &delta); err != nil || delta.Delta == "" {
		return totalTokens
	}
	sb.WriteString(delta.Delta)
	totalTokens++
	outChan <- providers.StreamChunk{
		Content:     sb.String(),
		Delta:       delta.Delta,
		ToolCalls:   toolCalls,
		TokenCount:  totalTokens,
		DeltaTokens: 1,
	}
	return totalTokens
}

// handleFuncArgsDelta processes function call arguments delta events.
// The raw data is parsed because call_id is a top-level field in the SSE event,
// not nested inside the delta field.
func (p *Provider) handleFuncArgsDelta(
	data string,
	toolCalls []types.MessageToolCall,
	idMap itemIDMap,
) []types.MessageToolCall {
	var delta struct {
		CallID      string `json:"call_id"`
		ItemID      string `json:"item_id"`
		OutputIndex *int   `json:"output_index"`
		Delta       string `json:"delta"`
	}
	if err := json.Unmarshal([]byte(data), &delta); err != nil {
		return toolCalls
	}

	// Look up the tool call index: try item_id, then call_id, then output_index.
	// The Responses API may send only output_index in delta events.
	idx := -1
	if delta.ItemID != "" {
		if i, ok := idMap[delta.ItemID]; ok {
			idx = i
		}
	}
	if idx < 0 && delta.CallID != "" {
		if i, ok := idMap[delta.CallID]; ok {
			idx = i
		}
	}
	if idx < 0 && delta.OutputIndex != nil {
		if i, ok := idMap[outputIndexKey(*delta.OutputIndex)]; ok {
			idx = i
		}
	}

	if idx >= 0 && idx < len(toolCalls) {
		currentArgs := string(toolCalls[idx].Args)
		if currentArgs == "{}" || currentArgs == "" {
			toolCalls[idx].Args = json.RawMessage(delta.Delta)
		} else {
			toolCalls[idx].Args = append(toolCalls[idx].Args, []byte(delta.Delta)...)
		}
	}

	return toolCalls
}

// itemIDMap tracks the mapping from Responses API item_id (fc_...) to the
// index in the toolCalls slice, enabling delta events to find their tool call.
// Also maps output_index (as "idx:N") for delta events that lack item_id/call_id.
type itemIDMap map[string]int

// outputIndexKey returns the idMap key for an output_index value.
func outputIndexKey(idx int) string {
	return fmt.Sprintf("idx:%d", idx)
}

// handleOutputAdded processes output item added events
func (p *Provider) handleOutputAdded(
	data string, toolCalls []types.MessageToolCall, idMap itemIDMap,
) []types.MessageToolCall {
	var item struct {
		OutputIndex *int `json:"output_index"`
		Item        struct {
			Type   string `json:"type"`
			ID     string `json:"id"`
			CallID string `json:"call_id"`
			Name   string `json:"name"`
		} `json:"item"`
	}
	if err := json.Unmarshal([]byte(data), &item); err == nil {
		if item.Item.Type == typeFunctionCall {
			idx := len(toolCalls)
			toolCalls = append(toolCalls, types.MessageToolCall{
				ID:   item.Item.CallID,
				Name: item.Item.Name,
				Args: json.RawMessage(""), // Will be populated by delta events
			})
			// Map id, call_id, and output_index to this index for delta matching.
			// Delta events may use any of these to reference the tool call.
			if item.Item.ID != "" {
				idMap[item.Item.ID] = idx
			}
			if item.Item.CallID != "" {
				idMap[item.Item.CallID] = idx
			}
			if item.OutputIndex != nil {
				idMap[outputIndexKey(*item.OutputIndex)] = idx
			}
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

	// Close the response body when context is canceled to unblock scanner.Scan()
	go func() {
		<-ctx.Done()
		_ = body.Close()
	}()

	// Wrap body with idle timeout detection to guard against stalled streams.
	// Duration is configured on the BaseProvider via SetStreamIdleTimeout and
	// falls back to providers.DefaultStreamIdleTimeout when unset.
	idleBody := providers.NewIdleTimeoutReader(body, p.StreamIdleTimeout())
	defer idleBody.Close()

	scanner := bufio.NewScanner(idleBody)
	var sb strings.Builder
	totalTokens := 0
	var accumulatedToolCalls []types.MessageToolCall
	var usage *responsesUsage
	idMap := make(itemIDMap)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			outChan <- providers.StreamChunk{
				Content:      sb.String(),
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
			p.sendFinalChunk(outChan, sb.String(), accumulatedToolCalls, totalTokens, usage)
			return
		}

		// Parse event
		var event responsesStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		// Handle different event types
		totalTokens, accumulatedToolCalls, usage = p.handleStreamEvent(
			&event, data, &sb, totalTokens, accumulatedToolCalls, usage, outChan, idMap,
		)

		// Check if we should return (completed or error events signal this via usage being set and returned)
		if event.Type == eventTypeCompleted || event.Type == eventTypeError {
			return
		}
	}

	if err := scanner.Err(); err != nil {
		outChan <- providers.StreamChunk{
			Content:      sb.String(),
			ToolCalls:    accumulatedToolCalls,
			Error:        err,
			FinishReason: providers.StringPtr(finishError),
		}
	}
}
