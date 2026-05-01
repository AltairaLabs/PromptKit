package audio

import (
	"errors"
	"io"
	"testing"
	"time"
)

func TestLocalSink_NoOpWhenContextFails(t *testing.T) {
	sink, err := NewLocalSink(LocalSinkConfig{
		Rate: Rate24k,
		newContext: func(_, _ int) (otoContext, error) {
			return nil, errors.New("no audio device")
		},
	})
	if err != nil {
		t.Fatalf("expected no-op fallback (nil error), got: %v", err)
	}
	if sink == nil {
		t.Fatal("expected non-nil noop sink")
	}
	// Should not panic
	sink.Push(Frame{Direction: DirectionInput, Samples: []int16{0, 1, 2}})
	sink.Close()
}

func TestLocalSink_StereoCompositionWithSilentChannel(t *testing.T) {
	stereo := composeStereo([]int16{1, 2, 3, 4}, nil)
	if len(stereo) != 8 {
		t.Fatalf("expected 8 samples (2x4), got %d", len(stereo))
	}
	// Left channel = input, right = silence
	wantLeft := []int16{1, 2, 3, 4}
	for i, want := range wantLeft {
		if stereo[2*i] != want {
			t.Fatalf("L[%d]=%d, want %d", i, stereo[2*i], want)
		}
		if stereo[2*i+1] != 0 {
			t.Fatalf("R[%d]=%d, want 0 (silence)", i, stereo[2*i+1])
		}
	}
}

func TestLocalSink_StereoCompositionBothPresent(t *testing.T) {
	stereo := composeStereo([]int16{1, 2}, []int16{10, 20})
	want := []int16{1, 10, 2, 20}
	if len(stereo) != len(want) {
		t.Fatalf("len: got %d, want %d", len(stereo), len(want))
	}
	for i, v := range want {
		if stereo[i] != v {
			t.Fatalf("[%d]=%d, want %d (full %v)", i, stereo[i], v, stereo)
		}
	}
}

func TestLocalSink_StereoCompositionEmpty(t *testing.T) {
	out := composeStereo(nil, nil)
	if len(out) != 0 {
		t.Fatalf("expected empty output, got %d samples", len(out))
	}
}

func TestLocalSink_StereoCompositionMismatchedLengths(t *testing.T) {
	// Right longer than left — left should pad with silence
	stereo := composeStereo([]int16{1, 2}, []int16{10, 20, 30, 40})
	if len(stereo) != 8 {
		t.Fatalf("expected 8 samples, got %d", len(stereo))
	}
	want := []int16{1, 10, 2, 20, 0, 30, 0, 40}
	for i, v := range want {
		if stereo[i] != v {
			t.Fatalf("[%d]=%d, want %d", i, stereo[i], v)
		}
	}
	_ = time.Now
}

func TestLocalSink_PushOnNoopDoesNotBlock(t *testing.T) {
	sink, err := NewLocalSink(LocalSinkConfig{
		Rate: Rate24k,
		newContext: func(_, _ int) (otoContext, error) {
			return nil, errors.New("noop")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			sink.Push(Frame{Direction: DirectionInput, Samples: []int16{int16(i)}})
		}
		close(done)
	}()
	select {
	case <-done:
		// ok
	case <-time.After(200 * time.Millisecond):
		t.Fatal("noop sink Push blocked")
	}
}

// fakeOtoContext is a test double for otoContext that captures what the sink
// hands to its player. The reader is invoked synchronously by the test to
// drain samples — there is no real audio thread.
type fakeOtoContext struct {
	reader      io.Reader
	playerSpawn chan io.Reader
}

func (f *fakeOtoContext) NewPlayer(r io.Reader) otoPlayer {
	f.reader = r
	if f.playerSpawn != nil {
		f.playerSpawn <- r
	}
	return &fakePlayer{}
}

type fakePlayer struct {
	played bool
}

func (p *fakePlayer) Play() { p.played = true }

func TestLocalSink_RealPathInterleavesStereoOnReaderPull(t *testing.T) {
	fake := &fakeOtoContext{}
	sink, err := NewLocalSink(LocalSinkConfig{
		Rate: Rate24k,
		newContext: func(_, _ int) (otoContext, error) {
			return fake, nil
		},
	})
	if err != nil {
		t.Fatalf("NewLocalSink: %v", err)
	}
	defer sink.Close()

	if fake.reader == nil {
		t.Fatal("expected sink to create a player + reader on construction")
	}

	// Push input + output; reader should produce interleaved stereo.
	sink.Push(Frame{Direction: DirectionInput, Samples: []int16{1, 2}})
	sink.Push(Frame{Direction: DirectionOutput, Samples: []int16{10, 20}})

	// Pull 8 bytes (4 stereo samples = 2 sample pairs).
	buf := make([]byte, 8)
	n, _ := fake.reader.Read(buf)
	if n != 8 {
		t.Fatalf("expected 8 bytes, got %d", n)
	}
	// Expected interleave (L=1,R=10), (L=2,R=20) in s16le.
	want := []byte{1, 0, 10, 0, 2, 0, 20, 0}
	for i, v := range want {
		if buf[i] != v {
			t.Fatalf("byte[%d]=%d, want %d (full %v)", i, buf[i], v, buf[:n])
		}
	}
}

func TestLocalSink_ReaderReturnsSilenceWhenIdle(t *testing.T) {
	fake := &fakeOtoContext{}
	sink, err := NewLocalSink(LocalSinkConfig{
		Rate: Rate24k,
		newContext: func(_, _ int) (otoContext, error) {
			return fake, nil
		},
	})
	if err != nil {
		t.Fatalf("NewLocalSink: %v", err)
	}
	defer sink.Close()

	buf := make([]byte, 8)
	n, err := fake.reader.Read(buf)
	if err != nil {
		t.Fatalf("reader returned error on idle: %v", err)
	}
	if n != 8 {
		t.Fatalf("expected reader to fill buffer with silence, got %d", n)
	}
	for i, b := range buf {
		if b != 0 {
			t.Fatalf("expected silence, byte[%d]=%d", i, b)
		}
	}
}

func TestLocalSink_ReaderReturnsEOFAfterClose(t *testing.T) {
	r := newStreamReader()
	r.close()
	buf := make([]byte, 8)
	n, err := r.Read(buf)
	if n != 0 {
		t.Fatalf("expected 0 bytes after close, got %d", n)
	}
	if err == nil {
		t.Fatal("expected EOF, got nil")
	}
}

func TestLocalSink_ReaderRoundsBufferToStereoFrames(t *testing.T) {
	r := newStreamReader()
	// 3-byte buffer can't hold a single stereo s16le frame (4 bytes); should
	// return 0, nil rather than panic.
	buf := make([]byte, 3)
	n, err := r.Read(buf)
	if n != 0 {
		t.Fatalf("expected 0 bytes, got %d", n)
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocalSink_ReaderHoldOverBetweenReads(t *testing.T) {
	r := newStreamReader()
	// Push a 4-sample left frame, then pull only 2 stereo pairs (8 bytes) — the
	// remaining 2 left samples should be held over for the next Read.
	r.left <- []int16{1, 2, 3, 4}

	buf1 := make([]byte, 8)
	n, err := r.Read(buf1)
	if err != nil {
		t.Fatalf("Read 1: %v", err)
	}
	if n != 8 {
		t.Fatalf("Read 1: expected 8 bytes, got %d", n)
	}
	// First two samples on left, right is silence.
	want1 := []byte{1, 0, 0, 0, 2, 0, 0, 0}
	for i, v := range want1 {
		if buf1[i] != v {
			t.Fatalf("Read 1 byte[%d]=%d, want %d", i, buf1[i], v)
		}
	}

	// Second read drains the hold-over without any new push.
	buf2 := make([]byte, 8)
	n, err = r.Read(buf2)
	if err != nil {
		t.Fatalf("Read 2: %v", err)
	}
	if n != 8 {
		t.Fatalf("Read 2: expected 8 bytes, got %d", n)
	}
	want2 := []byte{3, 0, 0, 0, 4, 0, 0, 0}
	for i, v := range want2 {
		if buf2[i] != v {
			t.Fatalf("Read 2 byte[%d]=%d, want %d", i, buf2[i], v)
		}
	}
}

func TestLocalSink_ReaderHandlesClosedChannel(t *testing.T) {
	r := newStreamReader()
	// Closing the left channel mid-pull should not panic; the reader should
	// return whatever it had and pad with silence.
	close(r.left)
	r.right <- []int16{99}

	buf := make([]byte, 4)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 4 {
		t.Fatalf("expected 4 bytes, got %d", n)
	}
	want := []byte{0, 0, 99, 0}
	for i, v := range want {
		if buf[i] != v {
			t.Fatalf("byte[%d]=%d, want %d", i, buf[i], v)
		}
	}
}

func TestLocalSink_StreamReaderCloseIdempotent(t *testing.T) {
	r := newStreamReader()
	r.close()
	r.close() // second call should be safe
	if !r.isClosed() {
		t.Fatal("expected stream to remain closed after second close")
	}
}
