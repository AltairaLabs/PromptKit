// Package stream provides streaming support for SDK v2.
//
// This package extends [sdk.Conversation.Stream] with more control over
// streaming behavior and chunk handling.
//
// Basic streaming is available directly on Conversation:
//
//	for chunk := range conv.Stream(ctx, "Tell me a story") {
//	    fmt.Print(chunk.Text)
//	}
//
// This package provides additional functionality for advanced use cases.
package stream

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Chunk represents a single chunk in a streaming response.
// This is an alias to the SDK's StreamChunk for convenience.
type Chunk struct {
	// Type of this chunk
	Type ChunkType

	// Text content (for ChunkText type)
	Text string

	// Tool call (for ChunkToolCall type)
	ToolCall *types.MessageToolCall

	// Media content (for ChunkMedia type)
	Media *types.MediaContent

	// Error (if any occurred)
	Error error

	// Done indicates this is the final chunk
	Done bool
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
)

// Streamer is the interface for streaming conversations.
// This is implemented by [sdk.Conversation].
type Streamer interface {
	// StreamRaw returns a channel of raw runtime chunks.
	// This is lower-level than the standard Stream method.
	StreamRaw(ctx context.Context, message any) (<-chan Chunk, error)
}

// Handler is a function that processes streaming chunks.
// Return an error to stop streaming.
type Handler func(chunk Chunk) error

// Process streams a message and processes each chunk with the handler.
// Streaming stops when:
//   - The handler returns an error
//   - The context is canceled
//   - The stream completes
//
// Example:
//
//	err := stream.Process(ctx, conv, "Tell me a story", func(chunk stream.Chunk) error {
//	    fmt.Print(chunk.Text)
//	    return nil
//	})
func Process(ctx context.Context, streamer Streamer, message any, handler Handler) error {
	chunks, err := streamer.StreamRaw(ctx, message)
	if err != nil {
		return err
	}

	for chunk := range chunks {
		if chunk.Error != nil {
			return chunk.Error
		}

		if err := handler(chunk); err != nil {
			return err
		}

		if chunk.Done {
			break
		}
	}

	return nil
}

// CollectText streams a message and collects all text content.
// This is useful when you want streaming for progress indication
// but ultimately need the complete text.
//
// Example:
//
//	text, err := stream.CollectText(ctx, conv, "Summarize this document")
func CollectText(ctx context.Context, streamer Streamer, message any) (string, error) {
	var result string

	err := Process(ctx, streamer, message, func(chunk Chunk) error {
		if chunk.Type == ChunkText {
			result += chunk.Text
		}
		return nil
	})

	return result, err
}
