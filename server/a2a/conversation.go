// Package a2aserver provides a standalone A2A-protocol HTTP server that can be
// backed by any conversation implementation satisfying the interfaces defined
// here. It imports only runtime/ and has no dependency on sdk/.
package a2aserver

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/types"
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
)

// StreamEvent is a single event on a streaming channel.
type StreamEvent struct {
	Kind       EventKind
	Text       string
	Media      *types.MediaContent
	ClientTool *PendingClientToolInfo
	Error      error
}

// Conversation is the non-streaming conversation interface the server uses.
type Conversation interface {
	Send(ctx context.Context, message any) (SendResult, error)
	Close() error
}

// StreamingConversation extends Conversation with streaming support.
type StreamingConversation interface {
	Conversation
	Stream(ctx context.Context, message any) <-chan StreamEvent
}

// ResumableConversation extends Conversation with the ability to submit
// client-side tool results and resume pipeline execution.
type ResumableConversation interface {
	Conversation
	SendToolResult(callID string, result any) error
	RejectClientTool(callID string, reason string)
	Resume(ctx context.Context) (SendResult, error)
	ResumeStream(ctx context.Context) <-chan StreamEvent
}

// ConversationOpener creates or retrieves a conversation for a given context ID.
type ConversationOpener func(contextID string) (Conversation, error)
