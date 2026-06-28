package voice

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	captureFramesPerBuffer  = 1600 // 100 ms @ 16 kHz
	playbackFramesPerBuffer = 960  // 40 ms @ 24 kHz
	captureChanBuffer       = 32

	// paInt16 is PortAudio's PaSampleFormat for signed 16-bit PCM.
	paInt16 = 0x00000008
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

	mu        sync.Mutex
	wg        sync.WaitGroup
	inStream  uintptr
	outStream uintptr
	inBuf     []int16
	outBuf    []int16
	captureCh chan []byte
	playCh    chan []byte
	started   bool
	closed    bool
	done      chan struct{}
}

// NewAudioIO loads libportaudio at runtime and constructs a PortAudio-backed
// AudioIO. It returns errPortAudioMissing (wrapped) when the library is not
// installed, so callers can surface a clear "install PortAudio" message instead
// of crashing.
func NewAudioIO() (AudioIO, error) {
	lib, err := loadPortAudio()
	if err != nil {
		return nil, err
	}
	if err := lib.paError("init", lib.initialize()); err != nil {
		return nil, err
	}
	return &portaudioIO{
		lib:       lib,
		inBuf:     make([]int16, captureFramesPerBuffer),
		outBuf:    make([]int16, playbackFramesPerBuffer),
		captureCh: make(chan []byte, captureChanBuffer),
		playCh:    make(chan []byte, captureChanBuffer),
		done:      make(chan struct{}),
	}, nil
}

func (p *portaudioIO) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		return nil
	}

	var in, out uintptr
	if err := p.lib.paError("open mic",
		p.lib.openDefaultStream(&in, 1, 0, paInt16, float64(CaptureSampleRate), uint64(len(p.inBuf)), 0, 0)); err != nil {
		return err
	}
	if err := p.lib.paError("open speaker",
		p.lib.openDefaultStream(&out, 0, 1, paInt16, float64(PlaybackSampleRate), uint64(len(p.outBuf)), 0, 0)); err != nil {
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
	p.wg.Add(2)
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
		frame := make([]byte, len(p.inBuf)*2)
		for i, s := range p.inBuf {
			binary.LittleEndian.PutUint16(frame[i*2:], uint16(s))
		}
		select {
		case p.captureCh <- frame:
		default: // drop on backpressure — observer cadence
		}
	}
}

func (p *portaudioIO) playLoop(ctx context.Context) {
	defer p.wg.Done()
	// buffer accumulates incoming PCM bytes across frames so we only write
	// complete outBuf-sized blocks to PortAudio (prevents truncation and
	// stale-sample padding that cause choppy output).
	buffer := make([]byte, 0, len(p.outBuf)*4)
	blockBytes := len(p.outBuf) * 2 // one int16 == 2 bytes per sample
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.done:
			return
		case frame, ok := <-p.playCh:
			if !ok {
				return
			}
			buffer = append(buffer, frame...)
			for len(buffer) >= blockBytes {
				for i := 0; i < len(p.outBuf); i++ {
					p.outBuf[i] = int16(binary.LittleEndian.Uint16(buffer[i*2:]))
				}
				//nolint:gosec // G103: handing a Go-owned PCM buffer to PortAudio's blocking write; KeepAlive pins it.
				p.lib.writeStream(p.outStream, uintptr(unsafe.Pointer(&p.outBuf[0])), uint64(len(p.outBuf)))
				runtime.KeepAlive(p.outBuf)
				buffer = buffer[blockBytes:]
			}
		}
	}
}

func (p *portaudioIO) CaptureChunks() <-chan []byte { return p.captureCh }

func (p *portaudioIO) Play(frame []byte) {
	select {
	case p.playCh <- frame:
	default:
	}
}

// Flush is completed in #1485 follow-up (stream restart); this minimal drain keeps the interface satisfied.
func (p *portaudioIO) Flush() {
	for {
		select {
		case <-p.playCh:
		default:
			return
		}
	}
}

func (p *portaudioIO) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	close(p.done)
	p.wg.Wait()
	if p.inStream != 0 {
		_ = p.lib.stopStream(p.inStream)
		_ = p.lib.closeStream(p.inStream)
	}
	if p.outStream != 0 {
		_ = p.lib.stopStream(p.outStream)
		_ = p.lib.closeStream(p.outStream)
	}
	return p.lib.paError("terminate", p.lib.terminate())
}
