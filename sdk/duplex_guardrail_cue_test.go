package sdk

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/recording"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A guardrail's message in a realtime voice session is spoken by the cue stage
// from a TTS service (WithTTS) or, without one, as a short tone, never by the
// model: it is not model output, so the output guardrails do not cut it, and
// it never enters the model's conversation.

const (
	brunoMessage = "We don't talk about Bruno."
	cueRate      = 24000
)

// cueTTS is a TTS service that renders PCM16 at 24 kHz in voice "alloy": for
// each text a ramp of samples whose length depends on the text, recording each
// synthesis.
type cueTTS struct {
	samples int
	mu      sync.Mutex
	texts   []string
}

func newCueTTS(samples int) *cueTTS { return &cueTTS{samples: samples} }

func (*cueTTS) Name() string { return "cue-mock" }
func (*cueTTS) SupportedVoices() []tts.Voice {
	return []tts.Voice{{ID: "alloy", Name: "Alloy"}}
}
func (*cueTTS) SupportedFormats() []tts.AudioFormat {
	return []tts.AudioFormat{{Name: "pcm", MIMEType: "audio/pcm", SampleRate: cueRate, BitDepth: 16, Channels: 1}}
}

func (m *cueTTS) Synthesize(_ context.Context, text string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
	m.mu.Lock()
	m.texts = append(m.texts, text)
	m.mu.Unlock()
	return io.NopCloser(bytes.NewReader(m.pcm(text))), nil
}

// pcm is the audio the service renders for text: a ramp that never falls to
// silence, so the cue stage trims none of it.
func (m *cueTTS) pcm(text string) []byte {
	n := m.samples + len(text)
	b := make([]byte, 2*n)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(b[2*i:], uint16(1000+i%700)) //nolint:gosec // small positive values
	}
	return b
}

// calls is how many times text was synthesized.
func (m *cueTTS) calls(text string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, t := range m.texts {
		if t == text {
			n++
		}
	}
	return n
}

// played is what the cue stage plays of the service's audio for text: the
// clip with its last 10 ms faded to silence, so it never ends on a click.
func (m *cueTTS) played(text string) []byte {
	pcm := m.pcm(text)
	fade := cueRate / 100
	split := len(pcm) - 2*fade
	return append(append([]byte(nil), pcm[:split]...), audio.FadeOutPCM16(pcm[split:], fade)...)
}

// writeBrunoPack writes a pack whose banned_words validator bans "Bruno" in
// the reply with the message brunoMessage.
func writeBrunoPack(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bruno.pack.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"name": "bruno",
		"version": "v1",
		"prompts": {
			"main": {
				"system_template": "You are a helpful assistant.",
				"validators": [
					{"type": "banned_words", "enabled": true, "message": "We don't talk about Bruno.",
					 "params": {"words": ["Bruno"]}}
				]
			}
		}
	}`), 0o600))
	return path
}

// spokenProvider is the mock realtime provider whose sessions report spoken
// replies in voice "alloy" at 24 kHz.
func spokenProvider() *mock.StreamingProvider {
	return mock.NewStreamingProvider("rt", "rt-model", false).
		WithOutputAudio(providers.OutputAudio{Voice: "alloy", SampleRate: cueRate, Family: "mock-live"})
}

// cueSession is a realtime conversation on a spoken mock session.
type cueSession struct {
	conv  *Conversation
	sess  *mock.MockStreamSession
	ch    <-chan providers.StreamChunk
	store statestore.Store
}

// openCueSession opens a realtime conversation on prov with opts and starts
// its session.
func openCueSession(t *testing.T, packPath string, prov *mock.StreamingProvider, opts ...Option) *cueSession {
	t.Helper()
	store := statestore.NewMemoryStore()
	conv, err := OpenDuplex(packPath, "main", append([]Option{
		WithProvider(prov), WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio),
		WithStateStore(store), WithConversationID("cue"),
	}, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	ch, err := conv.Response()
	require.NoError(t, err)
	require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{
		MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: make([]byte, 320)},
	}))
	require.Eventually(t, func() bool { return prov.GetSession() != nil }, 5*time.Second, time.Millisecond)
	return &cueSession{conv: conv, sess: prov.GetSession(), ch: ch, store: store}
}

// replyChunk is a reply chunk saying text, with replyAudio as its audio.
func replyChunk(text string) *providers.StreamChunk {
	return &providers.StreamChunk{Delta: text, Metadata: map[string]interface{}{"type": "output_transcription"},
		MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: replyAudio, SampleRate: cueRate}}
}

// replyAudio is a reply chunk's audio: 10 ms of a constant level, unlike any
// clip's.
var replyAudio = bytes.Repeat([]byte{0x10, 0x27}, cueRate/100)

func endChunk() *providers.StreamChunk {
	stop := "stop"
	return &providers.StreamChunk{FinishReason: &stop}
}

// cueRead is what the caller read from Response(): the trace, in order, with
// runs of cue audio as one "cue-audio" entry, and the cue audio's bytes.
type cueRead struct {
	trace []string
	cue   []byte
}

// read reads Response() until done reports the read complete.
func (s *cueSession) read(t *testing.T, r *cueRead, done func(*cueRead) bool) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for !done(r) {
		select {
		case c, ok := <-s.ch:
			require.True(t, ok, "the session ended")
			require.NoError(t, c.Error)
			r.add(c)
		case <-timeout:
			require.FailNow(t, "the caller never got it all", "%v", r.trace)
		}
	}
}

func (r *cueRead) add(c providers.StreamChunk) {
	if c.Interrupted {
		r.trace = append(r.trace, "interrupted")
	}
	if c.Delta != "" {
		r.trace = append(r.trace, "text:"+c.Delta)
	}
	if c.MediaData != nil && len(c.MediaData.Data) > 0 {
		if kind, _ := c.Metadata["cue"].(string); kind != "" {
			if n := len(r.trace); n == 0 || r.trace[n-1] != "cue-audio:"+kind {
				r.trace = append(r.trace, "cue-audio:"+kind)
			}
			r.cue = append(r.cue, c.MediaData.Data...)
		} else {
			r.trace = append(r.trace, "audio")
		}
	}
	if done, _ := c.Metadata["assistant_turn_complete"].(bool); done {
		fr, _ := c.Metadata["finish_reason"].(string)
		r.trace = append(r.trace, "end:"+fr)
	}
}

// ended reports whether the trace holds a turn end.
func (r *cueRead) ended() bool {
	for _, e := range r.trace {
		if len(e) > 4 && e[:4] == "end:" {
			return true
		}
	}
	return false
}

// cutBruno has the session's reply say "Let me tell you about Bruno", which
// the guardrail cuts, and reads what the caller gets: everything up to the
// end of the cue's clip of want bytes, then, once the blocked reply ends, its
// turn end.
func (s *cueSession) cutBruno(t *testing.T, want int) *cueRead {
	t.Helper()
	r := &cueRead{}
	s.sess.EmitChunk(replyChunk("Let me tell you about "))
	s.sess.EmitChunk(replyChunk("Bruno"))
	s.read(t, r, func(r *cueRead) bool { return len(r.cue) >= want })
	s.sess.EmitChunk(endChunk())
	s.read(t, r, (*cueRead).ended)
	return r
}

// A guardrail that cuts a spoken reply has its message spoken from the TTS
// service: the caller gets the reply's audio, the cut, the message's text,
// then exactly the service's audio for it, then the turn's end. The message
// is the turn's text, with one failed validation, and no message of its own;
// the model is sent nothing.
func TestDuplexGuardrailCue_CutReplySpeaksTheMessageFromTTS(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	var mu sync.Mutex
	var failed []*events.ValidationEventData
	bus.Subscribe(events.EventValidationFailed, func(e *events.Event) {
		if d, ok := e.Data.(*events.ValidationEventData); ok {
			mu.Lock()
			failed = append(failed, d)
			mu.Unlock()
		}
	})
	speech := newCueTTS(cueRate) // a second of speech
	s := openCueSession(t, writeBrunoPack(t), spokenProvider(),
		WithTTS(speech), WithEventBus(bus), WithCueClipCache(stage.NewMemoryClipCache(1<<20)))

	r := s.cutBruno(t, len(speech.played(brunoMessage)))

	assert.Equal(t, []string{
		"text:Let me tell you about ", "audio",
		"interrupted", "text:" + brunoMessage, "cue-audio:guardrail_message",
		"end:safety",
	}, r.trace)
	assert.Equal(t, speech.played(brunoMessage), r.cue, "the TTS service's audio for the message, in full")
	assert.Empty(t, s.sess.GetTexts(), "the model is asked to say nothing")
	assert.Equal(t, 1, s.sess.Interrupts(), "the model is only asked to stop the blocked reply")

	require.NoError(t, s.conv.Close())
	state, err := s.store.Load(context.Background(), "cue")
	require.NoError(t, err)
	var assistants []types.Message
	for _, m := range state.Messages {
		if m.Role == "assistant" {
			assistants = append(assistants, m)
		}
	}
	require.Len(t, assistants, 1, "no message for the spoken message")
	assert.Equal(t, brunoMessage, assistants[0].Content)
	assert.Equal(t, types.FinishReasonSafety, assistants[0].FinishReason)
	var failedValidations int
	for _, v := range assistants[0].Validations {
		if !v.Passed {
			failedValidations++
		}
	}
	assert.Equal(t, 1, failedValidations, "the output guardrail fired once: the spoken message is not judged")
	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, failed, 1)
}

// Without a TTS service the caller hears the earcon, a short tone, and sees
// the text.
func TestDuplexGuardrailCue_CutReplyWithoutTTSPlaysTheEarcon(t *testing.T) {
	s := openCueSession(t, writeBrunoPack(t), spokenProvider(), WithCueClipCache(stage.NewMemoryClipCache(1<<20)))

	r := s.cutBruno(t, len(stage.EarconPCM(cueRate)))

	assert.Equal(t, []string{
		"text:Let me tell you about ", "audio",
		"interrupted", "text:" + brunoMessage, "cue-audio:guardrail_message",
		"end:safety",
	}, r.trace)
	assert.Equal(t, stage.EarconPCM(cueRate), r.cue)
	assert.Empty(t, s.sess.GetTexts())
}

// A typed turn an input guardrail blocks is spoken too: the caller sees the
// message and hears exactly the service's audio for it, with no cut (no reply
// was playing). The blocked text never reaches the model.
func TestDuplexGuardrailCue_BlockedTypedTurnIsSpoken(t *testing.T) {
	speech := newCueTTS(cueRate / 2)
	s := openCueSession(t, writeIngestionTestPack(t), spokenProvider(), WithTTS(speech),
		WithCueClipCache(stage.NewMemoryClipCache(1<<20)),
		WithGuardrail(guardrails.Input("banned_words", map[string]any{"words": []any{"Bruno"}},
			guardrails.WithMessage(brunoMessage))))

	require.NoError(t, s.conv.SendText(context.Background(), "tell me about Bruno"))
	r := &cueRead{}
	want := speech.played(brunoMessage)
	s.read(t, r, func(r *cueRead) bool { return r.ended() && len(r.cue) >= len(want) })

	assert.Equal(t, want, r.cue)
	assert.NotContains(t, r.trace, "interrupted", "no reply was playing")
	assert.Contains(t, r.trace, "end:safety")
	text := indexOf(r.trace, "text:"+brunoMessage)
	audioAt := indexOf(r.trace, "cue-audio:guardrail_message")
	require.GreaterOrEqual(t, text, 0, "the message is shown: %v", r.trace)
	assert.Greater(t, audioAt, text, "the clip plays after its text: %v", r.trace)
	assert.Empty(t, s.sess.GetTexts(), "the blocked input never reaches the model")
}

func indexOf(list []string, s string) int {
	for i, e := range list {
		if e == s {
			return i
		}
	}
	return -1
}

// Conversations that share a clip cache render each message once: the second
// plays the first's clip without asking the service again.
func TestDuplexGuardrailCue_SharedClipCacheRendersOnce(t *testing.T) {
	speech := newCueTTS(cueRate / 4)
	cache := stage.NewMemoryClipCache(1 << 20)
	want := speech.played(brunoMessage)
	for i := range 2 {
		s := openCueSession(t, writeBrunoPack(t), spokenProvider(), WithTTS(speech), WithCueClipCache(cache))
		r := s.cutBruno(t, len(want))
		assert.Equal(t, want, r.cue, "conversation %d", i+1)
		require.NoError(t, s.conv.Close())
	}
	assert.Equal(t, 1, speech.calls(brunoMessage))
}

// A clip already in the conversation's cache (WithCueClipCache) plays as it is,
// and the TTS service is not asked for it.
func TestDuplexGuardrailCue_ClipCacheIsUsed(t *testing.T) {
	speech := newCueTTS(cueRate / 4)
	cache := stage.NewMemoryClipCache(1 << 20)
	held := bytes.Repeat([]byte{0x34, 0x12}, cueRate/5)
	key := stage.ClipKeyFor(brunoMessage, "alloy", "tts:"+speech.Name(),
		stage.ClipFormat{Encoding: stage.AudioFormatPCM16, SampleRate: cueRate, Channels: 1})
	require.NoError(t, cache.Put(context.Background(), key, stage.CueClip{Audio: &stage.AudioData{
		Samples: held, SampleRate: cueRate, Channels: 1, Format: stage.AudioFormatPCM16,
	}}))
	s := openCueSession(t, writeBrunoPack(t), spokenProvider(), WithTTS(speech), WithCueClipCache(cache))

	r := s.cutBruno(t, len(held))

	assert.Equal(t, held, r.cue)
	assert.Zero(t, speech.calls(brunoMessage))
}

// A recording holds the clip as assistant audio, marked as the cue's, and no
// message for it.
func TestDuplexGuardrailCue_RecordingHoldsTheClip(t *testing.T) {
	evStore, err := events.NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = evStore.Close() })
	speech := newCueTTS(cueRate / 4)
	s := openCueSession(t, writeBrunoPack(t), spokenProvider(), WithTTS(speech),
		WithCueClipCache(stage.NewMemoryClipCache(1<<20)), WithEventStore(evStore), WithRecording(nil))

	want := speech.played(brunoMessage)
	s.cutBruno(t, len(want))
	sessionID := s.conv.ID()
	require.NoError(t, s.conv.Close())

	rec, err := recording.Export(context.Background(), evStore, sessionID)
	require.NoError(t, err)
	evs, err := rec.ToTypedEvents()
	require.NoError(t, err)
	var clip []byte
	var assistants []string
	for _, e := range evs {
		switch d := e.Data.(type) {
		case *events.AudioEventData:
			if d.CueKind == string(stage.CueGuardrailMessage) {
				assert.Equal(t, "cue", d.GeneratedFrom)
				clip = append(clip, d.Payload.InlineData...)
			}
		case *events.MessageCreatedData:
			if d.Role == "assistant" {
				assistants = append(assistants, d.Content)
			}
		}
	}
	assert.Equal(t, want, clip, "the recorded clip is what the caller heard")
	assert.Equal(t, []string{brunoMessage}, assistants, "one assistant message, the blocked turn's")
}

// writeRecorder is a speaker that keeps every frame written to it, and counts
// the flushes (barge-in) instead of dropping what it holds.
type writeRecorder struct {
	mu      sync.Mutex
	data    []byte
	flushes int
}

func (w *writeRecorder) Write(f audio.MediaFrame) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.data = append(w.data, f.Data...)
}
func (w *writeRecorder) Flush()                { w.mu.Lock(); w.flushes++; w.mu.Unlock() }
func (w *writeRecorder) Kind() audio.MediaKind { return audio.KindAudio }
func (w *writeRecorder) Close() error          { return nil }

func (w *writeRecorder) written() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.data...)
}

// A caller who talks over the spoken message cuts it as they would a reply:
// the speaker gets only the start of the clip.
func TestDuplexGuardrailCue_BargeInCutsTheClip(t *testing.T) {
	mic := audio.NewMemSource(audio.KindAudio, 4)
	speaker := &writeRecorder{}
	speech := newCueTTS(3 * cueRate) // three seconds, played in real time
	prov := spokenProvider()
	conv, err := OpenVoice(writeBrunoPack(t), "main",
		WithProvider(prov), WithSkipSchemaValidation(), WithTTS(speech),
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
	require.Eventually(t, func() bool { return len(speaker.written()) >= len(clip)/4 }, 5*time.Second,
		5*time.Millisecond, "the clip starts playing")
	sess.EmitChunk(&providers.StreamChunk{Interrupted: true}) // the caller talks over it
	time.Sleep(time.Second)

	got := speaker.written()
	require.Less(t, len(got), len(clip), "the rest of the clip is not played")
	assert.Equal(t, clip[:len(got)], got, "what played is the start of the clip")
	cancel()
	require.NoError(t, conv.Close())
}
