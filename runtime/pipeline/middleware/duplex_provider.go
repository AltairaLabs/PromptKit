package middleware

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// DuplexProviderMiddleware handles bidirectional streaming through a WebSocket session.
// It forwards chunks from StreamInput to the provider's WebSocket session and
// forwards responses from the session to StreamOutput.
//
// This middleware is used for ASM (Audio Streaming Model) mode where audio chunks
// are streamed directly to the provider's WebSocket connection (bidiGenerateContent API).
type duplexProviderMiddleware struct {
	session providers.StreamInputSession
	config  *ProviderMiddlewareConfig
}

// DuplexProviderMiddleware creates a middleware for duplex streaming with a WebSocket session.
// The session handles bidirectional communication with the provider's streaming API.
func DuplexProviderMiddleware(
	session providers.StreamInputSession,
	config *ProviderMiddlewareConfig,
) pipeline.Middleware {
	return &duplexProviderMiddleware{
		session: session,
		config:  config,
	}
}

// Process handles duplex streaming by forwarding chunks between StreamInput and the WebSocket session.
// It operates in two concurrent goroutines:
// 1. Forward chunks from StreamInput to session
// 2. Forward responses from session to StreamOutput
func (m *duplexProviderMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	if err := m.validateContext(execCtx); err != nil {
		return err
	}

	logger.Debug("DuplexProviderMiddleware: starting bidirectional streaming")

	ctx := execCtx.Context
	startTime := time.Now()

	// Channel to signal when input forwarding is done
	inputDone := make(chan error, 1)

	// Start input forwarding goroutine
	go m.forwardInputChunks(ctx, execCtx.StreamInput, inputDone)

	// Forward responses from session to output (blocks until complete)
	assistantContent, totalTokens, responseErr := m.forwardResponseChunks(ctx, execCtx.StreamOutput)

	// Finalize
	return m.finalize(execCtx, next, inputDone, assistantContent, totalTokens, responseErr, startTime)
}

// validateContext checks that the execution context has required fields
func (m *duplexProviderMiddleware) validateContext(execCtx *pipeline.ExecutionContext) error {
	if m.session == nil {
		return fmt.Errorf("duplex provider middleware: no stream session configured")
	}
	if !execCtx.StreamMode {
		return fmt.Errorf("duplex provider middleware: only available in stream mode")
	}
	if execCtx.StreamInput == nil || execCtx.StreamOutput == nil {
		return fmt.Errorf("duplex provider middleware: StreamInput and StreamOutput channels required")
	}
	return nil
}

// forwardInputChunks forwards chunks from StreamInput to the WebSocket session
func (m *duplexProviderMiddleware) forwardInputChunks(
	ctx context.Context,
	streamInput chan providers.StreamChunk,
	done chan error,
) {
	for {
		select {
		case <-ctx.Done():
			done <- ctx.Err()
			return
		case chunk, ok := <-streamInput:
			if !ok {
				logger.Debug("DuplexProviderMiddleware: StreamInput closed, ending session input")
				done <- nil
				return
			}
			m.sendChunkToSession(ctx, &chunk)
		}
	}
}

// sendChunkToSession sends a single chunk to the WebSocket session
func (m *duplexProviderMiddleware) sendChunkToSession(ctx context.Context, chunk *providers.StreamChunk) {
	if chunk.MediaDelta != nil && chunk.MediaDelta.Data != nil {
		m.sendMediaChunk(ctx, chunk)
	} else if chunk.Content != "" {
		m.sendTextChunk(ctx, chunk)
	}
}

// sendMediaChunk sends a media chunk to the session
func (m *duplexProviderMiddleware) sendMediaChunk(ctx context.Context, chunk *providers.StreamChunk) {
	dataStr := *chunk.MediaDelta.Data
	var audioData []byte
	var err error

	// Try base64 decode first
	audioData, err = base64.StdEncoding.DecodeString(dataStr)
	if err != nil {
		// If decode fails, assume it's raw bytes masquerading as string
		audioData = []byte(dataStr)
	}

	mediaChunk := &types.MediaChunk{
		Data:        audioData,
		SequenceNum: int64(chunk.TokenCount),
		Timestamp:   time.Now(),
		IsLast:      chunk.FinishReason != nil,
	}

	logger.Debug("DuplexProviderMiddleware: forwarding media chunk to session",
		"dataLen", len(mediaChunk.Data),
		"isLast", mediaChunk.IsLast)

	if err := m.session.SendChunk(ctx, mediaChunk); err != nil {
		logger.Error("DuplexProviderMiddleware: failed to send chunk to session", "error", err)
	}
}

// sendTextChunk sends a text chunk to the session
func (m *duplexProviderMiddleware) sendTextChunk(ctx context.Context, chunk *providers.StreamChunk) {
	logger.Debug("DuplexProviderMiddleware: forwarding text to session", "content", chunk.Content)
	if err := m.session.SendText(ctx, chunk.Content); err != nil {
		logger.Error("DuplexProviderMiddleware: failed to send text to session", "error", err)
	}
}

// forwardResponseChunks forwards responses from session to StreamOutput
func (m *duplexProviderMiddleware) forwardResponseChunks(
	ctx context.Context,
	streamOutput chan providers.StreamChunk,
) (content string, tokens int, err error) {
	var assistantContent string
	var totalTokens int

	responseChannel := m.session.Response()
	for {
		select {
		case <-ctx.Done():
			content = assistantContent
			tokens = totalTokens
			err = ctx.Err()
			return content, tokens, err
		case chunk, ok := <-responseChannel:
			if !ok {
				logger.Debug("DuplexProviderMiddleware: session response channel closed")
				content = assistantContent
				tokens = totalTokens
				err = nil
				return content, tokens, err
			}

			assistantContent, totalTokens = m.accumulateChunkData(&chunk, assistantContent, totalTokens)

			if handleErr := m.handleResponseChunk(ctx, &chunk, streamOutput); handleErr != nil {
				content = assistantContent
				tokens = totalTokens
				err = handleErr
				return content, tokens, err
			}

			if m.isFinished(&chunk) {
				content = assistantContent
				tokens = totalTokens
				err = nil
				return content, tokens, err
			}
		}
	}
}

// accumulateChunkData accumulates content and token counts from a chunk
func (m *duplexProviderMiddleware) accumulateChunkData(
	chunk *providers.StreamChunk,
	assistantContent string,
	totalTokens int,
) (newContent string, newTokens int) {
	newContent = assistantContent + chunk.Content
	newTokens = totalTokens
	if chunk.Metadata != nil {
		if tokens, ok := chunk.Metadata["total_tokens"].(int); ok {
			newTokens = tokens
		}
	}
	return
}

// isFinished checks if the chunk indicates streaming is complete
func (m *duplexProviderMiddleware) isFinished(chunk *providers.StreamChunk) bool {
	if chunk.FinishReason != nil && *chunk.FinishReason != "" {
		logger.Debug("DuplexProviderMiddleware: streaming finished", "reason", *chunk.FinishReason)
		return true
	}
	return false
}

// handleResponseChunk processes and forwards a single response chunk
func (m *duplexProviderMiddleware) handleResponseChunk(
	ctx context.Context,
	chunk *providers.StreamChunk,
	streamOutput chan providers.StreamChunk,
) error {
	if chunk.Error != nil {
		logger.Error("DuplexProviderMiddleware: chunk error from session", "error", chunk.Error)
		// Forward error chunk
		select {
		case streamOutput <- *chunk:
		case <-ctx.Done():
			return ctx.Err()
		}
		return fmt.Errorf("provider middleware: streaming failed: %w", chunk.Error)
	}

	logger.Debug("DuplexProviderMiddleware: forwarding response chunk",
		"content", chunk.Content,
		"hasMedia", chunk.MediaDelta != nil,
		"finishReason", chunk.FinishReason)

	select {
	case streamOutput <- *chunk:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// finalize completes the streaming process
func (m *duplexProviderMiddleware) finalize(
	execCtx *pipeline.ExecutionContext,
	next func() error,
	inputDone chan error,
	assistantContent string,
	totalTokens int,
	responseErr error,
	startTime time.Time,
) error {
	duration := time.Since(startTime)
	logger.Debug("DuplexProviderMiddleware: streaming completed",
		"duration", duration,
		"contentLength", len(assistantContent),
		"totalTokens", totalTokens)

	// Wait for input forwarding to complete
	select {
	case inputErr := <-inputDone:
		if inputErr != nil && responseErr == nil {
			responseErr = fmt.Errorf("failed to forward input: %w", inputErr)
		}
	case <-time.After(1 * time.Second):
		logger.Warn("DuplexProviderMiddleware: timeout waiting for input forwarding to complete")
	}

	// Add assistant message to conversation history
	if assistantContent != "" {
		execCtx.Messages = append(execCtx.Messages, types.Message{
			Role:    "assistant",
			Content: assistantContent,
		})
		execCtx.Response = &pipeline.Response{
			Content: assistantContent,
		}
	}

	// Call next middleware
	nextErr := next()

	// Return first error encountered
	if responseErr != nil {
		return responseErr
	}
	return nextErr
}

// StreamChunk is a no-op for duplex provider middleware.
// Chunk processing happens in the Process method.
func (m *duplexProviderMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	return nil
}
