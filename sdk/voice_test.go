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
// Its Start RETURNS IMMEDIATELY after "starting", exactly as a real device
// session does (the PortAudio helper opens its streams, launches capture and
// playback goroutines, and returns) — rather than blocking until ctx. Modeling
// that is what catches a Start that mistakes the device-start returning for the
// session ending.
type fakeAudioSession struct {
	sources []audio.Source
	sinks   []audio.Sink
	started atomic.Bool
}

func (s *fakeAudioSession) Start(_ context.Context) error {
	s.started.Store(true)
	return nil // device now running in the background; Start does not block
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

// TestOpenVoice_StartStaysAliveUntilCancel is the regression test for the
// device-start lifecycle bug: a real session's Start returns as soon as the
// device is running, and Start must keep pumping until ctx is canceled — not
// tear the session down the instant Start returns. Against the old code (which
// treated the device-start returning as the session ending) Start returned
// almost immediately; here it must stay alive until we cancel.
func TestOpenVoice_StartStaysAliveUntilCancel(t *testing.T) {
	sess := &fakeAudioSession{
		sources: []audio.Source{audio.NewMemSource(audio.KindAudio, 1)}, // open, no frames
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

	require.Eventually(t, func() bool { return sess.started.Load() }, time.Second, 5*time.Millisecond,
		"Start must bring the audio session up")

	// The device has started (started==true) and Start returned nil from the
	// session — Start must NOT have torn down. Prove it is still running.
	select {
	case err := <-done:
		t.Fatalf("Start returned before cancel (%v) — device-start return was mistaken for session end", err)
	case <-time.After(300 * time.Millisecond):
		// still running, as required
	}

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err, "a context-cancel shutdown is clean, not an error")
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}
}

// TestOpenVoice_StartPumpsMicFramesWhileAlive proves the input pump actually runs
// while the session is alive: with a size-1 source buffer, a first frame is
// consumed by the pump (emptying the buffer) so a second Push does not block.
// If Start had torn down, nothing would drain the buffer and the second Push
// would time out.
func TestOpenVoice_StartPumpsMicFramesWhileAlive(t *testing.T) {
	src := audio.NewMemSource(audio.KindAudio, 1)
	sess := &fakeAudioSession{
		sources: []audio.Source{src},
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
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- conv.Start(ctx) }()

	frame := audio.MediaFrame{Kind: audio.KindAudio, Data: []byte{1, 2}, Format: audio.Format{SampleRate: 16000, Channels: 1}}
	src.Push(frame) // fills the size-1 buffer

	pushed := make(chan struct{})
	go func() { src.Push(frame); close(pushed) }() // blocks until the pump drains the first
	select {
	case <-pushed:
		// pump consumed the first frame — the input pump is running
	case <-time.After(2 * time.Second):
		t.Fatal("input pump did not drain the source — Start is not pumping while alive")
	}

	cancel()
	<-done
}
