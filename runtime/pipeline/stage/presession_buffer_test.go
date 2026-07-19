package stage

import (
	"testing"
)

// presessionAudioElem returns a StreamElement carrying n bytes of audio.
func presessionAudioElem(n int) StreamElement {
	return StreamElement{Audio: &AudioData{Samples: make([]byte, n), SampleRate: 16000}}
}

// TestPreSessionBufferBoundsRetainedAudio covers unbounded growth while the
// duplex session is being created.
//
// Input elements are buffered until the WebSocket session exists. Nothing caps
// that buffer, and the drain goroutine consuming the input channel removes the
// backpressure the pipeline would otherwise apply, so a producer that pushes
// faster than real time — which the SDK's SendChunk permits, with no pacing on
// the path — accumulates without limit. Measured at 1.7-2.8 GB retained per
// second of unpaced feed.
func TestPreSessionBufferBoundsRetainedAudio(t *testing.T) {
	const maxBytes = 64 * 1024
	buf := newPreSessionBuffer(maxBytes)

	// Push far more than the cap.
	for range 1000 {
		buf.add(presessionAudioElem(3200))
	}

	if got := buf.bytes(); got > maxBytes {
		t.Errorf("buffered %d bytes against a %d cap; the pre-session buffer is unbounded",
			got, maxBytes)
	}
}

// TestPreSessionBufferKeepsNewestAudio pins which elements survive the bound.
//
// The buffer holds what the user said while the session was connecting, so when
// it overflows the most recent speech is what still matters — dropping from the
// wrong end would replay stale audio and discard what was said most recently.
func TestPreSessionBufferKeepsNewestAudio(t *testing.T) {
	const elemBytes = 100
	const maxBytes = 5 * elemBytes
	buf := newPreSessionBuffer(maxBytes)

	const total = 50
	for i := range total {
		e := presessionAudioElem(elemBytes)
		e.Audio.Samples[0] = byte(i) // tag each element with its index
		buf.add(e)
	}

	elems := buf.elements()
	if len(elems) == 0 {
		t.Fatal("buffer is empty")
	}

	last := elems[len(elems)-1]
	if got := last.Audio.Samples[0]; got != byte(total-1) {
		t.Errorf("newest retained element is tagged %d, want %d; "+
			"the bound is dropping the newest audio instead of the oldest", got, total-1)
	}
}

// TestPreSessionBufferKeepsNonAudioElements pins that the byte bound does not
// silently discard control elements.
//
// The bound exists to cap audio memory. Text and EndOfStream elements carry
// negligible bytes but real meaning — dropping an EndOfStream would strand the
// first turn — so they must not be evicted by an audio-driven cap.
func TestPreSessionBufferKeepsNonAudioElements(t *testing.T) {
	const maxBytes = 1024
	buf := newPreSessionBuffer(maxBytes)

	text := "hello"
	buf.add(StreamElement{Text: &text})
	for range 100 {
		buf.add(presessionAudioElem(3200)) // blow well past the cap
	}

	for _, e := range buf.elements() {
		if e.Text != nil {
			return // survived
		}
	}
	t.Error("the text element was evicted by the audio byte cap; " +
		"only audio should be subject to the byte bound")
}
