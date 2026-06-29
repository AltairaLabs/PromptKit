package audio

// portaudio_session.go contains the testable session/option/source/sink layer
// that wraps the hardware-bound portaudioIO (portaudio_io_interactive.go).
// All types and functions here are covered by hardware-free unit tests.

import (
	"context"
	"sync"
)

const (
	// CaptureSampleRate is the default mic capture rate (16 kHz mono PCM16),
	// matching the VAD/STT pipeline default. Aliased to SampleRate16kHz to avoid
	// a duplicate value declaration. Pass WithCaptureRate to override.
	CaptureSampleRate = SampleRate16kHz
	// PlaybackSampleRate is the default speaker playback rate (24 kHz mono
	// PCM16), matching TTS / realtime-provider output. Aliased to SampleRate24kHz.
	// Pass WithPlaybackRate to override.
	PlaybackSampleRate = SampleRate24kHz

	// captureChanBuffer is the channel buffer depth for captured PCM frames.
	captureChanBuffer = 32
	// bytesPerSample is the width of one PCM16 sample (signed 16-bit == 2 bytes).
	bytesPerSample = 2

	// captureWindowDivisor divides the capture rate to get a 100 ms buffer
	// (rate / captureWindowDivisor == samples per 100 ms).
	captureWindowDivisor = 10
	// playbackWindowMs is the playback buffer target window in milliseconds (40 ms).
	playbackWindowMs = 40
	// msPerSecond converts milliseconds to samples for buffer size computation.
	msPerSecond = 1000
)

// sessionConfig holds the configurable parameters for a PortAudio session.
// It is populated from SessionOption values and used by newAudioIO.
type sessionConfig struct {
	captureRate  int  // mic sample rate in Hz
	playbackRate int  // speaker sample rate in Hz
	duplex       bool // use the single 48 kHz duplex stream (opt-in; for same-device open-speaker AEC)
}

// SessionOption is a functional option for NewPortAudioSession.
type SessionOption func(*sessionConfig)

// WithCaptureRate sets the microphone capture sample rate (default 16000 Hz).
// The capture frames-per-buffer is derived as rate/captureWindowDivisor,
// giving a 100 ms window (e.g. 24000/10 = 2400 frames at 24 kHz).
func WithCaptureRate(hz int) SessionOption {
	return func(c *sessionConfig) { c.captureRate = hz }
}

// WithPlaybackRate sets the speaker playback sample rate (default 24000 Hz).
// The playback frames-per-buffer is derived as rate*playbackWindowMs/msPerSecond,
// giving a 40 ms window (e.g. 48000*40/1000 = 1920 frames at 48 kHz).
func WithPlaybackRate(hz int) SessionOption {
	return func(c *sessionConfig) { c.playbackRate = hz }
}

// WithDuplex selects the single 48 kHz duplex stream instead of the default two
// independent mic/speaker streams. The duplex stream gives AEC a shared clock,
// but it only behaves on a single same-device audio path (built-in mic+speakers)
// — on Bluetooth/AirPods or any split mic/speaker setup the clocks differ and
// playback drifts. It is therefore OFF by default and intended only for the
// open-speaker AEC path; everything else uses the robust two-stream path.
func WithDuplex() SessionOption {
	return func(c *sessionConfig) { c.duplex = true }
}

// buildSessionConfig applies opts over the default 16 kHz capture / 24 kHz
// playback configuration and returns the resulting sessionConfig.
func buildSessionConfig(opts []SessionOption) sessionConfig {
	cfg := sessionConfig{
		captureRate:  CaptureSampleRate,
		playbackRate: PlaybackSampleRate,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// ensureTwoStreamBuffers allocates the two-stream (half-duplex) buffers and
// channels if they are nil, sizing them from the configured rates exactly as the
// two-stream branch of newAudioIO does: inBuf = captureRate/captureWindowDivisor
// (100 ms), outBuf = playbackRate*playbackWindowMs/msPerSecond (40 ms). It is
// used both by newAudioIO's two-stream construction and by the duplex→half-duplex
// fallback in startDuplexLocked, so the buffer sizing lives in exactly one place.
func (p *portaudioIO) ensureTwoStreamBuffers() {
	if p.inBuf == nil {
		p.inBuf = make([]int16, p.captureRate/captureWindowDivisor)
	}
	if p.outBuf == nil {
		p.outBuf = make([]int16, p.playbackRate*playbackWindowMs/msPerSecond)
	}
	if p.playCh == nil {
		p.playCh = make(chan []byte, captureChanBuffer)
	}
	if p.flushCh == nil {
		p.flushCh = make(chan struct{}, 1)
	}
}

// portaudioSession adapts the PortAudio-backed portaudioIO to the
// Session/Source/Sink interfaces. It drives a single 48 kHz duplex stream
// (resampling at the STT/TTS seams); the two-stream core is retained on
// portaudioIO for Task 3.4's try-duplex-else-fallback.
type portaudioSession struct {
	io     *portaudioIO
	source *portaudioSource
	sink   *portaudioSink
}

// NewPortAudioSession loads libportaudio and returns a Session exposing one
// audio Source (microphone) and one audio Sink (speaker). By default it drives
// two independent mic/speaker streams (each paced by its own device clock) —
// robust on any device including AirPods/Bluetooth and split mic/speaker setups.
// Pass WithDuplex to use the single 48 kHz duplex stream instead (same-device
// open-speaker AEC only). The Source emits frames at the capture rate (default
// 16 kHz) and the Sink accepts frames at the playback rate (default 24 kHz);
// pass WithCaptureRate / WithPlaybackRate to override. It returns
// errPortAudioMissing (wrapped) when the library is absent.
func NewPortAudioSession(opts ...SessionOption) (Session, error) {
	cfg := buildSessionConfig(opts)
	io, err := newAudioIO(cfg, cfg.duplex)
	if err != nil {
		return nil, err
	}
	s := &portaudioSession{io: io}
	s.source = &portaudioSource{io: io}
	s.sink = &portaudioSink{io: io}
	return s, nil
}

// Start begins media flow on the duplex stream; it delegates to the underlying I/O.
func (s *portaudioSession) Start(ctx context.Context) error { return s.io.Start(ctx) }

// Sources returns the single microphone Source.
func (s *portaudioSession) Sources() []Source { return []Source{s.source} }

// Sinks returns the single speaker Sink.
func (s *portaudioSession) Sinks() []Sink { return []Sink{s.sink} }

// Close stops both streams and terminates PortAudio. It is idempotent.
func (s *portaudioSession) Close() error { return s.io.Close() }

// portaudioSource adapts the mic captureCh ([]byte PCM16) to a stream of
// MediaFrames. The conversion goroutine starts lazily on the first Frames call.
type portaudioSource struct {
	io     *portaudioIO
	once   sync.Once
	frames chan MediaFrame
}

// Frames returns a channel of captured audio MediaFrames. The channel is closed
// when the underlying session closes (io.done). PTS is a best-effort monotonic
// sample counter; the load-bearing duplex clock arrives in Phase 3.
func (s *portaudioSource) Frames() <-chan MediaFrame {
	s.once.Do(func() {
		s.frames = make(chan MediaFrame, captureChanBuffer)
		go s.pump()
	})
	return s.frames
}

func (s *portaudioSource) pump() {
	defer close(s.frames)
	in := s.io.CaptureChunks()
	clk := newSampleClock(s.io.captureRate)
	for {
		select {
		case <-s.io.done:
			return
		case data, ok := <-in:
			if !ok {
				return
			}
			frame := MediaFrame{
				Kind:   KindAudio,
				Data:   data,
				PTS:    clk.pts(),
				Format: Format{SampleRate: s.io.captureRate, Channels: 1},
			}
			// Advance the clock by the samples in this frame (PCM16 = 2 bytes/sample).
			clk.advance(int64(len(data) / bytesPerSample))
			select {
			case s.frames <- frame:
			case <-s.io.done:
				return
			}
		}
	}
}

// Kind reports that this Source produces audio.
func (s *portaudioSource) Kind() MediaKind { return KindAudio }

// Close stops the source by closing the underlying session (idempotent).
func (s *portaudioSource) Close() error { return s.io.Close() }

// portaudioSink adapts the speaker playback path to the Sink interface.
type portaudioSink struct {
	io *portaudioIO
}

// Write enqueues the frame's PCM16 bytes for speaker playback. Callers must
// write frames at the session's configured playback rate (WithPlaybackRate,
// default 24 kHz); f.Format.SampleRate is informational. In duplex mode the
// bytes are resampled from the configured playback rate up to the 48 kHz device
// rate at this seam before entering the jitter buffer.
//
// CADENCE CONTRACT: callers must also write at roughly REAL-TIME cadence. The
// duplex playback jitter buffer holds only ~200 ms (timing jitter), NOT whole
// utterances; an unpaced bulk write longer than that is silently dropped
// oldest-first (the session logs one warning on first overflow). The Arena
// interactive pipeline satisfies this with its audio-pacing-output stage; a
// direct writer (e.g. a future OpenVoice streaming TTS straight to this Sink)
// must pace or chunk its writes to real time.
func (s *portaudioSink) Write(f MediaFrame) { s.io.Play(f.Data) }

// Flush drops all queued and in-flight playback (Phase-1 flush machinery).
func (s *portaudioSink) Flush() { s.io.Flush() }

// Kind reports that this Sink consumes audio.
func (s *portaudioSink) Kind() MediaKind { return KindAudio }

// Close stops the sink by closing the underlying session (idempotent).
func (s *portaudioSink) Close() error { return s.io.Close() }
