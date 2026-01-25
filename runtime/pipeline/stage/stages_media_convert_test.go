package stage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/media"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// createMinimalWAV creates a minimal valid WAV file for testing.
// Returns a valid WAV byte slice that can be processed by ffmpeg.
func createMinimalWAV() []byte {
	// Create a minimal valid WAV file with PCM audio
	// Header: 44 bytes + data
	sampleRate := uint32(16000)
	bitsPerSample := uint16(16)
	numChannels := uint16(1)
	numSamples := 1600 // 0.1 seconds at 16kHz

	// Create sample data (silence)
	dataSize := numSamples * int(bitsPerSample/8) * int(numChannels)
	data := make([]byte, dataSize)

	// Create WAV header
	wav := make([]byte, 0, 44+dataSize)

	// RIFF header
	wav = append(wav, []byte("RIFF")...)
	fileSize := make([]byte, 4)
	binary.LittleEndian.PutUint32(fileSize, uint32(36+dataSize))
	wav = append(wav, fileSize...)
	wav = append(wav, []byte("WAVE")...)

	// fmt subchunk
	wav = append(wav, []byte("fmt ")...)
	subchunk1Size := make([]byte, 4)
	binary.LittleEndian.PutUint32(subchunk1Size, 16) // PCM
	wav = append(wav, subchunk1Size...)

	audioFormat := make([]byte, 2)
	binary.LittleEndian.PutUint16(audioFormat, 1) // PCM
	wav = append(wav, audioFormat...)

	channels := make([]byte, 2)
	binary.LittleEndian.PutUint16(channels, numChannels)
	wav = append(wav, channels...)

	sampleRateBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(sampleRateBytes, sampleRate)
	wav = append(wav, sampleRateBytes...)

	byteRate := make([]byte, 4)
	binary.LittleEndian.PutUint32(byteRate, sampleRate*uint32(numChannels)*uint32(bitsPerSample)/8)
	wav = append(wav, byteRate...)

	blockAlign := make([]byte, 2)
	binary.LittleEndian.PutUint16(blockAlign, numChannels*bitsPerSample/8)
	wav = append(wav, blockAlign...)

	bps := make([]byte, 2)
	binary.LittleEndian.PutUint16(bps, bitsPerSample)
	wav = append(wav, bps...)

	// data subchunk
	wav = append(wav, []byte("data")...)
	dataSizeBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(dataSizeBytes, uint32(dataSize))
	wav = append(wav, dataSizeBytes...)
	wav = append(wav, data...)

	return wav
}

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

func TestMediaConvertStage_MessageWithAudioParts(t *testing.T) {
	config := MediaConvertConfig{
		TargetAudioFormats:   []string{media.MIMETypeAudioWAV},
		AudioConverterConfig: media.DefaultAudioConverterConfig(),
		PassthroughOnError:   true,
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create message with audio part already in target format
	audioData := base64.StdEncoding.EncodeToString([]byte("fake wav audio"))
	msg := &types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			types.NewTextPart("Hello"),
			{
				Type: types.ContentTypeAudio,
				Media: &types.MediaContent{
					Data:     &audioData,
					MIMEType: media.MIMETypeAudioWAV, // Already in target format
				},
			},
		},
	}
	input <- NewMessageElement(msg)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	if result.Message == nil {
		t.Fatal("expected message in result")
	}
	if len(result.Message.Parts) != 2 {
		t.Errorf("expected 2 parts, got %d", len(result.Message.Parts))
	}
	// Audio should be unchanged since it's already in target format
	if result.Message.Parts[1].Media.MIMEType != media.MIMETypeAudioWAV {
		t.Errorf("expected MIME type %s, got %s", media.MIMETypeAudioWAV, result.Message.Parts[1].Media.MIMEType)
	}
}

func TestMediaConvertStage_MessageWithoutParts(t *testing.T) {
	config := MediaConvertConfig{
		TargetAudioFormats: []string{media.MIMETypeAudioWAV},
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create message with no parts (text-only)
	msg := &types.Message{
		Role:    "user",
		Content: "Hello, world!",
		Parts:   nil,
	}
	input <- NewMessageElement(msg)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	if result.Message == nil {
		t.Fatal("expected message in result")
	}
	if result.Message.GetContent() != "Hello, world!" {
		t.Errorf("expected content 'Hello, world!', got %q", result.Message.GetContent())
	}
}

func TestMediaConvertStage_MessageWithNilMedia(t *testing.T) {
	config := MediaConvertConfig{
		TargetAudioFormats: []string{media.MIMETypeAudioWAV},
		PassthroughOnError: true,
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create message with audio part but nil media
	msg := &types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type:  types.ContentTypeAudio,
				Media: nil, // nil media
			},
		},
	}
	input <- NewMessageElement(msg)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	if result.Message == nil {
		t.Fatal("expected message in result")
	}
	// Should pass through without error
	if result.Error != nil {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestMediaConvertStage_MessageWithNilData(t *testing.T) {
	config := MediaConvertConfig{
		TargetAudioFormats: []string{media.MIMETypeAudioWAV},
		PassthroughOnError: true,
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create message with audio part but nil data
	msg := &types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeAudio,
				Media: &types.MediaContent{
					Data:     nil, // nil data
					MIMEType: "audio/mp3",
				},
			},
		},
	}
	input <- NewMessageElement(msg)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	if result.Message == nil {
		t.Fatal("expected message in result")
	}
	// Should pass through without error (nil data is handled gracefully)
	if result.Error != nil {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestMediaConvertStage_MessageWithEmptyMIMEType(t *testing.T) {
	config := MediaConvertConfig{
		TargetAudioFormats: []string{media.MIMETypeAudioWAV},
		PassthroughOnError: true,
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create message with audio part but empty MIME type
	audioData := base64.StdEncoding.EncodeToString([]byte("fake audio"))
	msg := &types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeAudio,
				Media: &types.MediaContent{
					Data:     &audioData,
					MIMEType: "", // empty MIME type
				},
			},
		},
	}
	input <- NewMessageElement(msg)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	if result.Message == nil {
		t.Fatal("expected message in result")
	}
	// Should pass through without error (empty MIME type logs warning but continues)
	if result.Error != nil {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestMediaConvertStage_MessageWithTextAndImageParts(t *testing.T) {
	config := MediaConvertConfig{
		TargetImageFormats: []string{"image/jpeg"},
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create message with text and image parts
	imageData := base64.StdEncoding.EncodeToString([]byte("fake image"))
	msg := &types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			types.NewTextPart("Look at this image"),
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					Data:     &imageData,
					MIMEType: "image/png", // Different format, but image conversion not implemented
				},
			},
		},
	}
	input <- NewMessageElement(msg)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	if result.Message == nil {
		t.Fatal("expected message in result")
	}
	// Image conversion not implemented, should pass through
	if len(result.Message.Parts) != 2 {
		t.Errorf("expected 2 parts, got %d", len(result.Message.Parts))
	}
}

func TestMediaConvertStage_MessageNoTargetFormats(t *testing.T) {
	// When no target formats specified, message audio should pass through unchanged
	config := MediaConvertConfig{
		PassthroughOnError: true,
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	audioData := base64.StdEncoding.EncodeToString([]byte("fake mp3 audio"))
	msg := &types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeAudio,
				Media: &types.MediaContent{
					Data:     &audioData,
					MIMEType: media.MIMETypeAudioMP3,
				},
			},
		},
	}
	input <- NewMessageElement(msg)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	if result.Message == nil {
		t.Fatal("expected message in result")
	}
	// Audio should be unchanged when no target formats
	if result.Message.Parts[0].Media.MIMEType != media.MIMETypeAudioMP3 {
		t.Errorf("expected MIME type %s, got %s", media.MIMETypeAudioMP3, result.Message.Parts[0].Media.MIMEType)
	}
}

func TestMediaConvertStage_AudioFormatToMIMEType(t *testing.T) {
	tests := []struct {
		format   AudioFormat
		expected string
	}{
		{AudioFormatPCM16, media.MIMETypeAudioWAV},
		{AudioFormatFloat32, media.MIMETypeAudioWAV},
		{AudioFormatOpus, media.MIMETypeAudioOGG},
		{AudioFormatMP3, media.MIMETypeAudioMP3},
		{AudioFormatAAC, media.MIMETypeAudioAAC},
		{AudioFormat(999), media.MIMETypeAudioWAV}, // unknown format defaults to WAV
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("format_%d", tt.format), func(t *testing.T) {
			result := audioFormatToMIMEType(tt.format)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestMediaConvertStage_MimeTypeToStageAudioFormat(t *testing.T) {
	tests := []struct {
		mimeType string
		expected AudioFormat
	}{
		{media.MIMETypeAudioWAV, AudioFormatPCM16},
		{media.MIMETypeAudioXWAV, AudioFormatPCM16},
		{media.MIMETypeAudioMP3, AudioFormatMP3},
		{media.MIMETypeAudioOGG, AudioFormatOpus},
		{media.MIMETypeAudioAAC, AudioFormatAAC},
		{media.MIMETypeAudioM4A, AudioFormatAAC},
		{"audio/unknown", AudioFormatPCM16}, // unknown defaults to PCM16
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			result := mimeTypeToStageAudioFormat(tt.mimeType)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestMediaConvertStage_MessageWithInvalidBase64(t *testing.T) {
	config := MediaConvertConfig{
		TargetAudioFormats: []string{media.MIMETypeAudioWAV},
		PassthroughOnError: true,
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create message with invalid base64 data
	invalidData := "not-valid-base64!!!"
	msg := &types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeAudio,
				Media: &types.MediaContent{
					Data:     &invalidData,
					MIMEType: media.MIMETypeAudioMP3, // Not in target, so needs conversion
				},
			},
		},
	}
	input <- NewMessageElement(msg)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	if result.Message == nil {
		t.Fatal("expected message in result")
	}
	// With PassthroughOnError=true, should not have an error on element
	if result.Error != nil {
		t.Errorf("expected passthrough, got error: %v", result.Error)
	}
}

func TestMediaConvertStage_MessageWithInvalidBase64_ErrorPropagation(t *testing.T) {
	config := MediaConvertConfig{
		TargetAudioFormats: []string{media.MIMETypeAudioWAV},
		PassthroughOnError: false, // Propagate errors
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create message with invalid base64 data
	invalidData := "not-valid-base64!!!"
	msg := &types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeAudio,
				Media: &types.MediaContent{
					Data:     &invalidData,
					MIMEType: media.MIMETypeAudioMP3, // Not in target, so needs conversion
				},
			},
		},
	}
	input <- NewMessageElement(msg)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected processing error: %v", err)
	}

	result := <-output
	if result.Message == nil {
		t.Fatal("expected message in result")
	}
	// With PassthroughOnError=false, should have an error on element
	if result.Error == nil {
		t.Error("expected error to be propagated")
	}
}

func TestMediaConvertStage_ImageConversionInvalidData(t *testing.T) {
	config := MediaConvertConfig{
		TargetImageFormats: []string{"image/jpeg"},
		PassthroughOnError: false, // Propagate errors
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create element with invalid image data
	elem := StreamElement{
		Image: &ImageData{
			Data:     []byte("fake image data"),
			MIMEType: "image/png", // Different from target
		},
	}
	input <- elem
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	// With PassthroughOnError=false, error should be propagated for invalid data
	if result.Error == nil {
		t.Error("expected error for invalid image data")
	}
}

func TestMediaConvertStage_ImageConversionSuccess(t *testing.T) {
	config := MediaConvertConfig{
		TargetImageFormats: []string{media.MIMETypeJPEG},
		ImageResizeConfig:  media.DefaultImageResizeConfig(),
		PassthroughOnError: false,
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create a minimal valid PNG image (1x1 red pixel)
	pngData := createMinimalPNG()

	elem := StreamElement{
		Image: &ImageData{
			Data:     pngData,
			MIMEType: media.MIMETypePNG, // Will be converted to JPEG
			Width:    1,
			Height:   1,
		},
	}
	input <- elem
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Image == nil {
		t.Fatal("expected image in result")
	}
	// Image should have been converted to JPEG
	if result.Image.MIMEType != media.MIMETypeJPEG {
		t.Errorf("expected MIME type %s, got %s", media.MIMETypeJPEG, result.Image.MIMEType)
	}
}

func TestMediaConvertStage_MessageImageConversionSuccess(t *testing.T) {
	config := MediaConvertConfig{
		TargetImageFormats: []string{media.MIMETypeJPEG},
		ImageResizeConfig:  media.DefaultImageResizeConfig(),
		PassthroughOnError: false,
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create a minimal valid PNG image
	pngData := createMinimalPNG()
	pngBase64 := base64.StdEncoding.EncodeToString(pngData)

	msg := &types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					Data:     &pngBase64,
					MIMEType: media.MIMETypePNG, // Will be converted to JPEG
				},
			},
		},
	}
	input <- NewMessageElement(msg)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	if result.Message == nil {
		t.Fatal("expected message in result")
	}
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	// Image should have been converted to JPEG
	if len(result.Message.Parts) != 1 {
		t.Errorf("expected 1 part, got %d", len(result.Message.Parts))
	}
	if result.Message.Parts[0].Media.MIMEType != media.MIMETypeJPEG {
		t.Errorf("expected MIME type %s, got %s", media.MIMETypeJPEG, result.Message.Parts[0].Media.MIMEType)
	}
}

// createMinimalPNG creates a minimal valid PNG image for testing.
func createMinimalPNG() []byte {
	// Create a 10x10 red image
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	red := color.RGBA{255, 0, 0, 255}
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, red)
		}
	}

	// Encode to PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func TestMediaConvertStage_MessageAudioConversionSuccess(t *testing.T) {
	// Skip if ffmpeg is not available
	converter := media.NewAudioConverter(media.DefaultAudioConverterConfig())
	if converter == nil {
		t.Skip("ffmpeg not available, skipping conversion test")
	}

	config := MediaConvertConfig{
		TargetAudioFormats:   []string{media.MIMETypeAudioMP3},
		AudioConverterConfig: media.DefaultAudioConverterConfig(),
		PassthroughOnError:   false,
	}
	stage := NewMediaConvertStage(&config)

	ctx := context.Background()
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create a valid WAV file and encode to base64
	wavData := createMinimalWAV()
	wavBase64 := base64.StdEncoding.EncodeToString(wavData)

	msg := &types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeAudio,
				Media: &types.MediaContent{
					Data:     &wavBase64,
					MIMEType: media.MIMETypeAudioWAV, // Will be converted to MP3
				},
			},
		},
	}
	input <- NewMessageElement(msg)
	close(input)

	err := stage.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := <-output
	if result.Message == nil {
		t.Fatal("expected message in result")
	}
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	// Audio should have been converted to MP3
	if len(result.Message.Parts) != 1 {
		t.Errorf("expected 1 part, got %d", len(result.Message.Parts))
	}
	if result.Message.Parts[0].Media.MIMEType != media.MIMETypeAudioMP3 {
		t.Errorf("expected MIME type %s, got %s", media.MIMETypeAudioMP3, result.Message.Parts[0].Media.MIMEType)
	}
	// Data should be base64 encoded MP3
	if result.Message.Parts[0].Media.Data == nil || *result.Message.Parts[0].Media.Data == "" {
		t.Error("expected converted audio data")
	}
}
