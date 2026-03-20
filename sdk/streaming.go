package sdk

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	streamPkg "github.com/AltairaLabs/PromptKit/sdk/stream"
)

// StreamChunk represents a single chunk in a streaming response.
type StreamChunk struct {
	// Type of this chunk
	Type ChunkType

	// Text content (for ChunkText type)
	Text string

	// Tool call (for ChunkToolCall type)
	ToolCall *types.MessageToolCall

	// Media content (for ChunkMedia type)
	Media *types.MediaContent

	// ClientTool contains a pending client tool request (for ChunkClientTool type).
	// The caller should fulfill it via SendToolResult/RejectClientTool, then call ResumeStream.
	ClientTool *PendingClientTool

	// Complete response (for ChunkDone type)
	Message *Response

	// Error (if any occurred)
	Error error
}

// ChunkType identifies the type of a streaming chunk.
type ChunkType int

const (
	// ChunkText indicates the chunk contains text content.
	ChunkText ChunkType = iota

	// ChunkToolCall indicates the chunk contains a tool call.
	ChunkToolCall

	// ChunkMedia indicates the chunk contains media content.
	ChunkMedia

	// ChunkDone indicates streaming is complete.
	ChunkDone

	// ChunkClientTool indicates a client tool request that needs caller fulfillment.
	ChunkClientTool
)

// String returns the string representation of the chunk type.
func (t ChunkType) String() string {
	switch t {
	case ChunkText:
		return contentTypeText
	case ChunkToolCall:
		return "tool_call"
	case ChunkMedia:
		return "media"
	case ChunkDone:
		return "done"
	case ChunkClientTool:
		return "client_tool"
	default:
		return "unknown"
	}
}

// streamState tracks state during stream processing.
type streamState struct {
	contentBuilder strings.Builder
	lastToolCalls  []types.MessageToolCall
	finalResult    *rtpipeline.ExecutionResult
	pendingTools   []PendingClientTool
}

// accumulatedContent returns the accumulated content as a string.
func (s *streamState) accumulatedContent() string {
	return s.contentBuilder.String()
}

// Stream sends a message and returns a channel of response chunks.
//
// Use this for real-time streaming of LLM responses:
//
//	for chunk := range conv.Stream(ctx, "Tell me a story") {
//	    if chunk.Error != nil {
//	        log.Printf("Error: %v", chunk.Error)
//	        break
//	    }
//	    fmt.Print(chunk.Text)
//	}
//
// The channel is closed when the response is complete or an error occurs.
// The final chunk (Type == ChunkDone) contains the complete Response.
func (c *Conversation) Stream(ctx context.Context, message any, opts ...SendOption) <-chan StreamChunk {
	ch := make(chan StreamChunk, streamChannelBufferSize)

	// Register the caller's context with the OTel listener before launching the
	// goroutine so the session exists when pipeline events start arriving.
	c.startOTelSession(ctx)

	go func() {
		defer close(ch)
		startTime := time.Now()

		c.mu.RLock()
		if err := c.requireUnary("Stream()"); err != nil {
			c.mu.RUnlock()
			select {
			case ch <- StreamChunk{Error: err}:
			case <-ctx.Done():
			}
			return
		}
		c.mu.RUnlock()

		// Build user message with options
		userMsg, err := c.buildStreamMessage(message, opts)
		if err != nil {
			select {
			case ch <- StreamChunk{Error: err}:
			case <-ctx.Done():
			}
			return
		}

		// Execute streaming pipeline
		if err := c.executeStreamingPipeline(ctx, userMsg, ch, startTime); err != nil {
			select {
			case ch <- StreamChunk{Error: err}:
			case <-ctx.Done():
			}
		}
	}()

	return ch
}

// buildStreamMessage constructs a user message from input and options.
func (c *Conversation) buildStreamMessage(message any, opts []SendOption) (*types.Message, error) {
	// Build user message from input
	var userMsg *types.Message
	switch m := message.(type) {
	case string:
		userMsg = &types.Message{Role: "user"}
		userMsg.AddTextPart(m)
	case *types.Message:
		userMsg = m
	default:
		return nil, fmt.Errorf("message must be string or *types.Message, got %T", message)
	}

	// Apply send options
	sendCfg := &sendConfig{}
	for _, opt := range opts {
		if err := opt(sendCfg); err != nil {
			return nil, fmt.Errorf("failed to apply send option: %w", err)
		}
	}

	// Add content parts to message
	if err := c.addContentParts(userMsg, sendCfg.parts); err != nil {
		return nil, err
	}

	return userMsg, nil
}

// addContentParts adds content parts from options to the message.
func (c *Conversation) addContentParts(msg *types.Message, parts []any) error {
	for _, part := range parts {
		switch p := part.(type) {
		case imageFilePart:
			if err := msg.AddImagePart(p.path, p.detail); err != nil {
				return fmt.Errorf("failed to add image from file: %w", err)
			}
		case imageURLPart:
			msg.AddImagePartFromURL(p.url, p.detail)
		case imageDataPart:
			base64Data := base64.StdEncoding.EncodeToString(p.data)
			contentPart := types.NewImagePartFromData(base64Data, p.mimeType, p.detail)
			msg.AddPart(contentPart)
		case audioFilePart:
			if err := msg.AddAudioPart(p.path); err != nil {
				return fmt.Errorf("failed to add audio from file: %w", err)
			}
		case audioDataPart:
			base64Data := base64.StdEncoding.EncodeToString(p.data)
			contentPart := types.NewAudioPartFromData(base64Data, p.mimeType)
			msg.AddPart(contentPart)
		case videoFilePart:
			if err := msg.AddVideoPart(p.path); err != nil {
				return fmt.Errorf("failed to add video from file: %w", err)
			}
		case videoDataPart:
			base64Data := base64.StdEncoding.EncodeToString(p.data)
			contentPart := types.NewVideoPartFromData(base64Data, p.mimeType)
			msg.AddPart(contentPart)
		case documentFilePart:
			if err := msg.AddDocumentPart(p.path); err != nil {
				return fmt.Errorf("failed to add document from file: %w", err)
			}
		case documentDataPart:
			base64Data := base64.StdEncoding.EncodeToString(p.data)
			contentPart := types.NewDocumentPartFromData(base64Data, p.mimeType)
			msg.AddPart(contentPart)
		case filePart:
			// Legacy file part - kept for backward compatibility
			// Try to detect if it's a document by checking the name extension
			msg.AddTextPart(fmt.Sprintf("[File: %s]\n%s", p.name, string(p.data)))
		default:
			return fmt.Errorf("unknown content part type: %T", part)
		}
	}
	return nil
}

// executeStreamingPipeline builds and executes the LLM pipeline in streaming mode.
func (c *Conversation) executeStreamingPipeline(
	ctx context.Context,
	userMsg *types.Message,
	outCh chan<- StreamChunk,
	startTime time.Time,
) error {
	// Execute streaming through the unary session (only called from Stream which checks mode)
	streamCh, err := c.unarySession.ExecuteStreamWithMessage(ctx, *userMsg)
	if err != nil {
		return fmt.Errorf("pipeline streaming failed: %w", err)
	}

	// Process stream and finalize
	state := &streamState{}
	if err := c.processAndFinalizeStreamWithState(ctx, streamCh, outCh, startTime, state); err != nil {
		return err
	}

	// Skip lifecycle hooks when pipeline is suspended for pending tools
	if len(state.pendingTools) == 0 {
		c.sessionHooks.IncrementTurn()
		c.sessionHooks.SessionUpdate(ctx)
		c.evalMW.dispatchTurnEvals(ctx)
	}

	return nil
}

// processAndFinalizeStreamWithState processes the streaming response, emits chunks, and sends
// the final ChunkDone. Uses a caller-provided state
// so the caller can inspect pendingTools after the stream completes.
func (c *Conversation) processAndFinalizeStreamWithState(
	ctx context.Context,
	streamCh <-chan providers.StreamChunk,
	outCh chan<- StreamChunk,
	startTime time.Time,
	state *streamState,
) error {
	// Process all stream chunks
	if err := c.processStreamChunks(ctx, streamCh, outCh, state); err != nil {
		return err
	}

	// Build response from accumulated data
	resp := c.buildStreamingResponse(state, startTime)

	// Emit final ChunkDone with complete response, respecting context cancellation
	// to avoid blocking indefinitely when the consumer has gone away.
	select {
	case outCh <- StreamChunk{Type: ChunkDone, Message: resp}:
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

// processStreamChunks processes provider chunks and emits SDK chunks.
// It respects context cancellation to avoid goroutine leaks when the consumer
// abandons the output channel.
func (c *Conversation) processStreamChunks(
	ctx context.Context,
	streamCh <-chan providers.StreamChunk,
	outCh chan<- StreamChunk,
	state *streamState,
) error {
	for chunk := range streamCh {
		if chunk.Error != nil {
			return chunk.Error
		}

		c.emitStreamChunk(ctx, &chunk, outCh, state)
	}
	return nil
}

// sendChunk sends a StreamChunk to outCh, returning immediately if ctx is canceled.
func sendChunk(ctx context.Context, outCh chan<- StreamChunk, chunk StreamChunk) {
	select {
	case outCh <- chunk:
	case <-ctx.Done():
	}
}

// emitStreamChunk converts a provider chunk to SDK chunk(s) and updates state.
// All channel sends respect context cancellation to prevent goroutine leaks.
func (c *Conversation) emitStreamChunk(
	ctx context.Context,
	chunk *providers.StreamChunk,
	outCh chan<- StreamChunk,
	state *streamState,
) {
	// Emit text delta
	if chunk.Delta != "" {
		state.contentBuilder.WriteString(chunk.Delta)
		sendChunk(ctx, outCh, StreamChunk{Type: ChunkText, Text: chunk.Delta})
	}

	// Emit media delta
	if chunk.MediaDelta != nil {
		sendChunk(ctx, outCh, StreamChunk{Type: ChunkMedia, Media: chunk.MediaDelta})
	}

	// Emit new tool calls
	if len(chunk.ToolCalls) > len(state.lastToolCalls) {
		for i := len(state.lastToolCalls); i < len(chunk.ToolCalls); i++ {
			sendChunk(ctx, outCh, StreamChunk{Type: ChunkToolCall, ToolCall: &chunk.ToolCalls[i]})
		}
		// Copy the slice to avoid holding a reference to the provider's internal slice
		state.lastToolCalls = make([]types.MessageToolCall, len(chunk.ToolCalls))
		copy(state.lastToolCalls, chunk.ToolCalls)
	}

	// Handle pending client tools
	if chunk.FinishReason != nil && *chunk.FinishReason == "pending_tools" && len(chunk.PendingTools) > 0 {
		for i := range chunk.PendingTools {
			pct := buildPendingClientToolFromExecution(&chunk.PendingTools[i])
			state.pendingTools = append(state.pendingTools, pct)
			sendChunk(ctx, outCh, StreamChunk{Type: ChunkClientTool, ClientTool: &pct})
		}
		if result, ok := chunk.FinalResult.(*rtpipeline.ExecutionResult); ok {
			state.finalResult = result
		}
		return
	}

	// Capture final result
	if chunk.FinishReason != nil {
		if result, ok := chunk.FinalResult.(*rtpipeline.ExecutionResult); ok {
			state.finalResult = result
		}
	}
}

// buildPendingClientToolFromExecution converts a tools.PendingToolExecution to a PendingClientTool.
func buildPendingClientToolFromExecution(pt *tools.PendingToolExecution) PendingClientTool {
	pct := PendingClientTool{
		CallID:   pt.CallID,
		ToolName: pt.ToolName,
		Args:     pt.Args,
	}
	if pt.PendingInfo != nil {
		pct.ConsentMsg = pt.PendingInfo.Message
		if cats, ok := pt.PendingInfo.Metadata["categories"]; ok {
			if catSlice, ok := cats.([]string); ok {
				pct.Categories = catSlice
			}
		}
	}
	return pct
}

// buildStreamingResponse creates a Response from streaming data.
func (c *Conversation) buildStreamingResponse(
	state *streamState,
	startTime time.Time,
) *Response {
	resp := &Response{
		duration: time.Since(startTime),
	}

	result := state.finalResult

	// Use result data if available
	if result != nil && result.Response != nil {
		resp.message = &types.Message{
			Role:     roleAssistant,
			Content:  result.Response.Content,
			Parts:    result.Response.Parts,
			CostInfo: &result.CostInfo,
		}

		if len(result.Response.ToolCalls) > 0 {
			resp.toolCalls = result.Response.ToolCalls
		}

		// Extract validations
		for i := len(result.Messages) - 1; i >= 0; i-- {
			if result.Messages[i].Role == roleAssistant && len(result.Messages[i].Validations) > 0 {
				resp.validations = result.Messages[i].Validations
				break
			}
		}
	} else {
		// Build from accumulated streaming data
		resp.message = &types.Message{
			Role:    roleAssistant,
			Content: state.accumulatedContent(),
		}
		if len(state.lastToolCalls) > 0 {
			resp.toolCalls = state.lastToolCalls
		}
	}

	// Populate pending client tools from stream state
	if len(state.pendingTools) > 0 {
		resp.clientTools = state.pendingTools
	}

	return resp
}

// StreamRaw returns a channel of streaming chunks for use with the stream package.
// This is a lower-level API that returns stream.Chunk types.
//
// Most users should use [Conversation.Stream] instead.
// StreamRaw is useful when working with [stream.Process] or [stream.CollectText].
//
//	err := stream.Process(ctx, conv, "Hello", func(chunk stream.Chunk) error {
//	    fmt.Print(chunk.Text)
//	    return nil
//	})
func (c *Conversation) StreamRaw(ctx context.Context, message any) (<-chan streamPkg.Chunk, error) {
	ch := make(chan streamPkg.Chunk, streamChannelBufferSize)

	go func() {
		defer close(ch)

		for sdkChunk := range c.Stream(ctx, message) {
			// Convert SDK StreamChunk to stream.Chunk
			chunk := streamPkg.Chunk{
				Error: sdkChunk.Error,
			}

			switch sdkChunk.Type {
			case ChunkText:
				chunk.Type = streamPkg.ChunkText
				chunk.Text = sdkChunk.Text
			case ChunkToolCall:
				chunk.Type = streamPkg.ChunkToolCall
				chunk.ToolCall = sdkChunk.ToolCall
			case ChunkMedia:
				chunk.Type = streamPkg.ChunkMedia
				chunk.Media = sdkChunk.Media
			case ChunkClientTool:
				// Client tool requests are SDK-level; skip in raw stream
				continue
			case ChunkDone:
				chunk.Done = true
			}

			ch <- chunk
		}
	}()

	return ch, nil
}
