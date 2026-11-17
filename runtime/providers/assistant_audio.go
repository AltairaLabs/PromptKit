package providers

import "github.com/AltairaLabs/PromptKit/runtime/v2/types"

// WithoutAssistantAudio returns msg without its audio parts when msg is an
// assistant message, for a model that cannot take audio: a realtime session
// stores each spoken reply with its audio and its transcript, and the
// transcript is what such a model is sent. Any other message is returned
// unchanged, so user audio meets the provider's own rule. The caller's
// message is not modified. An assistant message left with no parts keeps its
// Content.
func WithoutAssistantAudio(msg types.Message) types.Message {
	if msg.Role != roleAssistant || !hasAudioPart(msg.Parts) {
		return msg
	}
	kept := make([]types.ContentPart, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		if part.Type != types.ContentTypeAudio {
			kept = append(kept, part)
		}
	}
	if len(kept) == 0 {
		kept = nil
	}
	msg.Parts = kept
	return msg
}

// hasAudioPart reports whether any part is audio.
func hasAudioPart(parts []types.ContentPart) bool {
	for i := range parts {
		if parts[i].Type == types.ContentTypeAudio {
			return true
		}
	}
	return false
}
