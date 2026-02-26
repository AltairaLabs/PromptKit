package sdk

import (
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// NewResponseForTest creates a Response for use in tests outside the sdk package.
// This is not intended for production use.
func NewResponseForTest(text string, toolCalls []types.MessageToolCall) *Response {
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

	return &Response{
		message:   msg,
		toolCalls: toolCalls,
	}
}
