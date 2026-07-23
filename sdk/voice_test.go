package sdk

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingSender captures the chunks pushed into the pipeline by the input pump.
type recordingSender struct {
	chunks []*providers.StreamChunk
}

func (r *recordingSender) SendChunk(_ context.Context, c *providers.StreamChunk) error {
	r.chunks = append(r.chunks, c)
	return nil
}

// fakeAudioSession is a hardware-free audio.Session over in-memory sources/sinks.
type fakeAudioSession struct {
	sources []audio.Source
	sinks   []audio.Sink
	started atomic.Bool
}

func (s *fakeAudioSession) Start(ctx context.Context) error {
	s.started.Store(true)
	<-ctx.Done() // mimic a device that runs until the session ends
	return ctx.Err()
}
func (s *fakeAudioSession) Sources() []audio.Source { return s.sources }
func (s *fakeAudioSession) Sinks() []audio.Sink     { return s.sinks }
func (s *fakeAudioSession) Close() error            { return nil }

// TestPumpAudioInput_ForwardsFramesAsChunks: microphone frames become PCM audio
// chunks fed to the pipeline, carrying their format.
func TestPumpAudioInput_ForwardsFramesAsChunks(t *testing.T) {
	src := audio.NewMemSource(audio.KindAudio, 4)
	src.Push(audio.MediaFrame{Kind: audio.KindAudio, Data: []byte{1, 2}, Format: audio.Format{SampleRate: 16000, Channels: 1}})
	src.Push(audio.MediaFrame{Kind: audio.KindAudio, Data: []byte{3, 4}, Format: audio.Format{SampleRate: 16000, Channels: 1}})
	require.NoError(t, src.Close())

	sender := &recordingSender{}
	require.NoError(t, pumpAudioInput(context.Background(), src, sender))

	require.Len(t, sender.chunks, 2)
	assert.Equal(t, []byte{1, 2}, sender.chunks[0].MediaData.Data)
	assert.Equal(t, "audio/pcm", sender.chunks[0].MediaData.MIMEType)
	assert.Equal(t, 16000, sender.chunks[0].MediaData.SampleRate)
	assert.Equal(t, 1, sender.chunks[0].MediaData.Channels)
}

// TestPumpAudioInput_StopsOnContextCancel: a cancelled context ends the pump even
// when the source is still open (no frames), so Start can unwind.
func TestPumpAudioInput_StopsOnContextCancel(t *testing.T) {
	src := audio.NewMemSource(audio.KindAudio, 1) // open, no frames
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, pumpAudioInput(ctx, src, &recordingSender{}), context.Canceled)
}

// TestPumpAudioOutput_WritesAudioToSink: the pipeline's audio responses are
// played to the speaker sink with their format.
func TestPumpAudioOutput_WritesAudioToSink(t *testing.T) {
	sink := audio.NewMemSink(audio.KindAudio)
	ch := make(chan providers.StreamChunk, 4)
	ch <- providers.StreamChunk{MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: []byte{5, 6}, SampleRate: 24000, Channels: 1}}
	ch <- providers.StreamChunk{MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: []byte{7, 8}, SampleRate: 24000, Channels: 1}}
	close(ch)

	require.NoError(t, pumpAudioOutput(context.Background(), ch, []audio.Sink{sink}, nil))

	written := sink.Written()
	require.Len(t, written, 2)
	assert.Equal(t, []byte{5, 6}, written[0].Data)
	assert.Equal(t, 24000, written[0].Format.SampleRate)
	assert.Equal(t, audio.KindAudio, written[0].Kind)
}

// TestPumpAudioOutput_FlushesOnBargeIn: an interrupted chunk drops queued
// playback, so the caller doesn't keep hearing a reply they talked over.
func TestPumpAudioOutput_FlushesOnBargeIn(t *testing.T) {
	sink := audio.NewMemSink(audio.KindAudio)
	ch := make(chan providers.StreamChunk, 4)
	ch <- providers.StreamChunk{MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: []byte{1}}}
	ch <- providers.StreamChunk{Interrupted: true}
	close(ch)

	require.NoError(t, pumpAudioOutput(context.Background(), ch, []audio.Sink{sink}, nil))
	assert.Empty(t, sink.Written(), "queued audio must be flushed on barge-in")
}

// TestPumpAudioOutput_InvokesObserverPerChunk: when a voice observer is set, it
// sees every response chunk (text, transcription, audio) so the app can display
// what's happening while Start manages the speaker.
func TestPumpAudioOutput_InvokesObserverPerChunk(t *testing.T) {
	sink := audio.NewMemSink(audio.KindAudio)
	ch := make(chan providers.StreamChunk, 4)
	ch <- providers.StreamChunk{Delta: "Hello"}                                                                // text only
	ch <- providers.StreamChunk{MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: []byte{9}}} // audio only
	close(ch)

	var seen []providers.StreamChunk
	require.NoError(t, pumpAudioOutput(context.Background(), ch, []audio.Sink{sink},
		func(c providers.StreamChunk) { seen = append(seen, c) }))

	require.Len(t, seen, 2, "observer must see every chunk")
	assert.Equal(t, "Hello", seen[0].Delta)
	require.Len(t, sink.Written(), 1, "audio still reaches the sink")
}

// TestWithVoiceObserver_SetsConfig: the option installs the observer.
func TestWithVoiceObserver_SetsConfig(t *testing.T) {
	c := &config{}
	require.NoError(t, WithVoiceObserver(func(providers.StreamChunk) {})(c))
	assert.NotNil(t, c.voiceObserver)
}

// TestOpenVoice_RequiresAudioSession: OpenVoice without a session is a
// configuration error pointing at WithAudioSession.
func TestOpenVoice_RequiresAudioSession(t *testing.T) {
	_, err := OpenVoice(writeIngestionTestPack(t), "main",
		WithProvider(&recordingTextProvider{}),
		WithSkipSchemaValidation(),
		WithVADMode(&convMockSTTService{}, newConvMockTTSService(), nil),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WithAudioSession")
}

// TestOpenVoice_StartPumpsSessionUntilCancel: with a bound session, OpenVoice
// returns a conversation whose Start runs the session and unwinds cleanly on
// context cancel — the end-to-end orchestration, hardware-free.
func TestOpenVoice_StartPumpsSessionUntilCancel(t *testing.T) {
	sess := &fakeAudioSession{
		sources: []audio.Source{audio.NewMemSource(audio.KindAudio, 1)},
		sinks:   []audio.Sink{audio.NewMemSink(audio.KindAudio)},
	}
	conv, err := OpenVoice(writeIngestionTestPack(t), "main",
		WithProvider(&recordingTextProvider{}),
		WithSkipSchemaValidation(),
		WithVADMode(&convMockSTTService{}, newConvMockTTSService(), nil),
		WithAudioSession(sess),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- conv.Start(ctx) }()

	// Give Start a beat to bring the session up, then stop it.
	assert.Eventually(t, func() bool { return sess.started.Load() }, time.Second, 5*time.Millisecond,
		"Start must bring the audio session up")
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err, "a context-cancel shutdown is clean, not an error")
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}
}
