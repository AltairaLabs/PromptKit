package a2aserver

import (
	"context"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// This file is the server's second mode.
//
// With a [ConversationOpener] the server holds conversations: it opens one per
// context id, caches it, reuses it when that id comes back, and expires it on
// its own clock. That suits an embedder running A2A and the runtime in one
// process, and it is unchanged.
//
// With a [MessageHandler] the server holds nothing. Each message is passed to
// the handler with the context of the request it arrived on, and the handler
// answers with a stream of events. What a conversation is, how long it lives,
// and whether two requests sharing a context id share anything are all the
// embedder's to decide — which is what a platform needs when the runtime lives
// somewhere else and A2A is only the protocol surface in front of it.
//
// The protocol half — JSON-RPC dispatch, the task state machine, the task
// store, SSE fan-out, agent cards — is identical either way.

// MessageHandler turns one inbound A2A message into a stream of events.
//
// Handle is called once per message and the server keeps no state between
// calls. ctx is the HTTP request's context, so whatever the embedder's
// middleware put on it — caller identity, tenant, trace — is readable here,
// which is the thing [ConversationOpener] cannot offer.
//
// The returned channel must be closed when the turn is over. A closing channel
// that sent no [EventDone] is treated as a completed turn.
type MessageHandler interface {
	Handle(ctx context.Context, req MessageRequest) <-chan StreamEvent
}

// MessageRequest is one inbound message, as the handler sees it.
type MessageRequest struct {
	// ContextID is client-supplied. The server attaches no meaning to it: the
	// embedder decides whether it identifies a session, and whether two callers
	// presenting the same one share anything.
	ContextID string

	// TaskID is the server-assigned id of the task this message created.
	TaskID string

	// Message is the inbound A2A message.
	Message a2a.Message
}

// ToolResultHandler is the optional client-tool half of [MessageHandler]. A
// handler that implements it can receive the results of client-side tool calls
// it previously asked for and continue the turn.
//
// Without it, a stateless server rejects tool-result messages rather than
// pretending to resume something it is not holding.
type ToolResultHandler interface {
	HandleToolResult(ctx context.Context, req ToolResultRequest) <-chan StreamEvent
}

// ToolResultRequest carries client-tool results back to the handler.
type ToolResultRequest struct {
	ContextID string
	TaskID    string

	// Results are the fulfilled tool calls, keyed by the call id the handler
	// supplied in its [EventClientTool] events.
	Results []ToolResult
}

// ToolResult is one fulfilled — or refused — client-side tool call.
type ToolResult struct {
	CallID string
	Result any

	// Rejected is true when the caller declined the tool. Reason carries their
	// explanation, if any.
	Rejected bool
	Reason   string
}

// NewStatelessServer creates a server that holds no conversations.
//
// It is [NewServer]'s sibling: same protocol, same options, but each message
// goes to the handler with its request context and nothing is kept between
// calls. Use it when the embedder owns conversations — because it already
// tracks sessions, because the runtime is in another process, or because the
// server needs to scale horizontally with only the task store shared.
func NewStatelessServer(h MessageHandler, opts ...Option) *Server {
	s := newServer(opts...)
	s.handler = h
	return s
}

// streamResult turns the events a handler produced into the [SendResult] the
// non-streaming path needs.
//
// message/send has to answer with a finished task, and finalizeTask asks a
// SendResult what happened. A handler yields only events, so the server drains
// them and synthesizes one. Doing it here means one code path: the same handler
// serves both RPCs, and message/stream is not a special case that gets richer
// data.
//
// The lossy part is Parts(): a handler holding richer content upstream can only
// express it as text runs and media events. That is deliberate — the
// alternative was an optional second interface for the non-streaming path, and
// a second path through the least-exercised half of the protocol costs more
// than the fidelity is worth. A handler that needs a distinct part emits a
// distinct event.
type streamResult struct {
	texts       []string
	media       []*types.MediaContent
	clientTools []PendingClientToolInfo
	pending     bool
	err         error
}

// drain consumes a handler's event channel, stopping when it closes or the
// context ends.
func drain(ctx context.Context, events <-chan StreamEvent) *streamResult {
	out := &streamResult{}
	for {
		select {
		case <-ctx.Done():
			return out
		case evt, ok := <-events:
			if !ok {
				return out
			}
			if out.consume(evt) {
				return out
			}
		}
	}
}

// consume folds one event in and reports whether the turn is over.
func (r *streamResult) consume(evt StreamEvent) (done bool) {
	if evt.Error != nil {
		r.err = evt.Error
		return true
	}
	switch evt.Kind {
	case EventText:
		r.texts = append(r.texts, evt.Text)
	case EventMedia:
		if evt.Media != nil {
			r.media = append(r.media, evt.Media)
		}
	case EventClientTool:
		if evt.ClientTool != nil {
			r.clientTools = append(r.clientTools, *evt.ClientTool)
		}
		// A client tool ends the turn: the caller has to act before anything
		// else can happen.
		r.pending = true
		return true
	case EventPending:
		// The handler is waiting on something the server cannot see — a human
		// approving a tool, most often. Recorded explicitly rather than
		// inferred, because inferring "still waiting" from the absence of
		// client tools would report a paused turn as completed.
		r.pending = true
		if evt.Text != "" {
			r.texts = append(r.texts, evt.Text)
		}
	case EventToolCall:
		// Suppressed, as on the streaming path: agent opacity.
	case EventDone:
		return true
	}
	return false
}

// HasPendingTools implements SendResult.
func (r *streamResult) HasPendingTools() bool { return r.pending }

// HasPendingClientTools implements SendResult.
func (r *streamResult) HasPendingClientTools() bool { return len(r.clientTools) > 0 }

// PendingClientTools implements SendResult.
func (r *streamResult) PendingClientTools() []PendingClientToolInfo { return r.clientTools }

// Parts implements SendResult: one part per text run and one per media event,
// in arrival order is not preserved between the two groups — text first, then
// media — because StreamEvent does not carry enough to interleave them
// faithfully and pretending otherwise would be worse than saying so.
func (r *streamResult) Parts() []types.ContentPart {
	parts := make([]types.ContentPart, 0, len(r.texts)+len(r.media))
	for _, text := range r.texts {
		if text != "" {
			parts = append(parts, types.NewTextPart(text))
		}
	}
	for _, media := range r.media {
		parts = append(parts, types.ContentPart{
			Type:  a2a.InferContentType(media.MIMEType),
			Media: media,
		})
	}
	return parts
}

// Text implements SendResult.
func (r *streamResult) Text() string { return strings.Join(r.texts, "") }

var _ SendResult = (*streamResult)(nil)
