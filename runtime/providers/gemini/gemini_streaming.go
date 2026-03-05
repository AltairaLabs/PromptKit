package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

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
		Model:    p.model,
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
	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?key=%s", p.baseURL, p.model, p.apiKey)
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

	outChan := make(chan providers.StreamChunk, providers.DefaultStreamBufferSize)

	go p.streamResponse(ctx, resp.Body, outChan)

	return outChan, nil
}

// processGeminiStreamChunk processes a single chunk from the Gemini stream
func (p *Provider) processGeminiStreamChunk(
	chunk geminiResponse,
	sb *strings.Builder,
	totalTokens int,
	toolCalls []types.MessageToolCall,
	outChan chan<- providers.StreamChunk,
) (newTotalTokens int, newToolCalls []types.MessageToolCall, finished bool) {
	if len(chunk.Candidates) == 0 {
		return totalTokens, toolCalls, false
	}

	candidate := chunk.Candidates[0]
	if len(candidate.Content.Parts) == 0 {
		return totalTokens, toolCalls, false
	}

	// Process all parts in the candidate
	for i, part := range candidate.Content.Parts {
		// Handle text content
		if part.Text != "" {
			sb.WriteString(part.Text)
			totalTokens++ // Approximate

			outChan <- providers.StreamChunk{
				Content:     sb.String(),
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
			Content:      sb.String(),
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
		return totalTokens, toolCalls, true // Signal finish
	}

	return totalTokens, toolCalls, false
}

// streamResponse reads a JSON array stream from Gemini and sends chunks incrementally.
// Gemini returns a JSON array [obj1, obj2, ...] where each element is a GenerateContentResponse.
// Instead of reading the entire body into memory, we use json.Decoder to parse each element
// as it arrives, preserving the streaming benefit.
func (p *Provider) streamResponse(ctx context.Context, body io.ReadCloser, outChan chan<- providers.StreamChunk) {
	defer close(outChan)
	defer body.Close()

	// Close the response body when context is canceled to unblock decoder reads
	go func() {
		<-ctx.Done()
		_ = body.Close()
	}()

	dec := json.NewDecoder(body)

	// Read the opening '[' token of the JSON array
	tok, err := dec.Token()
	if err != nil {
		outChan <- providers.StreamChunk{
			Error:        fmt.Errorf("failed to read response stream: %w", err),
			FinishReason: providers.StringPtr("error"),
		}
		return
	}

	delim, ok := tok.(json.Delim)
	if !ok || delim != '[' {
		outChan <- providers.StreamChunk{
			Error:        fmt.Errorf("expected JSON array start '[', got %v", tok),
			FinishReason: providers.StringPtr("error"),
		}
		return
	}

	var sb strings.Builder
	totalTokens := 0
	var toolCalls []types.MessageToolCall

	// Decode each element incrementally as it arrives
	for dec.More() {
		select {
		case <-ctx.Done():
			outChan <- providers.StreamChunk{
				Content:      sb.String(),
				Error:        ctx.Err(),
				FinishReason: providers.StringPtr("canceled"),
			}
			return
		default:
		}

		var chunk geminiResponse
		if err := dec.Decode(&chunk); err != nil {
			outChan <- providers.StreamChunk{
				Content:      sb.String(),
				Error:        fmt.Errorf("failed to decode streaming chunk: %w", err),
				FinishReason: providers.StringPtr("error"),
			}
			return
		}

		var finished bool
		totalTokens, toolCalls, finished = p.processGeminiStreamChunk(
			chunk, &sb, totalTokens, toolCalls, outChan)
		if finished {
			return
		}
	}

	// Read the closing ']' token
	if _, err := dec.Token(); err != nil {
		outChan <- providers.StreamChunk{
			Content:      sb.String(),
			Error:        fmt.Errorf("failed to read closing token: %w", err),
			FinishReason: providers.StringPtr("error"),
		}
		return
	}

	// No finish reason received, send final chunk
	outChan <- providers.StreamChunk{
		Content:      sb.String(),
		TokenCount:   totalTokens,
		ToolCalls:    toolCalls,
		FinishReason: providers.StringPtr("stop"),
	}
}
