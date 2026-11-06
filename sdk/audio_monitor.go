package sdk

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	rtaudio "github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	intpipeline "github.com/AltairaLabs/PromptKit/sdk/v2/internal/pipeline"
)

// Audio monitor sample rates.
const (
	audioMonitorRate16k    = 16000
	audioMonitorRate24k    = 24000
	audioMonitorRate48k    = 48000
	audioMonitorBufferSize = 256
	pcm16BytesPerSample    = 2
	pcm16FullScale         = 32768.0
)

// AudioMonitorDirection says whose audio an [AudioMonitorFrame] carries.
type AudioMonitorDirection string

const (
	// AudioMonitorInput is the user's audio, before the provider sees it.
	AudioMonitorInput AudioMonitorDirection = "input"
	// AudioMonitorOutput is the agent's audio, as the pipeline produces it.
	AudioMonitorOutput AudioMonitorDirection = "output"
)

// AudioMonitorFrame is one chunk of monitored audio, delivered on
// [Conversation.AudioMonitor].
type AudioMonitorFrame struct {
	// Direction is whose audio this is.
	Direction AudioMonitorDirection
	// Samples is mono 16-bit PCM at SampleRate, a copy the receiver owns.
	Samples []int16
	// SampleRate is the monitor's Rate.
	SampleRate int
	// Level is the frame's RMS level, from 0 (silence) to 1 (full scale).
	// It is set only when [AudioMonitorOptions].LevelMeter is true.
	Level float64
	// Timestamp is the time the pipeline stamped on the audio element.
	Timestamp time.Time
}

// stdoutIsTerminal reports whether the process is attached to a terminal. A
// variable so tests can pin the auto mode's answer.
var stdoutIsTerminal = func() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// validateAudioMonitorOptions fills Rate's default and rejects an unknown mode
// or rate.
func validateAudioMonitorOptions(opts *AudioMonitorOptions) error {
	switch opts.Mode {
	case "", AudioMonitorAuto, AudioMonitorOn, AudioMonitorOff:
	default:
		return fmt.Errorf("audio monitor: unknown mode %q (want auto, on or off)", opts.Mode)
	}
	switch opts.Rate {
	case 0:
		opts.Rate = audioMonitorRate24k
	case audioMonitorRate16k, audioMonitorRate24k, audioMonitorRate48k:
	default:
		return fmt.Errorf("audio monitor: rate %d must be 16000, 24000 or 48000", opts.Rate)
	}
	return nil
}

// audioMonitorEnabled resolves the mode: on, or auto (the default) with a
// terminal attached.
func audioMonitorEnabled(opts *AudioMonitorOptions) bool {
	if opts == nil {
		return false
	}
	switch opts.Mode {
	case AudioMonitorOn:
		return true
	case AudioMonitorOff:
		return false
	case AudioMonitorAuto, "": // unset means auto
		return stdoutIsTerminal()
	default: // WithAudioMonitor rejects any other mode
		return false
	}
}

// audioMonitor fans tapped pipeline audio out to a buffered channel. Sending
// never blocks the pipeline: a frame that finds the buffer full is dropped.
type audioMonitor struct {
	opts   AudioMonitorOptions
	mu     sync.Mutex
	ch     chan AudioMonitorFrame
	closed bool
}

func newAudioMonitor(opts AudioMonitorOptions) *audioMonitor {
	return &audioMonitor{opts: opts, ch: make(chan AudioMonitorFrame, audioMonitorBufferSize)}
}

// tap is the pipeline's AudioTapFunc.
func (m *audioMonitor) tap(position stage.RecordingPosition, audio *stage.AudioData, ts time.Time) {
	if audio.Format != stage.AudioFormatPCM16 {
		return
	}
	samples, err := pcm16ToMonitorSamples(audio, m.opts.Rate)
	if err != nil || len(samples) == 0 {
		return
	}
	frame := AudioMonitorFrame{
		Direction:  AudioMonitorOutput,
		Samples:    samples,
		SampleRate: m.opts.Rate,
		Timestamp:  ts,
	}
	if position == stage.RecordingPositionInput {
		frame.Direction = AudioMonitorInput
	}
	if m.opts.LevelMeter {
		frame.Level = rmsLevel(samples)
	}
	m.publish(frame)
}

func (m *audioMonitor) publish(frame AudioMonitorFrame) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	select {
	case m.ch <- frame:
	default:
		logger.Debug("audio monitor: consumer behind, frame dropped", "direction", frame.Direction)
	}
}

// close closes the channel; later taps are ignored.
func (m *audioMonitor) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		close(m.ch)
	}
}

// pcm16ToMonitorSamples resamples mono PCM16 bytes to rate and decodes them.
func pcm16ToMonitorSamples(audio *stage.AudioData, rate int) ([]int16, error) {
	data := audio.Samples
	if audio.SampleRate > 0 && audio.SampleRate != rate {
		out, err := rtaudio.ResamplePCM16(data, audio.SampleRate, rate)
		if err != nil {
			return nil, err
		}
		data = out
	}
	samples := make([]int16, len(data)/pcm16BytesPerSample)
	for i := range samples {
		//nolint:gosec // PCM16 decode: the full uint16 range maps onto int16.
		samples[i] = int16(binary.LittleEndian.Uint16(data[pcm16BytesPerSample*i:]))
	}
	return samples, nil
}

// rmsLevel is the RMS of samples, normalized to [0, 1].
func rmsLevel(samples []int16) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, s := range samples {
		v := float64(s) / pcm16FullScale
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(samples)))
}

// audioTap returns the pipeline tap for this conversation's monitor, creating
// the monitor on first use, or nil when monitoring is off.
func (c *Conversation) audioTap() intpipeline.AudioTapFunc {
	if m := c.ensureAudioMonitor(); m != nil {
		return m.tap
	}
	return nil
}

// ensureAudioMonitor creates the monitor when WithAudioMonitor enabled one.
func (c *Conversation) ensureAudioMonitor() *audioMonitor {
	c.audioMonitorOnce.Do(func() {
		if c.config != nil && audioMonitorEnabled(c.config.audioMonitorOpts) {
			c.audioMonitor = newAudioMonitor(*c.config.audioMonitorOpts)
		}
	})
	return c.audioMonitor
}

// AudioMonitor returns the channel that carries the conversation's monitored
// audio when [WithAudioMonitor] turned monitoring on, and nil otherwise. Each
// [AudioMonitorFrame] holds one chunk of the user's audio (before the provider
// sees it) or of the agent's audio (as the pipeline produces it), resampled to
// the monitor's Rate.
//
// Delivery never slows the conversation: the channel is buffered, and a frame
// that finds it full is dropped. Close closes the channel.
func (c *Conversation) AudioMonitor() <-chan AudioMonitorFrame {
	if m := c.ensureAudioMonitor(); m != nil {
		return m.ch
	}
	return nil
}

// closeAudioMonitor closes the monitor channel, if there is one.
func (c *Conversation) closeAudioMonitor() {
	if m := c.ensureAudioMonitor(); m != nil {
		m.close()
	}
}
