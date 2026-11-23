package stage

// Helpers for stage's tests of the cue stage chained with other stages. They
// are copies of internal/cue's own test helpers: a stage test cannot use a
// cue test helper.

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/cue"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const cueTestRate = 24000

const brunoText = "We don't talk about Bruno."

// testCueBackchannel is a preemptible cue kind no source emits yet: a short
// acknowledgment played while the caller speaks, which gives way to reply audio.
const testCueBackchannel CueKind = "backchannel"

// cueFadeOut is how much of a clip's end the cue stage fades to silence.
const cueFadeOut = 10 * time.Millisecond

// pcm24k is the format of a clip a 24 kHz session plays.
var pcm24k = cueclip.Format{SampleRate: 24000, Channels: 1}

// cueRamp is n samples of PCM16 mono that never fall to silence, so no byte of
// it is trimmed and every byte is distinct from a neighbor's.
func cueRamp(n int) []byte {
	b := make([]byte, 2*n)
	for i := range n {
		binary.LittleEndian.PutUint16(b[2*i:], uint16(1000+i%700)) //nolint:gosec // small positive values
	}
	return b
}

// cueFaded is what a cue stage plays of pcm, a clip already at rate with no
// silence at either end: pcm with its last cueFadeOut faded to silence.
func cueFaded(pcm []byte, rate int) []byte {
	n := rate * int(cueFadeOut/time.Millisecond) / 1000
	split := len(pcm) - 2*n
	out := append([]byte(nil), pcm[:split]...)
	return append(out, audio.FadeOutPCM16(pcm[split:], n)...)
}

func pcmClip(pcm []byte, rate int) cue.Clip {
	return cue.Clip{Audio: &AudioData{Samples: pcm, SampleRate: rate, Channels: 1, Format: AudioFormatPCM16}}
}

// guardrailCueElem is the element a provider stage emits for a guardrail's
// cut: the message as text marked safety, carrying the cue.
func guardrailCueElem(text string) StreamElement {
	fr := types.FinishReasonSafety
	e := StreamElement{Text: &text, Cue: &Cue{Kind: CueGuardrailMessage, Text: text}}
	e.Meta.FinishReason = &fr
	return e
}

// processor is the part of a Stage a chain needs.
type processor interface {
	Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error
}

// stageChain runs stages connected in order.
type stageChain struct {
	in   chan StreamElement
	out  chan StreamElement
	errs chan error
	n    int
}

func startChain(t *testing.T, stages ...processor) *stageChain {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c := &stageChain{in: make(chan StreamElement, 64), errs: make(chan error, len(stages)), n: len(stages)}
	in := c.in
	for _, s := range stages {
		out := make(chan StreamElement, 64)
		go func(s processor, in <-chan StreamElement, out chan StreamElement) {
			c.errs <- s.Process(ctx, in, out)
		}(s, in, out)
		in = out
	}
	c.out = in
	return c
}

// next is the chain's next output element.
func (c *stageChain) next(t *testing.T) StreamElement {
	t.Helper()
	select {
	case e, ok := <-c.out:
		require.True(t, ok, "the chain closed its output early")
		return e
	case <-time.After(3 * time.Second):
		require.FailNow(t, "no element came out")
		return StreamElement{}
	}
}

// finish closes the input and returns everything still to come out.
func (c *stageChain) finish(t *testing.T) []StreamElement {
	t.Helper()
	close(c.in)
	var got []StreamElement
	for {
		select {
		case e, ok := <-c.out:
			if !ok {
				for range c.n {
					require.NoError(t, <-c.errs)
				}
				return got
			}
			got = append(got, e)
		case <-time.After(3 * time.Second):
			require.FailNow(t, "the chain never finished")
			return nil
		}
	}
}

// cueAudioOf concatenates the samples of the cue audio elements in elems.
func cueAudioOf(elems []StreamElement) []byte {
	var b []byte
	for _, e := range elems {
		if e.Audio != nil && e.Meta.CueAudio != nil {
			b = append(b, e.Audio.Samples...)
		}
	}
	return b
}

// pendingRender is a render that finishes when released, or fails when its
// context ends.
func pendingRender(clip cue.Clip, release <-chan struct{}) func(context.Context, string) (cue.Clip, error) {
	return func(ctx context.Context, _ string) (cue.Clip, error) {
		select {
		case <-release:
			return clip, nil
		case <-ctx.Done():
			return cue.Clip{}, ctx.Err()
		}
	}
}

// replyAudioOf is a reply audio element carrying pcm at the test rate.
func replyAudioOf(pcm []byte) StreamElement {
	return StreamElement{Audio: &AudioData{Samples: pcm, SampleRate: cueTestRate, Channels: 1,
		Format: AudioFormatPCM16}}
}

// storedTexts records each text the bank stores a rendered clip for.
func storedTexts(s *cue.CueStage) <-chan string {
	ch := make(chan string, 8)
	cue.OnStored(cue.BankOf(s), func(text string, _ cue.Clip) { ch <- text })
	return ch
}

func waitStored(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case text := <-ch:
		return text
	case <-time.After(3 * time.Second):
		require.FailNow(t, "the rendered clip was never stored")
		return ""
	}
}

// fakeTTS is a TTS service that records each synthesis and answers with
// audio(text). With gate set, a synthesis waits for it to close.
type fakeTTS struct {
	voices  []tts.Voice
	formats []tts.AudioFormat
	audio   func(text string) ([]byte, error)
	gate    chan struct{}

	mu      sync.Mutex
	configs []tts.SynthesisConfig
}

func (f *fakeTTS) Name() string                        { return "fake" }
func (f *fakeTTS) SupportedVoices() []tts.Voice        { return f.voices }
func (f *fakeTTS) SupportedFormats() []tts.AudioFormat { return f.formats }

func (f *fakeTTS) Synthesize(ctx context.Context, text string, cfg tts.SynthesisConfig) (io.ReadCloser, error) {
	f.mu.Lock()
	f.configs = append(f.configs, cfg)
	f.mu.Unlock()
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	pcm, err := f.audio(text)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(pcm)), nil
}

func (f *fakeTTS) lastConfig() tts.SynthesisConfig {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.configs[len(f.configs)-1]
}

// newFakeTTS answers every text with pcm, as PCM at 24 kHz.
func newFakeTTS(pcm []byte) *fakeTTS {
	return &fakeTTS{
		voices: []tts.Voice{{ID: "alloy", Name: "Alloy"}, {ID: "v-2", Name: "Verse"}},
		formats: []tts.AudioFormat{tts.FormatMP3,
			{Name: "pcm", MIMEType: "audio/pcm", SampleRate: 24000, BitDepth: 16, Channels: 1}},
		audio: func(string) ([]byte, error) { return pcm, nil },
	}
}

// spokenSession is a session whose replies are spoken in voice at rate.
func spokenSession(voice string, rate int) *providersmock.MockStreamSession {
	return providersmock.NewMockStreamSession().WithOutputAudio(
		providers.OutputAudio{Voice: voice, SampleRate: rate, Family: "test-live"})
}
