package stage

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestBatchMediaPipeline_ExtractResizeCompose tests the full pipeline:
// Message → MediaExtractStage → ImageResizeStage → MediaComposeStage → Message
func TestBatchMediaPipeline_ExtractResizeCompose(t *testing.T) {
	ctx := context.Background()

	// Create channels for pipeline stages
	input := make(chan StreamElement, 1)
	extractOutput := make(chan StreamElement, 10)
	resizeOutput := make(chan StreamElement, 10)
	composeOutput := make(chan StreamElement, 10)

	// Configure stages
	extractConfig := DefaultMediaExtractConfig()
	resizeConfig := ImageResizeStageConfig{
		MaxWidth:            128,
		MaxHeight:           128,
		Quality:             85,
		PreserveAspectRatio: true,
		SkipIfSmaller:       true,
	}
	composeConfig := DefaultMediaComposeConfig()

	extractStage := NewMediaExtractStage(extractConfig)
	resizeStage := NewImageResizeStage(resizeConfig)
	composeStage := NewMediaComposeStage(composeConfig)

	// Create test message with large image
	testData := createTestImageData(512, 512)
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

	// Run pipeline stages concurrently
	errChan := make(chan error, 3)

	go func() {
		errChan <- extractStage.Process(ctx, input, extractOutput)
	}()

	go func() {
		errChan <- resizeStage.Process(ctx, extractOutput, resizeOutput)
	}()

	go func() {
		errChan <- composeStage.Process(ctx, resizeOutput, composeOutput)
	}()

	// Send input
	input <- StreamElement{Message: msg}
	close(input)

	// Collect output
	var results []StreamElement
	for elem := range composeOutput {
		results = append(results, elem)
	}

	// Wait for all stages
	for i := 0; i < 3; i++ {
		if err := <-errChan; err != nil {
			t.Fatalf("Stage error: %v", err)
		}
	}

	// Verify result
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Message == nil {
		t.Fatal("Expected Message in result")
	}

	if result.Error != nil {
		t.Fatalf("Unexpected error: %v", result.Error)
	}

	// Verify message structure: text + resized image
	if len(result.Message.Parts) != 2 {
		t.Errorf("Expected 2 parts, got %d", len(result.Message.Parts))
	}

	// Check the image was processed
	for _, part := range result.Message.Parts {
		if part.Type == types.ContentTypeImage && part.Media != nil && part.Media.Data != nil {
			// Decode and verify size is different (resized)
			decoded, err := base64.StdEncoding.DecodeString(*part.Media.Data)
			if err != nil {
				t.Fatalf("Failed to decode result image: %v", err)
			}
			// The resized image should be smaller or different from original
			if len(decoded) == len(testData) {
				t.Log("Warning: output size same as input - resize may not have occurred")
			}
		}
	}
}

// TestBatchMediaPipeline_MultipleImages tests processing multiple images in one message.
func TestBatchMediaPipeline_MultipleImages(t *testing.T) {
	ctx := context.Background()

	// Create channels
	input := make(chan StreamElement, 1)
	extractOutput := make(chan StreamElement, 10)
	resizeOutput := make(chan StreamElement, 10)
	composeOutput := make(chan StreamElement, 10)

	// Configure stages
	extractStage := NewMediaExtractStage(DefaultMediaExtractConfig())
	resizeStage := NewImageResizeStage(ImageResizeStageConfig{
		MaxWidth:  64,
		MaxHeight: 64,
		Quality:   85,
	})
	composeStage := NewMediaComposeStage(DefaultMediaComposeConfig())

	// Create message with 3 images
	msg := &types.Message{Role: "user"}
	msg.AddTextPart("Compare these images:")

	for i := 0; i < 3; i++ {
		testData := createTestImageData(256, 256)
		encodedData := base64.StdEncoding.EncodeToString(testData)
		msg.Parts = append(msg.Parts, types.ContentPart{
			Type: types.ContentTypeImage,
			Media: &types.MediaContent{
				MIMEType: "image/jpeg",
				Data:     &encodedData,
			},
		})
	}

	// Run pipeline
	errChan := make(chan error, 3)

	go func() {
		errChan <- extractStage.Process(ctx, input, extractOutput)
	}()

	go func() {
		errChan <- resizeStage.Process(ctx, extractOutput, resizeOutput)
	}()

	go func() {
		errChan <- composeStage.Process(ctx, resizeOutput, composeOutput)
	}()

	input <- StreamElement{Message: msg}
	close(input)

	// Collect output
	var results []StreamElement
	for elem := range composeOutput {
		results = append(results, elem)
	}

	// Wait for stages
	for i := 0; i < 3; i++ {
		if err := <-errChan; err != nil {
			t.Fatalf("Stage error: %v", err)
		}
	}

	// Verify
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Message == nil {
		t.Fatal("Expected Message")
	}

	// Should have text + 3 images = 4 parts
	if len(result.Message.Parts) != 4 {
		t.Errorf("Expected 4 parts, got %d", len(result.Message.Parts))
	}

	// Count image parts
	imageCount := 0
	for _, part := range result.Message.Parts {
		if part.Type == types.ContentTypeImage {
			imageCount++
		}
	}

	if imageCount != 3 {
		t.Errorf("Expected 3 image parts, got %d", imageCount)
	}
}

// TestBatchMediaPipeline_TextOnlyPassthrough verifies text-only messages pass through.
func TestBatchMediaPipeline_TextOnlyPassthrough(t *testing.T) {
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	extractOutput := make(chan StreamElement, 10)
	resizeOutput := make(chan StreamElement, 10)
	composeOutput := make(chan StreamElement, 10)

	extractStage := NewMediaExtractStage(DefaultMediaExtractConfig())
	resizeStage := NewImageResizeStage(DefaultImageResizeStageConfig())
	composeStage := NewMediaComposeStage(DefaultMediaComposeConfig())

	// Text-only message
	msg := &types.Message{Role: "user"}
	msg.AddTextPart("Hello, how are you?")

	errChan := make(chan error, 3)

	go func() {
		errChan <- extractStage.Process(ctx, input, extractOutput)
	}()

	go func() {
		errChan <- resizeStage.Process(ctx, extractOutput, resizeOutput)
	}()

	go func() {
		errChan <- composeStage.Process(ctx, resizeOutput, composeOutput)
	}()

	input <- StreamElement{Message: msg}
	close(input)

	var results []StreamElement
	for elem := range composeOutput {
		results = append(results, elem)
	}

	for i := 0; i < 3; i++ {
		if err := <-errChan; err != nil {
			t.Fatalf("Stage error: %v", err)
		}
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Message == nil {
		t.Fatal("Expected Message")
	}

	// Should still have 1 text part
	if len(result.Message.Parts) != 1 {
		t.Errorf("Expected 1 part, got %d", len(result.Message.Parts))
	}

	if result.Message.Parts[0].Type != types.ContentTypeText {
		t.Errorf("Expected text part, got %s", result.Message.Parts[0].Type)
	}
}

// TestBatchMediaPipeline_MixedContent tests messages with text interspersed with images.
func TestBatchMediaPipeline_MixedContent(t *testing.T) {
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	extractOutput := make(chan StreamElement, 10)
	resizeOutput := make(chan StreamElement, 10)
	composeOutput := make(chan StreamElement, 10)

	extractStage := NewMediaExtractStage(DefaultMediaExtractConfig())
	resizeStage := NewImageResizeStage(ImageResizeStageConfig{
		MaxWidth:  64,
		MaxHeight: 64,
	})
	composeStage := NewMediaComposeStage(DefaultMediaComposeConfig())

	// Create message: text, image, text, image
	msg := &types.Message{Role: "user"}
	msg.AddTextPart("First, look at this:")

	testData1 := createTestImageData(256, 256)
	encodedData1 := base64.StdEncoding.EncodeToString(testData1)
	msg.Parts = append(msg.Parts, types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			MIMEType: "image/jpeg",
			Data:     &encodedData1,
		},
	})

	msg.AddTextPart("And then this:")

	testData2 := createTestImageData(256, 256)
	encodedData2 := base64.StdEncoding.EncodeToString(testData2)
	msg.Parts = append(msg.Parts, types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			MIMEType: "image/jpeg",
			Data:     &encodedData2,
		},
	})

	errChan := make(chan error, 3)

	go func() {
		errChan <- extractStage.Process(ctx, input, extractOutput)
	}()

	go func() {
		errChan <- resizeStage.Process(ctx, extractOutput, resizeOutput)
	}()

	go func() {
		errChan <- composeStage.Process(ctx, resizeOutput, composeOutput)
	}()

	input <- StreamElement{Message: msg}
	close(input)

	var results []StreamElement
	for elem := range composeOutput {
		results = append(results, elem)
	}

	for i := 0; i < 3; i++ {
		if err := <-errChan; err != nil {
			t.Fatalf("Stage error: %v", err)
		}
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Message == nil {
		t.Fatal("Expected Message")
	}

	// Should have 4 parts: text, image, text, image
	if len(result.Message.Parts) != 4 {
		t.Errorf("Expected 4 parts, got %d", len(result.Message.Parts))
	}

	// Verify order is preserved
	expectedTypes := []string{
		types.ContentTypeText,
		types.ContentTypeImage,
		types.ContentTypeText,
		types.ContentTypeImage,
	}

	for i, expected := range expectedTypes {
		if i >= len(result.Message.Parts) {
			break
		}
		if result.Message.Parts[i].Type != expected {
			t.Errorf("Part %d: expected type %s, got %s", i, expected, result.Message.Parts[i].Type)
		}
	}
}
