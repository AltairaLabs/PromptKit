package voice

import (
	"context"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
)

// captureChanBuffer bounds the mic forwarding channel. It matches the buffer the
// runtime/audio capture path uses so backpressure behaves identically.
const captureChanBuffer = 32

// sessionAudioIO adapts a runtime/audio Session to the voice.AudioIO interface.
// It bridges the MediaFrame Source/Sink to the []byte capture channel plus the
// Play/Flush/Close shape Arena's Driver and pipeline seam expect.
type sessionAudioIO struct {
	sess   audio.Session
	source audio.Source
	sink   audio.Sink

	mu        sync.Mutex
	captureCh chan []byte
}

func newSessionAudioIO(sess audio.Session) *sessionAudioIO {
	return &sessionAudioIO{
		sess:   sess,
		source: sess.Sources()[0],
		sink:   sess.Sinks()[0],
	}
}

// Start opens the underlying session's devices.
func (s *sessionAudioIO) Start(ctx context.Context) error { return s.sess.Start(ctx) }

// CaptureChunks lazily starts a forwarding goroutine that reads the Source's
// MediaFrames and emits their raw PCM16 bytes. Frames are dropped on backpressure
// (preserving the hardware observer cadence); the channel closes when Frames does.
func (s *sessionAudioIO) CaptureChunks() <-chan []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.captureCh == nil {
		s.captureCh = make(chan []byte, captureChanBuffer)
		go s.pump(s.captureCh)
	}
	return s.captureCh
}

func (s *sessionAudioIO) pump(out chan<- []byte) {
	defer close(out)
	for frame := range s.source.Frames() {
		select {
		case out <- frame.Data:
		default: // drop on backpressure — preserve observer cadence
		}
	}
}

// Play enqueues a PCM16 frame for speaker playback at the playback sample rate.
func (s *sessionAudioIO) Play(frame []byte) {
	s.sink.Write(audio.MediaFrame{
		Kind:   audio.KindAudio,
		Data:   frame,
		Format: audio.Format{SampleRate: PlaybackSampleRate, Channels: 1},
	})
}

// Flush drops all queued and in-flight speaker audio (barge-in).
func (s *sessionAudioIO) Flush() { s.sink.Flush() }

// Close stops the underlying session and releases its resources.
func (s *sessionAudioIO) Close() error { return s.sess.Close() }
