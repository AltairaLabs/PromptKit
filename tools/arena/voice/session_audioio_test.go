package voice

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
)

// fakeSession is an audio.Session backed by MemSource/MemSink so the adapter can
// be exercised without PortAudio or hardware.
type fakeSession struct {
	src  *audio.MemSource
	sink *audio.MemSink
}

func newFakeSession() *fakeSession {
	return &fakeSession{
		src:  audio.NewMemSource(audio.KindAudio, 4),
		sink: audio.NewMemSink(audio.KindAudio),
	}
}

func (f *fakeSession) Start(context.Context) error { return nil }
func (f *fakeSession) Sources() []audio.Source     { return []audio.Source{f.src} }
func (f *fakeSession) Sinks() []audio.Sink         { return []audio.Sink{f.sink} }
func (f *fakeSession) Close() error                { _ = f.src.Close(); return nil }

// TestSessionAudioIO_CaptureForwardsFrameData proves CaptureChunks forwards the
// raw PCM16 bytes of each MediaFrame and closes when the source's Frames close.
func TestSessionAudioIO_CaptureForwardsFrameData(t *testing.T) {
	fs := newFakeSession()
	io := newSessionAudioIO(fs)

	fs.src.Push(audio.MediaFrame{Kind: audio.KindAudio, Data: []byte{1, 2, 3, 4}})
	fs.src.Push(audio.MediaFrame{Kind: audio.KindAudio, Data: []byte{5, 6}})

	ch := io.CaptureChunks()
	select {
	case got := <-ch:
		if string(got) != string([]byte{1, 2, 3, 4}) {
			t.Fatalf("first chunk = %v, want [1 2 3 4]", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first chunk")
	}
	select {
	case got := <-ch:
		if string(got) != string([]byte{5, 6}) {
			t.Fatalf("second chunk = %v, want [5 6]", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second chunk")
	}

	// Closing the source closes the forwarded channel.
	_ = fs.src.Close()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected capture channel to be closed after source close")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for capture channel close")
	}
}

// TestSessionAudioIO_PlayAndFlushReachSink proves Play writes a 24 kHz audio
// MediaFrame to the sink and Flush drops queued frames.
func TestSessionAudioIO_PlayAndFlushReachSink(t *testing.T) {
	fs := newFakeSession()
	io := newSessionAudioIO(fs)

	io.Play([]byte{9, 8, 7, 6})
	written := fs.sink.Written()
	if len(written) != 1 {
		t.Fatalf("expected 1 written frame, got %d", len(written))
	}
	if string(written[0].Data) != string([]byte{9, 8, 7, 6}) {
		t.Fatalf("written data = %v, want [9 8 7 6]", written[0].Data)
	}
	if written[0].Kind != audio.KindAudio {
		t.Fatalf("written kind = %v, want KindAudio", written[0].Kind)
	}
	if written[0].Format.SampleRate != PlaybackSampleRate {
		t.Fatalf("written sample rate = %d, want %d", written[0].Format.SampleRate, PlaybackSampleRate)
	}

	io.Flush()
	if got := fs.sink.Written(); len(got) != 0 {
		t.Fatalf("expected sink drained after flush, got %d frames", len(got))
	}
}

// TestSessionAudioIO_StartAndClose proves Start and Close delegate to the session.
func TestSessionAudioIO_StartAndClose(t *testing.T) {
	fs := newFakeSession()
	io := newSessionAudioIO(fs)
	if err := io.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := io.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
