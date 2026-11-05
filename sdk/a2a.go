package sdk

import (
	a2aserver "github.com/AltairaLabs/PromptKit/server/a2a/v2"
)

// A2AOpener returns an A2AConversationOpener backed by SDK conversations.
// Each call to the returned function opens a new conversation with sdk.Open
// using the provided pack path, prompt name, and options. The context ID is not
// passed to the conversation, so each one gets a generated conversation ID
// unless opts include [WithConversationID].
func A2AOpener(packPath, promptName string, opts ...Option) A2AConversationOpener {
	return func(contextID string) (a2aserver.Conversation, error) {
		conv, err := Open(packPath, promptName, opts...)
		if err != nil {
			return nil, err
		}
		return &streamConvAdapter{convAdapter: convAdapter{c: conv}}, nil
	}
}
