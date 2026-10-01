package providers

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestStreamPump_ForwardsInOrderAndDrains(t *testing.T) {
	in := make(chan StreamChunk, 4)
	p := NewStreamPump(context.Background(), in, 4)
	p.Start()

	in <- StreamChunk{Content: "a"}
	in <- StreamChunk{Content: "b"}
	in <- StreamChunk{Content: "c"}
	close(in) // pump drains the queue then closes Response()

	var got []string
	for c := range p.Response() {
		got = append(got, c.Content)
	}
	p.Wait() // returns once the pump goroutine has finished

	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("expected [a b c] in order, got %v", got)
	}
}

func TestStreamPump_BargeFiresSignalAndDropsQueuedAudio(t *testing.T) {
	in := make(chan StreamChunk, 8)
	p := NewStreamPump(context.Background(), in, 1) // tiny out buffer forces internal queueing
	p.Start()

	in <- StreamChunk{Content: "text-1"}
	in <- StreamChunk{MediaData: &StreamMediaData{Data: []byte{1, 2}}}
	in <- StreamChunk{MediaData: &StreamMediaData{Data: []byte{3, 4}}}
	in <- StreamChunk{Content: "text-2"}

	// Let the pump pull the buffered inputs into its internal queue before barge-in.
	deadline := time.Now().Add(time.Second)
	for len(in) > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	p.Barge()           // fires BargeIn(), starts Dropping()
	in <- BargeMarker() // drops the queued audio, keeps text/control

	select {
	case <-p.BargeIn():
	case <-time.After(time.Second):
		t.Fatal("Barge() did not fire the BargeIn signal")
	}
	if !p.Dropping() {
		t.Error("Barge() should leave the pump Dropping()")
	}

	close(in)
	var texts, audio int
	for c := range p.Response() {
		switch {
		case c.Content != "":
			texts++
		case c.MediaData != nil && len(c.MediaData.Data) > 0:
			audio++
		}
	}
	p.Wait()

	if audio != 0 {
		t.Errorf("queued audio should be dropped on barge-in, got %d audio chunks", audio)
	}
	if texts == 0 {
		t.Error("non-audio chunks should survive barge-in")
	}

	p.ClearDrop()
	if p.Dropping() {
		t.Error("ClearDrop() should clear the dropping state")
	}
}

// The barge-in purge is ordered with the stream: audio queued before the
// marker is dropped, audio after it — the next response's opening chunks — is
// kept, however the pump's reads interleave. An out-of-band purge request could
// arrive after the next response's first chunk had been queued and drop it.
func TestStreamPump_BargeMarkerDropsOnlyAudioBeforeIt(t *testing.T) {
	in := make(chan StreamChunk, 8)
	audio := func(b byte) StreamChunk { return StreamChunk{MediaData: &StreamMediaData{Data: []byte{b}}} }

	// Everything is already buffered before the pump starts, so it reads the
	// pre-barge audio, the marker and the next response's audio back to back.
	in <- audio(1)
	in <- audio(2)
	in <- StreamChunk{Content: "partial transcript"}
	in <- BargeMarker()
	in <- audio(100)
	in <- audio(101)
	close(in)

	p := NewStreamPump(context.Background(), in, 1) // one slot: at most audio(1) can already be out
	p.Start()
	// Hold the consumer back until the pump has read every input, so the
	// pre-barge audio is in its queue (or the one-slot output) at the marker.
	deadline := time.Now().Add(time.Second)
	for len(in) > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	var got []byte
	var texts int
	for c := range p.Response() {
		switch {
		case c.MediaData != nil:
			got = append(got, c.MediaData.Data...)
		case c.Content != "":
			texts++
		default:
			t.Errorf("the marker must not be forwarded: %+v", c)
		}
	}
	p.Wait()

	// Audio already handed to Response() cannot be recalled, so audio(1) may
	// have got out first; audio(2) was still queued at the marker and must be
	// dropped, and the next response's audio must all arrive, in order.
	if bytes.IndexByte(got, 2) >= 0 {
		t.Errorf("audio queued before the marker was delivered: %v", got)
	}
	if !bytes.HasSuffix(got, []byte{100, 101}) {
		t.Errorf("audio after the marker = %v, want it to end with [100 101]", got)
	}
	if texts != 1 {
		t.Errorf("text queued before the marker should survive, got %d text chunks", texts)
	}
}

func TestStreamPump_ContextCancelClosesResponse(t *testing.T) {
	in := make(chan StreamChunk) // unbuffered; never closed
	ctx, cancel := context.WithCancel(context.Background())
	p := NewStreamPump(ctx, in, 1)
	p.Start()

	cancel() // the pump returns via ctx.Done and closes Response()

	select {
	case _, ok := <-p.Response():
		if ok {
			// drain any in-flight value, then confirm closure
			for range p.Response() { //nolint:revive // draining to closure
			}
		}
	case <-time.After(time.Second):
		t.Fatal("Response() not closed after context cancel")
	}
	p.Wait()
}

func TestZeroValueBargeInSignalViaPumpIsSafe(t *testing.T) {
	// dropAudioChunks is the pure drop helper used by the pump.
	in := []StreamChunk{
		{Content: "keep"},
		{MediaData: &StreamMediaData{Data: []byte{1}}},
		{Metadata: map[string]interface{}{"type": "input_transcription"}},
		{MediaData: &StreamMediaData{Data: []byte{2}}},
	}
	out := dropAudioChunks(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 non-audio chunks kept, got %d", len(out))
	}
	for _, c := range out {
		if c.MediaData != nil && len(c.MediaData.Data) > 0 {
			t.Error("audio chunk was not dropped")
		}
	}
}
