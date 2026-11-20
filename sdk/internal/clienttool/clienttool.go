// Package clienttool is the client-tool plumbing the AG-UI adapter
// (sdk/agui) needs from an sdk.Conversation and that is not part of the
// public SDK API: settling an answer now, as the model will be given it.
//
// Package sdk registers the lookup in its init, so the adapter can reach a
// conversation's Settler without the methods being exported on it.
package clienttool

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Settler settles client tool answers on a conversation.
type Settler interface {
	// HasToolResultRewriters reports whether the conversation has a tool hook
	// that rewrites tool results.
	HasToolResultRewriters() bool
	// SettleClientTool answers the deferred client tool call callID and
	// prepares the tool result the model will be given for it; answered is
	// false for a call that already has an answer.
	SettleClientTool(ctx context.Context, callID string, result any, failure error) (
		prepared types.MessageToolResult, answered bool, err error)
	// SettleClientToolRejection is SettleClientTool for a rejection.
	SettleClientToolRejection(ctx context.Context, callID, reason string) (
		prepared types.MessageToolResult, answered bool, err error)
}

// settlerOf is the lookup package sdk registers.
var settlerOf func(conv any) Settler

// Register sets the lookup SettlerOf uses. Package sdk calls it in its init.
func Register(f func(conv any) Settler) { settlerOf = f }

// SettlerOf returns conv's Settler, or nil when conv is not a conversation
// that settles answers.
func SettlerOf(conv any) Settler {
	if settlerOf == nil {
		return nil
	}
	return settlerOf(conv)
}
