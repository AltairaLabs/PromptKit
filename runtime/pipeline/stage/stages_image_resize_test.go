package stage

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// createTestImageData creates test image data with specified dimensions.
func createTestImageData(width, height int) []byte {
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

func TestImageResizeStage_BasicOperation(t *testing.T) {
	config := ImageResizeStageConfig{
		MaxWidth:            512,
		MaxHeight:           512,
		Quality:             85,
		PreserveAspectRatio: true,
		SkipIfSmaller:       true,
	}

	stage := NewImageResizeStage(config)

	// Verify stage properties
	if stage.Name() != "image-resize" {
		t.Errorf("Expected name 'image-resize', got '%s'", stage.Name())
	}

	if stage.Type() != StageTypeTransform {
		t.Errorf("Expected type Transform, got %v", stage.Type())
	}
}

func TestImageResizeStage_ProcessImage(t *testing.T) {
	config := ImageResizeStageConfig{
		MaxWidth:            512,
		MaxHeight:           512,
		Quality:             85,
		PreserveAspectRatio: true,
		SkipIfSmaller:       true,
	}

	stage := NewImageResizeStage(config)
	ctx := context.Background()

	// Create input channel with a large image
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create test image data (1024x1024)
	testData := createTestImageData(1024, 1024)
	input <- StreamElement{
		Image: &ImageData{
			Data:     testData,
			MIMEType: "image/jpeg",
			Width:    1024,
			Height:   1024,
			Format:   "jpeg",
		},
	}
	close(input)

	// Run the stage
	go func() {
		if err := stage.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	// Collect output
	result := <-output

	if result.Error != nil {
		t.Fatalf("Unexpected error: %v", result.Error)
	}

	if result.Image == nil {
		t.Fatal("Expected image in result")
	}

	// Verify dimensions were reduced
	if result.Image.Width > 512 || result.Image.Height > 512 {
		t.Errorf("Expected dimensions <= 512, got %dx%d", result.Image.Width, result.Image.Height)
	}
}

func TestImageResizeStage_PassthroughSmallImage(t *testing.T) {
	config := ImageResizeStageConfig{
		MaxWidth:            1024,
		MaxHeight:           1024,
		Quality:             85,
		PreserveAspectRatio: true,
		SkipIfSmaller:       true,
	}

	stage := NewImageResizeStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create small test image (256x256)
	testData := createTestImageData(256, 256)
	originalLen := len(testData)

	input <- StreamElement{
		Image: &ImageData{
			Data:     testData,
			MIMEType: "image/jpeg",
			Width:    256,
			Height:   256,
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

	// Verify dimensions unchanged
	if result.Image.Width != 256 || result.Image.Height != 256 {
		t.Errorf("Expected dimensions 256x256, got %dx%d", result.Image.Width, result.Image.Height)
	}

	// Data should be unchanged (passthrough)
	if len(result.Image.Data) != originalLen {
		t.Error("Expected data to be passed through unchanged")
	}
}

func TestImageResizeStage_PassthroughNonImageElements(t *testing.T) {
	config := DefaultImageResizeStageConfig()
	stage := NewImageResizeStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 2)
	output := make(chan StreamElement, 2)

	// Send a text element
	text := "Hello world"
	input <- StreamElement{Text: &text}

	// Send an end-of-stream element
	input <- StreamElement{EndOfStream: true}
	close(input)

	go func() {
		if err := stage.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	// Collect outputs
	result1 := <-output
	result2 := <-output

	// Text should pass through unchanged
	if result1.Text == nil || *result1.Text != "Hello world" {
		t.Error("Text element not passed through correctly")
	}

	// EOS should pass through
	if !result2.EndOfStream {
		t.Error("EndOfStream not passed through correctly")
	}
}

func TestImageResizeStage_ContextCancellation(t *testing.T) {
	config := DefaultImageResizeStageConfig()
	stage := NewImageResizeStage(config)

	ctx, cancel := context.WithCancel(context.Background())

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement) // Unbuffered - will block on send

	testData := createTestImageData(256, 256)
	input <- StreamElement{
		Image: &ImageData{
			Data:     testData,
			MIMEType: "image/jpeg",
			Width:    256,
			Height:   256,
			Format:   "jpeg",
		},
	}
	close(input)

	// Cancel context after a short delay to trigger during blocked send
	go func() {
		cancel()
	}()

	err := stage.Process(ctx, input, output)
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}
}

func TestDefaultImageResizeStageConfig(t *testing.T) {
	config := DefaultImageResizeStageConfig()

	if config.MaxWidth != 1024 {
		t.Errorf("Expected MaxWidth 1024, got %d", config.MaxWidth)
	}
	if config.MaxHeight != 1024 {
		t.Errorf("Expected MaxHeight 1024, got %d", config.MaxHeight)
	}
	if config.Quality != 85 {
		t.Errorf("Expected Quality 85, got %d", config.Quality)
	}
	if !config.PreserveAspectRatio {
		t.Error("Expected PreserveAspectRatio to be true")
	}
	if !config.SkipIfSmaller {
		t.Error("Expected SkipIfSmaller to be true")
	}
}
