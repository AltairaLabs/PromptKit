package sdk

import (
	a2aserver "github.com/AltairaLabs/PromptKit/server/a2a"
)

// A2AOpener returns an A2AConversationOpener backed by SDK conversations.
// Each call to the returned function opens a new conversation for the given
// context ID using sdk.Open with the provided pack path, prompt name, and options.
func A2AOpener(packPath, promptName string, opts ...Option) A2AConversationOpener {
	return func(contextID string) (a2aserver.Conversation, error) {
		conv, err := Open(packPath, promptName, opts...)
		if err != nil {
			return nil, err
		}
		return &streamConvAdapter{convAdapter: convAdapter{c: conv}}, nil
	}
}
