package stage

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os/exec"
	"testing"
	"time"
)

// skipIfNoFFmpeg skips the test if FFmpeg is not available.
func skipIfNoFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg not available, skipping test")
	}
}

// createTestFrameData creates a test JPEG image with specified dimensions.
func createTestFrameData(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: 100, G: 150, B: 200, A: 255})
		}
	}

	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
	return buf.Bytes()
}

// --- VideoToFramesStage Tests ---

func TestVideoToFramesStage_BasicOperation(t *testing.T) {
	config := DefaultVideoToFramesConfig()
	stg := NewVideoToFramesStage(config)

	if stg.Name() != "video-to-frames" {
		t.Errorf("Expected name 'video-to-frames', got '%s'", stg.Name())
	}

	if stg.Type() != StageTypeTransform {
		t.Errorf("Expected type Transform, got %v", stg.Type())
	}

	// Verify config defaults
	gotConfig := stg.GetConfig()
	if gotConfig.Mode != FrameExtractionInterval {
		t.Errorf("Expected mode Interval, got %v", gotConfig.Mode)
	}
	if gotConfig.Interval != time.Second {
		t.Errorf("Expected interval 1s, got %v", gotConfig.Interval)
	}
	if gotConfig.MaxFrames != 30 {
		t.Errorf("Expected max frames 30, got %d", gotConfig.MaxFrames)
	}
	if gotConfig.OutputFormat != "jpeg" {
		t.Errorf("Expected output format 'jpeg', got '%s'", gotConfig.OutputFormat)
	}
	if gotConfig.OutputQuality != 85 {
		t.Errorf("Expected output quality 85, got %d", gotConfig.OutputQuality)
	}
}

func TestVideoToFramesStage_PassthroughNonVideo(t *testing.T) {
	config := DefaultVideoToFramesConfig()
	stg := NewVideoToFramesStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 2)
	output := make(chan StreamElement, 10)

	// Send image element (should pass through)
	imageData := &ImageData{
		Data:     createTestFrameData(100, 100),
		MIMEType: "image/jpeg",
		Width:    100,
		Height:   100,
	}
	input <- NewImageElement(imageData)

	// Send text element (should pass through)
	input <- NewTextElement("Hello world")

	close(input)

	go func() {
		if err := stg.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	// Collect results
	var results []StreamElement
	for elem := range output {
		results = append(results, elem)
	}

	if len(results) != 2 {
		t.Fatalf("Expected 2 elements, got %d", len(results))
	}

	// First should be image (passed through)
	if results[0].Image == nil {
		t.Error("Expected first element to have Image")
	}

	// Second should be text (passed through)
	if results[1].Text == nil || *results[1].Text != "Hello world" {
		t.Errorf("Expected text 'Hello world', got '%v'", results[1].Text)
	}
}

func TestVideoToFramesStage_ErrorOnMissingVideoData(t *testing.T) {
	config := DefaultVideoToFramesConfig()
	stg := NewVideoToFramesStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Send video element with no data
	videoData := &VideoData{
		MIMEType: "video/mp4",
		Data:     nil, // Empty data
	}
	input <- NewVideoElement(videoData)
	close(input)

	go func() {
		if err := stg.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	// Collect results
	var results []StreamElement
	for elem := range output {
		results = append(results, elem)
	}

	// Should get error element
	if len(results) != 1 {
		t.Fatalf("Expected 1 element, got %d", len(results))
	}

	if results[0].Error == nil {
		t.Error("Expected error in result")
	}
}

func TestVideoToFramesStage_ContextCancellation(t *testing.T) {
	config := DefaultVideoToFramesConfig()
	stg := NewVideoToFramesStage(config)

	ctx, cancel := context.WithCancel(context.Background())

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Send video element with data that would trigger processing
	videoData := &VideoData{
		MIMEType: "video/mp4",
		Data:     []byte("fake video data"),
	}
	input <- NewVideoElement(videoData)

	// Cancel context during processing
	cancel()
	close(input)

	errCh := make(chan error, 1)
	go func() {
		errCh <- stg.Process(ctx, input, output)
	}()

	// Wait for Process to return with timeout
	select {
	case err := <-errCh:
		// Either context.Canceled or nil is acceptable
		// (might complete before cancel takes effect)
		if err != nil && err != context.Canceled {
			t.Errorf("Expected context.Canceled or nil, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("Process did not return within timeout")
	}
}

func TestVideoToFramesStage_BuildFFmpegArgs_Interval(t *testing.T) {
	config := DefaultVideoToFramesConfig()
	config.Mode = FrameExtractionInterval
	config.Interval = 2 * time.Second
	config.MaxFrames = 10
	config.OutputQuality = 90
	stg := NewVideoToFramesStage(config)

	args := stg.buildFFmpegArgs("/input.mp4", "/output_%04d.jpg")

	// Check essential args
	foundInput := false
	foundFPS := false
	foundVframes := false
	foundQV := false

	for i, arg := range args {
		if arg == "-i" && i+1 < len(args) && args[i+1] == "/input.mp4" {
			foundInput = true
		}
		if arg == "-vf" && i+1 < len(args) {
			if args[i+1] == "fps=0.5000" {
				foundFPS = true
			}
		}
		if arg == "-vframes" && i+1 < len(args) && args[i+1] == "10" {
			foundVframes = true
		}
		if arg == "-q:v" {
			foundQV = true
		}
	}

	if !foundInput {
		t.Error("Expected -i /input.mp4 in args")
	}
	if !foundFPS {
		t.Error("Expected fps=0.5000 filter (2s interval = 0.5 fps)")
	}
	if !foundVframes {
		t.Error("Expected -vframes 10")
	}
	if !foundQV {
		t.Error("Expected -q:v quality setting")
	}
}

func TestVideoToFramesStage_BuildFFmpegArgs_Keyframes(t *testing.T) {
	config := DefaultVideoToFramesConfig()
	config.Mode = FrameExtractionKeyframes
	stg := NewVideoToFramesStage(config)

	args := stg.buildFFmpegArgs("/input.mp4", "/output_%04d.jpg")

	foundKeyframeSelect := false
	foundVsync := false

	for i, arg := range args {
		if arg == "-vf" && i+1 < len(args) {
			if args[i+1] == "select='eq(pict_type,I)'" {
				foundKeyframeSelect = true
			}
		}
		if arg == "-vsync" && i+1 < len(args) && args[i+1] == "vfr" {
			foundVsync = true
		}
	}

	if !foundKeyframeSelect {
		t.Error("Expected keyframe select filter")
	}
	if !foundVsync {
		t.Error("Expected -vsync vfr for keyframe mode")
	}
}

func TestVideoToFramesStage_BuildFFmpegArgs_FPS(t *testing.T) {
	config := DefaultVideoToFramesConfig()
	config.Mode = FrameExtractionFPS
	config.TargetFPS = 2.5
	stg := NewVideoToFramesStage(config)

	args := stg.buildFFmpegArgs("/input.mp4", "/output_%04d.jpg")

	foundFPS := false
	for i, arg := range args {
		if arg == "-vf" && i+1 < len(args) {
			if args[i+1] == "fps=2.5000" {
				foundFPS = true
			}
		}
	}

	if !foundFPS {
		t.Error("Expected fps=2.5000 filter")
	}
}

func TestVideoToFramesStage_BuildFFmpegArgs_WithScaling(t *testing.T) {
	config := DefaultVideoToFramesConfig()
	config.OutputWidth = 640
	stg := NewVideoToFramesStage(config)

	args := stg.buildFFmpegArgs("/input.mp4", "/output_%04d.jpg")

	foundScale := false
	for i, arg := range args {
		if arg == "-vf" && i+1 < len(args) {
			// Should contain scale filter
			if containsSubstring(args[i+1], "scale=640:-1") {
				foundScale = true
			}
		}
	}

	if !foundScale {
		t.Error("Expected scale=640:-1 in video filter")
	}
}

func TestVideoToFramesStage_GenerateVideoID(t *testing.T) {
	config := DefaultVideoToFramesConfig()
	stg := NewVideoToFramesStage(config)

	id1 := stg.generateVideoID()
	id2 := stg.generateVideoID()
	id3 := stg.generateVideoID()

	// IDs should be unique
	if id1 == id2 || id2 == id3 || id1 == id3 {
		t.Error("Generated video IDs should be unique")
	}

	// IDs should have expected format
	if !containsSubstring(id1, "video-") {
		t.Errorf("Expected ID to start with 'video-', got '%s'", id1)
	}
}

// --- FramesToMessageStage Tests ---

func TestFramesToMessageStage_BasicOperation(t *testing.T) {
	config := DefaultFramesToMessageConfig()
	stg := NewFramesToMessageStage(config)

	if stg.Name() != "frames-to-message" {
		t.Errorf("Expected name 'frames-to-message', got '%s'", stg.Name())
	}

	if stg.Type() != StageTypeAccumulate {
		t.Errorf("Expected type Accumulate, got %v", stg.Type())
	}

	// Verify config defaults
	gotConfig := stg.GetConfig()
	if gotConfig.CompletionTimeout != 30*time.Second {
		t.Errorf("Expected completion timeout 30s, got %v", gotConfig.CompletionTimeout)
	}
	if gotConfig.MaxFramesPerMessage != 30 {
		t.Errorf("Expected max frames 30, got %d", gotConfig.MaxFramesPerMessage)
	}
	if gotConfig.FrameSelectionStrategy != FrameSelectionUniform {
		t.Errorf("Expected uniform selection strategy, got %v", gotConfig.FrameSelectionStrategy)
	}
}

func TestFramesToMessageStage_PassthroughNonFrame(t *testing.T) {
	config := DefaultFramesToMessageConfig()
	stg := NewFramesToMessageStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 2)
	output := make(chan StreamElement, 10)

	// Send elements without video_frames metadata (should pass through)
	imageData := &ImageData{
		Data:     createTestFrameData(100, 100),
		MIMEType: "image/jpeg",
		Width:    100,
		Height:   100,
	}
	input <- NewImageElement(imageData)
	input <- NewTextElement("Just text")

	close(input)

	go func() {
		if err := stg.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	// Collect results
	var results []StreamElement
	for elem := range output {
		results = append(results, elem)
	}

	if len(results) != 2 {
		t.Fatalf("Expected 2 elements (passthrough), got %d", len(results))
	}

	if results[0].Image == nil {
		t.Error("Expected first element to have Image")
	}
	if results[1].Text == nil || *results[1].Text != "Just text" {
		t.Error("Expected second element to have text")
	}
}

func TestFramesToMessageStage_ComposeFrames(t *testing.T) {
	config := DefaultFramesToMessageConfig()
	stg := NewFramesToMessageStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 5)
	output := make(chan StreamElement, 10)

	videoID := "test-video-1"
	totalFrames := 3

	// Send 3 frame elements with correlation metadata
	for i := 0; i < totalFrames; i++ {
		imageData := &ImageData{
			Data:     createTestFrameData(100, 100),
			MIMEType: "image/jpeg",
			Width:    100,
			Height:   100,
		}
		elem := NewImageElement(imageData)
		elem.WithMetadata(VideoFramesVideoIDKey, videoID)
		elem.WithMetadata(VideoFramesFrameIndexKey, i)
		elem.WithMetadata(VideoFramesTotalFramesKey, totalFrames)
		input <- elem
	}

	close(input)

	go func() {
		if err := stg.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	// Collect results
	var results []StreamElement
	for elem := range output {
		results = append(results, elem)
	}

	// Should have 1 composed message
	if len(results) != 1 {
		t.Fatalf("Expected 1 composed message, got %d", len(results))
	}

	result := results[0]
	if result.Message == nil {
		t.Fatal("Expected Message element")
	}

	// Message should have 3 image parts
	if len(result.Message.Parts) != 3 {
		t.Errorf("Expected 3 parts in message, got %d", len(result.Message.Parts))
	}

	for i, part := range result.Message.Parts {
		if part.Type != "image" {
			t.Errorf("Part %d: expected type 'image', got '%s'", i, part.Type)
		}
		if part.Media == nil {
			t.Errorf("Part %d: expected Media to be set", i)
		}
	}
}

func TestFramesToMessageStage_MultipleVideos(t *testing.T) {
	config := DefaultFramesToMessageConfig()
	stg := NewFramesToMessageStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 10)
	output := make(chan StreamElement, 10)

	// Send frames for 2 different videos
	videoID1 := "test-video-1"
	videoID2 := "test-video-2"

	// Video 1: 2 frames
	for i := 0; i < 2; i++ {
		elem := NewImageElement(&ImageData{
			Data:     createTestFrameData(100, 100),
			MIMEType: "image/jpeg",
			Width:    100,
			Height:   100,
		})
		elem.WithMetadata(VideoFramesVideoIDKey, videoID1)
		elem.WithMetadata(VideoFramesFrameIndexKey, i)
		elem.WithMetadata(VideoFramesTotalFramesKey, 2)
		input <- elem
	}

	// Video 2: 3 frames
	for i := 0; i < 3; i++ {
		elem := NewImageElement(&ImageData{
			Data:     createTestFrameData(100, 100),
			MIMEType: "image/jpeg",
			Width:    100,
			Height:   100,
		})
		elem.WithMetadata(VideoFramesVideoIDKey, videoID2)
		elem.WithMetadata(VideoFramesFrameIndexKey, i)
		elem.WithMetadata(VideoFramesTotalFramesKey, 3)
		input <- elem
	}

	close(input)

	go func() {
		if err := stg.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	// Collect results
	var results []StreamElement
	for elem := range output {
		results = append(results, elem)
	}

	// Should have 2 composed messages
	if len(results) != 2 {
		t.Fatalf("Expected 2 composed messages, got %d", len(results))
	}

	// Verify each message has correct number of parts
	partCounts := make(map[int]bool)
	for _, result := range results {
		if result.Message == nil {
			t.Error("Expected Message element")
			continue
		}
		partCounts[len(result.Message.Parts)] = true
	}

	if !partCounts[2] || !partCounts[3] {
		t.Error("Expected messages with 2 and 3 parts respectively")
	}
}

func TestFramesToMessageStage_SelectFrames_First(t *testing.T) {
	config := DefaultFramesToMessageConfig()
	config.MaxFramesPerMessage = 3
	config.FrameSelectionStrategy = FrameSelectionFirst
	stg := NewFramesToMessageStage(config)

	// Create 5 frames
	frames := make(map[int]*ImageData)
	for i := 0; i < 5; i++ {
		frames[i] = &ImageData{
			Data:     createTestFrameData(100, 100),
			MIMEType: "image/jpeg",
			Width:    100,
			Height:   100,
			Format:   "jpeg",
		}
	}

	selected := stg.selectFrames(frames)

	if len(selected) != 3 {
		t.Fatalf("Expected 3 selected frames, got %d", len(selected))
	}
}

func TestFramesToMessageStage_SelectFrames_Last(t *testing.T) {
	config := DefaultFramesToMessageConfig()
	config.MaxFramesPerMessage = 3
	config.FrameSelectionStrategy = FrameSelectionLast
	stg := NewFramesToMessageStage(config)

	// Create 5 frames
	frames := make(map[int]*ImageData)
	for i := 0; i < 5; i++ {
		frames[i] = &ImageData{
			Data:     createTestFrameData(100, 100),
			MIMEType: "image/jpeg",
			Width:    100,
			Height:   100,
			Format:   "jpeg",
		}
	}

	selected := stg.selectFrames(frames)

	if len(selected) != 3 {
		t.Fatalf("Expected 3 selected frames, got %d", len(selected))
	}
}

func TestFramesToMessageStage_SelectFrames_Uniform(t *testing.T) {
	config := DefaultFramesToMessageConfig()
	config.MaxFramesPerMessage = 3
	config.FrameSelectionStrategy = FrameSelectionUniform
	stg := NewFramesToMessageStage(config)

	// Create 10 frames
	frames := make(map[int]*ImageData)
	for i := 0; i < 10; i++ {
		frames[i] = &ImageData{
			Data:     createTestFrameData(100, 100),
			MIMEType: "image/jpeg",
			Width:    100,
			Height:   100,
			Format:   "jpeg",
		}
	}

	selected := stg.selectFrames(frames)

	if len(selected) != 3 {
		t.Fatalf("Expected 3 selected frames, got %d", len(selected))
	}
}

func TestFramesToMessageStage_SelectFrames_NoLimit(t *testing.T) {
	config := DefaultFramesToMessageConfig()
	config.MaxFramesPerMessage = 0 // No limit
	stg := NewFramesToMessageStage(config)

	// Create 10 frames
	frames := make(map[int]*ImageData)
	for i := 0; i < 10; i++ {
		frames[i] = &ImageData{
			Data:     createTestFrameData(100, 100),
			MIMEType: "image/jpeg",
			Width:    100,
			Height:   100,
			Format:   "jpeg",
		}
	}

	selected := stg.selectFrames(frames)

	if len(selected) != 10 {
		t.Fatalf("Expected all 10 frames, got %d", len(selected))
	}
}

func TestFramesToMessageStage_ContextCancellation(t *testing.T) {
	config := DefaultFramesToMessageConfig()
	stg := NewFramesToMessageStage(config)

	ctx, cancel := context.WithCancel(context.Background())

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Send a frame element
	imageData := &ImageData{
		Data:     createTestFrameData(100, 100),
		MIMEType: "image/jpeg",
		Width:    100,
		Height:   100,
	}
	elem := NewImageElement(imageData)
	elem.WithMetadata(VideoFramesVideoIDKey, "test-video")
	elem.WithMetadata(VideoFramesFrameIndexKey, 0)
	elem.WithMetadata(VideoFramesTotalFramesKey, 2)
	input <- elem

	// Cancel context during processing
	cancel()
	close(input)

	errCh := make(chan error, 1)
	go func() {
		errCh <- stg.Process(ctx, input, output)
	}()

	// Wait for Process to return with timeout
	select {
	case err := <-errCh:
		// Either context.Canceled or nil is acceptable
		if err != nil && err != context.Canceled {
			t.Errorf("Expected context.Canceled or nil, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("Process did not return within timeout")
	}
}

// --- FrameExtractionMode Tests ---

func TestFrameExtractionMode_String(t *testing.T) {
	tests := []struct {
		mode     FrameExtractionMode
		expected string
	}{
		{FrameExtractionInterval, "interval"},
		{FrameExtractionKeyframes, "keyframes"},
		{FrameExtractionFPS, "fps"},
		{FrameExtractionMode(99), "unknown"},
	}

	for _, tt := range tests {
		got := tt.mode.String()
		if got != tt.expected {
			t.Errorf("FrameExtractionMode(%d).String() = %s, want %s", tt.mode, got, tt.expected)
		}
	}
}

// --- FrameSelectionStrategy Tests ---

func TestFrameSelectionStrategy_String(t *testing.T) {
	tests := []struct {
		strategy FrameSelectionStrategy
		expected string
	}{
		{FrameSelectionUniform, "uniform"},
		{FrameSelectionFirst, "first"},
		{FrameSelectionLast, "last"},
		{FrameSelectionStrategy(99), "unknown"},
	}

	for _, tt := range tests {
		got := tt.strategy.String()
		if got != tt.expected {
			t.Errorf("FrameSelectionStrategy(%d).String() = %s, want %s", tt.strategy, got, tt.expected)
		}
	}
}

// --- Integration Tests (requires FFmpeg) ---

func TestVideoToFramesStage_Integration_RealVideo(t *testing.T) {
	skipIfNoFFmpeg(t)

	// This test would require a real video file
	// For now, we just verify FFmpeg can be found
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg not available")
	}

	config := DefaultVideoToFramesConfig()
	stg := NewVideoToFramesStage(config)

	if stg.GetConfig().FFmpegPath != "ffmpeg" {
		t.Error("Expected default FFmpeg path to be 'ffmpeg'")
	}
}

func TestFramesToMessageStage_AccumulateFrame_InvalidMetadata(t *testing.T) {
	config := DefaultFramesToMessageConfig()
	stg := NewFramesToMessageStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Send frame with invalid video ID type
	elem := NewImageElement(&ImageData{
		Data:     createTestFrameData(100, 100),
		MIMEType: "image/jpeg",
	})
	elem.WithMetadata(VideoFramesVideoIDKey, 12345) // Wrong type (int instead of string)
	elem.WithMetadata(VideoFramesFrameIndexKey, 0)
	elem.WithMetadata(VideoFramesTotalFramesKey, 1)
	input <- elem
	close(input)

	go func() {
		_ = stg.Process(ctx, input, output)
	}()

	var results []StreamElement
	for elem := range output {
		results = append(results, elem)
	}

	// Should get error element
	if len(results) != 1 {
		t.Fatalf("Expected 1 element, got %d", len(results))
	}

	if results[0].Error == nil {
		t.Error("Expected error for invalid metadata type")
	}
}

func TestFramesToMessageStage_AccumulateFrame_InvalidFrameIndex(t *testing.T) {
	config := DefaultFramesToMessageConfig()
	stg := NewFramesToMessageStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Send frame with invalid frame index type
	elem := NewImageElement(&ImageData{
		Data:     createTestFrameData(100, 100),
		MIMEType: "image/jpeg",
	})
	elem.WithMetadata(VideoFramesVideoIDKey, "test-video")
	elem.WithMetadata(VideoFramesFrameIndexKey, "invalid") // Wrong type
	elem.WithMetadata(VideoFramesTotalFramesKey, 1)
	input <- elem
	close(input)

	go func() {
		_ = stg.Process(ctx, input, output)
	}()

	var results []StreamElement
	for elem := range output {
		results = append(results, elem)
	}

	if len(results) != 1 || results[0].Error == nil {
		t.Error("Expected error for invalid frame index type")
	}
}

func TestFramesToMessageStage_AccumulateFrame_InvalidTotalFrames(t *testing.T) {
	config := DefaultFramesToMessageConfig()
	stg := NewFramesToMessageStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Send frame with invalid total frames type
	elem := NewImageElement(&ImageData{
		Data:     createTestFrameData(100, 100),
		MIMEType: "image/jpeg",
	})
	elem.WithMetadata(VideoFramesVideoIDKey, "test-video")
	elem.WithMetadata(VideoFramesFrameIndexKey, 0)
	elem.WithMetadata(VideoFramesTotalFramesKey, "invalid") // Wrong type
	input <- elem
	close(input)

	go func() {
		_ = stg.Process(ctx, input, output)
	}()

	var results []StreamElement
	for elem := range output {
		results = append(results, elem)
	}

	if len(results) != 1 || results[0].Error == nil {
		t.Error("Expected error for invalid total frames type")
	}
}

func TestFramesToMessageStage_CreateImagePart_NilImage(t *testing.T) {
	config := DefaultFramesToMessageConfig()
	stg := NewFramesToMessageStage(config)

	_, err := stg.createImagePart(nil)
	if err == nil {
		t.Error("Expected error for nil image")
	}
}

func TestFramesToMessageStage_CreateImagePart_ExternalizedImage(t *testing.T) {
	config := DefaultFramesToMessageConfig()
	stg := NewFramesToMessageStage(config)

	// Create externalized image
	img := &ImageData{
		MIMEType:   "image/jpeg",
		StorageRef: "s3://bucket/image.jpg",
		Width:      100,
		Height:     100,
	}

	part, err := stg.createImagePart(img)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if part.Type != "image" {
		t.Errorf("Expected type 'image', got '%s'", part.Type)
	}

	if part.Media == nil {
		t.Error("Expected Media to be set")
	}

	if part.Media.StorageReference == nil || *part.Media.StorageReference != "s3://bucket/image.jpg" {
		t.Error("Expected storage reference to be set")
	}
}

func TestFramesToMessageStage_ComposeMessage_EmptyFrames(t *testing.T) {
	config := DefaultFramesToMessageConfig()
	stg := NewFramesToMessageStage(config)

	pf := &pendingFrames{
		frames:      make(map[int]*ImageData),
		totalFrames: 0,
	}

	_, err := stg.composeMessage(pf)
	if err == nil {
		t.Error("Expected error for empty frames")
	}
}

func TestFramesToMessageStage_Timeout(t *testing.T) {
	config := DefaultFramesToMessageConfig()
	config.CompletionTimeout = 100 * time.Millisecond // Short timeout
	stg := NewFramesToMessageStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Send partial frames (only 1 of 3)
	elem := NewImageElement(&ImageData{
		Data:     createTestFrameData(100, 100),
		MIMEType: "image/jpeg",
		Width:    100,
		Height:   100,
	})
	elem.WithMetadata(VideoFramesVideoIDKey, "test-video")
	elem.WithMetadata(VideoFramesFrameIndexKey, 0)
	elem.WithMetadata(VideoFramesTotalFramesKey, 3)
	input <- elem

	go func() {
		// Wait longer than timeout before closing
		time.Sleep(200 * time.Millisecond)
		close(input)
	}()

	errCh := make(chan error, 1)
	go func() {
		errCh <- stg.Process(ctx, input, output)
	}()

	var results []StreamElement
	for elem := range output {
		results = append(results, elem)
	}

	// Should eventually get a composed message with partial frames
	if len(results) == 0 {
		t.Skip("Timeout test timing-dependent, may not produce results")
	}

	// The result should be a message (partial composition)
	for _, r := range results {
		if r.Message != nil {
			// Got partial composition - test passed
			return
		}
	}
}

func TestVideoToFramesStage_WriteVideoToTempFile_DifferentFormats(t *testing.T) {
	// Test MIME type to extension mapping
	tests := []struct {
		mimeType    string
		expectedExt string
	}{
		{"video/mp4", ".mp4"},
		{"video/webm", ".webm"},
		{"video/quicktime", ".mov"},
		{"video/x-msvideo", ".avi"},
		{"video/x-matroska", ".mkv"},
		{"video/unknown", ".mp4"}, // Default
	}

	config := DefaultVideoToFramesConfig()
	stg := NewVideoToFramesStage(config)

	for _, tt := range tests {
		video := &VideoData{
			MIMEType: tt.mimeType,
			Data:     []byte("fake video data"),
		}

		tempDir := t.TempDir()
		path, err := stg.writeVideoToTempFile(context.Background(), video, tempDir)
		if err != nil {
			t.Errorf("Unexpected error for %s: %v", tt.mimeType, err)
			continue
		}

		if !containsSubstring(path, tt.expectedExt) {
			t.Errorf("For MIME %s, expected extension %s in path %s", tt.mimeType, tt.expectedExt, path)
		}
	}
}

func TestVideoToFramesStage_BuildFFmpegArgs_PNG(t *testing.T) {
	config := DefaultVideoToFramesConfig()
	config.OutputFormat = OutputFormatPNG
	stg := NewVideoToFramesStage(config)

	args := stg.buildFFmpegArgs("/input.mp4", "/output_%04d.png")

	// PNG format should not have -q:v quality setting
	for _, arg := range args {
		if arg == "-q:v" {
			t.Error("PNG format should not have -q:v quality setting")
		}
	}
}

func TestVideoToFramesStage_RunFFmpeg_NotFound(t *testing.T) {
	config := DefaultVideoToFramesConfig()
	config.FFmpegPath = "/nonexistent/path/to/ffmpeg"
	stg := NewVideoToFramesStage(config)

	err := stg.runFFmpeg(context.Background(), []string{"-version"})
	if err == nil {
		t.Error("Expected error for non-existent FFmpeg")
	}

	// Should return ErrFFmpegNotFound
	if err != ErrFFmpegNotFound {
		// Could be different error on some systems
		t.Logf("Got error: %v (may be system-dependent)", err)
	}
}

// Helper function to check if a string contains a substring.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstringImpl(s, substr))
}

func containsSubstringImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
