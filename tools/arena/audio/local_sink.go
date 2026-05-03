package audio

import (
	"context"
	"io"
	"math"
	"os"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

const (
	// drainPollInterval is the cadence at which Close polls the stream's
	// pending-frame queues while waiting for oto to consume them.
	drainPollInterval = 50 * time.Millisecond
	// drainMaxWait caps the total time Close waits for pending audio to
	// drain. Beyond this we accept losing the tail of the run rather than
	// blocking shutdown indefinitely.
	drainMaxWait = 8 * time.Second
	// drainTailGrace is the small extra wait after the queues empty so
	// oto's internal buffer can finish playing the last samples it pulled.
	drainTailGrace = 500 * time.Millisecond
	// localSinkBufFrames is the channel buffer depth for each direction's
	// pending mono frames. Frames overflow drop silently — the sink trails
	// the run when the audio device can't keep up, but never blocks the
	// pipeline. Sized to absorb a multi-second utterance arriving in burst
	// mode (selfplay/scripted user audio is sent as fast as possible to
	// avoid mid-utterance turn detection on real providers).
	localSinkBufFrames = 512
	// channelStereo is the channel count for the stereo output stream.
	channelStereo = 2
	// bytesPerSample is the byte size of one s16le PCM sample.
	bytesPerSample = 2
	// s16leHighByteShift is the bit shift to extract the high byte of an
	// int16 sample for little-endian PCM encoding.
	s16leHighByteShift = 8
	// captureWriterBufFrames is the channel depth between the audio
	// thread and the capture-file writer goroutine. Sized to absorb
	// short disk-write hiccups without blocking; sustained slow disk
	// causes drops in the writer (preferable to underruns at oto).
	captureWriterBufFrames = 256
	// msPerSecond converts a millisecond count to seconds.
	msPerSecond = 1000
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
	// RMSPublisher, if non-nil, is called from the audio thread at ~30 Hz
	// with the RMS levels of the samples just sent to the audio device. This
	// is what drives the level meter — values reflect what is *playing*,
	// not what is queued, so the meter stays in sync with the user's ears.
	RMSPublisher func(RMSFrame)
	// CapturePath, when non-empty, names a file the sink writes every byte
	// oto (or the headless pull goroutine) pulls into. Format: interleaved
	// s16le stereo at Rate. Useful for offline analysis of what the audio
	// device saw without involving the host audio stack.
	CapturePath string
	// Headless skips opening an oto context and instead drives Read from a
	// software ticker at audio rate. Useful for CI / bash invocation and
	// tests where there's no audio device — RMS publishing and capture
	// still work, but nothing actually plays through speakers.
	Headless bool
	// newContext is for testing only; nil in production uses the real
	// oto.NewContext constructor.
	newContext func(rate, channels int) (otoContext, error)
}

// LocalSink plays Frame stereo (user L / agent R) on the host audio device
// via oto/v3.
//
// LocalSink is an OBSERVER, not part of the data path. It subscribes to
// audio coming off the AudioRouter; Push is fire-and-forget and drops on
// full buffer. That drop semantics is load-bearing — see the
// observer-model doc in types.go. The sink cannot be allowed to push
// back on its producer because the same audio is fanned out to the
// duplex provider's session, and provider VAD timing must not be warped
// by who happens to be listening locally (often nobody, in parallel CI).
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

	stream        *streamReader
	player        otoPlayer
	captureWriter *captureWriter

	// Headless mode plumbing — only set when LocalSinkConfig.Headless was true.
	headlessStop chan struct{}
	headlessDone chan struct{}

	closeOnce sync.Once
}

// NewLocalSink constructs a sink. On failure to open the audio device,
// returns a no-op sink and a nil error — local playback is best-effort.
//
// When cfg.Headless is true, oto is bypassed entirely and a ticker
// goroutine drains the streamReader at audio rate. RMS publishing and
// capture still work; nothing actually reaches host speakers. Useful
// for CI / bash invocations where there's no audio device.
func NewLocalSink(cfg LocalSinkConfig) (*LocalSink, error) {
	if cfg.Rate == 0 {
		cfg.Rate = Rate24k
	}

	s := &LocalSink{rate: cfg.Rate}

	if !cfg.Headless {
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
		s.applyConfig(cfg)
		s.player = ctx.NewPlayer(s.stream)
		s.player.Play()
		return s, nil
	}

	// Headless: drive Read ourselves. Pull at the same cadence oto would
	// (~5 ms worth of stereo samples per pull) so capture file timing
	// matches a real-device run.
	s.stream = newStreamReader()
	s.applyConfig(cfg)
	s.headlessStop = make(chan struct{})
	s.headlessDone = make(chan struct{})
	go s.headlessPump()
	logger.Info("audio: LocalSink running headless (no host playback)",
		"rate", cfg.Rate, "capture", cfg.CapturePath)

	return s, nil
}

// applyConfig wires the optional RMS publisher and capture file onto the
// streamReader. Shared between oto and headless construction paths.
func (s *LocalSink) applyConfig(cfg LocalSinkConfig) {
	if cfg.RMSPublisher != nil {
		s.stream.rmsPublisher = cfg.RMSPublisher
		s.stream.rmsSamplesPerEmit = cfg.Rate / rmsCadenceHz
	}
	if cfg.CapturePath != "" {
		f, ferr := os.Create(cfg.CapturePath)
		if ferr != nil {
			logger.Warn("audio: capture disabled (open failed)", "path", cfg.CapturePath, "error", ferr)
			return
		}
		writer := newCaptureWriter(f, captureWriterBufFrames)
		s.stream.capture = writer
		s.captureWriter = writer
		logger.Info("audio: capturing sink output to file",
			"path", cfg.CapturePath, "rate", cfg.Rate, "channels", channelStereo)
	}
}

// headlessPump drives the streamReader at audio rate from a goroutine
// when oto isn't in the loop. Pulls 5 ms worth of stereo samples every
// 5 ms, matching the cadence the real audio device uses.
func (s *LocalSink) headlessPump() {
	defer close(s.headlessDone)
	const pullMs = 5
	pullSamples := s.rate * pullMs / msPerSecond
	pullBytes := pullSamples * channelStereo * bytesPerSample
	buf := make([]byte, pullBytes)
	ticker := time.NewTicker(pullMs * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.headlessStop:
			return
		case <-ticker.C:
			n, err := s.stream.Read(buf)
			_ = n
			if err == io.EOF {
				return
			}
		}
	}
}

// Push enqueues a frame for playback. Non-blocking by contract; drops on
// full buffer. The drop semantics is load-bearing — this sink is an
// observer on a fan-out bus that also feeds the duplex provider's
// session, so blocking here would warp the provider's VAD timing via
// the broadcast. See the observer-model doc in types.go before
// considering any change to this. In no-op mode Push is an immediate
// return.
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
		// drop — sink running behind audio device consumption.
		// Required: see Push doc + types.go observer-model section.
	}
}

// WaitDrained blocks until both per-direction audio queues are empty,
// or until the deadline elapses, then waits a small grace period for
// the host audio device's own output buffer to flush.
//
// LocalSink remains a passive observer — this method exists so a
// controller (the duplex executor's turn loop) can opt-in to "wait
// for local playback to finish before starting the next turn." The
// pipeline's data path doesn't change; nobody pushes back on the
// producer.
//
// No-op when the sink is in noop mode (no audio device).
func (s *LocalSink) WaitDrained(ctx context.Context, timeout time.Duration) {
	if s.noop || s.stream == nil {
		return
	}
	if timeout <= 0 {
		timeout = drainMaxWait
	}
	s.stream.drain(timeout, drainPollInterval)
	// Same grace period Close uses, for the same reason: the per-direction
	// queues may be empty while oto's internal buffer still has the tail.
	select {
	case <-ctx.Done():
	case <-time.After(drainTailGrace):
	}
}

func (s *LocalSink) Close() {
	s.closeOnce.Do(func() {
		if s.noop {
			return
		}
		if s.stream != nil {
			s.stream.drain(drainMaxWait, drainPollInterval)
			time.Sleep(drainTailGrace)
			s.stream.close()
		}
		if s.headlessStop != nil {
			close(s.headlessStop)
			<-s.headlessDone
		}
		if s.captureWriter != nil {
			_ = s.captureWriter.Close()
		}
	})
}

// captureWriter writes audio bytes to a file from a goroutine, isolating
// the audio thread from disk-write latency. The audio thread submits a
// byte slice via Write; the writer goroutine drains a buffered channel
// to disk. On full buffer (slow disk) frames are dropped — losing
// capture data is preferable to causing oto underruns and audible gaps
// in real-time playback.
//
// Same close-race pattern as AudioRouter: Write must not be allowed to
// send into the channel after Close has closed it (sending to a closed
// channel panics, even from a select with default). The closeMu RWMutex
// excludes Write from the channel close, so Write either runs entirely
// before Close (and may successfully send) or entirely after (and
// observes closed=true and drops).
type captureWriter struct {
	file io.WriteCloser
	ch   chan []byte
	done chan struct{}

	closeMu sync.RWMutex
	closed  bool
}

func newCaptureWriter(f io.WriteCloser, bufFrames int) *captureWriter {
	cw := &captureWriter{
		file: f,
		ch:   make(chan []byte, bufFrames),
		done: make(chan struct{}),
	}
	go cw.run()
	return cw
}

func (cw *captureWriter) run() {
	defer close(cw.done)
	for buf := range cw.ch {
		if _, err := cw.file.Write(buf); err != nil {
			logger.Warn("audio capture writer: write failed; dropping further frames",
				"error", err)
			// Drain remaining without writing so producers' sends complete.
			//nolint:revive // intentional drain — body must be empty to discard frames
			for range cw.ch {
			}
			return
		}
	}
}

// Write enqueues a copy of p for the writer goroutine to flush. The copy
// is necessary because the audio thread reuses its scratch buffer
// across reads. Non-blocking; drops on full channel and on post-Close.
func (cw *captureWriter) Write(p []byte) (int, error) {
	cw.closeMu.RLock()
	defer cw.closeMu.RUnlock()
	if cw.closed {
		// Silently drop — Close has run; nothing more to capture.
		return len(p), nil
	}
	cp := make([]byte, len(p))
	copy(cp, p)
	select {
	case cw.ch <- cp:
		return len(p), nil
	default:
		// Drop — disk is too slow. Audio playback must not stall.
		return len(p), nil
	}
}

// Close flushes pending writes and closes the underlying file. Idempotent
// in the sense that LocalSink.Close calls it under sync.Once.
//
// Order matters: take the close write lock (waits for any in-flight
// Write to complete), set closed=true, only then close the channel.
// This prevents send-to-closed-channel panics in racing Writes.
func (cw *captureWriter) Close() error {
	cw.closeMu.Lock()
	cw.closed = true
	close(cw.ch)
	cw.closeMu.Unlock()
	<-cw.done
	return cw.file.Close()
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

	// Playback-latency instrumentation. firstReadObserved* records the
	// wall-clock moment the audio thread first consumed real (non-silence)
	// samples for each direction since the last full silence. Compared
	// with upstream "DuplexProviderStage: forwarding response element"
	// timestamps, this tells us how long after a chunk reaches the
	// pipeline the user actually hears it. activityHigh* track whether
	// we're in an "audio playing" phase so we log once per phase, not
	// once per oto pull.
	activityHighLeft   bool
	activityHighRight  bool
	pulledSamplesLeft  int64
	pulledSamplesRight int64

	// RMS publishing — the audio thread accumulates squared samples per
	// direction as it builds the stereo output, and emits an RMSFrame to
	// rmsPublisher every rmsSamplesPerEmit mono samples. This drives the
	// level meter from playback timing rather than burst-arrival timing.
	rmsPublisher      func(RMSFrame)
	rmsSamplesPerEmit int
	rmsLeftSumSq      float64
	rmsRightSumSq     float64
	rmsCount          int

	// capture, when set, receives every byte that Read produced for the
	// audio device. Lets a developer dump exactly what oto would have
	// played to disk for offline analysis. The Write method is expected
	// to be non-blocking from the audio thread's perspective —
	// captureWriter satisfies that by hopping to a writer goroutine.
	capture io.Writer
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

// drain blocks until both per-direction queues are empty or maxWait elapses.
// It does not guarantee oto has finished playing — callers add a small grace
// period after drain returns to give the audio device its own buffer time.
func (r *streamReader) drain(maxWait, poll time.Duration) {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if len(r.left) == 0 && len(r.right) == 0 {
			return
		}
		time.Sleep(poll)
	}
}

func (r *streamReader) isClosed() bool {
	r.closedMu.Lock()
	defer r.closedMu.Unlock()
	return r.closed
}

// logPlaybackTransition emits a single log line each time a direction
// transitions between "silent" and "playing real samples." The
// transition into "playing" is what we care about for the playback-
// latency question — its timestamp can be diffed against the
// "DuplexProviderStage: forwarding response element hasAudio=true"
// timestamp to compute "lag from chunk-forwarded to chunk-consumed-by-oto."
//
// We also count cumulative real samples per direction so a debugger
// can sanity-check the playback rate after the fact.
func (r *streamReader) logPlaybackTransition(dir Direction, samples []int16) {
	hasReal := !allZeroInt16(samples)
	switch dir {
	case DirectionInput:
		if hasReal {
			r.pulledSamplesLeft += int64(len(samples))
			if !r.activityHighLeft {
				r.activityHighLeft = true
				logger.Debug("LocalSink: started consuming real input samples",
					"direction", string(dir), "samples_in_pull", len(samples))
			}
		} else if r.activityHighLeft {
			r.activityHighLeft = false
			logger.Debug("LocalSink: input went silent",
				"direction", string(dir),
				"cumulative_real_samples", r.pulledSamplesLeft)
		}
	case DirectionOutput:
		if hasReal {
			r.pulledSamplesRight += int64(len(samples))
			if !r.activityHighRight {
				r.activityHighRight = true
				logger.Debug("LocalSink: started consuming real output samples",
					"direction", string(dir), "samples_in_pull", len(samples))
			}
		} else if r.activityHighRight {
			r.activityHighRight = false
			logger.Debug("LocalSink: output went silent",
				"direction", string(dir),
				"cumulative_real_samples", r.pulledSamplesRight)
		}
	}
}

// allZeroInt16 returns true when every sample in s is zero. Padding
// produces zero samples; "real" audio almost never does so this is a
// reliable cheap detector of the silence/playing boundary.
func allZeroInt16(s []int16) bool {
	for _, v := range s {
		if v != 0 {
			return false
		}
	}
	return true
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

	// Playback-latency instrumentation. Log a single line at the
	// start of each "phase" of audio for each direction. Compared
	// with upstream "DuplexProviderStage: forwarding response
	// element hasAudio=true" timestamps, this shows how long after
	// a chunk reaches the pipeline the audio thread actually
	// consumed it (i.e. when oto would be playing it).
	r.logPlaybackTransition(DirectionInput, left)
	r.logPlaybackTransition(DirectionOutput, right)

	// Pad the shorter channel with silence so both align to wantPairs.
	left = padToLen(left, wantPairs)
	right = padToLen(right, wantPairs)

	r.accumulateAndEmitRMS(left, right)

	encodeStereoS16LE(composeStereo(left, right), p[:usable])
	if r.capture != nil {
		// Hand off to capture writer; expected to be non-blocking
		// (captureWriter copies the bytes onto a buffered channel and
		// drops on full so the audio thread can never stall on disk).
		_, _ = r.capture.Write(p[:usable])
	}
	return usable, nil
}

// accumulateAndEmitRMS folds the just-played samples into per-direction
// running RMS state. Whenever rmsSamplesPerEmit samples have been processed
// it publishes one RMSFrame and resets the accumulator. No-op when the
// publisher is nil (e.g. in tests).
func (r *streamReader) accumulateAndEmitRMS(left, right []int16) {
	if r.rmsPublisher == nil || r.rmsSamplesPerEmit <= 0 {
		return
	}
	for i := 0; i < len(left) || i < len(right); i++ {
		if i < len(left) {
			f := float64(left[i]) / int16ScaleFactor
			r.rmsLeftSumSq += f * f
		}
		if i < len(right) {
			f := float64(right[i]) / int16ScaleFactor
			r.rmsRightSumSq += f * f
		}
		r.rmsCount++
		if r.rmsCount >= r.rmsSamplesPerEmit {
			r.flushRMS()
		}
	}
}

func (r *streamReader) flushRMS() {
	count := float64(r.rmsCount)
	leftRMS := math.Sqrt(r.rmsLeftSumSq / count)
	rightRMS := math.Sqrt(r.rmsRightSumSq / count)
	if leftRMS > rmsMaxLevel {
		leftRMS = rmsMaxLevel
	}
	if rightRMS > rmsMaxLevel {
		rightRMS = rmsMaxLevel
	}
	r.rmsPublisher(RMSFrame{
		UserLevel:  float32(leftRMS),
		AgentLevel: float32(rightRMS),
		Timestamp:  time.Now(),
	})
	r.rmsLeftSumSq = 0
	r.rmsRightSumSq = 0
	r.rmsCount = 0
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
