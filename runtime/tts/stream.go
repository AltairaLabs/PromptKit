package tts

import (
	"io"
	"sync"
	"unicode/utf8"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// chunkSize is the number of bytes read per audio chunk from the synthesis reader.
// 4 KiB balances chunk granularity against syscall overhead.
const chunkSize = 4096

// ttsStream implements base.TTSStream. It wraps the chunk channel produced
// by an async reader goroutine and exposes Cost() once the channel drains.
// Close() cancels an in-progress stream by draining and discarding chunks.
type ttsStream struct {
	ch     <-chan audio.Chunk
	mu     sync.Mutex
	cost   *types.CostInfo
	closed bool
}

// newReaderStream starts a goroutine that reads from r in chunkSize increments,
// emitting audio.Chunk values to the returned channel. On completion (io.EOF or
// error) it stamps cost from the pricing descriptor using the character count of
// text, then closes the channel.
func newReaderStream(
	r io.ReadCloser,
	text string,
	pricing *base.PricingDescriptor,
	implName string,
) *ttsStream {
	ch := make(chan audio.Chunk, streamChannelBuffer)
	s := &ttsStream{ch: ch}

	go func() {
		defer func() {
			_ = r.Close()
			close(ch)
		}()

		buf := make([]byte, chunkSize)
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
				break
			}
		}

		// Stamp cost after the channel is drained.
		cost := computeStreamCost(text, pricing, implName)
		s.mu.Lock()
		s.cost = cost
		s.mu.Unlock()
	}()

	return s
}

// computeStreamCost builds a CostInfo for a TTS call given text and pricing.
// Returns nil when pricing is nil (free/local provider).
func computeStreamCost(text string, pricing *base.PricingDescriptor, implName string) *types.CostInfo {
	if pricing == nil {
		return nil
	}
	charCount := utf8.RuneCountInString(text)
	info := &types.CostInfo{
		Quantities:   map[string]float64{"character": float64(charCount)},
		ProviderName: implName,
		Capability:   string(base.ProviderTypeTTS),
	}
	usd, _, err := base.ComputeCost(pricing, info)
	if err != nil {
		// Pricing mismatch — return quantities without a dollar amount.
		return info
	}
	info.TotalCost = usd
	return info
}

// Chunks implements base.TTSStream.
func (s *ttsStream) Chunks() <-chan audio.Chunk { return s.ch }

// Cost implements base.TTSStream. Returns nil until the chunk channel closes.
func (s *ttsStream) Cost() *types.CostInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cost
}

// Close implements base.TTSStream. Drains and discards remaining chunks so
// the producer goroutine can exit. Safe to call multiple times.
func (s *ttsStream) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	// Drain so the goroutine unblocks.
	for range s.ch { //nolint:revive // intentional drain
	}
	return nil
}
