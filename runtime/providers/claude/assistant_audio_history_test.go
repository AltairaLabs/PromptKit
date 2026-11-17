package claude

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A spoken reply stored by a realtime session (transcript plus audio) is sent
// to Claude as its transcript, instead of failing the request.
func TestPredict_SpokenReplyIsSentAsItsTranscript(t *testing.T) {
	user, reply, pcm := "where is order 7?", "It has shipped.", "AAAAAAAA"
	msgs := []types.Message{
		{Role: "user", Parts: []types.ContentPart{{Type: types.ContentTypeText, Text: &user}}},
		{Role: "assistant", Content: reply, Parts: []types.ContentPart{
			{Type: types.ContentTypeText, Text: &reply},
			{Type: types.ContentTypeAudio, Media: &types.MediaContent{Data: &pcm, MIMEType: "audio/pcm"}},
		}},
		{Role: "user", Content: "thanks"},
	}
	for _, stream := range []bool{false, true} {
		got := captureMessages(t, stream, providers.ProviderDefaults{}, func(p *ToolProvider) {
			if stream {
				ch, err := p.PredictStream(context.Background(), providers.PredictionRequest{Messages: msgs})
				require.NoError(t, err)
				for range ch {
				}
				return
			}
			_, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: msgs})
			require.NoError(t, err)
		})
		assert.JSONEq(t, `[
			{"role":"user","content":[{"type":"text","text":"where is order 7?"}]},
			{"role":"assistant","content":[{"type":"text","text":"It has shipped."}]},
			{"role":"user","content":[{"type":"text","text":"thanks"}]}
		]`, string(got))
	}
}

// The caller's own audio is still refused.
func TestPredict_UserAudioIsStillRefused(t *testing.T) {
	pcm := "AAAAAAAA"
	msgs := []types.Message{{Role: "user", Parts: []types.ContentPart{
		{Type: types.ContentTypeAudio, Media: &types.MediaContent{Data: &pcm, MIMEType: "audio/wav"}}}}}
	t.Setenv("ANTHROPIC_API_KEY", "k")
	p := NewToolProvider("audio", "claude-sonnet-4-6", "http://127.0.0.1:1", providers.ProviderDefaults{}, false)
	_, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: msgs})
	require.ErrorContains(t, err, "claude does not support audio content")
}
