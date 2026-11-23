package duplex

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/cue"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An input element carrying only a cue (a backchannel a mic-side stage asks
// for) goes to the output, not to the session.
func TestDuplexProviderStage_ForwardsCueOnlyInput(t *testing.T) {
	sess := newRecordingSession()
	s := stageWithSession(sess)
	elem := core.StreamElement{Cue: &core.Cue{Kind: testCueBackchannel, Text: "mm-hm", Preemptible: true}}
	out := make(chan core.StreamElement, 4)
	done := make(chan error, 1)

	require.False(t, s.handleInputElement(context.Background(), &elem, out, done))
	require.Len(t, out, 1)
	assert.Equal(t, &core.Cue{Kind: testCueBackchannel, Text: "mm-hm", Preemptible: true}, (<-out).Cue)
	assert.Empty(t, done)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	assert.Empty(t, sess.texts)
	assert.Empty(t, sess.chunks)
}

// When the provider stage creates its session, the cue stage starts
// rendering its guardrails' messages in the session's voice, and the first
// reply does not wait for it.
func TestCueVoice_SessionStartDoesNotDelayTheFirstReply(t *testing.T) {
	pcm := cueRamp(4800)
	svc := newFakeTTS(pcm)
	svc.gate = make(chan struct{})
	provider := providersmock.NewStreamingProvider("t", "m", false).WithAutoRespond("Hello.").
		WithOutputAudio(providers.OutputAudio{Voice: "Verse", SampleRate: 24000, Family: "test-live"})
	duplex := NewDuplexProviderStage(provider, baseConfig())
	duplex.SetHookRegistry(hooks.NewRegistry(hooks.WithProviderHook(blockMessageHook{msg: "Guarded."})))
	cs := newCueStageFor(duplex, cue.Config{TTS: svc, Cache: cueclip.NewMemoryCache(1 << 20)})
	stored := storedTexts(cs)

	text := "hi"
	out, err := runDuplexStage(t, duplex, []core.StreamElement{{Text: &text, EndOfStream: true}})
	require.NoError(t, err)
	var replies []string
	for _, e := range out {
		if e.Message != nil && e.Message.Role == core.RoleAssistant {
			replies = append(replies, e.Message.Content)
		}
	}
	require.NotEmpty(t, replies, "the reply came while the clip was still rendering")
	assert.Equal(t, "Hello.", replies[0])
	select {
	case got := <-stored:
		require.FailNow(t, fmt.Sprintf("the clip for %q rendered before it was released", got))
	default:
	}

	close(svc.gate)
	assert.Equal(t, "Guarded.", waitStored(t, stored))
	assert.Equal(t, "v-2", svc.lastConfig().Voice, "in the session's voice")
	clip, isReady, ok := cue.Held(cue.BankOf(cs), "Guarded.")
	require.True(t, ok)
	assert.True(t, isReady)
	assert.Equal(t, cueFaded(pcm, 24000), clip.Audio.Samples)
}

// Cue audio plays out before a held Message is released, but it is not the
// reply's audio: a reply after a cue reports its own audio only.
func TestCueStage_PlayoutHoldKeepsCueAudioOutOfTheReply(t *testing.T) {
	r := startHold(t)
	cueAudio := replyAudio(500 * time.Millisecond)
	cueAudio.Meta.CueAudio = &core.Cue{Kind: core.CueGuardrailMessage, Text: brunoText}
	r.pass(t, cueAudio, replyAudio(time.Second), replyEnd("A long story."))

	r.clock.advance(900 * time.Millisecond) // the cue, then 400 ms of the reply
	r.pass(t, bargeInElem())
	rel := r.next(t)
	require.True(t, rel.Meta.PlayoutRelease)
	assert.Equal(t, playoutMeta{Finish: "stop", Cut: types.PlayoutCutBargeIn, HeardMs: 400, AudioMs: 1000, Fraction: 0.4},
		playoutOf(rel.Message))
	close(r.in)
	require.NoError(t, <-r.done)
}

// A held Message waits for cue audio ahead of it to play out.
func TestCueStage_PlayoutHoldWaitsForCueAudio(t *testing.T) {
	r := startHold(t)
	cueAudio := replyAudio(500 * time.Millisecond)
	cueAudio.Meta.CueAudio = &core.Cue{Kind: core.CueGuardrailMessage, Text: brunoText}
	got := r.pass(t, replyAudio(100*time.Millisecond), cueAudio, replyEnd("Hi."))
	assert.True(t, got[2].Meta.PlayoutHeld)
	r.clock.advance(550 * time.Millisecond)
	assert.True(t, r.idle(t), "the cue is still playing")
	r.clock.advance(50 * time.Millisecond)
	rel := r.next(t)
	require.True(t, rel.Meta.PlayoutRelease)
	assert.Equal(t, "Hi.", rel.Message.Content)
	close(r.in)
	require.NoError(t, <-r.done)
}
