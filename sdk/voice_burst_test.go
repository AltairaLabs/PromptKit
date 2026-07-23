package sdk

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/require"
)

// burstSession is a streaming session that mimics a realtime provider on a long
// reply: the first audio input triggers a burst of many audio response chunks,
// and the session stays open (continuous), like OpenAI Realtime.
type burstSession struct {
	providers.BargeInSignal
	burst     int
	resp      chan providers.StreamChunk
	started   atomic.Bool
	done      chan struct{}
	closeOnce sync.Once
}

func newBurstSession(n int) *burstSession {
	return &burstSession{burst: n, resp: make(chan providers.StreamChunk, 8), done: make(chan struct{})}
}

func (s *burstSession) SendChunk(_ context.Context, _ *types.MediaChunk) error {
	if s.started.CompareAndSwap(false, true) {
		go func() {
			defer close(s.resp) // owner closes Response, per the interface contract
			for i := 0; i < s.burst; i++ {
				select {
				case s.resp <- providers.StreamChunk{MediaData: &providers.StreamMediaData{
					Data: []byte{byte(i), byte(i >> 8)}, SampleRate: 24000, Channels: 1,
				}}:
				case <-s.done:
					return
				}
			}
			<-s.done // stay open (continuous) until Close, like a realtime session
		}()
	}
	return nil
}
func (s *burstSession) SendText(_ context.Context, _ string) error          { return nil }
func (s *burstSession) SendSystemContext(_ context.Context, _ string) error { return nil }
func (s *burstSession) Response() <-chan providers.StreamChunk              { return s.resp }
func (s *burstSession) Close() error                                        { s.closeOnce.Do(func() { close(s.done) }); return nil }
func (s *burstSession) Error() error                                        { return nil }
func (s *burstSession) Done() <-chan struct{}                               { return s.done }

// burstProvider is a StreamInputSupport provider whose sessions burst audio.
type burstProvider struct {
	*mock.StreamingProvider
	burst int
}

func (p *burstProvider) CreateStreamSession(
	_ context.Context, _ *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	return newBurstSession(p.burst), nil
}

// TestVoiceStreaming_LongReplyReachesSpeaker reproduces the reported failure
// through the full OpenVoice + Start path: a long assistant reply (a burst of
// audio chunks from a continuous streaming session) must all reach the speaker
// sink, and the session must stay alive.
func TestVoiceStreaming_LongReplyReachesSpeaker(t *testing.T) {
	const burst = 300

	mic := audio.NewMemSource(audio.KindAudio, 4)
	speaker := audio.NewMemSink(audio.KindAudio)
	sess := &fakeAudioSession{sources: []audio.Source{mic}, sinks: []audio.Sink{speaker}}

	prov := &burstProvider{StreamingProvider: mock.NewStreamingProvider("mock", "m", false), burst: burst}
	var observed atomic.Int64
	conv, err := OpenVoice(writeIngestionTestPack(t), "main",
		WithProvider(prov),
		WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{
				Type: types.ContentTypeAudio, SampleRate: 24000, Channels: 1, Encoding: "pcm16", BitDepth: 16,
			},
		}),
		WithSkipSchemaValidation(),
		WithAudioSession(sess),
		WithVoiceObserver(func(providers.StreamChunk) { observed.Add(1) }),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- conv.Start(ctx) }()

	mic.Push(audio.MediaFrame{Kind: audio.KindAudio, Data: []byte{1, 2}, Format: audio.Format{SampleRate: 24000, Channels: 1}})

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && len(speaker.Written()) < burst {
		time.Sleep(20 * time.Millisecond)
	}

	select {
	case err := <-done:
		t.Fatalf("Start returned mid-reply (%v) — the session died before the reply finished", err)
	default:
	}
	require.GreaterOrEqual(t, len(speaker.Written()), burst,
		"only %d of %d reply audio chunks reached the speaker (observer saw %d)", len(speaker.Written()), burst, observed.Load())

	// Closing a live streaming voice conversation must drain promptly, not block
	// on the full 30s drain timeout — the hang users hit on a realtime session.
	closeStart := time.Now()
	require.NoError(t, conv.Close())
	require.Less(t, time.Since(closeStart), 5*time.Second,
		"Close blocked on the drain timeout instead of draining promptly")
}
