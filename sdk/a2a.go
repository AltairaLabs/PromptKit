package sdk

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// sdkConv captures the subset of *Conversation methods the adapter needs.
type sdkConv interface {
	Send(ctx context.Context, message any, opts ...SendOption) (*Response, error)
	Stream(ctx context.Context, message any, opts ...SendOption) <-chan StreamChunk
	Close() error
}

// A2AAdapter wraps a *Conversation to satisfy a2a.StreamingConversation.
type A2AAdapter struct {
	conv sdkConv
}

// NewA2AAdapter creates an adapter that implements a2a.StreamingConversation
// by delegating to the given SDK Conversation.
func NewA2AAdapter(conv *Conversation) *A2AAdapter {
	return &A2AAdapter{conv: conv}
}

// Send forwards a message to the underlying conversation and converts the
// response to an a2a.ConversationResult.
func (a *A2AAdapter) Send(ctx context.Context, msg *types.Message) (*a2a.ConversationResult, error) {
	resp, err := a.conv.Send(ctx, msg)
	if err != nil {
		return nil, err
	}
	return &a2a.ConversationResult{
		Parts:        resp.Parts(),
		PendingTools: len(resp.PendingTools()) > 0,
	}, nil
}

// Stream forwards a message to the underlying conversation and returns a
// channel of a2a.StreamChunk values converted from SDK chunks.
func (a *A2AAdapter) Stream(ctx context.Context, msg *types.Message) (<-chan a2a.StreamChunk, error) {
	sdkCh := a.conv.Stream(ctx, msg)
	out := make(chan a2a.StreamChunk, cap(sdkCh))

	go func() {
		defer close(out)
		for chunk := range sdkCh {
			out <- convertChunk(chunk)
		}
	}()

	return out, nil
}

// Close delegates to the underlying conversation's Close method.
func (a *A2AAdapter) Close() error {
	return a.conv.Close()
}

// convertChunk maps an SDK StreamChunk to an a2a StreamChunk.
func convertChunk(c StreamChunk) a2a.StreamChunk {
	if c.Error != nil {
		return a2a.StreamChunk{Error: c.Error}
	}
	switch c.Type {
	case ChunkText:
		return a2a.StreamChunk{Type: a2a.StreamChunkText, Text: c.Text}
	case ChunkMedia:
		return a2a.StreamChunk{Type: a2a.StreamChunkMedia, Media: c.Media}
	case ChunkToolCall:
		return a2a.StreamChunk{Type: a2a.StreamChunkToolCall}
	case ChunkDone:
		return a2a.StreamChunk{Type: a2a.StreamChunkDone}
	default:
		return a2a.StreamChunk{Type: a2a.StreamChunkText}
	}
}

// A2AOpener returns an a2a.ConversationOpener backed by SDK conversations.
// Each call to the returned function opens a new conversation for the given
// context ID using sdk.Open with the provided pack path, prompt name, and options.
func A2AOpener(packPath, promptName string, opts ...Option) a2a.ConversationOpener {
	return func(contextID string) (a2a.Conversation, error) {
		conv, err := Open(packPath, promptName, opts...)
		if err != nil {
			return nil, err
		}
		return NewA2AAdapter(conv), nil
	}
}
