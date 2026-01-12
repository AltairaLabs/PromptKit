package streaming

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

func TestNewVideoStreamer(t *testing.T) {
	tests := []struct {
		name            string
		chunkDurationMs int
		expectedMs      int
	}{
		{"positive duration", 500, 500},
		{"zero uses default", 0, DefaultChunkDurationMs},
		{"negative uses default", -100, DefaultChunkDurationMs},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			streamer := NewVideoStreamer(tt.chunkDurationMs)
			if streamer.ChunkDurationMs != tt.expectedMs {
				t.Errorf("Expected ChunkDurationMs %d, got %d", tt.expectedMs, streamer.ChunkDurationMs)
			}
		})
	}
}

func TestVideoStreamer_SendChunk(t *testing.T) {
	streamer := NewVideoStreamer(1000)
	ctx := context.Background()
	output := make(chan stage.StreamElement, 1)

	testData := []byte{0x00, 0x00, 0x01, 0x67} // Mock H.264 SPS header
	mimeType := "video/h264"
	chunkIndex := 5
	isKeyFrame := true
	timestamp := time.Now()

	err := streamer.SendChunk(ctx, testData, mimeType, chunkIndex, isKeyFrame, timestamp, output)
	if err != nil {
		t.Fatalf("SendChunk returned error: %v", err)
	}

	select {
	case elem := <-output:
		if elem.Video == nil {
			t.Fatal("Expected Video in element")
		}
		if string(elem.Video.Data) != string(testData) {
			t.Error("Video data mismatch")
		}
		if elem.Video.MIMEType != mimeType {
			t.Errorf("Expected MIMEType %s, got %s", mimeType, elem.Video.MIMEType)
		}
		if elem.Video.FrameNum != int64(chunkIndex) {
			t.Errorf("Expected FrameNum %d, got %d", chunkIndex, elem.Video.FrameNum)
		}
		if elem.Video.IsKeyFrame != isKeyFrame {
			t.Errorf("Expected IsKeyFrame %v, got %v", isKeyFrame, elem.Video.IsKeyFrame)
		}
		if !elem.Video.Timestamp.Equal(timestamp) {
			t.Error("Timestamp mismatch")
		}
		if elem.Priority != stage.PriorityHigh {
			t.Errorf("Expected PriorityHigh, got %v", elem.Priority)
		}
		if elem.Metadata["passthrough"] != true {
			t.Error("Expected passthrough metadata to be true")
		}
		if elem.Metadata["is_key_frame"] != true {
			t.Error("Expected is_key_frame metadata to be true")
		}
	default:
		t.Fatal("Expected element in output channel")
	}
}

func TestVideoStreamer_SendChunk_NonKeyFrame(t *testing.T) {
	streamer := NewVideoStreamer(1000)
	ctx := context.Background()
	output := make(chan stage.StreamElement, 1)

	err := streamer.SendChunk(ctx, []byte{1, 2, 3}, "video/h264", 0, false, time.Now(), output)
	if err != nil {
		t.Fatalf("SendChunk returned error: %v", err)
	}

	elem := <-output
	if elem.Video.IsKeyFrame {
		t.Error("Expected IsKeyFrame to be false")
	}
	if elem.Metadata["is_key_frame"] != false {
		t.Error("Expected is_key_frame metadata to be false")
	}
}

func TestVideoStreamer_SendChunkWithDimensions(t *testing.T) {
	streamer := NewVideoStreamer(1000)
	ctx := context.Background()
	output := make(chan stage.StreamElement, 1)

	testData := []byte{1, 2, 3, 4}
	width := 1920
	height := 1080
	frameRate := 30.0
	duration := 33 * time.Millisecond

	err := streamer.SendChunkWithDimensions(ctx, testData, "video/webm", width, height, frameRate, 0, true, time.Now(), duration, output)
	if err != nil {
		t.Fatalf("SendChunkWithDimensions returned error: %v", err)
	}

	elem := <-output
	if elem.Video == nil {
		t.Fatal("Expected Video in element")
	}
	if elem.Video.Width != width {
		t.Errorf("Expected Width %d, got %d", width, elem.Video.Width)
	}
	if elem.Video.Height != height {
		t.Errorf("Expected Height %d, got %d", height, elem.Video.Height)
	}
	if elem.Video.FrameRate != frameRate {
		t.Errorf("Expected FrameRate %f, got %f", frameRate, elem.Video.FrameRate)
	}
	if elem.Video.Duration != duration {
		t.Errorf("Expected Duration %v, got %v", duration, elem.Video.Duration)
	}
}

func TestVideoStreamer_SendChunk_ContextCancelled(t *testing.T) {
	streamer := NewVideoStreamer(1000)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	output := make(chan stage.StreamElement) // Unbuffered - will block

	err := streamer.SendChunk(ctx, []byte{1, 2, 3}, "video/h264", 0, true, time.Now(), output)
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got: %v", err)
	}
}

func TestVideoStreamer_StreamChunksBurst(t *testing.T) {
	streamer := NewVideoStreamer(1000)
	ctx := context.Background()
	output := make(chan stage.StreamElement, 10)

	now := time.Now()
	chunks := []VideoChunk{
		{Data: []byte{1, 2, 3}, IsKeyFrame: true, Timestamp: now, Duration: 100 * time.Millisecond},
		{Data: []byte{4, 5, 6}, IsKeyFrame: false, Timestamp: now.Add(100 * time.Millisecond), Duration: 100 * time.Millisecond},
		{Data: []byte{7, 8, 9}, IsKeyFrame: false, Timestamp: now.Add(200 * time.Millisecond), Duration: 100 * time.Millisecond},
	}

	err := streamer.StreamChunksBurst(ctx, chunks, "video/h264", output)
	if err != nil {
		t.Fatalf("StreamChunksBurst returned error: %v", err)
	}

	// Verify all chunks were sent
	for i := range chunks {
		select {
		case elem := <-output:
			if elem.Video == nil {
				t.Fatalf("Chunk %d: Expected Video in element", i)
			}
			if elem.Video.FrameNum != int64(i) {
				t.Errorf("Chunk %d: Expected FrameNum %d, got %d", i, i, elem.Video.FrameNum)
			}
			if i == 0 && !elem.Video.IsKeyFrame {
				t.Error("First chunk should be keyframe")
			}
		default:
			t.Fatalf("Missing chunk %d", i)
		}
	}
}

func TestVideoStreamer_StreamChunksBurst_Empty(t *testing.T) {
	streamer := NewVideoStreamer(1000)
	ctx := context.Background()
	output := make(chan stage.StreamElement, 1)

	err := streamer.StreamChunksBurst(ctx, []VideoChunk{}, "video/h264", output)
	if err != nil {
		t.Fatalf("StreamChunksBurst with empty chunks returned error: %v", err)
	}

	// No elements should be sent
	select {
	case <-output:
		t.Fatal("Expected no elements for empty chunks")
	default:
		// Expected
	}
}

func TestVideoStreamer_StreamChunksRealtime(t *testing.T) {
	// Use short chunk duration for fast test
	streamer := NewVideoStreamer(10) // 10ms chunks
	ctx := context.Background()
	output := make(chan stage.StreamElement, 10)

	chunks := []VideoChunk{
		{Data: []byte{1, 2, 3}, IsKeyFrame: true, Timestamp: time.Now(), Duration: 10 * time.Millisecond},
		{Data: []byte{4, 5, 6}, IsKeyFrame: false, Timestamp: time.Now(), Duration: 10 * time.Millisecond},
	}

	start := time.Now()
	err := streamer.StreamChunksRealtime(ctx, chunks, "video/h264", output)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("StreamChunksRealtime returned error: %v", err)
	}

	// With 10ms chunks, 2 chunks should take ~10ms (one interval between chunks)
	if elapsed < 5*time.Millisecond || elapsed > 100*time.Millisecond {
		t.Logf("Note: Realtime streaming took %v for 2 chunks", elapsed)
	}

	// Verify all chunks were sent
	for i := range chunks {
		select {
		case elem := <-output:
			if elem.Video == nil {
				t.Fatalf("Chunk %d: Expected Video in element", i)
			}
		default:
			t.Fatalf("Missing chunk %d", i)
		}
	}
}

func TestVideoStreamer_StreamChunksRealtime_UsesChunkDuration(t *testing.T) {
	// Default to longer duration but chunks have short duration
	streamer := NewVideoStreamer(5000) // 5 seconds default
	ctx := context.Background()
	output := make(chan stage.StreamElement, 10)

	// Chunks with explicit short duration override default
	chunks := []VideoChunk{
		{Data: []byte{1, 2, 3}, IsKeyFrame: true, Timestamp: time.Now(), Duration: 5 * time.Millisecond},
		{Data: []byte{4, 5, 6}, IsKeyFrame: false, Timestamp: time.Now(), Duration: 5 * time.Millisecond},
	}

	start := time.Now()
	err := streamer.StreamChunksRealtime(ctx, chunks, "video/h264", output)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("StreamChunksRealtime returned error: %v", err)
	}

	// Should use chunk's 5ms duration, not default 5s
	if elapsed > 100*time.Millisecond {
		t.Errorf("Expected fast completion using chunk duration, took %v", elapsed)
	}
}

func TestVideoStreamer_StreamChunksRealtime_ContextCancelled(t *testing.T) {
	// Use long default duration so there's time to cancel
	streamer := NewVideoStreamer(5000) // 5 seconds
	ctx, cancel := context.WithCancel(context.Background())
	output := make(chan stage.StreamElement, 10)

	// Chunks without duration will use default (5s)
	chunks := []VideoChunk{
		{Data: []byte{1, 2, 3}, IsKeyFrame: true, Timestamp: time.Now()},
		{Data: []byte{4, 5, 6}, IsKeyFrame: false, Timestamp: time.Now()},
		{Data: []byte{7, 8, 9}, IsKeyFrame: false, Timestamp: time.Now()},
	}

	// Cancel shortly after first chunk
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := streamer.StreamChunksRealtime(ctx, chunks, "video/h264", output)
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got: %v", err)
	}
}

func TestSendVideoEndOfStream(t *testing.T) {
	ctx := context.Background()
	output := make(chan stage.StreamElement, 1)

	err := SendVideoEndOfStream(ctx, output)
	if err != nil {
		t.Fatalf("SendVideoEndOfStream returned error: %v", err)
	}

	elem := <-output
	if !elem.EndOfStream {
		t.Error("Expected EndOfStream to be true")
	}
	if elem.Metadata["media_type"] != "video" {
		t.Errorf("Expected media_type 'video', got %v", elem.Metadata["media_type"])
	}
}

func TestSendVideoEndOfStream_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	output := make(chan stage.StreamElement) // Unbuffered

	err := SendVideoEndOfStream(ctx, output)
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got: %v", err)
	}
}

func TestVideoStreamer_getChunkDurationMs(t *testing.T) {
	tests := []struct {
		name       string
		configMs   int
		expectedMs int
	}{
		{"positive duration", 500, 500},
		{"zero returns default", 0, DefaultChunkDurationMs},
		{"negative returns default", -100, DefaultChunkDurationMs},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			streamer := &VideoStreamer{ChunkDurationMs: tt.configMs}
			ms := streamer.getChunkDurationMs()
			if ms != tt.expectedMs {
				t.Errorf("Expected %d, got %d", tt.expectedMs, ms)
			}
		})
	}
}
