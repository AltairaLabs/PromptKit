package audio

import (
	"io"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

const (
	// localSinkBufFrames is the channel buffer depth for each direction's
	// pending mono frames. Frames overflow drop silently — the sink trails
	// the run when the audio device can't keep up, but never blocks the
	// pipeline.
	localSinkBufFrames = 10
	// channelStereo is the channel count for the stereo output stream.
	channelStereo = 2
	// bytesPerSample is the byte size of one s16le PCM sample.
	bytesPerSample = 2
	// s16leHighByteShift is the bit shift to extract the high byte of an
	// int16 sample for little-endian PCM encoding.
	s16leHighByteShift = 8
)

// otoContext is the subset of *oto.Context the sink depends on. It exists so
// tests can substitute a fake without needing a real audio device.
type otoContext interface {
	NewPlayer(reader io.Reader) otoPlayer
}

// otoPlayer is the subset of *oto.Player the sink depends on. Note that
// oto/v3 does not expose a working Close — Player resources are reclaimed
// by GC finalizer — so the sink relies on stream EOF to halt playback.
type otoPlayer interface {
	Play()
}

// LocalSinkConfig configures the host-audio playback sink.
type LocalSinkConfig struct {
	// Rate is the canonical sample rate for the stereo output stream.
	// If 0, defaults to Rate24k.
	Rate int
	// newContext is for testing only; nil in production uses the real
	// oto.NewContext constructor.
	newContext func(rate, channels int) (otoContext, error)
}

// LocalSink plays Frame stereo (user L / agent R) on the host audio device
// via oto/v3.
//
// On any oto failure (no audio device, headless Linux without ALSA/Pulse)
// the sink falls back to no-op — the run continues, only local playback is
// missing. SSE / TUI monitoring surfaces remain operational regardless.
//
// Lifecycle: NewLocalSink either opens an oto context and creates a Player
// fed by an internal io.Reader, or returns a noop sink. Push enqueues mono
// frames per direction; the io.Reader interleaves them into stereo s16le on
// demand from oto's audio thread. Close releases the player.
type LocalSink struct {
	rate int
	noop bool

	stream *streamReader
	player otoPlayer

	closeOnce sync.Once
}

// NewLocalSink constructs a sink. On failure to open the audio device,
// returns a no-op sink and a nil error — local playback is best-effort.
func NewLocalSink(cfg LocalSinkConfig) (*LocalSink, error) {
	if cfg.Rate == 0 {
		cfg.Rate = Rate24k
	}

	s := &LocalSink{rate: cfg.Rate}

	newCtx := cfg.newContext
	if newCtx == nil {
		newCtx = realOtoContext
	}

	ctx, err := newCtx(cfg.Rate, channelStereo)
	if err != nil {
		logger.Warn("audio: LocalSink running as no-op (audio device unavailable)", "error", err)
		s.noop = true
		return s, nil
	}

	s.stream = newStreamReader()
	s.player = ctx.NewPlayer(s.stream)
	s.player.Play()

	return s, nil
}

// Push enqueues a frame for playback. Non-blocking; drops on full buffer.
// In no-op mode this is an immediate return.
func (s *LocalSink) Push(frame Frame) {
	if s.noop || s.stream == nil {
		return
	}
	target := s.stream.left
	if frame.Direction == DirectionOutput {
		target = s.stream.right
	}
	select {
	case target <- frame.Samples:
	default:
		// drop — sink running behind audio device consumption
	}
}

// Close signals the stream reader to return EOF. The underlying
// oto.Player is reclaimed by GC finalizer (oto/v3 has no working Close).
// Safe to call multiple times.
func (s *LocalSink) Close() {
	s.closeOnce.Do(func() {
		if s.noop {
			return
		}
		if s.stream != nil {
			s.stream.close()
		}
	})
}

// streamReader is the io.Reader oto/v3 pulls from on its audio thread.
// It composes interleaved stereo s16le from per-direction mono frame
// channels. When no frames are available, it produces silence rather than
// blocking the audio thread (blocking would cause underruns and gaps in
// playback for both channels).
type streamReader struct {
	left  chan []int16
	right chan []int16

	// Held-over samples for whichever channel had a longer/shorter frame
	// than the read buffer requested. Drained before pulling new frames.
	leftHold  []int16
	rightHold []int16

	closed   bool
	closedMu sync.Mutex
}

func newStreamReader() *streamReader {
	return &streamReader{
		left:  make(chan []int16, localSinkBufFrames),
		right: make(chan []int16, localSinkBufFrames),
	}
}

func (r *streamReader) close() {
	r.closedMu.Lock()
	defer r.closedMu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
}

func (r *streamReader) isClosed() bool {
	r.closedMu.Lock()
	defer r.closedMu.Unlock()
	return r.closed
}

// Read implements io.Reader. It produces interleaved stereo s16le into p.
// Layout: [L0_lo, L0_hi, R0_lo, R0_hi, L1_lo, L1_hi, R1_lo, R1_hi, ...].
// When no input or output frames are queued for a channel, that channel
// emits silence rather than blocking, so the player keeps producing audio
// even with one-sided activity.
func (r *streamReader) Read(p []byte) (int, error) {
	if r.isClosed() {
		return 0, io.EOF
	}

	// Round buffer down to whole stereo sample frames (4 bytes = L+R s16le).
	stereoSampleSize := channelStereo * bytesPerSample
	usable := len(p) - (len(p) % stereoSampleSize)
	if usable == 0 {
		return 0, nil
	}

	wantPairs := usable / stereoSampleSize

	left := r.takeSamples(&r.leftHold, r.left, wantPairs)
	right := r.takeSamples(&r.rightHold, r.right, wantPairs)

	// Pad the shorter channel with silence so both align to wantPairs.
	left = padToLen(left, wantPairs)
	right = padToLen(right, wantPairs)

	encodeStereoS16LE(composeStereo(left, right), p[:usable])
	return usable, nil
}

// padToLen returns samples padded with zeros up to want length. If samples
// is already long enough it is returned unchanged.
func padToLen(samples []int16, want int) []int16 {
	if len(samples) >= want {
		return samples
	}
	out := make([]int16, want)
	copy(out, samples)
	return out
}

// encodeStereoS16LE writes interleaved int16 samples into dst as little-endian
// bytes. dst must be at least 2*len(samples) bytes long. Negative samples are
// emitted in two's complement, matching the s16le wire format.
func encodeStereoS16LE(samples []int16, dst []byte) {
	for i, s := range samples {
		// byte() truncates to the low 8 bits; the arithmetic right shift
		// sign-extends but byte() then drops everything except the low 8
		// bits, which yields the high byte of the two's-complement encoding.
		dst[bytesPerSample*i] = byte(s)
		dst[bytesPerSample*i+1] = byte(s >> s16leHighByteShift)
	}
}

// takeSamples drains up to want samples for one direction, preferring the
// hold-over buffer first then non-blocking pulls from the channel. Returns
// fewer than want samples when nothing more is available — the caller pads
// with silence.
func (r *streamReader) takeSamples(hold *[]int16, ch chan []int16, want int) []int16 {
	out := make([]int16, 0, want)

	if len(*hold) > 0 {
		take := len(*hold)
		if take > want {
			take = want
		}
		out = append(out, (*hold)[:take]...)
		*hold = (*hold)[take:]
	}

	for len(out) < want {
		select {
		case frame, ok := <-ch:
			if !ok {
				return out
			}
			need := want - len(out)
			if len(frame) <= need {
				out = append(out, frame...)
			} else {
				out = append(out, frame[:need]...)
				*hold = append(*hold, frame[need:]...)
			}
		default:
			return out
		}
	}

	return out
}

// composeStereo interleaves two mono streams into stereo s16le. nil/empty
// channels become silence at the matching length. The longer of the two
// determines the output length; the shorter is padded with zeros.
func composeStereo(left, right []int16) []int16 {
	n := len(left)
	if len(right) > n {
		n = len(right)
	}
	if n == 0 {
		return nil
	}
	out := make([]int16, channelStereo*n)
	for i := 0; i < n; i++ {
		var l, r int16
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out[channelStereo*i] = l
		out[channelStereo*i+1] = r
	}
	return out
}
