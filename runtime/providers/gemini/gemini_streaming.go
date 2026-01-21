package gemini

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

// PredictStream performs a streaming prediction request to Gemini
//
//nolint:gocritic // hugeParam: interface signature requires value receiver
func (p *Provider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	// Enrich context with provider and model info for logging
	ctx = logger.WithLoggingContext(ctx, &logger.LoggingFields{
		Provider: p.ID(),
		Model:    p.modelName,
	})

	// Convert messages to Gemini format and apply defaults
	contents, systemInstruction, temperature, topP, maxTokens := p.prepareGeminiRequest(req)

	// Create streaming request
	geminiReq := p.buildGeminiRequest(contents, systemInstruction, temperature, topP, maxTokens)

	// Apply response format if specified
	p.applyResponseFormat(&geminiReq, req.ResponseFormat)

	reqBody, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request
	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?key=%s", p.BaseURL, p.modelName, p.ApiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	//nolint:bodyclose // body is closed in streamResponse goroutine
	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if err := providers.CheckHTTPError(resp, logger.RedactSensitiveData(url)); err != nil {
		return nil, err
	}

	outChan := make(chan providers.StreamChunk)

	go p.streamResponse(ctx, resp.Body, outChan)

	return outChan, nil
}

// processGeminiStreamChunk processes a single chunk from the Gemini stream
func (p *Provider) processGeminiStreamChunk(
	chunk geminiResponse,
	accumulated string,
	totalTokens int,
	toolCalls []types.MessageToolCall,
	outChan chan<- providers.StreamChunk,
) (newAccumulated string, newTotalTokens int, newToolCalls []types.MessageToolCall, finished bool) {
	if len(chunk.Candidates) == 0 {
		return accumulated, totalTokens, toolCalls, false
	}

	candidate := chunk.Candidates[0]
	if len(candidate.Content.Parts) == 0 {
		return accumulated, totalTokens, toolCalls, false
	}

	// Process all parts in the candidate
	for i, part := range candidate.Content.Parts {
		// Handle text content
		if part.Text != "" {
			accumulated += part.Text
			totalTokens++ // Approximate

			outChan <- providers.StreamChunk{
				Content:     accumulated,
				Delta:       part.Text,
				TokenCount:  totalTokens,
				DeltaTokens: 1,
			}
		}

		// Handle function calls
		if part.FunctionCall != nil {
			toolCall := types.MessageToolCall{
				ID:   fmt.Sprintf("call_%d", len(toolCalls)+i),
				Name: part.FunctionCall.Name,
			}
			if part.FunctionCall.Args != nil {
				toolCall.Args = part.FunctionCall.Args
			}
			toolCalls = append(toolCalls, toolCall)
		}
	}

	if candidate.FinishReason != "" {
		finalChunk := providers.StreamChunk{
			Content:      accumulated,
			TokenCount:   totalTokens,
			FinishReason: &candidate.FinishReason,
			ToolCalls:    toolCalls,
		}

		// Extract cost from usage metadata if available
		if chunk.UsageMetadata != nil {
			costBreakdown := p.CalculateCost(
				chunk.UsageMetadata.PromptTokenCount,
				chunk.UsageMetadata.CandidatesTokenCount,
				chunk.UsageMetadata.CachedContentTokenCount,
			)
			finalChunk.CostInfo = &costBreakdown
		}

		outChan <- finalChunk
		return accumulated, totalTokens, toolCalls, true // Signal finish
	}

	return accumulated, totalTokens, toolCalls, false
}

// streamResponse reads JSON stream from Gemini and sends chunks
func (p *Provider) streamResponse(ctx context.Context, body io.ReadCloser, outChan chan<- providers.StreamChunk) {
	defer close(outChan)
	defer body.Close()

	// Gemini returns JSON array: [{"candidates": [...], ...}, {"candidates": [...], ...}]
	// We need to read the entire body and parse it as an array
	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		outChan <- providers.StreamChunk{
			Error:        fmt.Errorf("failed to read response body: %w", err),
			FinishReason: providers.StringPtr("error"),
		}
		return
	}

	// Parse as array of responses
	var responses []geminiResponse
	if err := json.Unmarshal(bodyBytes, &responses); err != nil {
		outChan <- providers.StreamChunk{
			Error:        fmt.Errorf("failed to parse streaming response: %w", err),
			FinishReason: providers.StringPtr("error"),
		}
		return
	}

	accumulated := ""
	totalTokens := 0
	var toolCalls []types.MessageToolCall

	// Process each chunk in the array
	for _, chunk := range responses {
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

		var finished bool
		accumulated, totalTokens, toolCalls, finished = p.processGeminiStreamChunk(
			chunk, accumulated, totalTokens, toolCalls, outChan)
		if finished {
			return
		}
	}

	// No finish reason received, send final chunk
	outChan <- providers.StreamChunk{
		Content:      accumulated,
		TokenCount:   totalTokens,
		ToolCalls:    toolCalls,
		FinishReason: providers.StringPtr("stop"),
	}
}
