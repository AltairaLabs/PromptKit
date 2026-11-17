package sdk

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/require"
)

// bargeInSession mimics a realtime provider the caller talks over: on the
// first audio input it sends a long reply's audio in one burst, faster than
// it plays (as OpenAI Realtime and Gemini Live do), then reports the barge-in
// with an Interrupted chunk and stays open.
type bargeInSession struct {
	providers.BargeInSignal
	chunks    int
	resp      chan providers.StreamChunk
	started   atomic.Bool
	done      chan struct{}
	closeOnce sync.Once
}

func (s *bargeInSession) SendChunk(_ context.Context, _ *types.MediaChunk) error {
	if s.started.CompareAndSwap(false, true) {
		go func() {
			defer close(s.resp)
			frame := make([]byte, 960) // 20 ms of 24 kHz PCM16 mono
			out := make([]providers.StreamChunk, 0, s.chunks+1)
			for i := 0; i < s.chunks; i++ {
				out = append(out, providers.StreamChunk{MediaData: &providers.StreamMediaData{
					Data: frame, SampleRate: 24000, Channels: 1,
				}})
			}
			out = append(out, providers.StreamChunk{Interrupted: true})
			for _, c := range out {
				select {
				case s.resp <- c:
				case <-s.done:
					return
				}
			}
			<-s.done
		}()
	}
	return nil
}
func (s *bargeInSession) SendText(_ context.Context, _ string) error          { return nil }
func (s *bargeInSession) SendSystemContext(_ context.Context, _ string) error { return nil }
func (s *bargeInSession) Response() <-chan providers.StreamChunk              { return s.resp }
func (s *bargeInSession) Close() error                                        { s.closeOnce.Do(func() { close(s.done) }); return nil }
func (s *bargeInSession) Error() error                                        { return nil }
func (s *bargeInSession) Done() <-chan struct{}                               { return s.done }

type bargeInProvider struct {
	*mock.StreamingProvider
	chunks int
}

func (p *bargeInProvider) CreateStreamSession(
	_ context.Context, _ *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	return &bargeInSession{
		chunks: p.chunks, resp: make(chan providers.StreamChunk, 8), done: make(chan struct{}),
	}, nil
}

// flushRecordingSink is a speaker that records how many frames it was given
// and when it was first flushed.
type flushRecordingSink struct {
	mu        sync.Mutex
	writes    int
	flushedAt time.Time
}

func (s *flushRecordingSink) Write(audio.MediaFrame) { s.mu.Lock(); s.writes++; s.mu.Unlock() }
func (s *flushRecordingSink) Flush() {
	s.mu.Lock()
	if s.flushedAt.IsZero() {
		s.flushedAt = time.Now()
	}
	s.mu.Unlock()
}
func (s *flushRecordingSink) Kind() audio.MediaKind { return audio.KindAudio }
func (s *flushRecordingSink) Close() error          { return nil }
func (s *flushRecordingSink) state() (int, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes, s.flushedAt
}

// TestVoiceStreaming_BargeInStopsQueuedReply pins barge-in on a paced speaker:
// the model sends 5 s of reply audio at once and then reports the caller
// talked over it. The speaker must be flushed at once, and the reply audio
// still queued for playback must never be written to it. Before the fix the
// interruption waited behind the queued audio and the caller heard the whole
// reply.
func TestVoiceStreaming_BargeInStopsQueuedReply(t *testing.T) {
	const chunks = 250 // 5 s of 20 ms chunks

	mic := audio.NewMemSource(audio.KindAudio, 4)
	speaker := &flushRecordingSink{}
	sess := &fakeAudioSession{sources: []audio.Source{mic}, sinks: []audio.Sink{speaker}}

	prov := &bargeInProvider{StreamingProvider: mock.NewStreamingProvider("mock", "m", false), chunks: chunks}
	conv, err := OpenVoice(writeIngestionTestPack(t), "main",
		WithProvider(prov),
		WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{
				Type: types.ContentTypeAudio, SampleRate: 24000, Channels: 1, Encoding: "pcm16", BitDepth: 16,
			},
		}),
		WithSkipSchemaValidation(),
		WithAudioSession(sess),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = conv.Start(ctx) }()

	start := time.Now()
	mic.Push(audio.MediaFrame{Kind: audio.KindAudio, Data: []byte{1, 2}, Format: audio.Format{SampleRate: 24000, Channels: 1}})

	deadline := start.Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if _, flushed := speaker.state(); !flushed.IsZero() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, flushedAt := speaker.state()
	require.False(t, flushedAt.IsZero(), "the barge-in never flushed the speaker within 4 s (the reply is 5 s long)")
	require.Less(t, flushedAt.Sub(start), 3*time.Second, "the barge-in reached the speaker only after the queued audio played")

	// Nothing of the cut reply is written after the flush.
	writesAtFlush, _ := speaker.state()
	time.Sleep(300 * time.Millisecond)
	writesAfter, _ := speaker.state()
	require.Equal(t, writesAtFlush, writesAfter, "reply audio kept playing after the barge-in")
	require.Less(t, writesAfter, chunks/5, "most of the cut reply was written to the speaker")

	cancel()
	require.NoError(t, conv.Close())
}
