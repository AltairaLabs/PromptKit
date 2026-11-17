package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func TestWithoutAssistantAudio(t *testing.T) {
	text, pcm := "It has shipped.", "AAAA"
	audio := types.ContentPart{Type: types.ContentTypeAudio, Media: &types.MediaContent{Data: &pcm, MIMEType: "audio/pcm"}}
	spoken := types.Message{Role: "assistant", Content: text,
		Parts: []types.ContentPart{{Type: types.ContentTypeText, Text: &text}, audio}}

	got := WithoutAssistantAudio(spoken)
	assert.Equal(t, []types.ContentPart{{Type: types.ContentTypeText, Text: &text}}, got.Parts)
	assert.Equal(t, text, got.GetContent())
	assert.Len(t, spoken.Parts, 2, "the caller's message is not modified")

	audioOnly := WithoutAssistantAudio(types.Message{Role: "assistant", Content: text,
		Parts: []types.ContentPart{audio}})
	assert.Nil(t, audioOnly.Parts)
	assert.Equal(t, text, audioOnly.GetContent(), "a message left with no parts keeps its Content")

	user := types.Message{Role: "user", Parts: []types.ContentPart{audio}}
	assert.Equal(t, user, WithoutAssistantAudio(user), "user audio is left to the provider's rule")
}
