package sdk

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	a2aserver "github.com/AltairaLabs/PromptKit/server/a2a"
)

// responseAdapter wraps *Response to satisfy a2aserver.SendResult.
type responseAdapter struct{ r *Response }

// HasPendingTools implements a2aserver.SendResult.
func (a *responseAdapter) HasPendingTools() bool { return len(a.r.PendingTools()) > 0 }

// Parts implements a2aserver.SendResult.
func (a *responseAdapter) Parts() []types.ContentPart { return a.r.Parts() }

// Text implements a2aserver.SendResult.
func (a *responseAdapter) Text() string { return a.r.Text() }

// convAdapter wraps *Conversation to satisfy a2aserver.Conversation.
type convAdapter struct{ c *Conversation }

// Send implements a2aserver.Conversation.
func (a *convAdapter) Send(ctx context.Context, message any) (a2aserver.SendResult, error) {
	resp, err := a.c.Send(ctx, message)
	if err != nil {
		return nil, err
	}
	return &responseAdapter{r: resp}, nil
}

// Close implements a2aserver.Conversation.
func (a *convAdapter) Close() error { return a.c.Close() }

// streamConvAdapter extends convAdapter with streaming support.
type streamConvAdapter struct{ convAdapter }

// Stream implements a2aserver.StreamingConversation.
func (a *streamConvAdapter) Stream(ctx context.Context, message any) <-chan a2aserver.StreamEvent {
	chunks := a.c.Stream(ctx, message)
	out := make(chan a2aserver.StreamEvent, cap(chunks))
	go func() {
		defer close(out)
		for chunk := range chunks {
			out <- chunkToEvent(chunk)
		}
	}()
	return out
}

// chunkToEvent converts an SDK StreamChunk to a server StreamEvent.
func chunkToEvent(c StreamChunk) a2aserver.StreamEvent {
	if c.Error != nil {
		return a2aserver.StreamEvent{Error: c.Error}
	}
	switch c.Type {
	case ChunkText:
		return a2aserver.StreamEvent{Kind: a2aserver.EventText, Text: c.Text}
	case ChunkMedia:
		return a2aserver.StreamEvent{Kind: a2aserver.EventMedia, Media: c.Media}
	case ChunkToolCall:
		return a2aserver.StreamEvent{Kind: a2aserver.EventToolCall}
	case ChunkDone:
		return a2aserver.StreamEvent{Kind: a2aserver.EventDone}
	default:
		return a2aserver.StreamEvent{}
	}
}
