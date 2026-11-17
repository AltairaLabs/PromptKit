package openai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// spokenHistory is a realtime conversation as the state store holds it: the
// assistant's spoken reply carries its transcript and its audio.
func spokenHistory() []types.Message {
	user, reply, pcm := "where is order 7?", "It has shipped.", "AAAAAAAA"
	return []types.Message{
		{Role: "user", Parts: []types.ContentPart{{Type: types.ContentTypeText, Text: &user}}},
		{Role: "assistant", Content: reply, Parts: []types.ContentPart{
			{Type: types.ContentTypeText, Text: &reply},
			{Type: types.ContentTypeAudio, Media: &types.MediaContent{Data: &pcm, MIMEType: "audio/pcm"}},
		}},
		{Role: "user", Content: "thanks"},
	}
}

// A text model on Chat Completions is sent a spoken reply's transcript, not
// its audio, instead of failing the request.
func TestPredict_SpokenReplyIsSentAsItsTranscript(t *testing.T) {
	body := captureChatBody(t, false, func(p *Provider) {
		p.apiMode = APIModeCompletions
		_, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: spokenHistory()})
		require.NoError(t, err)
	})
	var got struct {
		Messages json.RawMessage `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	assert.JSONEq(t, `[
		{"role":"user","content":[{"type":"text","text":"where is order 7?"}]},
		{"role":"assistant","content":[{"type":"text","text":"It has shipped."}]},
		{"role":"user","content":"thanks"}
	]`, string(got.Messages))
}

// On the Responses API too.
func TestResponses_SpokenReplyIsSentAsItsTranscript(t *testing.T) {
	p := NewProviderWithConfig("responses-spoken", "gpt-6-luna", "https://api.openai.com/v1",
		providers.ProviderDefaults{}, false, nil)
	prepared, err := p.prepareResponsesMessages(context.Background(),
		providers.PredictionRequest{Messages: spokenHistory()})
	require.NoError(t, err)
	body, err := json.Marshal(p.buildResponsesRequest(prepared, nil, "")["input"])
	require.NoError(t, err)
	assert.JSONEq(t, `[
		{"role":"user","content":[{"type":"input_text","text":"where is order 7?"}]},
		{"role":"assistant","content":[{"type":"output_text","text":"It has shipped."}]},
		{"role":"user","content":"thanks"}
	]`, string(body))
}

// The caller's own audio still meets the model's rule: a text model refuses it.
func TestPredict_UserAudioStillNeedsAnAudioModel(t *testing.T) {
	pcm := "AAAAAAAA"
	msgs := []types.Message{{Role: "user", Parts: []types.ContentPart{
		{Type: types.ContentTypeAudio, Media: &types.MediaContent{Data: &pcm, MIMEType: "audio/wav"}}}}}
	captureChatBody(t, false, func(p *Provider) {
		_, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: msgs})
		require.ErrorContains(t, err, "audio content requires audio model with Chat Completions API")
	})
	p := NewProviderWithConfig("responses-spoken", "gpt-6-luna", "https://api.openai.com/v1",
		providers.ProviderDefaults{}, false, nil)
	_, err := p.prepareResponsesMessages(context.Background(), providers.PredictionRequest{Messages: msgs})
	require.ErrorContains(t, err, "audio content requires audio model with Chat Completions API")
}

// Chat Completions takes no audio on an assistant message even for an audio
// model, so a spoken reply goes to one as its transcript too.
func TestPredict_AudioModelIsSentTheSpokenReplyTranscript(t *testing.T) {
	reply, wav := "It has shipped.", "UklGRg=="
	msgs := []types.Message{
		{Role: "user", Content: "where is order 7?"},
		{Role: "assistant", Content: reply, Parts: []types.ContentPart{
			{Type: types.ContentTypeText, Text: &reply},
			{Type: types.ContentTypeAudio, Media: &types.MediaContent{Data: &wav, MIMEType: "audio/wav"}},
		}},
	}
	body := captureChatBody(t, false, func(p *Provider) {
		p.model, p.apiMode = "gpt-audio", APIModeCompletions
		_, err := p.Predict(context.Background(), providers.PredictionRequest{Messages: msgs})
		require.NoError(t, err)
	})
	var got struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	require.Len(t, got.Messages, 2)
	assert.JSONEq(t, `[{"type":"text","text":"It has shipped."}]`, string(got.Messages[1].Content))
}
