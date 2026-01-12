package stage

import (
	"context"
	"encoding/base64"
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestMediaExtractStage_BasicOperation(t *testing.T) {
	config := DefaultMediaExtractConfig()
	stg := NewMediaExtractStage(config)

	if stg.Name() != "media-extract" {
		t.Errorf("Expected name 'media-extract', got '%s'", stg.Name())
	}

	if stg.Type() != StageTypeTransform {
		t.Errorf("Expected type Transform, got %v", stg.Type())
	}
}

func TestMediaExtractStage_ExtractSingleImage(t *testing.T) {
	config := DefaultMediaExtractConfig()
	stg := NewMediaExtractStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Create message with single image
	testData := createTestImageData(256, 256)
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
		if err := stg.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	// Collect results
	var results []StreamElement
	for elem := range output {
		results = append(results, elem)
	}

	// Should have 1 image element
	if len(results) != 1 {
		t.Fatalf("Expected 1 element, got %d", len(results))
	}

	result := results[0]
	if result.Image == nil {
		t.Fatal("Expected Image element")
	}

	if result.Error != nil {
		t.Fatalf("Unexpected error: %v", result.Error)
	}

	// Check correlation metadata
	msgID := result.GetMetadata(MediaExtractMessageIDKey)
	if msgID == nil {
		t.Error("Expected message ID metadata")
	}

	partIdx := result.GetMetadata(MediaExtractPartIndexKey)
	if partIdx != 0 {
		t.Errorf("Expected part index 0, got %v", partIdx)
	}

	totalParts := result.GetMetadata(MediaExtractTotalPartsKey)
	if totalParts != 1 {
		t.Errorf("Expected total parts 1, got %v", totalParts)
	}

	mediaType := result.GetMetadata(MediaExtractMediaTypeKey)
	if mediaType != types.ContentTypeImage {
		t.Errorf("Expected media type 'image', got %v", mediaType)
	}
}

func TestMediaExtractStage_ExtractMultipleImages(t *testing.T) {
	config := DefaultMediaExtractConfig()
	stg := NewMediaExtractStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Create message with 3 images
	msg := &types.Message{Role: "user"}
	msg.AddTextPart("Compare these images:")

	for i := 0; i < 3; i++ {
		testData := createTestImageData(128, 128)
		encodedData := base64.StdEncoding.EncodeToString(testData)
		msg.Parts = append(msg.Parts, types.ContentPart{
			Type: types.ContentTypeImage,
			Media: &types.MediaContent{
				MIMEType: "image/jpeg",
				Data:     &encodedData,
			},
		})
	}

	input <- StreamElement{Message: msg}
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

	// Should have 3 image elements
	if len(results) != 3 {
		t.Fatalf("Expected 3 elements, got %d", len(results))
	}

	// Verify all have same message ID
	var msgID interface{}
	for i, result := range results {
		if result.Image == nil {
			t.Errorf("Element %d: Expected Image", i)
		}

		currentMsgID := result.GetMetadata(MediaExtractMessageIDKey)
		if i == 0 {
			msgID = currentMsgID
		} else if currentMsgID != msgID {
			t.Errorf("Element %d: Message ID mismatch", i)
		}

		partIdx := result.GetMetadata(MediaExtractPartIndexKey)
		if partIdx != i {
			t.Errorf("Element %d: Expected part index %d, got %v", i, i, partIdx)
		}

		totalParts := result.GetMetadata(MediaExtractTotalPartsKey)
		if totalParts != 3 {
			t.Errorf("Element %d: Expected total parts 3, got %v", i, totalParts)
		}
	}
}

func TestMediaExtractStage_PassthroughTextOnly(t *testing.T) {
	config := DefaultMediaExtractConfig()
	stg := NewMediaExtractStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Create message with only text
	msg := &types.Message{Role: "user"}
	msg.AddTextPart("Hello, how are you?")

	input <- StreamElement{Message: msg}
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

	// Should pass through the original message
	if len(results) != 1 {
		t.Fatalf("Expected 1 element, got %d", len(results))
	}

	result := results[0]
	if result.Message == nil {
		t.Fatal("Expected Message element")
	}

	if result.Image != nil || result.Video != nil {
		t.Error("Expected no Image or Video")
	}
}

func TestMediaExtractStage_PassthroughNonMessage(t *testing.T) {
	config := DefaultMediaExtractConfig()
	stg := NewMediaExtractStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Send text element (not a message)
	text := "some text"
	input <- StreamElement{Text: &text}
	close(input)

	go func() {
		if err := stg.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	result := <-output
	if result.Text == nil || *result.Text != text {
		t.Error("Expected text element to pass through")
	}
}

func TestMediaExtractStage_DisableImages(t *testing.T) {
	config := MediaExtractConfig{
		ExtractImages:       false, // Disabled
		ExtractVideos:       true,
		PreserveStorageRefs: true,
	}
	stg := NewMediaExtractStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Create message with image
	testData := createTestImageData(128, 128)
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
		if err := stg.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	result := <-output

	// Should pass through message since images are disabled
	if result.Message == nil {
		t.Fatal("Expected Message to pass through")
	}
	if result.Image != nil {
		t.Error("Expected no Image when extraction disabled")
	}
}

func TestMediaExtractStage_PreserveStorageRef(t *testing.T) {
	config := MediaExtractConfig{
		ExtractImages:       true,
		PreserveStorageRefs: true,
	}
	stg := NewMediaExtractStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Create message with storage reference
	storageRef := "storage://bucket/image.jpg"
	msg := &types.Message{Role: "user"}
	msg.Parts = append(msg.Parts, types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			MIMEType:         "image/jpeg",
			StorageReference: &storageRef,
		},
	})

	input <- StreamElement{Message: msg}
	close(input)

	go func() {
		if err := stg.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	result := <-output

	if result.Image == nil {
		t.Fatal("Expected Image element")
	}

	// Should have storage ref, no data (lazy loading)
	if result.Image.StorageRef != "storage://bucket/image.jpg" {
		t.Errorf("Expected storage ref, got %v", result.Image.StorageRef)
	}
	if len(result.Image.Data) > 0 {
		t.Error("Expected no data with preserved storage ref")
	}
}

func TestMediaExtractStage_ContextCancellation(t *testing.T) {
	config := DefaultMediaExtractConfig()
	stg := NewMediaExtractStage(config)
	ctx, cancel := context.WithCancel(context.Background())

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement) // Unbuffered to block

	// Create message with image
	testData := createTestImageData(128, 128)
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

	// Cancel context
	cancel()

	err := stg.Process(ctx, input, output)

	if err != nil && err != context.Canceled {
		t.Errorf("Expected nil or context.Canceled, got %v", err)
	}
}

func TestMediaExtractStage_OriginalMessagePreserved(t *testing.T) {
	config := DefaultMediaExtractConfig()
	stg := NewMediaExtractStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Create message
	testData := createTestImageData(128, 128)
	encodedData := base64.StdEncoding.EncodeToString(testData)

	msg := &types.Message{Role: "user"}
	msg.AddTextPart("Analyze this image")
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
		if err := stg.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	result := <-output

	// Verify original message is preserved in metadata
	origMsg := result.GetMetadata(MediaExtractOriginalMessageKey)
	if origMsg == nil {
		t.Fatal("Expected original message in metadata")
	}

	preserved, ok := origMsg.(*types.Message)
	if !ok {
		t.Fatalf("Expected *types.Message, got %T", origMsg)
	}

	if preserved.Role != "user" {
		t.Errorf("Expected role 'user', got '%s'", preserved.Role)
	}
}

func TestMediaExtractStage_InvalidBase64(t *testing.T) {
	config := DefaultMediaExtractConfig()
	stg := NewMediaExtractStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Create message with invalid base64
	invalidData := "not-valid-base64!@#$"
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
		_ = stg.Process(ctx, input, output)
	}()

	result := <-output

	// Should have an error
	if result.Error == nil {
		t.Error("Expected error for invalid base64 data")
	}
}

func TestMediaExtractStage_GetConfig(t *testing.T) {
	config := MediaExtractConfig{
		ExtractImages:       false,
		ExtractVideos:       true,
		PreserveStorageRefs: false,
	}

	stg := NewMediaExtractStage(config)
	got := stg.GetConfig()

	if got.ExtractImages != false {
		t.Error("Expected ExtractImages false")
	}
	if got.ExtractVideos != true {
		t.Error("Expected ExtractVideos true")
	}
	if got.PreserveStorageRefs != false {
		t.Error("Expected PreserveStorageRefs false")
	}
}

func TestDefaultMediaExtractConfig(t *testing.T) {
	config := DefaultMediaExtractConfig()

	if !config.ExtractImages {
		t.Error("Expected ExtractImages true by default")
	}
	if !config.ExtractVideos {
		t.Error("Expected ExtractVideos true by default")
	}
	if !config.PreserveStorageRefs {
		t.Error("Expected PreserveStorageRefs true by default")
	}
}

func TestMediaExtractStage_ExtractVideo(t *testing.T) {
	config := DefaultMediaExtractConfig()
	stg := NewMediaExtractStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Create message with video
	videoData := []byte{0, 0, 0, 1, 2, 3, 4, 5} // Fake video data
	encodedData := base64.StdEncoding.EncodeToString(videoData)

	msg := &types.Message{Role: "user"}
	msg.Parts = append(msg.Parts, types.ContentPart{
		Type: types.ContentTypeVideo,
		Media: &types.MediaContent{
			MIMEType: "video/mp4",
			Data:     &encodedData,
		},
	})

	input <- StreamElement{Message: msg}
	close(input)

	go func() {
		if err := stg.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	var results []StreamElement
	for elem := range output {
		results = append(results, elem)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 element, got %d", len(results))
	}

	if results[0].Video == nil {
		t.Fatal("Expected Video element")
	}

	if results[0].Video.MIMEType != "video/mp4" {
		t.Errorf("Expected video/mp4, got %s", results[0].Video.MIMEType)
	}
}

func TestMediaExtractStage_DisableVideos(t *testing.T) {
	config := MediaExtractConfig{
		ExtractImages:       true,
		ExtractVideos:       false, // Disabled
		PreserveStorageRefs: true,
	}
	stg := NewMediaExtractStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	videoData := []byte{0, 0, 0, 1, 2, 3}
	encodedData := base64.StdEncoding.EncodeToString(videoData)

	msg := &types.Message{Role: "user"}
	msg.Parts = append(msg.Parts, types.ContentPart{
		Type: types.ContentTypeVideo,
		Media: &types.MediaContent{
			MIMEType: "video/mp4",
			Data:     &encodedData,
		},
	})

	input <- StreamElement{Message: msg}
	close(input)

	go func() {
		if err := stg.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	result := <-output

	// Should pass through message since videos are disabled
	if result.Message == nil {
		t.Fatal("Expected Message to pass through")
	}
	if result.Video != nil {
		t.Error("Expected no Video when extraction disabled")
	}
}

func TestMediaExtractStage_VideoWithStorageRef(t *testing.T) {
	config := MediaExtractConfig{
		ExtractVideos:       true,
		PreserveStorageRefs: true,
	}
	stg := NewMediaExtractStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	storageRef := "storage://bucket/video.mp4"
	msg := &types.Message{Role: "user"}
	msg.Parts = append(msg.Parts, types.ContentPart{
		Type: types.ContentTypeVideo,
		Media: &types.MediaContent{
			MIMEType:         "video/mp4",
			StorageReference: &storageRef,
		},
	})

	input <- StreamElement{Message: msg}
	close(input)

	go func() {
		if err := stg.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	result := <-output

	if result.Video == nil {
		t.Fatal("Expected Video element")
	}

	if result.Video.StorageRef != "storage://bucket/video.mp4" {
		t.Errorf("Expected storage ref, got %v", result.Video.StorageRef)
	}
}

func TestMediaExtractStage_VideoWithMetadata(t *testing.T) {
	config := DefaultMediaExtractConfig()
	stg := NewMediaExtractStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	videoData := []byte{0, 0, 0, 1, 2, 3}
	encodedData := base64.StdEncoding.EncodeToString(videoData)
	width := 1920
	height := 1080
	fps := 30
	duration := 60
	format := "h264"

	msg := &types.Message{Role: "user"}
	msg.Parts = append(msg.Parts, types.ContentPart{
		Type: types.ContentTypeVideo,
		Media: &types.MediaContent{
			MIMEType: "video/mp4",
			Data:     &encodedData,
			Width:    &width,
			Height:   &height,
			FPS:      &fps,
			Duration: &duration,
			Format:   &format,
		},
	})

	input <- StreamElement{Message: msg}
	close(input)

	go func() {
		if err := stg.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	result := <-output

	if result.Video == nil {
		t.Fatal("Expected Video element")
	}

	if result.Video.Width != 1920 {
		t.Errorf("Expected width 1920, got %d", result.Video.Width)
	}
	if result.Video.Height != 1080 {
		t.Errorf("Expected height 1080, got %d", result.Video.Height)
	}
	if result.Video.FrameRate != 30 {
		t.Errorf("Expected framerate 30, got %f", result.Video.FrameRate)
	}
}

func TestMediaContentToImageData_WithMetadata(t *testing.T) {
	width := 800
	height := 600
	format := "png"
	testData := []byte{1, 2, 3, 4}
	encodedData := base64.StdEncoding.EncodeToString(testData)

	media := &types.MediaContent{
		MIMEType: "image/png",
		Data:     &encodedData,
		Width:    &width,
		Height:   &height,
		Format:   &format,
	}

	imageData, err := mediaContentToImageData(media, true)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if imageData.Width != 800 {
		t.Errorf("Expected width 800, got %d", imageData.Width)
	}
	if imageData.Height != 600 {
		t.Errorf("Expected height 600, got %d", imageData.Height)
	}
	if imageData.Format != "png" {
		t.Errorf("Expected format 'png', got %s", imageData.Format)
	}
}

func TestMediaContentToImageData_NoDataSource(t *testing.T) {
	media := &types.MediaContent{
		MIMEType: "image/jpeg",
	}

	_, err := mediaContentToImageData(media, true)
	if err == nil {
		t.Error("Expected error for media with no data source")
	}
}

func TestMediaContentToVideoData_NoDataSource(t *testing.T) {
	media := &types.MediaContent{
		MIMEType: "video/mp4",
	}

	_, err := mediaContentToVideoData(media, true)
	if err == nil {
		t.Error("Expected error for media with no data source")
	}
}

func TestMediaContentToImageData_StorageRefWithoutPreserve(t *testing.T) {
	storageRef := "storage://test"
	media := &types.MediaContent{
		MIMEType:         "image/jpeg",
		StorageReference: &storageRef,
	}

	_, err := mediaContentToImageData(media, false)
	if err == nil {
		t.Error("Expected error when storage ref not preserved and no storage service")
	}
}

func TestMediaContentToVideoData_StorageRefWithoutPreserve(t *testing.T) {
	storageRef := "storage://test"
	media := &types.MediaContent{
		MIMEType:         "video/mp4",
		StorageReference: &storageRef,
	}

	_, err := mediaContentToVideoData(media, false)
	if err == nil {
		t.Error("Expected error when storage ref not preserved and no storage service")
	}
}

func TestMediaContentToImageData_URLError(t *testing.T) {
	url := "https://example.com/image.jpg"
	media := &types.MediaContent{
		MIMEType: "image/jpeg",
		URL:      &url,
	}

	_, err := mediaContentToImageData(media, true)
	if err == nil {
		t.Error("Expected error for URL-based media")
	}
}

func TestMediaContentToVideoData_URLError(t *testing.T) {
	url := "https://example.com/video.mp4"
	media := &types.MediaContent{
		MIMEType: "video/mp4",
		URL:      &url,
	}

	_, err := mediaContentToVideoData(media, true)
	if err == nil {
		t.Error("Expected error for URL-based media")
	}
}

func TestMediaContentToImageData_FilePath(t *testing.T) {
	// Create a temp file with test image data
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test.jpg"
	testData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10} // JPEG header-ish
	if err := writeTestFile(tmpFile, testData); err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	media := &types.MediaContent{
		MIMEType: "image/jpeg",
		FilePath: &tmpFile,
	}

	imageData, err := mediaContentToImageData(media, true)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if imageData.MIMEType != "image/jpeg" {
		t.Errorf("Expected mime type image/jpeg, got %s", imageData.MIMEType)
	}
	if len(imageData.Data) != len(testData) {
		t.Errorf("Expected data length %d, got %d", len(testData), len(imageData.Data))
	}
}

func TestMediaContentToVideoData_FilePath(t *testing.T) {
	// Create a temp file with test video data
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test.mp4"
	testData := []byte{0x00, 0x00, 0x00, 0x1C, 0x66, 0x74} // MP4 header-ish
	if err := writeTestFile(tmpFile, testData); err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	media := &types.MediaContent{
		MIMEType: "video/mp4",
		FilePath: &tmpFile,
	}

	videoData, err := mediaContentToVideoData(media, true)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if videoData.MIMEType != "video/mp4" {
		t.Errorf("Expected mime type video/mp4, got %s", videoData.MIMEType)
	}
	if len(videoData.Data) != len(testData) {
		t.Errorf("Expected data length %d, got %d", len(testData), len(videoData.Data))
	}
}

func TestMediaContentToImageData_FilePathNotExists(t *testing.T) {
	filePath := "/nonexistent/path/image.jpg"
	media := &types.MediaContent{
		MIMEType: "image/jpeg",
		FilePath: &filePath,
	}

	_, err := mediaContentToImageData(media, true)
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestMediaContentToVideoData_FilePathNotExists(t *testing.T) {
	filePath := "/nonexistent/path/video.mp4"
	media := &types.MediaContent{
		MIMEType: "video/mp4",
		FilePath: &filePath,
	}

	_, err := mediaContentToVideoData(media, true)
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func writeTestFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}
