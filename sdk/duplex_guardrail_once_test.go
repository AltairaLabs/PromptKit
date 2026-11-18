package sdk

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A realtime session's turn is cut once and its guardrail message spoken
// once, however many guardrails fire on it, and an input guardrail's verdict
// on the caller's speech never holds up the reply streaming meanwhile. These
// replay the order OpenAI Realtime delivers a blocked turn in: the model's
// reply streams (and an output guardrail cuts it on its text) before the
// caller's transcript arrives, which an input guardrail then blocks too.

// callerSaid is OpenAI Realtime's transcript of the caller's speech, which
// the session calls item.
func callerSaid(text, item string) *providers.StreamChunk {
	return &providers.StreamChunk{Metadata: map[string]interface{}{
		"type": "input_transcription", "transcription": text, "transcription_final": true,
		providers.InputItemIDKey: item,
	}}
}

// inputBruno is an input guardrail banning "Bruno" that says brunoMessage.
func inputBruno() Option {
	return WithGuardrail(guardrails.Input("banned_words", map[string]any{"words": []any{"Bruno"}},
		guardrails.WithMessage(brunoMessage)))
}

// count is how many entries of trace are entry.
func count(trace []string, entry string) int {
	n := 0
	for _, e := range trace {
		if e == entry {
			n++
		}
	}
	return n
}

// The caller's transcript reaches the session while the reply an output
// guardrail cut is still ending: the caller is told to stop once, sees the
// message once and hears it once, in full.
func TestDuplexGuardrailOnce_InputBlockMidCutSpeaksOnce(t *testing.T) {
	speech := newCueTTS(cueRate / 2)
	s := openCueSession(t, writeBrunoPack(t), spokenProvider(), WithTTS(speech), inputBruno(),
		WithCueClipCache(stage.NewMemoryClipCache(1<<20)))
	want := speech.played(brunoMessage)

	r := &cueRead{}
	s.sess.EmitChunk(replyChunk("Sure! "))
	s.sess.EmitChunk(replyChunk("Bruno is"))
	s.sess.EmitChunk(callerSaid("Tell me about Bruno.", "item_A"))
	s.read(t, r, func(r *cueRead) bool { return len(r.cue) >= len(want) })
	s.sess.EmitChunk(endChunk()) // the clip may still be rendering until now: its end follows it
	s.read(t, r, (*cueRead).ended)

	assert.Equal(t, []string{
		"text:Sure! ", "audio",
		"interrupted", "text:" + brunoMessage, "cue-audio:guardrail_message",
		"end:safety",
	}, r.trace)
	assert.Equal(t, want, r.cue, "the message's clip once, in full")
	assert.Equal(t, 1, s.sess.Interrupts(), "the model is asked once to stop the reply")
}

// The caller's transcript arrives after the cut reply ended (OpenAI's
// transcript of a short utterance comes after the reply): the input block cuts
// nothing more, so the message's clip is not cut as it plays.
func TestDuplexGuardrailOnce_LateInputBlockLeavesTheClip(t *testing.T) {
	speech := newCueTTS(cueRate / 2)
	s := openCueSession(t, writeBrunoPack(t), spokenProvider(), WithTTS(speech), inputBruno(),
		WithCueClipCache(stage.NewMemoryClipCache(1<<20)))
	want := speech.played(brunoMessage)

	r := &cueRead{}
	s.sess.EmitChunk(replyChunk("Bruno"))
	s.read(t, r, func(r *cueRead) bool { return len(r.cue) >= len(want) })
	s.sess.EmitChunk(endChunk())
	s.read(t, r, (*cueRead).ended)
	s.sess.EmitChunk(callerSaid("Tell me about Bruno.", "item_A"))
	s.sess.EmitChunk(replyChunk("Hello again"))
	s.sess.EmitChunk(endChunk())
	s.read(t, r, func(r *cueRead) bool { return count(r.trace, "end:stop") == 1 })

	assert.Equal(t, []string{
		"interrupted", "text:" + brunoMessage, "cue-audio:guardrail_message",
		"end:safety",
		"text:Hello again", "audio", "end:stop",
	}, r.trace, "no second cut after the message")
	assert.Equal(t, want, r.cue)
	assert.Equal(t, 1, s.sess.Interrupts())
}

// heldInputHook is an input guardrail that takes as long as the test says (a
// judge model's round trip): each verdict waits for release. It blocks input
// naming word with message.
type heldInputHook struct {
	word, message string
	release       chan struct{}
	mu            sync.Mutex
	judging       int
}

func newHeldInputHook(word, message string) *heldInputHook {
	return &heldInputHook{word: word, message: message, release: make(chan struct{})}
}

func (*heldInputHook) Name() string { return "held_input" }

func (h *heldInputHook) BeforeCall(ctx context.Context, req *hooks.ProviderRequest) hooks.Decision {
	h.mu.Lock()
	h.judging++
	h.mu.Unlock()
	select {
	case <-h.release:
	case <-ctx.Done():
		return hooks.Allow
	}
	var last string
	for _, m := range req.Messages {
		if m.Role == "user" {
			last = m.GetContent()
		}
	}
	if !strings.Contains(last, h.word) {
		return hooks.Allow
	}
	req.Replacement = h.message
	return hooks.Enforced("about "+h.word, map[string]any{"validator_type": "held_input"})
}

func (*heldInputHook) AfterCall(context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse) hooks.Decision {
	return hooks.Allow
}

// started reports whether a verdict is being judged.
func (h *heldInputHook) started() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.judging > 0
}

// The reply keeps streaming while an input guardrail judges the caller's
// transcript, and a verdict that allows it changes nothing.
func TestDuplexGuardrailOnce_ReplyStreamsWhileTheInputIsJudged(t *testing.T) {
	judge := newHeldInputHook("Bruno", brunoMessage)
	s := openCueSession(t, writeIngestionTestPack(t), spokenProvider(), WithProviderHook(judge))

	r := &cueRead{}
	s.sess.EmitChunk(replyChunk("Sure, "))
	s.sess.EmitChunk(callerSaid("Tell me a story.", "item_A"))
	require.Eventually(t, judge.started, 5*time.Second, time.Millisecond)
	s.sess.EmitChunk(replyChunk("once upon a time"))
	s.read(t, r, func(r *cueRead) bool { return count(r.trace, "text:once upon a time") == 1 })
	close(judge.release)
	s.sess.EmitChunk(endChunk())
	s.read(t, r, (*cueRead).ended)

	assert.Equal(t, []string{"text:Sure, ", "audio", "text:once upon a time", "audio", "end:stop"}, r.trace)
}

// A verdict that blocks the caller's turn after the reply went on streaming
// cuts the reply as soon as it is in, once, with the message spoken once.
func TestDuplexGuardrailOnce_SlowInputBlockCutsTheStreamingReply(t *testing.T) {
	judge := newHeldInputHook("Bruno", brunoMessage)
	speech := newCueTTS(cueRate / 2)
	s := openCueSession(t, writeIngestionTestPack(t), spokenProvider(), WithProviderHook(judge),
		WithTTS(speech), WithCueClipCache(stage.NewMemoryClipCache(1<<20)))
	want := speech.played(brunoMessage)

	r := &cueRead{}
	s.sess.EmitChunk(replyChunk("Sure, "))
	s.sess.EmitChunk(callerSaid("Tell me about Bruno.", "item_A"))
	require.Eventually(t, judge.started, 5*time.Second, time.Millisecond)
	s.sess.EmitChunk(replyChunk("he lives"))
	s.read(t, r, func(r *cueRead) bool { return count(r.trace, "text:he lives") == 1 })
	close(judge.release)
	s.read(t, r, func(r *cueRead) bool { return count(r.trace, "interrupted") == 1 }) // before the reply ends
	s.sess.EmitChunk(replyChunk(" in a tower"))                                       // dropped: the reply is cut
	s.sess.EmitChunk(endChunk())
	s.read(t, r, func(r *cueRead) bool { return r.ended() && len(r.cue) >= len(want) })

	// The message is rendered when first cued, so the turn's end (no audio)
	// may go ahead of its clip.
	require.Len(t, r.trace, 8, "%v", r.trace)
	assert.Equal(t, []string{
		"text:Sure, ", "audio", "text:he lives", "audio",
		"interrupted", "text:" + brunoMessage,
	}, r.trace[:6])
	assert.ElementsMatch(t, []string{"cue-audio:guardrail_message", "end:safety"}, r.trace[6:])
	assert.Equal(t, want, r.cue)
	assert.Equal(t, 1, s.sess.Interrupts())

	require.NoError(t, s.conv.Close())
	state, err := s.store.Load(context.Background(), "cue")
	require.NoError(t, err)
	var reply *types.Message
	for i := range state.Messages {
		if state.Messages[i].Role == "assistant" {
			reply = &state.Messages[i]
		}
	}
	require.NotNil(t, reply)
	assert.Equal(t, brunoMessage, reply.Content)
	assert.Equal(t, types.FinishReasonSafety, reply.FinishReason)
}

// Through a paced speaker: a late input block on a turn already cut leaves
// the message's clip to play in full, and the speaker is flushed only by the
// cut itself.
func TestDuplexGuardrailOnce_SpeakerHearsTheWholeMessage(t *testing.T) {
	mic := audio.NewMemSource(audio.KindAudio, 4)
	speaker := &writeRecorder{}
	speech := newCueTTS(cueRate) // a second, played in real time
	prov := spokenProvider()
	conv, err := OpenVoice(writeBrunoPack(t), "main",
		WithProvider(prov), WithSkipSchemaValidation(), WithTTS(speech), inputBruno(),
		WithCueClipCache(stage.NewMemoryClipCache(1<<20)),
		WithStreamingConfig(&providers.StreamingInputConfig{Config: types.StreamingMediaConfig{
			Type: types.ContentTypeAudio, SampleRate: cueRate, Channels: 1, Encoding: "pcm16", BitDepth: 16,
		}}),
		WithAudioSession(&fakeAudioSession{sources: []audio.Source{mic}, sinks: []audio.Sink{speaker}}),
	)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = conv.Start(ctx) }()
	mic.Push(audio.MediaFrame{Kind: audio.KindAudio, Data: []byte{1, 2}, Format: audio.Format{SampleRate: cueRate, Channels: 1}})
	require.Eventually(t, func() bool { return prov.GetSession() != nil }, 5*time.Second, time.Millisecond)
	sess := prov.GetSession()
	clip := speech.played(brunoMessage)

	sess.EmitChunk(replyChunk("Bruno")) // cut at once: none of the reply plays
	sess.EmitChunk(endChunk())
	require.Eventually(t, func() bool { return len(speaker.written()) >= len(clip)/4 }, 5*time.Second,
		5*time.Millisecond, "the clip starts playing")
	sess.EmitChunk(callerSaid("Tell me about Bruno.", "item_A")) // blocked too, late
	require.Eventually(t, func() bool { return len(speaker.written()) >= len(clip) }, 5*time.Second,
		5*time.Millisecond, "the clip plays to its end")

	assert.Equal(t, clip, speaker.written(), "the whole clip, once")
	cancel()
	require.NoError(t, conv.Close())
}
