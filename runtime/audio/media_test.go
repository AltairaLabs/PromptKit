package audio

import (
	"testing"
	"time"
)

func TestMemSink_FlushDropsQueued(t *testing.T) {
	s := NewMemSink(KindAudio)
	s.Write(MediaFrame{Kind: KindAudio, Data: []byte{1, 2}, PTS: 0})
	s.Write(MediaFrame{Kind: KindAudio, Data: []byte{3, 4}, PTS: 10 * time.Millisecond})
	s.Flush()
	if got := s.Written(); len(got) != 0 {
		t.Fatalf("expected queue dropped after flush, got %d frames", len(got))
	}
}

func TestMemSink_WriteAndRead(t *testing.T) {
	s := NewMemSink(KindAudio)
	if s.Kind() != KindAudio {
		t.Fatalf("expected KindAudio, got %v", s.Kind())
	}
	f1 := MediaFrame{Kind: KindAudio, Data: []byte{0xAB}, PTS: 5 * time.Millisecond, Format: Format{SampleRate: 16000, Channels: 1}}
	s.Write(f1)
	got := s.Written()
	if len(got) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(got))
	}
	if got[0].PTS != 5*time.Millisecond {
		t.Errorf("PTS mismatch: got %v", got[0].PTS)
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close() unexpected error: %v", err)
	}
}

func TestMemSource_RoundTrip(t *testing.T) {
	src := NewMemSource(KindAudio, 4)
	if src.Kind() != KindAudio {
		t.Fatalf("expected KindAudio, got %v", src.Kind())
	}
	f := MediaFrame{Kind: KindAudio, Data: []byte{0x01, 0x02}, PTS: 20 * time.Millisecond}
	src.Push(f)
	_ = src.Close()

	var frames []MediaFrame
	for fr := range src.Frames() {
		frames = append(frames, fr)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}
	if frames[0].PTS != 20*time.Millisecond {
		t.Errorf("PTS mismatch: got %v", frames[0].PTS)
	}
}

func TestMemSource_DoubleCloseNoPanic(t *testing.T) {
	src := NewMemSource(KindAudio, 1)
	if err := src.Close(); err != nil {
		t.Fatalf("first Close() error: %v", err)
	}
	// Second Close must not panic on an already-closed channel.
	if err := src.Close(); err != nil {
		t.Fatalf("second Close() error: %v", err)
	}
}

// Ensure compile-time interface satisfaction.
var _ Source = (*MemSource)(nil)
var _ Sink = (*MemSink)(nil)
