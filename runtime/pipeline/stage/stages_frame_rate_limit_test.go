package stage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestNewFrameRateLimitStage(t *testing.T) {
	tests := []struct {
		name        string
		fps         float64
		expectedFPS float64
	}{
		{"positive FPS", 2.0, 2.0},
		{"zero FPS uses default", 0, DefaultFrameRateLimitFPS},
		{"negative FPS uses default", -1.0, DefaultFrameRateLimitFPS},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := FrameRateLimitConfig{TargetFPS: tt.fps}
			stage := NewFrameRateLimitStage(config)

			actualConfig := stage.GetConfig()
			// Check if FPS was normalized (0 or negative -> default)
			if tt.fps <= 0 {
				// Stage should have frame interval based on default
				expectedInterval := time.Duration(float64(time.Second) / tt.expectedFPS)
				if stage.frameInterval != expectedInterval {
					t.Errorf("Expected frame interval %v, got %v", expectedInterval, stage.frameInterval)
				}
			} else {
				if actualConfig.TargetFPS != tt.expectedFPS {
					t.Errorf("Expected TargetFPS %f, got %f", tt.expectedFPS, actualConfig.TargetFPS)
				}
			}
		})
	}
}

func TestFrameRateLimitStage_BasicOperation(t *testing.T) {
	config := DefaultFrameRateLimitConfig()
	stage := NewFrameRateLimitStage(config)

	if stage.Name() != "frame-rate-limit" {
		t.Errorf("Expected name 'frame-rate-limit', got '%s'", stage.Name())
	}

	if stage.Type() != StageTypeTransform {
		t.Errorf("Expected type Transform, got %v", stage.Type())
	}
}

func TestFrameRateLimitStage_PassthroughNonMedia(t *testing.T) {
	config := DefaultFrameRateLimitConfig()
	stage := NewFrameRateLimitStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 3)
	output := make(chan StreamElement, 3)

	// Send text element
	text := "Hello"
	input <- StreamElement{Text: &text}

	// Send message element
	input <- StreamElement{Message: &types.Message{Role: "user", Content: "Hi"}}

	// Send end-of-stream
	input <- StreamElement{EndOfStream: true}
	close(input)

	go func() {
		if err := stage.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	// All should pass through
	result1 := <-output
	if result1.Text == nil || *result1.Text != "Hello" {
		t.Error("Text element not passed through")
	}

	result2 := <-output
	if result2.Message == nil || result2.Message.Content != "Hi" {
		t.Error("Message element not passed through")
	}

	result3 := <-output
	if !result3.EndOfStream {
		t.Error("EndOfStream not passed through")
	}
}

func TestFrameRateLimitStage_PassthroughAudio(t *testing.T) {
	config := DefaultFrameRateLimitConfig()
	config.PassthroughAudio = true
	stage := NewFrameRateLimitStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	input <- StreamElement{
		Audio: &AudioData{
			Samples:  []byte{1, 2, 3},
			Encoding: "pcm",
		},
	}
	close(input)

	go func() {
		if err := stage.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	result := <-output
	if result.Audio == nil {
		t.Error("Audio should pass through when PassthroughAudio is true")
	}
}

func TestFrameRateLimitStage_FirstFrameAlwaysPasses(t *testing.T) {
	// Use very low FPS (1 per 10 seconds) to ensure rate limiting would kick in
	config := FrameRateLimitConfig{
		TargetFPS:           0.1, // 1 frame per 10 seconds
		PassthroughAudio:    true,
		PassthroughNonMedia: true,
	}
	stage := NewFrameRateLimitStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// First frame should always pass regardless of FPS
	input <- StreamElement{
		Image: &ImageData{
			Data:     []byte{1, 2, 3},
			MIMEType: "image/jpeg",
		},
	}
	close(input)

	go func() {
		if err := stage.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	result := <-output
	if result.Image == nil {
		t.Error("First frame should always pass")
	}

	emitted, dropped := stage.GetStats()
	if emitted != 1 {
		t.Errorf("Expected 1 emitted frame, got %d", emitted)
	}
	if dropped != 0 {
		t.Errorf("Expected 0 dropped frames, got %d", dropped)
	}
}

func TestFrameRateLimitStage_DropsExcessFrames(t *testing.T) {
	// Use high FPS that will definitely drop frames sent immediately
	config := FrameRateLimitConfig{
		TargetFPS:           1.0, // 1 FPS - will drop frames sent < 1s apart
		PassthroughAudio:    true,
		PassthroughNonMedia: true,
	}
	stage := NewFrameRateLimitStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 5)
	output := make(chan StreamElement, 5)

	// Send 5 frames immediately - only first should pass
	for i := range 5 {
		input <- StreamElement{
			Image: &ImageData{
				Data:     []byte{byte(i)},
				MIMEType: "image/jpeg",
			},
		}
	}
	close(input)

	go func() {
		if err := stage.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	// Drain output channel to wait for processing to complete
	for range output {
	}

	emitted, dropped := stage.GetStats()
	if emitted != 1 {
		t.Errorf("Expected 1 emitted frame (first only), got %d", emitted)
	}
	if dropped != 4 {
		t.Errorf("Expected 4 dropped frames, got %d", dropped)
	}
}

func TestFrameRateLimitStage_VideoFrames(t *testing.T) {
	config := FrameRateLimitConfig{
		TargetFPS:           1.0,
		PassthroughAudio:    true,
		PassthroughNonMedia: true,
	}
	stage := NewFrameRateLimitStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 3)
	output := make(chan StreamElement, 3)

	// Send 3 video frames immediately
	for i := range 3 {
		input <- StreamElement{
			Video: &VideoData{
				Data:     []byte{byte(i)},
				MIMEType: "video/h264",
			},
		}
	}
	close(input)

	go func() {
		if err := stage.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	// Drain output channel to wait for processing to complete
	for range output {
	}

	emitted, dropped := stage.GetStats()
	if emitted != 1 {
		t.Errorf("Expected 1 emitted video frame, got %d", emitted)
	}
	if dropped != 2 {
		t.Errorf("Expected 2 dropped video frames, got %d", dropped)
	}
}

func TestFrameRateLimitStage_HighFPS_PassesMoreFrames(t *testing.T) {
	// Very high FPS - should pass all frames sent with small delays
	config := FrameRateLimitConfig{
		TargetFPS:           1000.0, // 1000 FPS = 1ms between frames
		PassthroughAudio:    true,
		PassthroughNonMedia: true,
	}
	stage := NewFrameRateLimitStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 5)
	output := make(chan StreamElement, 5)

	// Send frames with 5ms delay between - all should pass at 1000 FPS
	go func() {
		for i := range 5 {
			input <- StreamElement{
				Image: &ImageData{
					Data:     []byte{byte(i)},
					MIMEType: "image/jpeg",
				},
			}
			if i < 4 {
				time.Sleep(5 * time.Millisecond)
			}
		}
		close(input)
	}()

	go func() {
		if err := stage.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	// Drain output channel to wait for processing to complete
	for range output {
	}

	emitted, dropped := stage.GetStats()
	// At 1000 FPS (1ms intervals) with 5ms between frames, all should pass
	if emitted < 3 {
		t.Errorf("Expected at least 3 emitted frames at high FPS, got %d (dropped: %d)", emitted, dropped)
	}
}

func TestFrameRateLimitStage_ErrorPassthrough(t *testing.T) {
	config := DefaultFrameRateLimitConfig()
	stage := NewFrameRateLimitStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	testErr := errors.New("test error")
	input <- StreamElement{Error: testErr}
	close(input)

	go func() {
		if err := stage.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	result := <-output
	if result.Error == nil {
		t.Error("Error should pass through")
	}
	if result.Error.Error() != "test error" {
		t.Errorf("Expected error message 'test error', got '%s'", result.Error.Error())
	}
}

func TestFrameRateLimitStage_ContextCancellation(t *testing.T) {
	config := DefaultFrameRateLimitConfig()
	stage := NewFrameRateLimitStage(config)

	ctx, cancel := context.WithCancel(context.Background())

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement) // Unbuffered - will block on send

	input <- StreamElement{
		Image: &ImageData{
			Data:     []byte{1, 2, 3},
			MIMEType: "image/jpeg",
		},
	}
	close(input)

	// Cancel context to trigger during blocked send
	go func() {
		cancel()
	}()

	err := stage.Process(ctx, input, output)
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got: %v", err)
	}
}

func TestFrameRateLimitStage_DisableAudioPassthrough(t *testing.T) {
	config := FrameRateLimitConfig{
		TargetFPS:           1.0,
		PassthroughAudio:    false, // Disable audio passthrough
		PassthroughNonMedia: true,
	}
	stage := NewFrameRateLimitStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 2)
	output := make(chan StreamElement, 2)

	// Send audio - with passthrough disabled, it passes as non-media
	input <- StreamElement{
		Audio: &AudioData{
			Samples:  []byte{1, 2, 3},
			Encoding: "pcm",
		},
	}
	close(input)

	go func() {
		if err := stage.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	// Audio should still pass through as non-media when PassthroughNonMedia is true
	result := <-output
	if result.Audio == nil {
		t.Error("Audio should pass through (as non-media element)")
	}
}

func TestDefaultFrameRateLimitConfig(t *testing.T) {
	config := DefaultFrameRateLimitConfig()

	if config.TargetFPS != DefaultFrameRateLimitFPS {
		t.Errorf("Expected TargetFPS %f, got %f", DefaultFrameRateLimitFPS, config.TargetFPS)
	}
	if config.DropStrategy != DropStrategyKeepLatest {
		t.Errorf("Expected DropStrategy KeepLatest, got %v", config.DropStrategy)
	}
	if !config.PassthroughAudio {
		t.Error("Expected PassthroughAudio to be true")
	}
	if !config.PassthroughNonMedia {
		t.Error("Expected PassthroughNonMedia to be true")
	}
}

func TestDropStrategy_String(t *testing.T) {
	tests := []struct {
		strategy DropStrategy
		expected string
	}{
		{DropStrategyKeepLatest, "keep_latest"},
		{DropStrategyUniform, "uniform"},
		{DropStrategy(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if tt.strategy.String() != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, tt.strategy.String())
			}
		})
	}
}

func TestFrameRateLimitStage_GetStats(t *testing.T) {
	config := DefaultFrameRateLimitConfig()
	stage := NewFrameRateLimitStage(config)

	emitted, dropped := stage.GetStats()
	if emitted != 0 {
		t.Errorf("Expected 0 emitted initially, got %d", emitted)
	}
	if dropped != 0 {
		t.Errorf("Expected 0 dropped initially, got %d", dropped)
	}
}

func TestFrameRateLimitStage_MixedContent(t *testing.T) {
	config := FrameRateLimitConfig{
		TargetFPS:           1.0,
		PassthroughAudio:    true,
		PassthroughNonMedia: true,
	}
	stage := NewFrameRateLimitStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 10)
	output := make(chan StreamElement, 10)

	// Send mixed content: text, image, audio, image, message, image
	text := "hello"
	input <- StreamElement{Text: &text}
	input <- StreamElement{Image: &ImageData{Data: []byte{1}, MIMEType: "image/jpeg"}}
	input <- StreamElement{Audio: &AudioData{Samples: []byte{2}, Encoding: "pcm"}}
	input <- StreamElement{Image: &ImageData{Data: []byte{3}, MIMEType: "image/jpeg"}}
	input <- StreamElement{Message: &types.Message{Role: "user", Content: "hi"}}
	input <- StreamElement{Image: &ImageData{Data: []byte{4}, MIMEType: "image/jpeg"}}
	close(input)

	go func() {
		if err := stage.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	// Collect results
	var results []StreamElement
	for elem := range output {
		results = append(results, elem)
	}

	// Expected: text, first image, audio, message (2nd and 3rd images dropped)
	// So we should have 4 elements
	if len(results) != 4 {
		t.Errorf("Expected 4 elements (text, 1 image, audio, message), got %d", len(results))
	}

	emitted, dropped := stage.GetStats()
	if emitted != 1 {
		t.Errorf("Expected 1 emitted image frame, got %d", emitted)
	}
	if dropped != 2 {
		t.Errorf("Expected 2 dropped image frames, got %d", dropped)
	}
}
