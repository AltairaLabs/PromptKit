package sdk

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// pcm16Tone returns n mono PCM16 samples all at value v.
func pcm16Tone(n int, v int16) []byte {
	b := make([]byte, n*pcm16BytesPerSample)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(b[i*pcm16BytesPerSample:], uint16(v))
	}
	return b
}

// pinTerminal makes the auto mode see (or not see) a terminal for one test.
func pinTerminal(t *testing.T, attached bool) {
	t.Helper()
	orig := stdoutIsTerminal
	stdoutIsTerminal = func() bool { return attached }
	t.Cleanup(func() { stdoutIsTerminal = orig })
}

// TestWithAudioMonitor_DeliversDuplexInputAudio drives a real duplex
// conversation: audio the caller sends must come out of AudioMonitor,
// resampled to the monitor's rate.
func TestWithAudioMonitor_DeliversDuplexInputAudio(t *testing.T) {
	conv, err := OpenDuplex(writeIngestionTestPack(t), "main",
		WithProvider(mock.NewStreamingProvider("mock", "mock-model", false).WithAutoRespond("hi")),
		WithSkipSchemaValidation(),
		WithAudioMonitor(AudioMonitorOptions{Mode: AudioMonitorOn, Rate: 24000, LevelMeter: true}),
	)
	require.NoError(t, err)
	frames := conv.AudioMonitor()
	require.NotNil(t, frames, "monitoring is on, so AudioMonitor must return a channel")

	require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{
		MediaData: &providers.StreamMediaData{
			Data: pcm16Tone(1600, 16384), MIMEType: "audio/pcm", SampleRate: 16000, Channels: 1,
		},
	}))

	select {
	case frame := <-frames:
		assert.Equal(t, AudioMonitorInput, frame.Direction)
		assert.Equal(t, 24000, frame.SampleRate)
		assert.InDelta(t, 2400, len(frame.Samples), 2, "100ms at 16kHz resampled to 24kHz")
		assert.InDelta(t, 0.5, frame.Level, 0.05, "LevelMeter sets the RMS level")
	case <-time.After(5 * time.Second):
		t.Fatal("no monitored frame for the audio sent")
	}

	require.NoError(t, conv.Close())
	for range frames { //nolint:revive // drain to the close
	}
}

func TestAudioMonitor_ModeResolution(t *testing.T) {
	newConv := func(opts *AudioMonitorOptions) *Conversation {
		return &Conversation{config: &config{audioMonitorOpts: opts}}
	}

	// enabled reports whether the conversation built a monitor channel.
	enabled := func(opts *AudioMonitorOptions) bool {
		return cap(newConv(opts).AudioMonitor()) == audioMonitorBufferSize
	}

	assert.False(t, enabled(nil), "no option, no monitor")
	assert.False(t, enabled(&AudioMonitorOptions{Mode: AudioMonitorOff}))
	assert.True(t, enabled(&AudioMonitorOptions{Mode: AudioMonitorOn, Rate: 16000}))

	pinTerminal(t, false)
	assert.False(t, enabled(&AudioMonitorOptions{Rate: 16000}), "auto without a terminal is off")
	assert.False(t, enabled(&AudioMonitorOptions{Mode: AudioMonitorAuto, Rate: 16000}))
	pinTerminal(t, true)
	assert.True(t, enabled(&AudioMonitorOptions{Rate: 16000}), "auto with a terminal is on")
	assert.False(t, enabled(&AudioMonitorOptions{Mode: AudioMonitorOff}), "off ignores the terminal")
}

func TestAudioMonitor_TapDirectionsLevelAndClose(t *testing.T) {
	conv := &Conversation{config: &config{audioMonitorOpts: &AudioMonitorOptions{Mode: AudioMonitorOn, Rate: 16000}}}
	tap := conv.audioTap()
	require.NotNil(t, tap)
	frames := conv.AudioMonitor()

	ts := time.Unix(100, 0)
	tap(stage.RecordingPositionOutput, &stage.AudioData{Samples: pcm16Tone(160, 8192), SampleRate: 16000}, ts)
	frame := <-frames
	assert.Equal(t, AudioMonitorOutput, frame.Direction)
	assert.Len(t, frame.Samples, 160, "no resample at the same rate")
	assert.Equal(t, int16(8192), frame.Samples[0])
	assert.Zero(t, frame.Level, "Level is set only with LevelMeter")
	assert.Equal(t, ts, frame.Timestamp)

	// Non-PCM audio is not decoded as PCM.
	tap(stage.RecordingPositionInput, &stage.AudioData{Samples: []byte{1, 2, 3, 4}, Format: stage.AudioFormatOpus}, ts)

	conv.closeAudioMonitor()
	tap(stage.RecordingPositionInput, &stage.AudioData{Samples: pcm16Tone(10, 1), SampleRate: 16000}, ts)
	_, open := <-frames
	assert.False(t, open, "close closes the channel and later taps are dropped")
}

func TestAudioMonitor_FullBufferDropsInsteadOfBlocking(t *testing.T) {
	m := newAudioMonitor(AudioMonitorOptions{Mode: AudioMonitorOn, Rate: 16000})
	done := make(chan struct{})
	go func() {
		for i := 0; i < audioMonitorBufferSize+10; i++ {
			m.tap(stage.RecordingPositionInput, &stage.AudioData{Samples: pcm16Tone(4, 1), SampleRate: 16000}, time.Time{})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("tap blocked on a full buffer")
	}
	assert.Len(t, m.ch, audioMonitorBufferSize)
}

func TestWithAudioMonitor_Validation(t *testing.T) {
	cfg := &config{}
	require.NoError(t, WithAudioMonitor(AudioMonitorOptions{Mode: AudioMonitorOn})(cfg))
	assert.Equal(t, 24000, cfg.audioMonitorOpts.Rate, "Rate 0 means 24000")

	assert.Error(t, WithAudioMonitor(AudioMonitorOptions{Rate: 44100})(&config{}))
	assert.Error(t, WithAudioMonitor(AudioMonitorOptions{Mode: "sometimes"})(&config{}))
}
