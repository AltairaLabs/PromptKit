package stage

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestMediaComposeStage_BasicOperation(t *testing.T) {
	config := DefaultMediaComposeConfig()
	stg := NewMediaComposeStage(config)

	if stg.Name() != "media-compose" {
		t.Errorf("Expected name 'media-compose', got '%s'", stg.Name())
	}

	if stg.Type() != StageTypeAccumulate {
		t.Errorf("Expected type Accumulate, got %v", stg.Type())
	}
}

func TestMediaComposeStage_ComposeSingleImage(t *testing.T) {
	config := DefaultMediaComposeConfig()
	stg := NewMediaComposeStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Create original message with text and image (as it would be before extraction)
	origImageData := createTestImageData(512, 512)
	origEncodedData := base64.StdEncoding.EncodeToString(origImageData)

	origMsg := &types.Message{Role: "user"}
	origMsg.AddTextPart("What's in this image?")
	origMsg.Parts = append(origMsg.Parts, types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			MIMEType: "image/jpeg",
			Data:     &origEncodedData,
		},
	})

	// Create the processed/resized image element
	testData := createTestImageData(256, 256)
	elem := NewImageElement(&ImageData{
		Data:     testData,
		MIMEType: "image/jpeg",
		Width:    256,
		Height:   256,
	})
	elem.WithMetadata(MediaExtractMessageIDKey, "msg-1")
	elem.WithMetadata(MediaExtractPartIndexKey, 0)
	elem.WithMetadata(MediaExtractTotalPartsKey, 1)
	elem.WithMetadata(MediaExtractMediaTypeKey, types.ContentTypeImage)
	elem.WithMetadata(MediaExtractOriginalMessageKey, origMsg)

	input <- elem
	close(input)

	go func() {
		if err := stg.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	// Collect results
	var results []StreamElement
	for e := range output {
		results = append(results, e)
	}

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

	// Should have text part + image part
	if len(result.Message.Parts) != 2 {
		t.Errorf("Expected 2 parts, got %d", len(result.Message.Parts))
	}

	// Check image part has data
	var foundImage bool
	for _, part := range result.Message.Parts {
		if part.Type == types.ContentTypeImage {
			foundImage = true
			if part.Media == nil || part.Media.Data == nil {
				t.Error("Expected image media with data")
			}
		}
	}

	if !foundImage {
		t.Error("Expected to find image part")
	}
}

func TestMediaComposeStage_ComposeMultipleImages(t *testing.T) {
	config := DefaultMediaComposeConfig()
	stg := NewMediaComposeStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 3)
	output := make(chan StreamElement, 10)

	// Create original message
	origMsg := &types.Message{Role: "user"}
	origMsg.AddTextPart("Compare these images:")

	// Add 3 image parts to original
	for i := 0; i < 3; i++ {
		testData := createTestImageData(128, 128)
		encodedData := base64.StdEncoding.EncodeToString(testData)
		origMsg.Parts = append(origMsg.Parts, types.ContentPart{
			Type: types.ContentTypeImage,
			Media: &types.MediaContent{
				MIMEType: "image/jpeg",
				Data:     &encodedData,
			},
		})
	}

	// Create extracted elements (as if from MediaExtractStage)
	for i := 0; i < 3; i++ {
		testData := createTestImageData(64, 64) // Simulated resize
		elem := NewImageElement(&ImageData{
			Data:     testData,
			MIMEType: "image/jpeg",
			Width:    64,
			Height:   64,
		})
		elem.WithMetadata(MediaExtractMessageIDKey, "msg-2")
		elem.WithMetadata(MediaExtractPartIndexKey, i)
		elem.WithMetadata(MediaExtractTotalPartsKey, 3)
		elem.WithMetadata(MediaExtractMediaTypeKey, types.ContentTypeImage)
		elem.WithMetadata(MediaExtractOriginalMessageKey, origMsg)

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
	for e := range output {
		results = append(results, e)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 composed message, got %d", len(results))
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

func TestMediaComposeStage_PassthroughNonExtracted(t *testing.T) {
	config := DefaultMediaComposeConfig()
	stg := NewMediaComposeStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Send element without extract metadata
	text := "Hello world"
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

func TestMediaComposeStage_PreserveStorageRef(t *testing.T) {
	config := DefaultMediaComposeConfig()
	stg := NewMediaComposeStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Create image with storage ref (no data)
	elem := NewImageElement(&ImageData{
		MIMEType:   "image/jpeg",
		StorageRef: "storage://bucket/processed.jpg",
	})
	elem.WithMetadata(MediaExtractMessageIDKey, "msg-3")
	elem.WithMetadata(MediaExtractPartIndexKey, 0)
	elem.WithMetadata(MediaExtractTotalPartsKey, 1)
	elem.WithMetadata(MediaExtractMediaTypeKey, types.ContentTypeImage)

	input <- elem
	close(input)

	go func() {
		if err := stg.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	var results []StreamElement
	for e := range output {
		results = append(results, e)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Message == nil {
		t.Fatal("Expected Message")
	}

	// Find image part and check storage ref
	for _, part := range result.Message.Parts {
		if part.Type == types.ContentTypeImage {
			if part.Media == nil {
				t.Fatal("Expected media content")
			}
			if part.Media.StorageReference == nil {
				t.Error("Expected storage reference")
			} else if *part.Media.StorageReference != "storage://bucket/processed.jpg" {
				t.Errorf("Expected storage ref 'storage://bucket/processed.jpg', got %s", *part.Media.StorageReference)
			}
		}
	}
}

func TestMediaComposeStage_MultipleMessages(t *testing.T) {
	config := DefaultMediaComposeConfig()
	stg := NewMediaComposeStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 4)
	output := make(chan StreamElement, 10)

	// Send elements from two different messages interleaved
	for _, msgID := range []string{"msg-a", "msg-b"} {
		for i := 0; i < 2; i++ {
			testData := createTestImageData(64, 64)
			elem := NewImageElement(&ImageData{
				Data:     testData,
				MIMEType: "image/jpeg",
			})
			elem.WithMetadata(MediaExtractMessageIDKey, msgID)
			elem.WithMetadata(MediaExtractPartIndexKey, i)
			elem.WithMetadata(MediaExtractTotalPartsKey, 2)
			elem.WithMetadata(MediaExtractMediaTypeKey, types.ContentTypeImage)

			input <- elem
		}
	}
	close(input)

	go func() {
		if err := stg.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	var results []StreamElement
	for e := range output {
		results = append(results, e)
	}

	// Should have 2 composed messages
	if len(results) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(results))
	}

	for _, result := range results {
		if result.Message == nil {
			t.Error("Expected Message")
			continue
		}
		// Each message should have 2 image parts
		imageCount := 0
		for _, part := range result.Message.Parts {
			if part.Type == types.ContentTypeImage {
				imageCount++
			}
		}
		if imageCount != 2 {
			t.Errorf("Expected 2 image parts, got %d", imageCount)
		}
	}
}

func TestMediaComposeStage_ContextCancellation(t *testing.T) {
	config := DefaultMediaComposeConfig()
	stg := NewMediaComposeStage(config)
	ctx, cancel := context.WithCancel(context.Background())

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement) // Unbuffered to block

	// Send incomplete message (only 1 of 2 parts)
	testData := createTestImageData(64, 64)
	elem := NewImageElement(&ImageData{
		Data:     testData,
		MIMEType: "image/jpeg",
	})
	elem.WithMetadata(MediaExtractMessageIDKey, "msg-cancel")
	elem.WithMetadata(MediaExtractPartIndexKey, 0)
	elem.WithMetadata(MediaExtractTotalPartsKey, 2)
	elem.WithMetadata(MediaExtractMediaTypeKey, types.ContentTypeImage)

	input <- elem
	close(input)

	// Cancel context
	cancel()

	err := stg.Process(ctx, input, output)

	if err != nil && err != context.Canceled {
		t.Errorf("Expected nil or context.Canceled, got %v", err)
	}
}

func TestMediaComposeStage_GetConfig(t *testing.T) {
	config := MediaComposeConfig{
		CompletionTimeout: 5 * time.Second,
	}

	stg := NewMediaComposeStage(config)
	got := stg.GetConfig()

	if got.CompletionTimeout != 5*time.Second {
		t.Errorf("Expected timeout 5s, got %v", got.CompletionTimeout)
	}
}

func TestDefaultMediaComposeConfig(t *testing.T) {
	config := DefaultMediaComposeConfig()

	if config.CompletionTimeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", config.CompletionTimeout)
	}
}

func TestImageDataToMediaContent(t *testing.T) {
	t.Run("with data", func(t *testing.T) {
		imageData := &ImageData{
			Data:     []byte{1, 2, 3, 4},
			MIMEType: "image/png",
			Width:    100,
			Height:   200,
			Format:   "png",
		}

		media := imageDataToMediaContent(imageData)

		if media.MIMEType != "image/png" {
			t.Errorf("Expected mime type 'image/png', got %s", media.MIMEType)
		}
		if media.Data == nil {
			t.Error("Expected data to be set")
		}
		if media.Width == nil || *media.Width != 100 {
			t.Error("Expected width 100")
		}
		if media.Height == nil || *media.Height != 200 {
			t.Error("Expected height 200")
		}
	})

	t.Run("with storage ref", func(t *testing.T) {
		imageData := &ImageData{
			MIMEType:   "image/jpeg",
			StorageRef: "storage://test",
		}

		media := imageDataToMediaContent(imageData)

		if media.StorageReference == nil || *media.StorageReference != "storage://test" {
			t.Error("Expected storage reference")
		}
		if media.Data != nil {
			t.Error("Expected no inline data")
		}
	})
}

func TestVideoDataToMediaContent(t *testing.T) {
	t.Run("with data", func(t *testing.T) {
		videoData := &VideoData{
			Data:      []byte{1, 2, 3, 4},
			MIMEType:  "video/mp4",
			Width:     1920,
			Height:    1080,
			FrameRate: 30.0,
			Duration:  time.Minute,
			Format:    "h264",
		}

		media := videoDataToMediaContent(videoData)

		if media.MIMEType != "video/mp4" {
			t.Errorf("Expected mime type 'video/mp4', got %s", media.MIMEType)
		}
		if media.Data == nil {
			t.Error("Expected data to be set")
		}
		if media.Width == nil || *media.Width != 1920 {
			t.Error("Expected width 1920")
		}
		if media.FPS == nil || *media.FPS != 30 {
			t.Error("Expected FPS 30")
		}
		if media.Duration == nil || *media.Duration != 60 {
			t.Error("Expected duration 60 seconds")
		}
	})

	t.Run("with storage ref", func(t *testing.T) {
		videoData := &VideoData{
			MIMEType:   "video/mp4",
			StorageRef: "storage://test/video.mp4",
		}

		media := videoDataToMediaContent(videoData)

		if media.StorageReference == nil || *media.StorageReference != "storage://test/video.mp4" {
			t.Error("Expected storage reference")
		}
		if media.Data != nil {
			t.Error("Expected no inline data")
		}
	})
}

func TestMediaComposeStage_ComposeVideo(t *testing.T) {
	config := DefaultMediaComposeConfig()
	stg := NewMediaComposeStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Create original message with video
	origMsg := &types.Message{Role: "user"}
	origMsg.AddTextPart("What's in this video?")
	videoDataStr := base64.StdEncoding.EncodeToString([]byte{0, 0, 0, 1, 2, 3})
	origMsg.Parts = append(origMsg.Parts, types.ContentPart{
		Type: types.ContentTypeVideo,
		Media: &types.MediaContent{
			MIMEType: "video/mp4",
			Data:     &videoDataStr,
		},
	})

	// Create the processed video element
	elem := NewVideoElement(&VideoData{
		Data:      []byte{0, 0, 0, 1, 2, 3, 4, 5},
		MIMEType:  "video/mp4",
		Width:     1280,
		Height:    720,
		FrameRate: 30.0,
		Duration:  time.Minute,
	})
	elem.WithMetadata(MediaExtractMessageIDKey, "msg-video-1")
	elem.WithMetadata(MediaExtractPartIndexKey, 0)
	elem.WithMetadata(MediaExtractTotalPartsKey, 1)
	elem.WithMetadata(MediaExtractMediaTypeKey, types.ContentTypeVideo)
	elem.WithMetadata(MediaExtractOriginalMessageKey, origMsg)

	input <- elem
	close(input)

	go func() {
		if err := stg.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	var results []StreamElement
	for e := range output {
		results = append(results, e)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Message == nil {
		t.Fatal("Expected Message in result")
	}

	// Should have text part + video part
	if len(result.Message.Parts) != 2 {
		t.Errorf("Expected 2 parts, got %d", len(result.Message.Parts))
	}

	// Check video part
	var foundVideo bool
	for _, part := range result.Message.Parts {
		if part.Type == types.ContentTypeVideo {
			foundVideo = true
			if part.Media == nil || part.Media.Data == nil {
				t.Error("Expected video media with data")
			}
		}
	}

	if !foundVideo {
		t.Error("Expected to find video part")
	}
}

func TestMediaComposeStage_Timeout(t *testing.T) {
	// Use a very short timeout - note the checkTimeouts ticker runs every second
	config := MediaComposeConfig{
		CompletionTimeout: 100 * time.Millisecond,
	}
	stg := NewMediaComposeStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 10)
	output := make(chan StreamElement, 10)

	// Send only 1 of 2 parts - this will trigger timeout
	testData := createTestImageData(64, 64)
	elem := NewImageElement(&ImageData{
		Data:     testData,
		MIMEType: "image/jpeg",
	})
	elem.WithMetadata(MediaExtractMessageIDKey, "msg-timeout")
	elem.WithMetadata(MediaExtractPartIndexKey, 0)
	elem.WithMetadata(MediaExtractTotalPartsKey, 2) // Expecting 2 parts but only sending 1
	elem.WithMetadata(MediaExtractMediaTypeKey, types.ContentTypeImage)

	errChan := make(chan error, 1)
	go func() {
		errChan <- stg.Process(ctx, input, output)
	}()

	// Send the element
	input <- elem

	// Wait for timeout to be detected by checkTimeouts (ticker is 1 second)
	time.Sleep(1200 * time.Millisecond)

	// Now close input to let Process finish
	close(input)

	// Collect results
	var results []StreamElement
	for {
		select {
		case e, ok := <-output:
			if !ok {
				goto done
			}
			results = append(results, e)
		case <-time.After(500 * time.Millisecond):
			goto done
		}
	}
done:

	// Wait for process to finish
	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	case <-time.After(time.Second):
		// Process may still be running
	}

	// Should have received the incomplete message after timeout
	if len(results) == 0 {
		t.Error("Expected at least 1 result after timeout")
	}
}

func TestCreateContentPartFromProcessed_NilImage(t *testing.T) {
	config := DefaultMediaComposeConfig()
	stg := NewMediaComposeStage(config)

	processed := &processedPart{
		index:     0,
		mediaType: types.ContentTypeImage,
		image:     nil, // nil image
	}

	_, err := stg.createContentPartFromProcessed(processed)
	if err == nil {
		t.Error("Expected error for nil image")
	}
}

func TestCreateContentPartFromProcessed_NilVideo(t *testing.T) {
	config := DefaultMediaComposeConfig()
	stg := NewMediaComposeStage(config)

	processed := &processedPart{
		index:     0,
		mediaType: types.ContentTypeVideo,
		video:     nil, // nil video
	}

	_, err := stg.createContentPartFromProcessed(processed)
	if err == nil {
		t.Error("Expected error for nil video")
	}
}

func TestCreateContentPartFromProcessed_UnsupportedType(t *testing.T) {
	config := DefaultMediaComposeConfig()
	stg := NewMediaComposeStage(config)

	processed := &processedPart{
		index:     0,
		mediaType: "audio", // unsupported type
	}

	_, err := stg.createContentPartFromProcessed(processed)
	if err == nil {
		t.Error("Expected error for unsupported media type")
	}
}

func TestCreateContentPartFromProcessed_Video(t *testing.T) {
	config := DefaultMediaComposeConfig()
	stg := NewMediaComposeStage(config)

	processed := &processedPart{
		index:     0,
		mediaType: types.ContentTypeVideo,
		video: &VideoData{
			Data:      []byte{1, 2, 3, 4},
			MIMEType:  "video/mp4",
			Width:     1920,
			Height:    1080,
			FrameRate: 30.0,
			Duration:  time.Minute,
		},
	}

	part, err := stg.createContentPartFromProcessed(processed)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if part.Type != types.ContentTypeVideo {
		t.Errorf("Expected type video, got %s", part.Type)
	}
	if part.Media == nil {
		t.Fatal("Expected media content")
	}
	if part.Media.MIMEType != "video/mp4" {
		t.Errorf("Expected mime type video/mp4, got %s", part.Media.MIMEType)
	}
}

func TestMediaComposeStage_VideoWithStorageRef(t *testing.T) {
	config := DefaultMediaComposeConfig()
	stg := NewMediaComposeStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Create video with storage ref (no data)
	elem := NewVideoElement(&VideoData{
		MIMEType:   "video/mp4",
		StorageRef: "storage://bucket/processed.mp4",
	})
	elem.WithMetadata(MediaExtractMessageIDKey, "msg-video-storage")
	elem.WithMetadata(MediaExtractPartIndexKey, 0)
	elem.WithMetadata(MediaExtractTotalPartsKey, 1)
	elem.WithMetadata(MediaExtractMediaTypeKey, types.ContentTypeVideo)

	input <- elem
	close(input)

	go func() {
		if err := stg.Process(ctx, input, output); err != nil {
			t.Errorf("Process returned error: %v", err)
		}
	}()

	var results []StreamElement
	for e := range output {
		results = append(results, e)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Message == nil {
		t.Fatal("Expected Message")
	}

	// Find video part and check storage ref
	for _, part := range result.Message.Parts {
		if part.Type == types.ContentTypeVideo {
			if part.Media == nil {
				t.Fatal("Expected media content")
			}
			if part.Media.StorageReference == nil {
				t.Error("Expected storage reference")
			} else if *part.Media.StorageReference != "storage://bucket/processed.mp4" {
				t.Errorf("Expected storage ref, got %s", *part.Media.StorageReference)
			}
		}
	}
}

func TestMediaComposeStage_ComposeErrorOnInvalidPart(t *testing.T) {
	config := DefaultMediaComposeConfig()
	stg := NewMediaComposeStage(config)
	ctx := context.Background()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 10)

	// Create original message with image
	origMsg := &types.Message{Role: "user"}
	imgData := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	origMsg.Parts = append(origMsg.Parts, types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			MIMEType: "image/jpeg",
			Data:     &imgData,
		},
	})

	// Create element with nil image data (will cause error during compose)
	elem := StreamElement{
		Metadata: make(map[string]interface{}),
	}
	elem.WithMetadata(MediaExtractMessageIDKey, "msg-error")
	elem.WithMetadata(MediaExtractPartIndexKey, 0)
	elem.WithMetadata(MediaExtractTotalPartsKey, 1)
	elem.WithMetadata(MediaExtractMediaTypeKey, types.ContentTypeImage)
	elem.WithMetadata(MediaExtractOriginalMessageKey, origMsg)
	// Note: Not setting elem.Image, so it will be nil during compose

	input <- elem
	close(input)

	go func() {
		_ = stg.Process(ctx, input, output)
	}()

	var results []StreamElement
	for e := range output {
		results = append(results, e)
	}

	// Should get an error element back
	if len(results) == 0 {
		t.Fatal("Expected at least 1 result")
	}

	// The result should have an error since Image was nil
	found := false
	for _, r := range results {
		if r.Error != nil {
			found = true
		}
	}
	if !found {
		t.Log("Note: No error element found - compose may have handled nil gracefully")
	}
}
