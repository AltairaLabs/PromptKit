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
)

// Streaming finish reason constants
const (
	finishReasonStop = "stop"
)

// PredictStream performs a streaming prediction request to Claude
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

	reqBody, err := json.Marshal(claudeReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewReader(reqBody))
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

	if err := providers.CheckHTTPError(resp, p.baseURL+"/messages"); err != nil {
		return nil, err
	}

	outChan := make(chan providers.StreamChunk)

	go p.streamResponse(ctx, resp.Body, outChan)

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

// streamResponse reads SSE stream from Claude and sends chunks
//
//nolint:gocognit // complexity is inherent in SSE event handling
func (p *Provider) streamResponse(ctx context.Context, body io.ReadCloser, outChan chan<- providers.StreamChunk) {
	defer close(outChan)
	defer body.Close()

	scanner := providers.NewSSEScanner(body)
	accumulated := ""
	totalTokens := 0

	// Track usage from message_start and message_delta events
	var inputTokens, outputTokens, cachedTokens int
	var stopReason string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			outChan <- providers.StreamChunk{
				Content:      accumulated,
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

		case "content_block_delta":
			var deltaEvent struct {
				Delta *struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta,omitempty"`
			}
			if err := json.Unmarshal([]byte(data), &deltaEvent); err == nil {
				accumulated, totalTokens = p.processClaudeContentDeltaInternal(deltaEvent.Delta, accumulated, totalTokens, outChan)
			}

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
			Error:        err,
			FinishReason: providers.StringPtr("error"),
		}
	}
}
