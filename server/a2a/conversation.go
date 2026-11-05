// Package a2aserver provides a standalone A2A-protocol HTTP server that can be
// backed by any conversation implementation satisfying the interfaces defined
// here. It imports only runtime/ and has no dependency on sdk/.
//
// The server speaks A2A 1.0 and 0.3 over one JSON-RPC endpoint, POST /a2a, and
// answers each request in the protocol version it asked for. It also serves the
// agent card at /.well-known/agent-card.json (and the legacy
// /.well-known/agent.json) and the liveness and readiness probes /healthz and
// /readyz.
//
// There are two ways to supply the agent. NewServer takes a ConversationOpener
// and owns the conversations: it opens one per context ID, caches it, and
// expires it when idle. NewStatelessServer takes a MessageHandler and keeps no
// conversations, so the embedder decides what a conversation is. In both modes
// the server runs the task state machine, stores tasks in a TaskStore, and
// streams updates over SSE. The server does not support push notifications.
package a2aserver

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// SendResult is what the server needs from a completed conversation turn.
type SendResult interface {
	// HasPendingTools reports whether there are tools awaiting approval
	// (HITL or client-side). When true the task transitions to input_required.
	HasPendingTools() bool

	// HasPendingClientTools reports whether there are client-side tools
	// awaiting fulfillment by the caller.
	HasPendingClientTools() bool

	// PendingClientTools returns metadata for each pending client tool.
	PendingClientTools() []PendingClientToolInfo

	// Parts returns the content parts of the response.
	Parts() []types.ContentPart

	// Text returns the text content of the response as a fallback
	// when Parts() is empty.
	Text() string
}

// PendingClientToolInfo describes a client-side tool call awaiting fulfillment.
type PendingClientToolInfo struct {
	CallID     string         `json:"call_id"`
	ToolName   string         `json:"tool_name"`
	Args       map[string]any `json:"args,omitempty"`
	ConsentMsg string         `json:"consent_message,omitempty"`
}

// EventKind discriminates the payload of a StreamEvent.
type EventKind int

const (
	// EventText indicates the event contains text content.
	EventText EventKind = iota

	// EventToolCall indicates a tool call (suppressed by the server).
	EventToolCall

	// EventMedia indicates the event contains media content.
	EventMedia

	// EventDone indicates the stream is complete.
	EventDone

	// EventClientTool indicates a client tool request awaiting fulfillment.
	EventClientTool

	// EventPending indicates the turn is paused on something the server cannot
	// see, most often a human approving a tool call. Text carries the reason.
	// The task moves to input-required instead of completed. A handler that
	// never pauses never emits it.
	EventPending
)

// StreamEvent is a single event on a streaming channel.
type StreamEvent struct {
	// Kind says which of the other fields applies.
	Kind EventKind
	// Text is the content of an EventText event, or the reason of an
	// EventPending event.
	Text string
	// Media is the content of an EventMedia event.
	Media *types.MediaContent
	// ClientTool describes the request of an EventClientTool event.
	ClientTool *PendingClientToolInfo
	// Error, when non-nil, ends the turn and fails the task.
	Error error
}

// Conversation is the non-streaming conversation interface the server uses.
type Conversation interface {
	// Send delivers a message and returns the completed turn. The message is the
	// result of a2a.MessageToMessage, a *types.Message.
	Send(ctx context.Context, message any) (SendResult, error)
	// Close releases the conversation. The server calls it on eviction and on
	// Shutdown.
	Close() error
}

// StreamingConversation extends Conversation with streaming support. A
// conversation that does not implement it is streamed from its Send result.
type StreamingConversation interface {
	Conversation
	// Stream delivers a message and returns the events of the turn. The channel
	// is closed when the turn ends.
	Stream(ctx context.Context, message any) <-chan StreamEvent
}

// ResumableConversation extends Conversation with the ability to submit
// client-side tool results and resume pipeline execution.
type ResumableConversation interface {
	Conversation
	// SendToolResult supplies the result of the client tool call callID.
	SendToolResult(callID string, result any) error
	// RejectClientTool records that the caller declined the client tool call
	// callID, with the caller's reason.
	RejectClientTool(callID string, reason string)
	// Resume continues the turn once the pending tool calls are resolved and
	// returns the completed turn.
	Resume(ctx context.Context) (SendResult, error)
	// ResumeStream is Resume for a streamed turn.
	ResumeStream(ctx context.Context) <-chan StreamEvent
}

// ConversationOpener creates or retrieves a conversation for a given context ID.
// NewServer calls it once per context ID and caches the result.
type ConversationOpener func(contextID string) (Conversation, error)
