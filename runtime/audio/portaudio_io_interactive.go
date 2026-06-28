package audio

// portaudio_io_interactive.go contains the hardware-bound PortAudio I/O core:
// dynamic library loading, the portaudioIO struct and all its methods
// (Start/captureLoop/playLoop/Close). These cannot be unit-tested without a
// real audio device, so they live in an *_interactive.go file to exclude them
// from the per-file coverage threshold.
//
// The testable session/option/source/sink layer lives in portaudio_session.go.

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	// paInt16 is PortAudio's PaSampleFormat for signed 16-bit PCM.
	paInt16 = 0x00000008
	// playbackAccumHeadroom over-allocates the playback accumulation buffer so
	// appends rarely re-grow it (4 output blocks of headroom).
	playbackAccumHeadroom = 4
)

// portAudioLib holds a dynamically-loaded libportaudio and its blocking-API
// entry points. The binary itself does NOT link PortAudio (the build stays pure
// Go, CGO_ENABLED=0); the library is dlopen-ed on demand so the only people who
// need PortAudio installed are those who use voice.
//
// Only the blocking read/write API is bound — no stream callbacks — which keeps
// the FFI surface to plain function calls that purego marshals directly.
type portAudioLib struct {
	handle uintptr

	initialize     func() int32
	terminate      func() int32
	getErrorText   func(int32) string
	getVersionText func() string
	// openDefaultStream mirrors Pa_OpenDefaultStream; callback/userData are 0
	// (blocking mode). format/framesPerBuffer are C `unsigned long` — passed as
	// uint64; values always fit in 32 bits so the low word is correct on Windows.
	openDefaultStream func(
		stream *uintptr, numInput, numOutput int32,
		format uint64, sampleRate float64, framesPerBuffer uint64,
		callback, userData uintptr,
	) int32
	startStream func(uintptr) int32
	stopStream  func(uintptr) int32
	closeStream func(uintptr) int32
	readStream  func(stream, buffer uintptr, frames uint64) int32
	writeStream func(stream, buffer uintptr, frames uint64) int32
}

// loadPortAudio dlopens libportaudio across the platform's usual names/paths and
// binds the blocking API. It returns a helpful, actionable error when the
// library is absent so voice can fail gracefully while the rest of the app keeps
// working.
func loadPortAudio() (*portAudioLib, error) {
	var handle uintptr
	var lastErr error
	for _, name := range portAudioCandidates() {
		h, err := openSharedLib(name)
		if err == nil && h != 0 {
			handle = h
			break
		}
		lastErr = err
	}
	if handle == 0 {
		return nil, fmt.Errorf("%w: %v", errPortAudioMissing, lastErr)
	}

	lib := &portAudioLib{handle: handle}
	purego.RegisterLibFunc(&lib.initialize, handle, "Pa_Initialize")
	purego.RegisterLibFunc(&lib.terminate, handle, "Pa_Terminate")
	purego.RegisterLibFunc(&lib.getErrorText, handle, "Pa_GetErrorText")
	purego.RegisterLibFunc(&lib.getVersionText, handle, "Pa_GetVersionText")
	purego.RegisterLibFunc(&lib.openDefaultStream, handle, "Pa_OpenDefaultStream")
	purego.RegisterLibFunc(&lib.startStream, handle, "Pa_StartStream")
	purego.RegisterLibFunc(&lib.stopStream, handle, "Pa_StopStream")
	purego.RegisterLibFunc(&lib.closeStream, handle, "Pa_CloseStream")
	purego.RegisterLibFunc(&lib.readStream, handle, "Pa_ReadStream")
	purego.RegisterLibFunc(&lib.writeStream, handle, "Pa_WriteStream")
	return lib, nil
}

// errPortAudioMissing is returned (wrapped) when libportaudio cannot be loaded.
var errPortAudioMissing = fmt.Errorf(
	"voice requires PortAudio installed on this machine — "+
		"macOS: `brew install portaudio`; "+
		"Debian/Ubuntu: `sudo apt install libportaudio2`; "+
		"Windows: place portaudio.dll on PATH. See %s",
	voiceDocsURL)

// paError converts a PortAudio return code into a Go error (nil on success).
func (l *portAudioLib) paError(op string, rc int32) error {
	if rc == 0 {
		return nil
	}
	return fmt.Errorf("portaudio %s: %s", op, l.getErrorText(rc))
}

type portaudioIO struct {
	lib *portAudioLib

	captureRate  int // mic sample rate in Hz (e.g. 16000)
	playbackRate int // speaker sample rate in Hz (e.g. 24000)

	mu        sync.Mutex
	wg        sync.WaitGroup
	inStream  uintptr
	outStream uintptr
	inBuf     []int16
	outBuf    []int16
	captureCh chan []byte
	playCh    chan []byte
	flushCh   chan struct{}
	started   bool
	closed    bool
	done      chan struct{}
}

// newAudioIO loads libportaudio at runtime and constructs the PortAudio-backed
// I/O core. It returns errPortAudioMissing (wrapped) when the library is not
// installed, so callers can surface a clear "install PortAudio" message instead
// of crashing. Callers use NewPortAudioSession, which wraps this in the
// Session/Source/Sink interfaces.
//
// Buffer sizes are derived from the configured rates to preserve the target
// windows: capture = rate/captureWindowDivisor (100 ms), playback =
// rate*playbackWindowMs/msPerSecond (40 ms).
func newAudioIO(cfg sessionConfig) (*portaudioIO, error) {
	lib, err := loadPortAudio()
	if err != nil {
		return nil, err
	}
	if err := lib.paError("init", lib.initialize()); err != nil {
		return nil, err
	}
	captureFrames := cfg.captureRate / captureWindowDivisor
	playbackFrames := cfg.playbackRate * playbackWindowMs / msPerSecond
	return &portaudioIO{
		lib:          lib,
		captureRate:  cfg.captureRate,
		playbackRate: cfg.playbackRate,
		inBuf:        make([]int16, captureFrames),
		outBuf:       make([]int16, playbackFrames),
		captureCh:    make(chan []byte, captureChanBuffer),
		playCh:       make(chan []byte, captureChanBuffer),
		flushCh:      make(chan struct{}, 1),
		done:         make(chan struct{}),
	}, nil
}

// Start opens the mic and speaker streams and launches the capture/playback
// goroutines. It is idempotent and safe to call once per session.
func (p *portaudioIO) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errors.New("audio io closed")
	}
	if p.started {
		return nil
	}

	var in, out uintptr
	if err := p.lib.paError("open mic",
		p.lib.openDefaultStream(&in, 1, 0, paInt16, float64(p.captureRate), uint64(len(p.inBuf)), 0, 0)); err != nil {
		return err
	}
	if err := p.lib.paError("open speaker",
		p.lib.openDefaultStream(&out, 0, 1, paInt16, float64(p.playbackRate), uint64(len(p.outBuf)), 0, 0)); err != nil {
		_ = p.lib.closeStream(in)
		return err
	}
	if err := p.lib.paError("start mic", p.lib.startStream(in)); err != nil {
		_ = p.lib.closeStream(in)
		_ = p.lib.closeStream(out)
		return err
	}
	if err := p.lib.paError("start speaker", p.lib.startStream(out)); err != nil {
		_ = p.lib.closeStream(in)
		_ = p.lib.closeStream(out)
		return err
	}
	p.inStream, p.outStream, p.started = in, out, true
	const numLoops = 2 // captureLoop + playLoop
	p.wg.Add(numLoops)
	go p.captureLoop(ctx)
	go p.playLoop(ctx)
	return nil
}

func (p *portaudioIO) captureLoop(ctx context.Context) {
	defer p.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.done:
			return
		default:
		}
		// Pa_ReadStream blocks until inBuf is filled. KeepAlive guards the
		// backing array across the C call (purego passes a raw pointer).
		//nolint:gosec // G103: handing a Go-owned PCM buffer to PortAudio's blocking read; KeepAlive pins it.
		rc := p.lib.readStream(p.inStream, uintptr(unsafe.Pointer(&p.inBuf[0])), uint64(len(p.inBuf)))
		runtime.KeepAlive(p.inBuf)
		if rc != 0 {
			return
		}
		frame := make([]byte, len(p.inBuf)*bytesPerSample)
		for i, s := range p.inBuf {
			//nolint:gosec // G115: deliberate PCM16 bit reinterpretation (int16 → wire bytes).
			binary.LittleEndian.PutUint16(frame[i*bytesPerSample:], uint16(s))
		}
		select {
		case p.captureCh <- frame:
		default: // drop on backpressure — observer cadence
		}
	}
}

//nolint:gocognit // hardware playback loop: ctx/done/flush/write branches kept inline for clarity.
func (p *portaudioIO) playLoop(ctx context.Context) {
	defer p.wg.Done()
	// buffer accumulates incoming PCM bytes across frames so we only write
	// complete outBuf-sized blocks to PortAudio (prevents truncation and
	// stale-sample padding that cause choppy output).
	buffer := make([]byte, 0, len(p.outBuf)*playbackAccumHeadroom)
	blockBytes := len(p.outBuf) * bytesPerSample
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.done:
			return
		case <-p.flushCh:
			// Flush: discard the accumulation buffer so no stale audio plays, then
			// stop+start the output stream to abort PortAudio's internal device buffer.
			// Snapshot the stream handle + lib under a brief lock and release it BEFORE
			// the blocking stop/start FFI calls — holding p.mu across them would stall a
			// concurrent Close() (same pattern Close() uses).
			buffer = buffer[:0]
			p.mu.Lock()
			outStream := p.outStream
			lib := p.lib
			p.mu.Unlock()
			if outStream != 0 {
				_ = lib.stopStream(outStream)
				_ = lib.startStream(outStream)
			}
		case frame, ok := <-p.playCh:
			if !ok {
				return
			}
			buffer = append(buffer, frame...)
			for len(buffer) >= blockBytes {
				for i := 0; i < len(p.outBuf); i++ {
					//nolint:gosec // G115: deliberate PCM16 bit reinterpretation (wire bytes → int16).
					p.outBuf[i] = int16(binary.LittleEndian.Uint16(buffer[i*bytesPerSample:]))
				}
				//nolint:gosec // G103: handing a Go-owned PCM buffer to PortAudio's blocking write; KeepAlive pins it.
				p.lib.writeStream(p.outStream, uintptr(unsafe.Pointer(&p.outBuf[0])), uint64(len(p.outBuf)))
				runtime.KeepAlive(p.outBuf)
				buffer = buffer[blockBytes:]
			}
		}
	}
}

// CaptureChunks returns the channel of captured mic PCM16 frames.
func (p *portaudioIO) CaptureChunks() <-chan []byte { return p.captureCh }

// Play enqueues a PCM16 frame for speaker playback, dropping it on backpressure.
func (p *portaudioIO) Play(frame []byte) {
	select {
	case p.playCh <- frame:
	default:
	}
}

// requestFlush drains queued play frames and signals playLoop to abort its
// in-flight accumulation and restart the output stream. Non-blocking.
func (p *portaudioIO) requestFlush() {
	for {
		select {
		case <-p.playCh:
		default:
			goto signal
		}
	}
signal:
	select {
	case p.flushCh <- struct{}{}:
	default:
	}
}

// Flush cuts playback immediately, dropping queued and in-flight speaker audio.
func (p *portaudioIO) Flush() { p.requestFlush() }

// Close stops the capture/playback goroutines, closes both streams, and
// terminates PortAudio. It is idempotent.
func (p *portaudioIO) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	// Snapshot stream handles before releasing the lock. inStream/outStream are
	// written only during Start() (which also holds mu), so the snapshots are
	// stable for the lifetime of this call.
	inStream := p.inStream
	outStream := p.outStream
	p.mu.Unlock()

	// Signal goroutines to exit, then wait. mu must NOT be held across wg.Wait():
	// playLoop's flush case acquires mu (to guard stopStream/startStream), so
	// holding mu here while waiting for playLoop to exit is a deadlock.
	close(p.done)
	p.wg.Wait()

	// Goroutines have exited; streams are no longer in use.
	if inStream != 0 {
		_ = p.lib.stopStream(inStream)
		_ = p.lib.closeStream(inStream)
	}
	if outStream != 0 {
		_ = p.lib.stopStream(outStream)
		_ = p.lib.closeStream(outStream)
	}
	return p.lib.paError("terminate", p.lib.terminate())
}
