package voice

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
)

// The helpers below are copies of stage's test helpers (cue_voice_test.go,
// playout_hold_test.go, stream_hold_test.go). A test in voice cannot import
// stage's, and stage keeps its own for the tests that stayed there.

// fakeTTS is a TTS service that records each synthesis and answers with
// audio(text).
type fakeTTS struct {
	voices  []tts.Voice
	formats []tts.AudioFormat
	audio   func(text string) ([]byte, error)

	mu      sync.Mutex
	configs []tts.SynthesisConfig
	texts   []string
}

func (f *fakeTTS) Name() string                        { return "fake" }
func (f *fakeTTS) SupportedVoices() []tts.Voice        { return f.voices }
func (f *fakeTTS) SupportedFormats() []tts.AudioFormat { return f.formats }

func (f *fakeTTS) Synthesize(_ context.Context, text string, cfg tts.SynthesisConfig) (io.ReadCloser, error) {
	f.mu.Lock()
	f.configs = append(f.configs, cfg)
	f.texts = append(f.texts, text)
	f.mu.Unlock()
	pcm, err := f.audio(text)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(pcm)), nil
}

var pcmAt24k = tts.AudioFormat{Name: "pcm", MIMEType: "audio/pcm", SampleRate: 24000, BitDepth: 16, Channels: 1}

// newFakeTTS answers every text with pcm, as PCM at 24 kHz.
func newFakeTTS(pcm []byte) *fakeTTS {
	return &fakeTTS{
		voices:  []tts.Voice{{ID: "alloy", Name: "Alloy"}, {ID: "v-2", Name: "Verse"}},
		formats: []tts.AudioFormat{tts.FormatMP3, pcmAt24k},
		audio:   func(string) ([]byte, error) { return pcm, nil },
	}
}

// fakeHoldClock is a manual clock: time moves only on advance, which fires
// the waits it passes.
type fakeHoldClock struct {
	mu      sync.Mutex
	t       time.Time
	waiters []fakeHoldWait
}

type fakeHoldWait struct {
	at time.Time
	c  chan time.Time
}

var holdT0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func (f *fakeHoldClock) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeHoldClock) until(deadline time.Time) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := make(chan time.Time, 1)
	if !deadline.After(f.t) {
		c <- f.t
		return c
	}
	f.waiters = append(f.waiters, fakeHoldWait{at: deadline, c: c})
	return c
}

func (f *fakeHoldClock) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = f.t.Add(d)
	kept := f.waiters[:0]
	for _, w := range f.waiters {
		if w.at.After(f.t) {
			kept = append(kept, w)
			continue
		}
		w.c <- f.t
	}
	f.waiters = kept
}

// holdTestDeadline bounds how long a test waits for a call or a receive that
// must not block, so a stage that hangs fails here, not at the test binary's
// timeout.
const holdTestDeadline = 5 * time.Second

// within returns what f returns, failing the test when f has not returned by
// holdTestDeadline. f runs on its own goroutine, so a call that spins or
// blocks forever still fails the test.
func within[T any](t *testing.T, what string, f func() T) T {
	t.Helper()
	done := make(chan T, 1)
	go func() { done <- f() }()
	select {
	case v := <-done:
		return v
	case <-time.After(holdTestDeadline):
		t.Fatalf("%s did not return within %v", what, holdTestDeadline)
		var zero T
		return zero
	}
}

// recvWithin receives from ch, failing the test when nothing arrives by
// holdTestDeadline.
func recvWithin[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	return within(t, "a receive", func() T { return <-ch })
}
