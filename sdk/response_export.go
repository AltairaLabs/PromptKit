package sdk

import (
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// ResponseTestOption configures a Response created by NewResponseForTest.
type ResponseTestOption func(*Response)

// WithClientToolsForTest attaches pending client tools to a test response.
func WithClientToolsForTest(tools []PendingClientTool) ResponseTestOption {
	return func(r *Response) {
		r.clientTools = tools
	}
}

// WithPendingToolsForTest attaches approval-held tool calls to a test response.
func WithPendingToolsForTest(tools []PendingTool) ResponseTestOption {
	return func(r *Response) {
		r.pendingTools = tools
	}
}

// WithTurnMessagesForTest sets the messages [Response.TurnMessages] returns.
func WithTurnMessagesForTest(msgs []types.Message) ResponseTestOption {
	return func(r *Response) {
		r.turnMessages = msgs
	}
}

// NewResponseForTest creates a Response for use in tests outside the sdk package.
// This is not intended for production use.
func NewResponseForTest(text string, toolCalls []types.MessageToolCall, opts ...ResponseTestOption) *Response {
	var msg *types.Message
	if text != "" {
		msg = &types.Message{
			Role:    "assistant",
			Content: text,
		}
	} else {
		msg = &types.Message{
			Role: "assistant",
		}
	}

	r := &Response{
		message:   msg,
		toolCalls: toolCalls,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}
