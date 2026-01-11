package stage

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestImagePreprocessStage_BasicOperation(t *testing.T) {
	config := DefaultImagePreprocessConfig()
	stage := NewImagePreprocessStage(config)

	if stage.Name() != "image-preprocess" {
		t.Errorf("Expected name 'image-preprocess', got '%s'", stage.Name())
	}

	if stage.Type() != StageTypeTransform {
		t.Errorf("Expected type Transform, got %v", stage.Type())
	}
}

func TestImagePreprocessStage_ProcessMessageWithImage(t *testing.T) {
	config := ImagePreprocessConfig{
		Resize: ImageResizeStageConfig{
			MaxWidth:            512,
			MaxHeight:           512,
			Quality:             85,
			PreserveAspectRatio: true,
			SkipIfSmaller:       true,
		},
		EnableResize: true,
	}

	stage := NewImagePreprocessStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create a message with a large image
	testData := createTestImageData(1024, 1024)
	encodedData := base64.StdEncoding.EncodeToString(testData)

	msg := &types.Message{Role: "user"}
	msg.AddTextPart("What's in this image?")
	msg.Parts = append(msg.Parts, types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			MIMEType: "image/jpeg",
			Data:     &encodedData,
		},
	})

	input <- StreamElement{Message: msg}
	close(input)

	go func() {
		if err := stage.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	result := <-output

	if result.Error != nil {
		t.Fatalf("Unexpected error: %v", result.Error)
	}

	if result.Message == nil {
		t.Fatal("Expected message in result")
	}

	// Verify image was processed
	for _, part := range result.Message.Parts {
		if part.Type == types.ContentTypeImage && part.Media != nil && part.Media.Data != nil {
			// Decode the result and check size
			processedData, err := base64.StdEncoding.DecodeString(*part.Media.Data)
			if err != nil {
				t.Fatalf("Failed to decode processed image: %v", err)
			}
			// Processed data should be smaller or same size (compression)
			if len(processedData) > len(testData)*2 {
				t.Error("Processed image unexpectedly larger than original")
			}
		}
	}
}

func TestImagePreprocessStage_DisabledResize(t *testing.T) {
	config := ImagePreprocessConfig{
		Resize:       DefaultImageResizeStageConfig(),
		EnableResize: false, // Disabled
	}

	stage := NewImagePreprocessStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	testData := createTestImageData(1024, 1024)
	encodedData := base64.StdEncoding.EncodeToString(testData)

	msg := &types.Message{Role: "user"}
	msg.Parts = append(msg.Parts, types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			MIMEType: "image/jpeg",
			Data:     &encodedData,
		},
	})

	input <- StreamElement{Message: msg}
	close(input)

	go func() {
		if err := stage.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	result := <-output

	// Image should be unchanged when resize is disabled
	for _, part := range result.Message.Parts {
		if part.Type == types.ContentTypeImage && part.Media != nil && part.Media.Data != nil {
			if *part.Media.Data != encodedData {
				t.Error("Expected image data to be unchanged when resize is disabled")
			}
		}
	}
}

func TestImagePreprocessStage_PassthroughNonImageMessages(t *testing.T) {
	config := DefaultImagePreprocessConfig()
	stage := NewImagePreprocessStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Message without images
	msg := &types.Message{Role: "user"}
	msg.AddTextPart("Hello, how are you?")

	input <- StreamElement{Message: msg}
	close(input)

	go func() {
		if err := stage.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	result := <-output

	if result.Error != nil {
		t.Fatalf("Unexpected error: %v", result.Error)
	}

	// Message should pass through unchanged
	if len(result.Message.Parts) != 1 {
		t.Errorf("Expected 1 part, got %d", len(result.Message.Parts))
	}

	if result.Message.Parts[0].Type != types.ContentTypeText {
		t.Error("Expected text part")
	}
}

func TestImagePreprocessStage_ProcessStandaloneImageData(t *testing.T) {
	config := ImagePreprocessConfig{
		Resize: ImageResizeStageConfig{
			MaxWidth:            256,
			MaxHeight:           256,
			Quality:             85,
			PreserveAspectRatio: true,
			SkipIfSmaller:       true,
		},
		EnableResize: true,
	}

	stage := NewImagePreprocessStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create standalone ImageData
	testData := createTestImageData(512, 512)
	input <- StreamElement{
		Image: &ImageData{
			Data:     testData,
			MIMEType: "image/jpeg",
			Width:    512,
			Height:   512,
			Format:   "jpeg",
		},
	}
	close(input)

	go func() {
		if err := stage.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	result := <-output

	if result.Error != nil {
		t.Fatalf("Unexpected error: %v", result.Error)
	}

	if result.Image == nil {
		t.Fatal("Expected image in result")
	}

	// Verify dimensions were reduced
	if result.Image.Width > 256 || result.Image.Height > 256 {
		t.Errorf("Expected dimensions <= 256, got %dx%d", result.Image.Width, result.Image.Height)
	}
}

func TestDefaultImagePreprocessConfig(t *testing.T) {
	config := DefaultImagePreprocessConfig()

	if !config.EnableResize {
		t.Error("Expected EnableResize to be true by default")
	}

	if config.Resize.MaxWidth != 1024 {
		t.Errorf("Expected MaxWidth 1024, got %d", config.Resize.MaxWidth)
	}

	if config.Resize.MaxHeight != 1024 {
		t.Errorf("Expected MaxHeight 1024, got %d", config.Resize.MaxHeight)
	}
}

func TestImagePreprocessStage_GetConfig(t *testing.T) {
	config := ImagePreprocessConfig{
		Resize: ImageResizeStageConfig{
			MaxWidth:  512,
			MaxHeight: 256,
		},
		EnableResize: true,
	}

	stage := NewImagePreprocessStage(config)
	got := stage.GetConfig()

	if got.Resize.MaxWidth != 512 {
		t.Errorf("Expected MaxWidth 512, got %d", got.Resize.MaxWidth)
	}
	if got.Resize.MaxHeight != 256 {
		t.Errorf("Expected MaxHeight 256, got %d", got.Resize.MaxHeight)
	}
}

func TestImagePreprocessStage_ContextCancellationOnSend(t *testing.T) {
	config := DefaultImagePreprocessConfig()
	stage := NewImagePreprocessStage(config)
	ctx, cancel := context.WithCancel(context.Background())

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement) // Unbuffered to block on send

	// Send message without image (so it tries to send to output)
	msg := &types.Message{Role: "user"}
	msg.AddTextPart("Hello")

	input <- StreamElement{Message: msg}
	close(input)

	// Cancel after the stage starts to block on output send
	go func() {
		cancel()
	}()

	err := stage.Process(ctx, input, output)

	// Should eventually return context error when trying to send to output
	if err != nil && err != context.Canceled {
		t.Errorf("Expected nil or context.Canceled error, got %v", err)
	}
}

func TestImagePreprocessStage_InvalidBase64(t *testing.T) {
	config := DefaultImagePreprocessConfig()
	stage := NewImagePreprocessStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	invalidData := "not-valid-base64!@#$%"
	msg := &types.Message{Role: "user"}
	msg.Parts = append(msg.Parts, types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			MIMEType: "image/jpeg",
			Data:     &invalidData,
		},
	})

	input <- StreamElement{Message: msg}
	close(input)

	go func() {
		_ = stage.Process(ctx, input, output)
	}()

	result := <-output

	// Should have an error
	if result.Error == nil {
		t.Error("Expected error for invalid base64 data")
	}
}

func TestImagePreprocessStage_NilMedia(t *testing.T) {
	config := DefaultImagePreprocessConfig()
	stage := NewImagePreprocessStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	msg := &types.Message{Role: "user"}
	msg.Parts = append(msg.Parts, types.ContentPart{
		Type:  types.ContentTypeImage,
		Media: nil, // Nil media
	})

	input <- StreamElement{Message: msg}
	close(input)

	go func() {
		if err := stage.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	result := <-output

	// Should pass through without error
	if result.Error != nil {
		t.Errorf("Unexpected error: %v", result.Error)
	}
}

func TestImagePreprocessStage_EmptyData(t *testing.T) {
	config := DefaultImagePreprocessConfig()
	stage := NewImagePreprocessStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	emptyData := ""
	msg := &types.Message{Role: "user"}
	msg.Parts = append(msg.Parts, types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			MIMEType: "image/jpeg",
			Data:     &emptyData, // Empty data
		},
	})

	input <- StreamElement{Message: msg}
	close(input)

	go func() {
		if err := stage.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	result := <-output

	// Should pass through without error (empty data is skipped)
	if result.Error != nil {
		t.Errorf("Unexpected error: %v", result.Error)
	}
}

func TestImagePreprocessStage_NoImageData(t *testing.T) {
	config := DefaultImagePreprocessConfig()
	stage := NewImagePreprocessStage(config)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Standalone ImageData with empty data
	input <- StreamElement{
		Image: &ImageData{
			Data: nil, // No data
		},
	}
	close(input)

	go func() {
		if err := stage.Process(context.Background(), input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	result := <-output

	// Should pass through without processing
	if result.Error != nil {
		t.Errorf("Unexpected error: %v", result.Error)
	}
}
