package sdk

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	a2aserver "github.com/AltairaLabs/PromptKit/server/a2a"
)

// responseAdapter wraps *Response to satisfy a2aserver.SendResult.
type responseAdapter struct{ r *Response }

// HasPendingTools implements a2aserver.SendResult.
// Returns true when HITL tools or client-side tools are pending.
func (a *responseAdapter) HasPendingTools() bool {
	return len(a.r.PendingTools()) > 0 || a.r.HasPendingClientTools()
}

// HasPendingClientTools implements a2aserver.SendResult.
func (a *responseAdapter) HasPendingClientTools() bool {
	return a.r.HasPendingClientTools()
}

// PendingClientTools implements a2aserver.SendResult.
func (a *responseAdapter) PendingClientTools() []a2aserver.PendingClientToolInfo {
	sdkTools := a.r.ClientTools()
	if len(sdkTools) == 0 {
		return nil
	}
	out := make([]a2aserver.PendingClientToolInfo, len(sdkTools))
	for i, t := range sdkTools {
		out[i] = a2aserver.PendingClientToolInfo{
			CallID:     t.CallID,
			ToolName:   t.ToolName,
			Args:       t.Args,
			ConsentMsg: t.ConsentMsg,
		}
	}
	return out
}

// Parts implements a2aserver.SendResult.
func (a *responseAdapter) Parts() []types.ContentPart { return a.r.Parts() }

// Text implements a2aserver.SendResult.
func (a *responseAdapter) Text() string { return a.r.Text() }

// conversationBackend is the subset of *Conversation that convAdapter needs.
// This enables unit testing without a fully initialized Conversation.
type conversationBackend interface {
	Send(ctx context.Context, message any, opts ...SendOption) (*Response, error)
	Close() error
	SendToolResult(ctx context.Context, callID string, result any) error
	RejectClientTool(ctx context.Context, callID, reason string)
	Resume(ctx context.Context) (*Response, error)
	ResumeStream(ctx context.Context) <-chan StreamChunk
	Stream(ctx context.Context, message any, opts ...SendOption) <-chan StreamChunk
}

// convAdapter wraps a conversationBackend to satisfy a2aserver.Conversation.
// It also implements a2aserver.ResumableConversation for client tool support.
type convAdapter struct{ c conversationBackend }

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

// SendToolResult implements a2aserver.ResumableConversation.
func (a *convAdapter) SendToolResult(callID string, result any) error {
	return a.c.SendToolResult(context.Background(), callID, result)
}

// RejectClientTool implements a2aserver.ResumableConversation.
func (a *convAdapter) RejectClientTool(callID, reason string) {
	a.c.RejectClientTool(context.Background(), callID, reason)
}

// Resume implements a2aserver.ResumableConversation.
func (a *convAdapter) Resume(ctx context.Context) (a2aserver.SendResult, error) {
	resp, err := a.c.Resume(ctx)
	if err != nil {
		return nil, err
	}
	return &responseAdapter{r: resp}, nil
}

// ResumeStream implements a2aserver.ResumableConversation.
func (a *convAdapter) ResumeStream(ctx context.Context) <-chan a2aserver.StreamEvent {
	chunks := a.c.ResumeStream(ctx)
	out := make(chan a2aserver.StreamEvent)
	go func() {
		defer close(out)
		for chunk := range chunks {
			out <- chunkToEvent(chunk)
		}
	}()
	return out
}

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
	case ChunkClientTool:
		return a2aserver.StreamEvent{
			Kind: a2aserver.EventClientTool,
			ClientTool: &a2aserver.PendingClientToolInfo{
				CallID:     c.ClientTool.CallID,
				ToolName:   c.ClientTool.ToolName,
				Args:       c.ClientTool.Args,
				ConsentMsg: c.ClientTool.ConsentMsg,
			},
		}
	case ChunkDone:
		return a2aserver.StreamEvent{Kind: a2aserver.EventDone}
	default:
		return a2aserver.StreamEvent{}
	}
}
