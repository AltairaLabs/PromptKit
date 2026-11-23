package stage

import (
	"context"
	"sync"
	"time"

	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Helpers copied from internal/duplex's test files for the duplex tests that
// stay in stage: stage's tests cannot reach another package's test helpers.
// replyAudio is d of 24 kHz mono PCM16 audio.
func replyAudio(d time.Duration) core.StreamElement {
	n := int(d.Seconds()*24000) * 2
	return core.StreamElement{Audio: &core.AudioData{Samples: make([]byte, n), SampleRate: 24000, Channels: 1,
		Format: core.AudioFormatPCM16}}
}

// replyEnd is a completed reply's turn-end.
func replyEnd(text string) core.StreamElement {
	return core.StreamElement{EndOfStream: true, Message: &types.Message{Role: core.RoleAssistant, Content: text,
		FinishReason: "stop", Meta: map[string]interface{}{"finish_reason": "stop"}}}
}

func bargeInElem() core.StreamElement {
	e := core.StreamElement{}
	e.Meta.Interrupted = true
	return e
}

// fakeHoldClock is a manual clock: time moves only on advance, which fires
// the waits it passes.
type fakeHoldClock struct {
	mu      sync.Mutex
	t       time.Time
	waiters []fakeHoldWait
}

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

var holdT0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// baseConfig returns a standard config for testing
func baseConfig() *providers.StreamingInputConfig {
	return &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			ChunkSize:  4096,
			SampleRate: 16000,
			Encoding:   "pcm_linear16",
			Channels:   1,
			BitDepth:   16,
		},
		Metadata: make(map[string]interface{}),
	}
}

// playoutMeta is a released reply's playout metadata.
type playoutMeta struct {
	Finish   string
	Cut      any
	HeardMs  int64
	AudioMs  int64
	Fraction float64
}

func playoutOf(m *types.Message) playoutMeta {
	heardMs, _ := m.Meta[types.MetaHeardMs].(int64)
	audioMs, _ := m.Meta[types.MetaAudioMs].(int64)
	fraction, _ := m.Meta[types.MetaPlayedFraction].(float64)
	return playoutMeta{Finish: m.FinishReason, Cut: m.Meta[types.MetaPlayoutCut], HeardMs: heardMs, AudioMs: audioMs,
		Fraction: fraction}
}

// chunkInjectingProvider wraps *mock.StreamingProvider and injects a fixed
// sequence of response chunks (including input_transcription metadata) into each
// session at CreateStreamSession time. This lets the streaming-materialization
// tests drive an exact StreamChunk sequence without modifying the mock package.
type chunkInjectingProvider struct {
	*providersmock.StreamingProvider
	chunks []providers.StreamChunk
}

func (p *chunkInjectingProvider) CreateStreamSession(
	ctx context.Context,
	req *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	sess, err := p.StreamingProvider.CreateStreamSession(ctx, req)
	if err != nil {
		return nil, err
	}
	if mockSess, ok := sess.(*providersmock.MockStreamSession); ok {
		mockSess.WithResponseChunks(p.chunks)
	}
	return sess, nil
}

type fakeHoldWait struct {
	at time.Time
	c  chan time.Time
}
