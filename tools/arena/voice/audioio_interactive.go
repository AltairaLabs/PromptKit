//go:build voice

package voice

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/gordonklaus/portaudio"
)

const (
	captureFramesPerBuffer  = 1600 // 100 ms @ 16 kHz
	playbackFramesPerBuffer = 960  // 40 ms @ 24 kHz
	captureChanBuffer       = 32
)

type portaudioIO struct {
	mu        sync.Mutex
	inStream  *portaudio.Stream
	outStream *portaudio.Stream
	inBuf     []int16
	outBuf    []int16
	captureCh chan []byte
	playCh    chan []byte
	started   bool
	closed    bool
	done      chan struct{}
}

// NewAudioIO constructs a PortAudio-backed AudioIO (voice builds only).
func NewAudioIO() (AudioIO, error) {
	if err := portaudio.Initialize(); err != nil {
		return nil, fmt.Errorf("portaudio init: %w", err)
	}
	return &portaudioIO{
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
	in, err := portaudio.OpenDefaultStream(1, 0, float64(CaptureSampleRate), len(p.inBuf), p.inBuf)
	if err != nil {
		return fmt.Errorf("open mic: %w", err)
	}
	out, err := portaudio.OpenDefaultStream(0, 1, float64(PlaybackSampleRate), len(p.outBuf), p.outBuf)
	if err != nil {
		_ = in.Close()
		return fmt.Errorf("open speaker: %w", err)
	}
	if err := in.Start(); err != nil {
		_ = in.Close()
		_ = out.Close()
		return fmt.Errorf("start mic: %w", err)
	}
	if err := out.Start(); err != nil {
		_ = in.Close()
		_ = out.Close()
		return fmt.Errorf("start speaker: %w", err)
	}
	p.inStream, p.outStream, p.started = in, out, true
	go p.captureLoop(ctx)
	go p.playLoop(ctx)
	return nil
}

func (p *portaudioIO) captureLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.done:
			return
		default:
		}
		if err := p.inStream.Read(); err != nil {
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
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.done:
			return
		case frame := <-p.playCh:
			for i := 0; i < len(p.outBuf) && i*2+1 < len(frame); i++ {
				p.outBuf[i] = int16(binary.LittleEndian.Uint16(frame[i*2:]))
			}
			_ = p.outStream.Write()
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

func (p *portaudioIO) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	close(p.done)
	if p.inStream != nil {
		_ = p.inStream.Stop()
		_ = p.inStream.Close()
	}
	if p.outStream != nil {
		_ = p.outStream.Stop()
		_ = p.outStream.Close()
	}
	return portaudio.Terminate()
}
