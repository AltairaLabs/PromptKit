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

	// Explicit context caching: reference the cached system prefix and drop the
	// inline systemInstruction (the API rejects sending both).
	if cc := p.resolveCachedContent(ctx, req.System, nil); cc != "" {
		geminiReq.CachedContent = cc
		geminiReq.SystemInstruction = nil
	}

	// Apply response format if specified
	p.applyResponseFormat(&geminiReq, req.ResponseFormat)

	reqBody, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Gemini's streamGenerateContent endpoint returns a JSON array
	// ([{...},{...},...]) parsed incrementally by json.Decoder in
	// streamResponse. The retry driver's JSONArrayFrameDetector reads
	// past the opening '[' and the first complete element so
	// peekFirstFrame can confirm the stream is "live" before handing
	// ownership to the consumer goroutine.
	url := p.generateContentURL("streamGenerateContent")
	requestFn := func(ctx context.Context) (*http.Request, error) {
		httpReq, reqErr := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
		if reqErr != nil {
			return nil, fmt.Errorf("failed to create request: %w", reqErr)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if authErr := p.applyAuth(ctx, httpReq); authErr != nil {
			return nil, fmt.Errorf("failed to apply authentication: %w", authErr)
		}
		return httpReq, nil
	}

	return p.RunStreamingRequest(ctx, &providers.StreamRetryRequest{
		Policy:        p.StreamRetryPolicy(),
		Budget:        p.StreamRetryBudget(),
		ProviderName:  p.ID(),
		Host:          providers.HostFromURL(url),
		IdleTimeout:   p.StreamIdleTimeout(),
		RequestFn:     requestFn,
		Client:        p.GetStreamingHTTPClient(),
		FrameDetector: providers.JSONArrayFrameDetector{},
	}, p.streamResponse)
}

// processGeminiStreamChunk processes a single chunk from the Gemini stream.
//
// TokenCount/DeltaTokens are populated ONLY from real usageMetadata (present
// on the terminal chunk), never from a per-part-incremented approximation —
// Gemini's "tokens" are not 1:1 with text parts (a part can be a whole
// sentence or a single character), so counting parts fabricated a number that
// looked precise but was not calibrated to anything real. Interim chunks
// leave TokenCount/DeltaTokens at their zero value.
func (p *Provider) processGeminiStreamChunk(
	chunk geminiResponse,
	sb *strings.Builder,
	toolCalls []types.MessageToolCall,
	outChan chan<- providers.StreamChunk,
) (newToolCalls []types.MessageToolCall, finished bool) {
	if len(chunk.Candidates) == 0 {
		return toolCalls, false
	}

	candidate := chunk.Candidates[0]

	// Process all parts in the candidate (a no-op when Parts is empty — an
	// empty-parts chunk may still carry a terminal finishReason, handled below).
	for i, part := range candidate.Content.Parts {
		// Reasoning ("thought") parts stream on Reasoning, not content.
		if part.Thought {
			if part.Text != "" || part.ThoughtSignature != "" {
				rc := providers.StreamChunk{Reasoning: part.Text}
				if part.ThoughtSignature != "" {
					rc.OpaqueReasoning = []types.OpaqueReasoning{{
						Provider: providerNameGemini, Kind: kindThoughtSignature, Data: part.ThoughtSignature,
					}}
				}
				outChan <- rc
			}
			continue
		}

		// Handle text content
		if part.Text != "" {
			sb.WriteString(part.Text)

			outChan <- providers.StreamChunk{
				Content: sb.String(),
				Delta:   part.Text,
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
			// Preserve Gemini 3's thoughtSignature so it can be replayed on
			// the next turn. Without this, Gemini 3 rejects the request.
			if part.ThoughtSignature != "" {
				toolCall.ProviderMetadata = map[string]string{
					providerMetaThoughtSignature: part.ThoughtSignature,
				}
			}
			toolCalls = append(toolCalls, toolCall)
		}
	}

	if candidate.FinishReason != "" {
		// A terminal chunk that produced no text and no tool calls across the
		// whole stream means the model was blocked or refused (SAFETY,
		// RECITATION, MAX_TOKENS, UNEXPECTED_TOOL_CALL, ...). Surface it as an
		// error instead of silently emitting an empty success — this mirrors
		// the non-streaming handleGeminiFinishReason path, which already errors
		// on a content-less terminal response.
		if sb.Len() == 0 && len(toolCalls) == 0 {
			outChan <- providers.StreamChunk{
				Error: fmt.Errorf(
					"gemini stream ended with finish reason %q and no content",
					candidate.FinishReason,
				),
				FinishReason: &candidate.FinishReason,
			}
			return toolCalls, true
		}

		normalized := normalizeFinishReason(candidate.FinishReason)
		finalChunk := providers.StreamChunk{
			Content:      sb.String(),
			FinishReason: &normalized,
			ToolCalls:    toolCalls,
		}

		// TokenCount and CostInfo come from real usageMetadata only — routed
		// through costFromUsage (not the CalculateCost wrapper) so thinking
		// tokens are priced too (see Task 10).
		if chunk.UsageMetadata != nil {
			finalChunk.TokenCount = chunk.UsageMetadata.CandidatesTokenCount
			costBreakdown := p.costFromUsage(*chunk.UsageMetadata)
			finalChunk.CostInfo = &costBreakdown
		}

		outChan <- finalChunk
		return toolCalls, true // Signal finish
	}

	return toolCalls, false
}

// streamResponse reads a JSON array stream from Gemini and sends chunks incrementally.
// Gemini returns a JSON array [obj1, obj2, ...] where each element is a GenerateContentResponse.
// Instead of reading the entire body into memory, we use json.Decoder to parse each element
// as it arrives, preserving the streaming benefit.
func (p *Provider) streamResponse(ctx context.Context, body io.ReadCloser, outChan chan<- providers.StreamChunk) {
	defer close(outChan)

	// Close the response body when context is canceled to unblock decoder reads
	go func() {
		<-ctx.Done()
		_ = body.Close()
	}()

	// Wrap body with idle timeout detection to guard against stalled streams.
	// Duration is configured on the BaseProvider via SetStreamIdleTimeout.
	idleBody := providers.NewIdleTimeoutReader(body, p.StreamIdleTimeout())
	defer idleBody.Close()

	dec := json.NewDecoder(idleBody)

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
		toolCalls, finished = p.processGeminiStreamChunk(chunk, &sb, toolCalls, outChan)
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

	// No finish reason received, send final chunk. No usageMetadata was seen
	// on this path, so TokenCount stays at its zero value rather than a
	// fabricated approximation.
	outChan <- providers.StreamChunk{
		Content:      sb.String(),
		ToolCalls:    toolCalls,
		FinishReason: providers.StringPtr("stop"),
	}
}
