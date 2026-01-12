package streaming

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

func TestNewImageStreamer(t *testing.T) {
	tests := []struct {
		name        string
		targetFPS   float64
		expectedFPS float64
	}{
		{"positive FPS", 2.0, 2.0},
		{"zero FPS uses default", 0, DefaultTargetFPS},
		{"negative FPS uses default", -1.0, DefaultTargetFPS},
		{"fractional FPS", 0.5, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			streamer := NewImageStreamer(tt.targetFPS)
			if streamer.TargetFPS != tt.expectedFPS {
				t.Errorf("Expected TargetFPS %f, got %f", tt.expectedFPS, streamer.TargetFPS)
			}
		})
	}
}

func TestImageStreamer_SendFrame(t *testing.T) {
	streamer := NewImageStreamer(1.0)
	ctx := context.Background()
	output := make(chan stage.StreamElement, 1)

	testData := []byte{0xFF, 0xD8, 0xFF} // Mock JPEG header
	mimeType := "image/jpeg"
	frameNum := int64(42)
	timestamp := time.Now()

	err := streamer.SendFrame(ctx, testData, mimeType, frameNum, timestamp, output)
	if err != nil {
		t.Fatalf("SendFrame returned error: %v", err)
	}

	select {
	case elem := <-output:
		if elem.Image == nil {
			t.Fatal("Expected Image in element")
		}
		if string(elem.Image.Data) != string(testData) {
			t.Error("Image data mismatch")
		}
		if elem.Image.MIMEType != mimeType {
			t.Errorf("Expected MIMEType %s, got %s", mimeType, elem.Image.MIMEType)
		}
		if elem.Image.FrameNum != frameNum {
			t.Errorf("Expected FrameNum %d, got %d", frameNum, elem.Image.FrameNum)
		}
		if !elem.Image.Timestamp.Equal(timestamp) {
			t.Error("Timestamp mismatch")
		}
		if elem.Priority != stage.PriorityHigh {
			t.Errorf("Expected PriorityHigh, got %v", elem.Priority)
		}
		if elem.Metadata["passthrough"] != true {
			t.Error("Expected passthrough metadata to be true")
		}
	default:
		t.Fatal("Expected element in output channel")
	}
}

func TestImageStreamer_SendFrameWithDimensions(t *testing.T) {
	streamer := NewImageStreamer(1.0)
	ctx := context.Background()
	output := make(chan stage.StreamElement, 1)

	testData := []byte{0xFF, 0xD8, 0xFF}
	width := 640
	height := 480

	err := streamer.SendFrameWithDimensions(ctx, testData, "image/jpeg", width, height, 0, time.Now(), output)
	if err != nil {
		t.Fatalf("SendFrameWithDimensions returned error: %v", err)
	}

	elem := <-output
	if elem.Image == nil {
		t.Fatal("Expected Image in element")
	}
	if elem.Image.Width != width {
		t.Errorf("Expected Width %d, got %d", width, elem.Image.Width)
	}
	if elem.Image.Height != height {
		t.Errorf("Expected Height %d, got %d", height, elem.Image.Height)
	}
}

func TestImageStreamer_SendFrame_ContextCancelled(t *testing.T) {
	streamer := NewImageStreamer(1.0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	output := make(chan stage.StreamElement) // Unbuffered - will block

	err := streamer.SendFrame(ctx, []byte{1, 2, 3}, "image/jpeg", 0, time.Now(), output)
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got: %v", err)
	}
}

func TestImageStreamer_StreamFramesBurst(t *testing.T) {
	streamer := NewImageStreamer(1.0)
	ctx := context.Background()
	output := make(chan stage.StreamElement, 10)

	frames := [][]byte{
		{1, 2, 3},
		{4, 5, 6},
		{7, 8, 9},
	}

	err := streamer.StreamFramesBurst(ctx, frames, "image/png", output)
	if err != nil {
		t.Fatalf("StreamFramesBurst returned error: %v", err)
	}

	// Verify all frames were sent
	for i := 0; i < len(frames); i++ {
		select {
		case elem := <-output:
			if elem.Image == nil {
				t.Fatalf("Frame %d: Expected Image in element", i)
			}
			if elem.Image.FrameNum != int64(i) {
				t.Errorf("Frame %d: Expected FrameNum %d, got %d", i, i, elem.Image.FrameNum)
			}
		default:
			t.Fatalf("Missing frame %d", i)
		}
	}
}

func TestImageStreamer_StreamFramesBurst_Empty(t *testing.T) {
	streamer := NewImageStreamer(1.0)
	ctx := context.Background()
	output := make(chan stage.StreamElement, 1)

	err := streamer.StreamFramesBurst(ctx, [][]byte{}, "image/jpeg", output)
	if err != nil {
		t.Fatalf("StreamFramesBurst with empty frames returned error: %v", err)
	}

	// No elements should be sent
	select {
	case <-output:
		t.Fatal("Expected no elements for empty frames")
	default:
		// Expected
	}
}

func TestImageStreamer_StreamFramesRealtime(t *testing.T) {
	// Use high FPS for fast test
	streamer := NewImageStreamer(100.0)
	ctx := context.Background()
	output := make(chan stage.StreamElement, 10)

	frames := [][]byte{
		{1, 2, 3},
		{4, 5, 6},
	}

	start := time.Now()
	err := streamer.StreamFramesRealtime(ctx, frames, "image/png", output)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("StreamFramesRealtime returned error: %v", err)
	}

	// With 100 FPS, 2 frames should take ~10ms (one interval between frames)
	// Allow some tolerance
	if elapsed < 5*time.Millisecond || elapsed > 100*time.Millisecond {
		t.Logf("Note: Realtime streaming took %v for 2 frames at 100 FPS", elapsed)
	}

	// Verify all frames were sent
	for i := 0; i < len(frames); i++ {
		select {
		case elem := <-output:
			if elem.Image == nil {
				t.Fatalf("Frame %d: Expected Image in element", i)
			}
		default:
			t.Fatalf("Missing frame %d", i)
		}
	}
}

func TestImageStreamer_StreamFramesRealtime_ContextCancelled(t *testing.T) {
	// Use low FPS so there's time to cancel
	streamer := NewImageStreamer(0.5) // 2 seconds between frames
	ctx, cancel := context.WithCancel(context.Background())
	output := make(chan stage.StreamElement, 10)

	frames := [][]byte{
		{1, 2, 3},
		{4, 5, 6},
		{7, 8, 9},
	}

	// Cancel after first frame
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := streamer.StreamFramesRealtime(ctx, frames, "image/png", output)
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got: %v", err)
	}
}

func TestSendImageEndOfStream(t *testing.T) {
	ctx := context.Background()
	output := make(chan stage.StreamElement, 1)

	err := SendImageEndOfStream(ctx, output)
	if err != nil {
		t.Fatalf("SendImageEndOfStream returned error: %v", err)
	}

	elem := <-output
	if !elem.EndOfStream {
		t.Error("Expected EndOfStream to be true")
	}
	if elem.Metadata["media_type"] != "image" {
		t.Errorf("Expected media_type 'image', got %v", elem.Metadata["media_type"])
	}
}

func TestSendImageEndOfStream_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	output := make(chan stage.StreamElement) // Unbuffered

	err := SendImageEndOfStream(ctx, output)
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got: %v", err)
	}
}

func TestImageStreamer_getTargetFPS(t *testing.T) {
	tests := []struct {
		name        string
		configFPS   float64
		expectedFPS float64
	}{
		{"positive FPS", 5.0, 5.0},
		{"zero FPS returns default", 0, DefaultTargetFPS},
		{"negative FPS returns default", -2.0, DefaultTargetFPS},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			streamer := &ImageStreamer{TargetFPS: tt.configFPS}
			fps := streamer.getTargetFPS()
			if fps != tt.expectedFPS {
				t.Errorf("Expected %f, got %f", tt.expectedFPS, fps)
			}
		})
	}
}

func TestImageStreamer_getFrameInterval(t *testing.T) {
	tests := []struct {
		name             string
		fps              float64
		expectedInterval time.Duration
	}{
		{"1 FPS", 1.0, time.Second},
		{"2 FPS", 2.0, 500 * time.Millisecond},
		{"10 FPS", 10.0, 100 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			streamer := NewImageStreamer(tt.fps)
			interval := streamer.getFrameInterval()
			if interval != tt.expectedInterval {
				t.Errorf("Expected %v, got %v", tt.expectedInterval, interval)
			}
		})
	}
}
