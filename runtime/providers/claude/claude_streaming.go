package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Streaming finish reason constants
const (
	finishReasonStop = "stop"
)

// PredictStream performs a streaming prediction request to Claude.
// On Bedrock, falls back to non-streaming Predict since Bedrock uses
// binary event-stream encoding that requires AWS-specific parsing.
//
//nolint:gocritic // hugeParam: interface signature requires value receiver
func (p *Provider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	// Enrich context with provider and model info for logging
	ctx = logger.WithLoggingContext(ctx, &logger.LoggingFields{
		Provider: p.ID(),
		Model:    p.model,
	})

	// Convert messages to Claude format (handles both text and multimodal)
	messages := p.convertMessagesToClaudeFormat(req.Messages)

	// Apply provider defaults
	temperature := req.Temperature
	if temperature == 0 {
		temperature = p.defaults.Temperature
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = p.defaults.MaxTokens
	}

	// Create streaming request
	// Note: Anthropic's newer models (Claude 4+) don't support both temperature and top_p
	// We only send temperature to avoid the "cannot both be specified" error
	claudeReq := map[string]any{
		"model":       p.model,
		"max_tokens":  maxTokens,
		"messages":    messages,
		"temperature": temperature,
		"stream":      true,
	}

	if req.System != "" {
		claudeReq["system"] = []claudeContentBlock{
			{
				Type: "text",
				Text: req.System,
			},
		}
	}

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
		outChan := make(chan providers.StreamChunk)
		go p.streamResponse(ctx, body, scanner, outChan)
		return outChan, nil
	}

	reqBody, err := json.Marshal(claudeReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request
	url := p.messagesURL()
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	httpReq.Header.Set(anthropicVersionKey, anthropicVersionValue)
	httpReq.Header.Set("Accept", "text/event-stream")

	// Apply authentication
	if authErr := p.applyAuth(ctx, httpReq); authErr != nil {
		return nil, fmt.Errorf("failed to apply authentication: %w", authErr)
	}

	//nolint:bodyclose // body is closed in streamResponse goroutine
	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if err := providers.CheckHTTPError(resp, url); err != nil {
		return nil, err
	}

	outChan := make(chan providers.StreamChunk)
	scanner := providers.NewSSEScanner(resp.Body)

	go p.streamResponse(ctx, resp.Body, scanner, outChan)

	return outChan, nil
}

// processClaudeContentDeltaInternal handles content_block_delta events with pre-parsed delta
func (p *Provider) processClaudeContentDeltaInternal(
	delta *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	},
	accumulated string,
	totalTokens int,
	outChan chan<- providers.StreamChunk,
) (newAccumulated string, newTokenCount int) {
	if delta == nil || delta.Type != textDeltaType {
		return accumulated, totalTokens
	}

	accumulated += delta.Text
	totalTokens++

	outChan <- providers.StreamChunk{
		Content:     accumulated,
		Delta:       delta.Text,
		TokenCount:  totalTokens,
		DeltaTokens: 1,
	}

	return accumulated, totalTokens
}

// processClaudeContentDelta handles content_block_delta events from Claude stream
// Used by integration tests to validate streaming behavior
//
//nolint:unused // Used by integration tests in claude_integration_test.go
func (p *Provider) processClaudeContentDelta(
	event struct {
		Type  string `json:"type"`
		Delta *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta,omitempty"`
		Message *struct {
			StopReason string       `json:"stop_reason"`
			Usage      *claudeUsage `json:"usage,omitempty"`
		} `json:"message,omitempty"`
	},
	accumulated string,
	totalTokens int,
	outChan chan<- providers.StreamChunk,
) (newAccumulated string, newTotalTokens int) {
	if event.Delta == nil || event.Delta.Type != textDeltaType {
		return accumulated, totalTokens
	}

	delta := event.Delta.Text
	accumulated += delta
	totalTokens++ // Approximate

	outChan <- providers.StreamChunk{
		Content:     accumulated,
		Delta:       delta,
		TokenCount:  totalTokens,
		DeltaTokens: 1,
	}

	return accumulated, totalTokens
}

// processClaudeMessageStop handles message_stop events from Claude stream
// Used by integration tests to validate streaming behavior
//
//nolint:unused // Used by integration tests in claude_integration_test.go
func (p *Provider) processClaudeMessageStop(
	event struct {
		Type  string `json:"type"`
		Delta *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta,omitempty"`
		Message *struct {
			StopReason string       `json:"stop_reason"`
			Usage      *claudeUsage `json:"usage,omitempty"`
		} `json:"message,omitempty"`
	},
	accumulated string,
	totalTokens int,
	outChan chan<- providers.StreamChunk,
) {
	finishReason := finishReasonStop
	finalChunk := providers.StreamChunk{
		Content:      accumulated,
		TokenCount:   totalTokens,
		FinishReason: &finishReason,
	}

	if event.Message != nil {
		if event.Message.StopReason != "" {
			finishReason = event.Message.StopReason
			finalChunk.FinishReason = &finishReason
		}

		// Extract cost from usage if available
		if event.Message.Usage != nil {
			tokensIn := event.Message.Usage.InputTokens
			tokensOut := event.Message.Usage.OutputTokens
			cachedTokens := event.Message.Usage.CacheReadInputTokens

			costBreakdown := p.CalculateCost(tokensIn, tokensOut, cachedTokens)
			finalChunk.CostInfo = &costBreakdown
		}
	}

	outChan <- finalChunk
}

// streamResponse reads a stream from Claude and sends chunks.
// The scanner parameter abstracts the underlying transport format (SSE or binary event-stream).
//
//nolint:gocognit // complexity is inherent in event handling
func (p *Provider) streamResponse(
	ctx context.Context, body io.ReadCloser, scanner providers.StreamScanner, outChan chan<- providers.StreamChunk,
) {
	defer close(outChan)
	defer body.Close()
	accumulated := ""
	totalTokens := 0

	// Track usage from message_start and message_delta events
	var inputTokens, outputTokens, cachedTokens int
	var stopReason string

	// Track tool calls from content_block_start and content_block_delta events
	var accumulatedToolCalls []types.MessageToolCall
	var currentToolCallIndex = -1
	var currentToolCallJSON string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			outChan <- providers.StreamChunk{
				Content:      accumulated,
				ToolCalls:    accumulatedToolCalls,
				Error:        ctx.Err(),
				FinishReason: providers.StringPtr("canceled"),
			}
			return
		default:
		}

		data := scanner.Data()

		// Parse the event to determine its type
		var baseEvent struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(data), &baseEvent); err != nil {
			continue // Skip malformed chunks
		}

		switch baseEvent.Type {
		case "message_start":
			// message_start contains input tokens
			var startEvent struct {
				Message *struct {
					Usage *claudeUsage `json:"usage,omitempty"`
				} `json:"message,omitempty"`
			}
			if err := json.Unmarshal([]byte(data), &startEvent); err == nil {
				if startEvent.Message != nil && startEvent.Message.Usage != nil {
					inputTokens = startEvent.Message.Usage.InputTokens
					cachedTokens = startEvent.Message.Usage.CacheReadInputTokens
				}
			}

		case "content_block_start":
			// content_block_start can indicate a new tool_use block
			var blockStartEvent struct {
				Index        int `json:"index"`
				ContentBlock *struct {
					Type string `json:"type"`
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content_block,omitempty"`
			}
			if err := json.Unmarshal([]byte(data), &blockStartEvent); err == nil {
				if blockStartEvent.ContentBlock != nil && blockStartEvent.ContentBlock.Type == "tool_use" {
					// Start a new tool call
					currentToolCallIndex = len(accumulatedToolCalls)
					currentToolCallJSON = ""
					accumulatedToolCalls = append(accumulatedToolCalls, types.MessageToolCall{
						ID:   blockStartEvent.ContentBlock.ID,
						Name: blockStartEvent.ContentBlock.Name,
						Args: json.RawMessage("{}"),
					})
				}
			}

		case "content_block_delta":
			var deltaEvent struct {
				Index int `json:"index"`
				Delta *struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta,omitempty"`
			}
			if err := json.Unmarshal([]byte(data), &deltaEvent); err == nil {
				if deltaEvent.Delta != nil {
					switch deltaEvent.Delta.Type {
					case textDeltaType:
						// Handle text delta - create a compatible struct for processClaudeContentDeltaInternal
						textDelta := &struct {
							Type string `json:"type"`
							Text string `json:"text"`
						}{
							Type: deltaEvent.Delta.Type,
							Text: deltaEvent.Delta.Text,
						}
						accumulated, totalTokens = p.processClaudeContentDeltaInternal(textDelta, accumulated, totalTokens, outChan)
					case "input_json_delta":
						// Handle tool call input JSON delta
						if currentToolCallIndex >= 0 && currentToolCallIndex < len(accumulatedToolCalls) {
							currentToolCallJSON += deltaEvent.Delta.PartialJSON
							// Update the tool call args with accumulated JSON
							accumulatedToolCalls[currentToolCallIndex].Args = json.RawMessage(currentToolCallJSON)
							// Emit chunk with updated tool calls
							outChan <- providers.StreamChunk{
								Content:    accumulated,
								ToolCalls:  accumulatedToolCalls,
								TokenCount: totalTokens,
							}
						}
					}
				}
			}

		case "content_block_stop":
			// Reset current tool call tracking when block ends
			currentToolCallIndex = -1
			currentToolCallJSON = ""

		case "message_delta":
			// message_delta contains output tokens and stop reason
			var messageDeltaEvent struct {
				Delta *struct {
					StopReason string `json:"stop_reason,omitempty"`
				} `json:"delta,omitempty"`
				Usage *claudeUsage `json:"usage,omitempty"`
			}
			if err := json.Unmarshal([]byte(data), &messageDeltaEvent); err == nil {
				if messageDeltaEvent.Delta != nil && messageDeltaEvent.Delta.StopReason != "" {
					stopReason = messageDeltaEvent.Delta.StopReason
				}
				if messageDeltaEvent.Usage != nil {
					outputTokens = messageDeltaEvent.Usage.OutputTokens
				}
			}

		case "message_stop":
			// Send final chunk with accumulated usage
			finishReason := stopReason
			if finishReason == "" {
				finishReason = finishReasonStop
			}
			finalChunk := providers.StreamChunk{
				Content:      accumulated,
				ToolCalls:    accumulatedToolCalls,
				TokenCount:   totalTokens,
				FinishReason: &finishReason,
			}

			// Calculate cost from accumulated usage
			if inputTokens > 0 || outputTokens > 0 {
				costBreakdown := p.CalculateCost(inputTokens, outputTokens, cachedTokens)
				finalChunk.CostInfo = &costBreakdown
			}

			outChan <- finalChunk
			return
		}
	}

	if err := scanner.Err(); err != nil {
		outChan <- providers.StreamChunk{
			Content:      accumulated,
			ToolCalls:    accumulatedToolCalls,
			Error:        err,
			FinishReason: providers.StringPtr("error"),
		}
	}
}
