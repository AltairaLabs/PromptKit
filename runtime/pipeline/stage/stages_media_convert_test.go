package stage

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/media"
)

func TestNewMediaConvertStage(t *testing.T) {
	t.Run("with default config", func(t *testing.T) {
		config := DefaultMediaConvertConfig()
		stage := NewMediaConvertStage(&config)

		if stage == nil {
			t.Fatal("expected non-nil stage")
		}
		if stage.Name() != "media-convert" {
			t.Errorf("expected name 'media-convert', got %q", stage.Name())
		}
		if stage.Type() != StageTypeTransform {
			t.Errorf("expected type StageTypeTransform, got %v", stage.Type())
		}
		if !stage.config.PassthroughOnError {
			t.Error("expected PassthroughOnError to be true by default")
		}
	})

	t.Run("with custom config", func(t *testing.T) {
		config := MediaConvertConfig{
			TargetAudioFormats: []string{media.MIMETypeAudioWAV, media.MIMETypeAudioMP3},
			PassthroughOnError: false,
		}
		stage := NewMediaConvertStage(&config)

		if len(stage.config.TargetAudioFormats) != 2 {
			t.Errorf("expected 2 target audio formats, got %d", len(stage.config.TargetAudioFormats))
		}
		if stage.config.PassthroughOnError {
			t.Error("expected PassthroughOnError to be false")
		}
	})
}

func TestMediaConvertStage_Process_NoConversionNeeded(t *testing.T) {
	config := MediaConvertConfig{
		TargetAudioFormats: []string{media.MIMETypeAudioWAV},
		PassthroughOnError: true,
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create audio element already in target format
	audioData := &AudioData{
		Samples:    []byte("test audio data"),
		SampleRate: 16000,
		Channels:   1,
		Format:     AudioFormatPCM16, // This maps to WAV
	}
	input <- NewAudioElement(audioData)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	if result.Audio == nil {
		t.Fatal("expected audio data in result")
	}
	if result.Error != nil {
		t.Errorf("unexpected error in result: %v", result.Error)
	}
	// Data should be unchanged since no conversion was needed
	if string(result.Audio.Samples) != "test audio data" {
		t.Error("audio data was modified when it shouldn't have been")
	}
}

func TestMediaConvertStage_Process_TextPassthrough(t *testing.T) {
	config := MediaConvertConfig{
		TargetAudioFormats: []string{media.MIMETypeAudioWAV},
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create text element (should pass through unchanged)
	input <- NewTextElement("Hello, world!")
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	if result.Text == nil {
		t.Fatal("expected text in result")
	}
	if *result.Text != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", *result.Text)
	}
}

func TestMediaConvertStage_Process_NoTargetFormats(t *testing.T) {
	// When no target formats specified, audio should pass through unchanged
	config := MediaConvertConfig{}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	audioData := &AudioData{
		Samples:    []byte("test audio"),
		Format:     AudioFormatMP3,
	}
	input <- NewAudioElement(audioData)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	if result.Audio == nil {
		t.Fatal("expected audio in result")
	}
	if result.Audio.Format != AudioFormatMP3 {
		t.Errorf("expected format to remain MP3, got %v", result.Audio.Format)
	}
}

func TestMediaConvertStage_Process_ContextCancellation(t *testing.T) {
	config := DefaultMediaConvertConfig()
	stage := NewMediaConvertStage(&config)

	ctx, cancel := context.WithCancel(context.Background())
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement) // Unbuffered - will block on send

	// Send an element first
	input <- NewTextElement("test")

	// Start processing in goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- stage.Process(ctx, input, output)
	}()

	// Cancel context while Process is trying to send to blocked output
	cancel()

	// Close input to allow Process to complete
	close(input)

	// Wait for the error
	err := <-errChan

	// Should get context.Canceled (or nil if it completed before cancel)
	// The important thing is it doesn't hang
	if err != nil && err != context.Canceled {
		t.Errorf("expected context.Canceled or nil, got %v", err)
	}
}

func TestMediaConvertStage_InputCapabilities(t *testing.T) {
	config := DefaultMediaConvertConfig()
	stage := NewMediaConvertStage(&config)

	caps := stage.InputCapabilities()

	// Should accept any content type
	expectedTypes := []ContentType{ContentTypeAudio, ContentTypeImage, ContentTypeVideo, ContentTypeAny}
	if len(caps.ContentTypes) != len(expectedTypes) {
		t.Errorf("expected %d content types, got %d", len(expectedTypes), len(caps.ContentTypes))
	}
}

func TestMediaConvertStage_OutputCapabilities(t *testing.T) {
	t.Run("no target formats", func(t *testing.T) {
		config := DefaultMediaConvertConfig()
		stage := NewMediaConvertStage(&config)

		caps := stage.OutputCapabilities()

		if caps.Audio != nil {
			t.Error("expected nil audio capability when no target formats")
		}
	})

	t.Run("with target audio formats", func(t *testing.T) {
		config := MediaConvertConfig{
			TargetAudioFormats: []string{media.MIMETypeAudioWAV, media.MIMETypeAudioMP3},
		}
		stage := NewMediaConvertStage(&config)

		caps := stage.OutputCapabilities()

		if caps.Audio == nil {
			t.Fatal("expected audio capability")
		}
		if len(caps.Audio.Formats) != 2 {
			t.Errorf("expected 2 audio formats, got %d", len(caps.Audio.Formats))
		}
	})
}

func TestMediaConvertStage_GetConfig(t *testing.T) {
	config := MediaConvertConfig{
		TargetAudioFormats: []string{media.MIMETypeAudioWAV},
		PassthroughOnError: false,
	}
	stage := NewMediaConvertStage(&config)

	retrievedConfig := stage.GetConfig()

	if len(retrievedConfig.TargetAudioFormats) != 1 {
		t.Errorf("expected 1 target audio format, got %d", len(retrievedConfig.TargetAudioFormats))
	}
	if retrievedConfig.PassthroughOnError {
		t.Error("expected PassthroughOnError to be false")
	}
}

func TestAudioFormatToMIMEType(t *testing.T) {
	tests := []struct {
		format   AudioFormat
		expected string
	}{
		{AudioFormatPCM16, media.MIMETypeAudioWAV},
		{AudioFormatFloat32, media.MIMETypeAudioWAV},
		{AudioFormatOpus, media.MIMETypeAudioOGG},
		{AudioFormatMP3, media.MIMETypeAudioMP3},
		{AudioFormatAAC, media.MIMETypeAudioAAC},
		{AudioFormat(999), media.MIMETypeAudioWAV}, // unknown defaults to WAV
	}

	for _, tt := range tests {
		t.Run(tt.format.String(), func(t *testing.T) {
			result := audioFormatToMIMEType(tt.format)
			if result != tt.expected {
				t.Errorf("audioFormatToMIMEType(%v) = %q, expected %q", tt.format, result, tt.expected)
			}
		})
	}
}

func TestMimeTypeToStageAudioFormat(t *testing.T) {
	tests := []struct {
		mimeType string
		expected AudioFormat
	}{
		{media.MIMETypeAudioWAV, AudioFormatPCM16},
		{"audio/x-wav", AudioFormatPCM16},
		{media.MIMETypeAudioMP3, AudioFormatMP3},
		{media.MIMETypeAudioOGG, AudioFormatOpus},
		{media.MIMETypeAudioAAC, AudioFormatAAC},
		{media.MIMETypeAudioM4A, AudioFormatAAC},
		{"unknown", AudioFormatPCM16}, // unknown defaults to PCM16
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			result := mimeTypeToStageAudioFormat(tt.mimeType)
			if result != tt.expected {
				t.Errorf("mimeTypeToStageAudioFormat(%q) = %v, expected %v", tt.mimeType, result, tt.expected)
			}
		})
	}
}

func TestMediaConvertStage_ImagePassthrough(t *testing.T) {
	config := MediaConvertConfig{
		TargetImageFormats: []string{"image/jpeg"},
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Image in target format should pass through
	imageData := &ImageData{
		Data:     []byte("fake jpeg data"),
		MIMEType: "image/jpeg",
	}
	input <- NewImageElement(imageData)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	if result.Image == nil {
		t.Fatal("expected image in result")
	}
	if result.Error != nil {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestMediaConvertStage_VideoConversionNotImplemented(t *testing.T) {
	config := MediaConvertConfig{
		TargetVideoFormats: []string{"video/mp4"},
		PassthroughOnError: true,
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Video in different format triggers conversion attempt (which will fail)
	videoData := &VideoData{
		Data:     []byte("fake webm data"),
		MIMEType: "video/webm",
	}
	input <- NewVideoElement(videoData)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	// With PassthroughOnError=true, should pass through despite conversion failure
	if result.Video == nil {
		t.Fatal("expected video in result (passthrough)")
	}
	// Error should not be set when PassthroughOnError is true
	if result.Error != nil {
		t.Errorf("expected no error with PassthroughOnError, got: %v", result.Error)
	}
}

func TestMediaConvertStage_VideoConversionErrorPropagation(t *testing.T) {
	config := MediaConvertConfig{
		TargetVideoFormats: []string{"video/mp4"},
		PassthroughOnError: false, // Errors should propagate
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	videoData := &VideoData{
		Data:     []byte("fake webm data"),
		MIMEType: "video/webm",
	}
	input <- NewVideoElement(videoData)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected process error: %v", err)
	}

	result := <-output
	// With PassthroughOnError=false, error should be in the element
	if result.Error == nil {
		t.Error("expected error in element when PassthroughOnError is false")
	}
}

func TestDefaultMediaConvertConfig(t *testing.T) {
	config := DefaultMediaConvertConfig()

	if !config.PassthroughOnError {
		t.Error("expected PassthroughOnError to be true by default")
	}
	if len(config.TargetAudioFormats) != 0 {
		t.Error("expected empty TargetAudioFormats by default")
	}
	if len(config.TargetImageFormats) != 0 {
		t.Error("expected empty TargetImageFormats by default")
	}
	if len(config.TargetVideoFormats) != 0 {
		t.Error("expected empty TargetVideoFormats by default")
	}
}

func TestMediaConvertStage_Process_MultipleElements(t *testing.T) {
	config := MediaConvertConfig{
		TargetAudioFormats: []string{media.MIMETypeAudioWAV},
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 3)
	output := make(chan StreamElement, 3)

	// Send multiple elements
	input <- NewTextElement("text1")
	input <- NewAudioElement(&AudioData{
		Samples: []byte("audio"),
		Format:  AudioFormatPCM16,
	})
	input <- NewTextElement("text2")
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Collect results
	var results []StreamElement
	for elem := range output {
		results = append(results, elem)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(results))
	}
}

func TestMediaConvertStage_Process_EmptyInput(t *testing.T) {
	config := DefaultMediaConvertConfig()
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement)
	output := make(chan StreamElement, 1)

	// Close input immediately (no elements)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Output should be closed with no elements
	_, ok := <-output
	if ok {
		t.Error("expected output channel to be closed immediately")
	}
}

func TestMediaConvertStage_Process_ErrorElement(t *testing.T) {
	config := DefaultMediaConvertConfig()
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Send error element
	input <- NewErrorElement(context.DeadlineExceeded)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	if result.Error != context.DeadlineExceeded {
		t.Error("error element should pass through unchanged")
	}
}

func TestMediaConvertStage_Process_EndOfStreamElement(t *testing.T) {
	config := DefaultMediaConvertConfig()
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Send end of stream
	input <- NewEndOfStreamElement()
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	if !result.EndOfStream {
		t.Error("EndOfStream element should pass through unchanged")
	}
}

func TestMediaConvertStage_ConvertAudioElement_NoData(t *testing.T) {
	config := MediaConvertConfig{
		TargetAudioFormats: []string{media.MIMETypeAudioMP3},
		PassthroughOnError: false,
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Audio with no samples
	audioData := &AudioData{
		Samples: []byte{}, // Empty
		Format:  AudioFormatPCM16,
	}
	input <- NewAudioElement(audioData)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	if result.Error == nil {
		t.Error("expected error for audio with no data")
	}
}

func TestMediaConvertStage_ImageAlreadySupported(t *testing.T) {
	config := MediaConvertConfig{
		TargetImageFormats: []string{"image/png", "image/jpeg"},
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	imageData := &ImageData{
		Data:     []byte("png data"),
		MIMEType: "image/png",
	}
	input <- NewImageElement(imageData)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	if result.Error != nil {
		t.Errorf("unexpected error: %v", result.Error)
	}
	if result.Image.MIMEType != "image/png" {
		t.Error("image should be unchanged when already supported")
	}
}

func TestMediaConvertStage_VideoAlreadySupported(t *testing.T) {
	config := MediaConvertConfig{
		TargetVideoFormats: []string{"video/mp4", "video/webm"},
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	videoData := &VideoData{
		Data:     []byte("mp4 data"),
		MIMEType: "video/mp4",
	}
	input <- NewVideoElement(videoData)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	if result.Error != nil {
		t.Errorf("unexpected error: %v", result.Error)
	}
	if result.Video.MIMEType != "video/mp4" {
		t.Error("video should be unchanged when already supported")
	}
}

func TestMediaConvertStage_MimeTypeToAudioFormat(t *testing.T) {
	tests := []struct {
		mimeType string
		expected AudioFormat
	}{
		{media.MIMETypeAudioWAV, AudioFormatPCM16},
		{media.MIMETypeAudioMP3, AudioFormatMP3},
		{media.MIMETypeAudioOGG, AudioFormatOpus},
		{"audio/aac", AudioFormatPCM16}, // Fallback for AAC (not in switch)
		{"unknown", AudioFormatPCM16},
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			result := mimeTypeToAudioFormat(tt.mimeType)
			if result != tt.expected {
				t.Errorf("mimeTypeToAudioFormat(%q) = %v, expected %v", tt.mimeType, result, tt.expected)
			}
		})
	}
}

func TestMediaConvertStage_StageType(t *testing.T) {
	config := DefaultMediaConvertConfig()
	stage := NewMediaConvertStage(&config)

	if stage.Type() != StageTypeTransform {
		t.Errorf("expected StageTypeTransform, got %v", stage.Type())
	}
}

func TestMediaConvertStage_NilAudio(t *testing.T) {
	config := MediaConvertConfig{
		TargetAudioFormats: []string{media.MIMETypeAudioWAV},
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()

	// Create element with nil audio
	elem := StreamElement{
		Audio: nil,
	}

	// This should not panic and return the element unchanged
	result := stage.convertElement(ctx, &elem)
	if result.Audio != nil {
		t.Error("nil audio should remain nil")
	}
}

func TestMediaConvertStage_NilImage(t *testing.T) {
	config := MediaConvertConfig{
		TargetImageFormats: []string{"image/jpeg"},
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()

	elem := StreamElement{
		Image: nil,
	}

	result := stage.convertElement(ctx, &elem)
	if result.Image != nil {
		t.Error("nil image should remain nil")
	}
}

func TestMediaConvertStage_NilVideo(t *testing.T) {
	config := MediaConvertConfig{
		TargetVideoFormats: []string{"video/mp4"},
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()

	elem := StreamElement{
		Video: nil,
	}

	result := stage.convertElement(ctx, &elem)
	if result.Video != nil {
		t.Error("nil video should remain nil")
	}
}
