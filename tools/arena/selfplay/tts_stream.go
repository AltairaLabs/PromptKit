package selfplay

import (
	"io"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// selfplayChunkSize is the read buffer size for selfplay TTS streams.
const selfplayChunkSize = 4096

// selfplayStreamBufSize is the channel buffer depth for the mockTTSStream
// goroutine. Sized so the producer can stay a few chunks ahead of the consumer
// without blocking on a full channel.
const selfplayStreamBufSize = 16

// mockTTSStream implements base.TTSStream for the selfplay mock TTS service.
// Cost is always nil — the mock provider has no billing.
type mockTTSStream struct {
	ch     <-chan audio.Chunk
	closed bool
	mu     sync.Mutex
}

// newMockTTSStream creates a base.TTSStream backed by r. A goroutine
// reads from r in selfplayChunkSize increments and forwards audio.Chunk
// values to the channel. The channel closes when r is exhausted.
func newMockTTSStream(r io.ReadCloser) base.TTSStream {
	ch := make(chan audio.Chunk, selfplayStreamBufSize)
	s := &mockTTSStream{ch: ch}

	go func() {
		defer func() {
			_ = r.Close()
			close(ch)
		}()
		buf := make([]byte, selfplayChunkSize)
		idx := 0
		for {
			n, err := r.Read(buf)
			if n > 0 {
				data := make([]byte, n)
				copy(data, buf[:n])
				ch <- audio.Chunk{Data: data, Index: idx}
				idx++
			}
			if err != nil {
				if err != io.EOF {
					ch <- audio.Chunk{Error: err, Index: idx}
				}
				return
			}
		}
	}()

	return s
}

// Chunks implements base.TTSStream.
func (s *mockTTSStream) Chunks() <-chan audio.Chunk { return s.ch }

// Cost implements base.TTSStream. Always nil for the mock provider.
func (s *mockTTSStream) Cost() *types.CostInfo { return nil }

// Close implements base.TTSStream. Drains remaining chunks safely.
func (s *mockTTSStream) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	for range s.ch { //nolint:revive // intentional drain
	}
	return nil
}
