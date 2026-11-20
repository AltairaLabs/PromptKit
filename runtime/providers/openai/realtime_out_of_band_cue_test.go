package openai

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// sessionProvider is the OpenAI provider with its stream sessions dialed to
// a test server: CreateStreamSession returns session, wrapped as the
// provider wraps its own.
type sessionProvider struct {
	*Provider
	session providers.StreamInputSession
}

func (p *sessionProvider) CreateStreamSession(
	context.Context, *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	return p.session, nil
}

// messageHook is a guardrail that allows everything and blocks with message.
type messageHook struct{ message string }

func (messageHook) Name() string { return "message" }
func (messageHook) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}
func (messageHook) AfterCall(context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse) hooks.Decision {
	return hooks.Allow
}
func (h messageHook) BlockMessages() []string { return []string{h.message} }

// A guardrail's message cued on an OpenAI Realtime session with no TTS
// service configured is spoken in the session's own voice, out of band:
// the cue plays the out-of-band audio, the server is asked for no reply
// (no model-spoken announcement) and no item is added to the conversation,
// and nothing of it comes out of the provider stage.
func TestCueStage_OpenAIRealtimeSpeaksTheMessageOutOfBand(t *testing.T) {
	srv := newOOBServer(t)
	srv.said <- brunoLine
	session := openOOBSession(t, srv)
	provider := &sessionProvider{
		Provider: NewProvider("openai", "gpt-realtime", "", providers.ProviderDefaults{}, false),
		session:  &realtimeSessionBookkeeping{RealtimeSession: session, release: func() {}},
	}
	duplex := stage.NewDuplexProviderStage(provider, &providers.StreamingInputConfig{})
	// A guardrail whose message is brunoLine: the cue stage renders it as
	// soon as the session starts.
	duplex.SetHookRegistry(hooks.NewRegistry(hooks.WithProviderHook(messageHook{brunoLine})))
	cue := stage.NewDuplexCueStage(duplex, stage.CueOptions{Cache: cueclip.NewMemoryCache(1 << 20)})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	in := make(chan stage.StreamElement, 4)
	out := make(chan stage.StreamElement, 64)
	done := make(chan error, 1)
	go func() { done <- duplex.Process(ctx, in, out) }()
	in <- stage.NewAudioElement(&stage.AudioData{Samples: make([]byte, 480), SampleRate: 24000, Channels: 1,
		Format: stage.AudioFormatPCM16})

	// The session starts rendering the message at once, out of band.
	// The caller's audio and the render go out on their own goroutines, in
	// either order.
	sent := map[string]map[string]any{}
	for i := 0; i < 2; i++ {
		ev := srv.await(t)
		sent[ev["type"].(string)] = ev
	}
	require.Contains(t, sent, "input_audio_buffer.append")
	require.Contains(t, sent, realtimeEventResponseCreate)
	assert.Equal(t, "none", sent[realtimeEventResponseCreate]["response"].(map[string]any)["conversation"])

	cueIn := make(chan stage.StreamElement, 1)
	cueOut := make(chan stage.StreamElement, 64)
	text := brunoLine
	cueIn <- stage.StreamElement{Text: &text, Cue: &stage.Cue{Kind: stage.CueGuardrailMessage, Text: text}}
	close(cueIn)
	require.NoError(t, cue.Process(ctx, cueIn, cueOut))
	var played []byte
	for e := range cueOut {
		if e.Audio != nil {
			assert.Equal(t, 24000, e.Audio.SampleRate)
			played = append(played, e.Audio.Samples...)
		}
	}
	assert.Len(t, played, len(oobAudio), "the out-of-band audio, not the earcon")
	assert.Equal(t, oobAudio[len(oobAudio)/2:len(oobAudio)/2+2], played[len(played)/2:len(played)/2+2],
		"the clip is the session's audio")

	srv.quiet(t) // no reply asked for, no item added
	close(in)
	require.NoError(t, session.Close()) // the call ends
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("the provider stage did not end")
	}
	for e := range out {
		assert.Nil(t, e.Audio, "no audio from the provider stage")
		assert.Nil(t, e.Text, "no text from the provider stage")
		assert.Nil(t, e.Message, "no message from the provider stage")
	}
}
