package sdk

import (
	"errors"
	"net/http"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	a2aserver "github.com/AltairaLabs/PromptKit/server/a2a/v2"
)

// A2AOpener returns an A2AConversationOpener backed by SDK conversations.
// Each call to the returned function opens a new conversation with sdk.Open
// using the provided pack path, prompt name, and options. The context ID is not
// passed to the conversation, so each one gets a generated conversation ID
// unless opts include [WithConversationID]. Likewise, a [WithCaller] in opts
// makes every conversation it opens, for whichever remote client, act for
// that one caller; [A2ACallerOpener] opens each for its request's caller.
func A2AOpener(packPath, promptName string, opts ...Option) A2AConversationOpener {
	return func(contextID string) (a2aserver.Conversation, error) {
		conv, err := Open(packPath, promptName, opts...)
		if err != nil {
			return nil, err
		}
		return &streamConvAdapter{convAdapter: convAdapter{c: conv}}, nil
	}
}

// ErrNilCallerOf is returned by [A2ACallerOpener] when callerOf is nil.
var ErrNilCallerOf = errors.New("A2ACallerOpener: callerOf must not be nil")

// A2ACallerOpener is A2AOpener for conversations that act for the caller of
// the request opening them. The A2A protocol carries no caller identity, so
// callerOf reads it from what the host's authentication put on the request
// (its context or headers); the conversation is opened with opts plus
// [WithCaller] of that caller, which overrides a WithCaller in opts. A nil
// callerOf returns [ErrNilCallerOf].
//
// Serve it with [NewA2AServerFor], whose callerID must identify the same
// caller (callerOf's ID, say): the server then refuses a message from
// another caller into a context this opener opened.
func A2ACallerOpener(
	packPath, promptName string, callerOf func(*http.Request) types.Caller, opts ...Option,
) (A2ARequestConversationOpener, error) {
	if callerOf == nil {
		return nil, ErrNilCallerOf
	}
	return func(r *http.Request, contextID string) (a2aserver.Conversation, error) {
		withCaller := append(append([]Option(nil), opts...), WithCaller(callerOf(r)))
		return A2AOpener(packPath, promptName, withCaller...)(contextID)
	}, nil
}
