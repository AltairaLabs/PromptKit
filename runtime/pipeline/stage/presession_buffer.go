package stage

// defaultPreSessionBufferBytes bounds audio held while the duplex session is
// being created. 2MB is ~60s of 16kHz mono PCM16 — far longer than any healthy
// WebSocket handshake, so a well-behaved connection never reaches it.
const defaultPreSessionBufferBytes = 2 * 1024 * 1024

// preSessionBuffer holds input elements arriving before the duplex session
// exists, bounded by the audio bytes retained.
//
// The buffer needs its own bound because the goroutine filling it consumes the
// input channel directly, which removes the backpressure the pipeline would
// otherwise apply. A producer pushing faster than real time — which the SDK's
// SendChunk permits, having no pacing anywhere on its path — therefore
// accumulates without limit for as long as session creation takes. Measured at
// 1.7-2.8 GB retained per second of unpaced feed.
//
// On overflow the oldest audio is dropped. What a speaker said most recently is
// what the first turn needs; replaying stale audio and discarding the newest
// would be the wrong trade.
//
// Only audio counts toward the bound. Text and EndOfStream elements carry
// negligible bytes but real meaning — dropping an EndOfStream would strand the
// first turn — so they are never evicted.
type preSessionBuffer struct {
	elems    []StreamElement
	maxBytes int
	curBytes int
}

// newPreSessionBuffer returns a buffer bounded to maxBytes of audio. A
// non-positive maxBytes disables the bound.
func newPreSessionBuffer(maxBytes int) *preSessionBuffer {
	return &preSessionBuffer{maxBytes: maxBytes}
}

// add appends an element, evicting the oldest audio if the bound is exceeded.
func (b *preSessionBuffer) add(elem StreamElement) {
	b.elems = append(b.elems, elem)
	b.curBytes += elemAudioBytes(elem)

	if b.maxBytes <= 0 {
		return
	}

	// Evict oldest audio until back under the bound, preserving non-audio.
	for b.curBytes > b.maxBytes {
		idx := b.firstAudioIndex()
		if idx < 0 {
			return // nothing left to evict
		}
		b.curBytes -= elemAudioBytes(b.elems[idx])
		b.elems = append(b.elems[:idx], b.elems[idx+1:]...)
	}
}

// firstAudioIndex returns the index of the oldest audio-bearing element, or -1.
func (b *preSessionBuffer) firstAudioIndex() int {
	for i := range b.elems {
		if elemAudioBytes(b.elems[i]) > 0 {
			return i
		}
	}
	return -1
}

// elements returns the retained elements in arrival order.
func (b *preSessionBuffer) elements() []StreamElement {
	return b.elems
}

// bytes returns the audio bytes currently retained.
func (b *preSessionBuffer) bytes() int {
	return b.curBytes
}

// len returns the number of retained elements.
func (b *preSessionBuffer) len() int {
	return len(b.elems)
}

// elemAudioBytes returns the audio payload size of an element, or 0.
func elemAudioBytes(elem StreamElement) int {
	if elem.Audio == nil {
		return 0
	}
	return len(elem.Audio.Samples)
}
