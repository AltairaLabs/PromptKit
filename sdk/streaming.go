package sdk

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
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
)

// String returns the string representation of the chunk type.
func (t ChunkType) String() string {
	switch t {
	case ChunkText:
		return "text"
	case ChunkToolCall:
		return "tool_call"
	case ChunkMedia:
		return "media"
	case ChunkDone:
		return "done"
	default:
		return "unknown"
	}
}

// streamState tracks state during stream processing.
type streamState struct {
	accumulatedContent string
	lastToolCalls      []types.MessageToolCall
	finalResult        *rtpipeline.ExecutionResult
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

	go func() {
		defer close(ch)
		startTime := time.Now()

		c.mu.RLock()
		if c.mode != UnaryMode {
			c.mu.RUnlock()
			ch <- StreamChunk{Error: fmt.Errorf("Stream() only available in unary mode; use OpenDuplex() for duplex streaming")}
			return
		}
		if c.closed {
			c.mu.RUnlock()
			ch <- StreamChunk{Error: ErrConversationClosed}
			return
		}
		c.mu.RUnlock()

		// Build user message with options
		userMsg, err := c.buildStreamMessage(message, opts)
		if err != nil {
			ch <- StreamChunk{Error: err}
			return
		}

		// Execute streaming pipeline
		if err := c.executeStreamingPipeline(ctx, userMsg, ch, startTime); err != nil {
			ch <- StreamChunk{Error: err}
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
		case filePart:
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
	return c.processAndFinalizeStream(streamCh, outCh, startTime)
}

// processAndFinalizeStream handles the streaming response and emits the final chunk.
func (c *Conversation) processAndFinalizeStream(
	streamCh <-chan providers.StreamChunk,
	outCh chan<- StreamChunk,
	startTime time.Time,
) error {
	state := &streamState{}

	// Process all stream chunks
	if err := c.processStreamChunks(streamCh, outCh, state); err != nil {
		return err
	}

	// Build response from accumulated data
	resp := c.buildStreamingResponse(state.finalResult, state.accumulatedContent, state.lastToolCalls, startTime)

	// Add assistant response to history
	c.finalizeStreamHistory(state)

	// Emit final ChunkDone with complete response
	outCh <- StreamChunk{
		Type:    ChunkDone,
		Message: resp,
	}

	return nil
}

// processStreamChunks processes provider chunks and emits SDK chunks.
func (c *Conversation) processStreamChunks(
	streamCh <-chan providers.StreamChunk,
	outCh chan<- StreamChunk,
	state *streamState,
) error {
	for chunk := range streamCh {
		if chunk.Error != nil {
			return chunk.Error
		}

		c.emitStreamChunk(&chunk, outCh, state)
	}
	return nil
}

// emitStreamChunk converts a provider chunk to SDK chunk(s) and updates state.
func (c *Conversation) emitStreamChunk(
	chunk *providers.StreamChunk,
	outCh chan<- StreamChunk,
	state *streamState,
) {
	// Emit text delta
	if chunk.Delta != "" {
		state.accumulatedContent += chunk.Delta
		outCh <- StreamChunk{Type: ChunkText, Text: chunk.Delta}
	}

	// Emit media delta
	if chunk.MediaDelta != nil {
		outCh <- StreamChunk{Type: ChunkMedia, Media: chunk.MediaDelta}
	}

	// Emit new tool calls
	if len(chunk.ToolCalls) > len(state.lastToolCalls) {
		for i := len(state.lastToolCalls); i < len(chunk.ToolCalls); i++ {
			outCh <- StreamChunk{Type: ChunkToolCall, ToolCall: &chunk.ToolCalls[i]}
		}
		state.lastToolCalls = chunk.ToolCalls
	}

	// Capture final result
	if chunk.FinishReason != nil {
		if result, ok := chunk.FinalResult.(*rtpipeline.ExecutionResult); ok {
			state.finalResult = result
		}
	}
}

// finalizeStreamHistory adds the assistant response to history after streaming.
func (c *Conversation) finalizeStreamHistory(state *streamState) {
	// State is managed by StateStore middleware in the pipeline
	// No local state tracking needed
}

// buildStreamingResponse creates a Response from streaming data.
func (c *Conversation) buildStreamingResponse(
	result *rtpipeline.ExecutionResult,
	content string,
	toolCalls []types.MessageToolCall,
	startTime time.Time,
) *Response {
	resp := &Response{
		duration: time.Since(startTime),
	}

	// Use result data if available
	if result != nil && result.Response != nil {
		resp.message = &types.Message{
			Role:     "assistant",
			Content:  result.Response.Content,
			CostInfo: &result.CostInfo,
		}

		if len(result.Response.ToolCalls) > 0 {
			resp.toolCalls = result.Response.ToolCalls
		}

		// Extract validations
		for i := len(result.Messages) - 1; i >= 0; i-- {
			if result.Messages[i].Role == "assistant" && len(result.Messages[i].Validations) > 0 {
				resp.validations = result.Messages[i].Validations
				break
			}
		}
	} else {
		// Build from accumulated streaming data
		resp.message = &types.Message{
			Role:    "assistant",
			Content: content,
		}
		if len(toolCalls) > 0 {
			resp.toolCalls = toolCalls
		}
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
			case ChunkDone:
				chunk.Done = true
			}

			ch <- chunk
		}
	}()

	return ch, nil
}
