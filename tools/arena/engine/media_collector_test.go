package engine

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestCollectMediaOutputs_EmptyMessages(t *testing.T) {
	messages := []types.Message{}
	outputs := CollectMediaOutputs(messages)

	if len(outputs) != 0 {
		t.Errorf("Expected 0 outputs for empty messages, got %d", len(outputs))
	}
}

func TestCollectMediaOutputs_NoMedia(t *testing.T) {
	messages := []types.Message{
		{
			Role:    "user",
			Content: "Hello",
		},
		{
			Role:    "assistant",
			Content: "Hi there",
		},
	}
	outputs := CollectMediaOutputs(messages)

	if len(outputs) != 0 {
		t.Errorf("Expected 0 outputs for text-only messages, got %d", len(outputs))
	}
}

func TestCollectMediaOutputs_OnlyUserMedia(t *testing.T) {
	data := "base64data"
	messages := []types.Message{
		{
			Role:    "user",
			Content: "Look at this image",
			Parts: []types.ContentPart{
				{
					Type: types.ContentTypeImage,
					Media: &types.MediaContent{
						MIMEType: "image/jpeg",
						Data:     &data,
					},
				},
			},
		},
		{
			Role:    "assistant",
			Content: "I see it",
		},
	}
	outputs := CollectMediaOutputs(messages)

	// Should ignore user media (only collects assistant-generated media)
	if len(outputs) != 0 {
		t.Errorf("Expected 0 outputs for user media (should only collect assistant media), got %d", len(outputs))
	}
}

func TestCollectMediaOutputs_AssistantMedia(t *testing.T) {
	data := "base64imagedata"
	messages := []types.Message{
		{
			Role:    "user",
			Content: "Generate an image",
		},
		{
			Role:    "assistant",
			Content: "Here's your image",
			Parts: []types.ContentPart{
				{
					Type: types.ContentTypeImage,
					Media: &types.MediaContent{
						MIMEType: "image/png",
						Data:     &data,
					},
				},
			},
		},
	}
	outputs := CollectMediaOutputs(messages)

	if len(outputs) != 1 {
		t.Fatalf("Expected 1 output, got %d", len(outputs))
	}

	output := outputs[0]
	if output.Type != types.ContentTypeImage {
		t.Errorf("Expected type %s, got %s", types.ContentTypeImage, output.Type)
	}
	if output.MIMEType != "image/png" {
		t.Errorf("Expected MIME type image/png, got %s", output.MIMEType)
	}
	if output.MessageIdx != 1 {
		t.Errorf("Expected MessageIdx 1, got %d", output.MessageIdx)
	}
	if output.PartIdx != 0 {
		t.Errorf("Expected PartIdx 0, got %d", output.PartIdx)
	}
}

func TestCollectMediaOutputs_MultipleMedia(t *testing.T) {
	imageData := "imagedata"
	audioData := "audiodata"
	videoData := "videodata"

	messages := []types.Message{
		{
			Role: "assistant",
			Parts: []types.ContentPart{
				{
					Type: types.ContentTypeImage,
					Media: &types.MediaContent{
						MIMEType: "image/jpeg",
						Data:     &imageData,
					},
				},
				{
					Type: types.ContentTypeAudio,
					Media: &types.MediaContent{
						MIMEType: "audio/mp3",
						Data:     &audioData,
					},
				},
			},
		},
		{
			Role: "assistant",
			Parts: []types.ContentPart{
				{
					Type: types.ContentTypeVideo,
					Media: &types.MediaContent{
						MIMEType: "video/mp4",
						Data:     &videoData,
					},
				},
			},
		},
	}
	outputs := CollectMediaOutputs(messages)

	if len(outputs) != 3 {
		t.Fatalf("Expected 3 outputs, got %d", len(outputs))
	}

	// Check first output (image from message 0)
	if outputs[0].Type != types.ContentTypeImage {
		t.Errorf("Output 0: expected type image, got %s", outputs[0].Type)
	}
	if outputs[0].MessageIdx != 0 || outputs[0].PartIdx != 0 {
		t.Errorf("Output 0: wrong indices, got msg=%d part=%d", outputs[0].MessageIdx, outputs[0].PartIdx)
	}

	// Check second output (audio from message 0)
	if outputs[1].Type != types.ContentTypeAudio {
		t.Errorf("Output 1: expected type audio, got %s", outputs[1].Type)
	}
	if outputs[1].MessageIdx != 0 || outputs[1].PartIdx != 1 {
		t.Errorf("Output 1: wrong indices, got msg=%d part=%d", outputs[1].MessageIdx, outputs[1].PartIdx)
	}

	// Check third output (video from message 1)
	if outputs[2].Type != types.ContentTypeVideo {
		t.Errorf("Output 2: expected type video, got %s", outputs[2].Type)
	}
	if outputs[2].MessageIdx != 1 || outputs[2].PartIdx != 0 {
		t.Errorf("Output 2: wrong indices, got msg=%d part=%d", outputs[2].MessageIdx, outputs[2].PartIdx)
	}
}

func TestCollectMediaFromMessage_NoMedia(t *testing.T) {
	msg := types.Message{
		Role:    "assistant",
		Content: "Text only",
	}
	outputs := collectMediaFromMessage(msg, 0)

	if len(outputs) != 0 {
		t.Errorf("Expected 0 outputs for text-only message, got %d", len(outputs))
	}
}

func TestCollectMediaFromMessage_NilMediaContent(t *testing.T) {
	msg := types.Message{
		Role: "assistant",
		Parts: []types.ContentPart{
			{
				Type:  types.ContentTypeImage,
				Media: nil, // nil media
			},
		},
	}
	outputs := collectMediaFromMessage(msg, 0)

	// Should skip parts with nil media
	if len(outputs) != 0 {
		t.Errorf("Expected 0 outputs for nil media, got %d", len(outputs))
	}
}

func TestCreateMediaOutput_WithMetadata(t *testing.T) {
	data := "base64data"
	width := 800
	height := 600
	duration := 120

	part := types.ContentPart{
		Type: types.ContentTypeVideo,
		Media: &types.MediaContent{
			MIMEType: "video/mp4",
			Data:     &data,
			Width:    &width,
			Height:   &height,
			Duration: &duration,
		},
	}

	output := createMediaOutput(part, 5, 2)

	if output.Type != types.ContentTypeVideo {
		t.Errorf("Expected type video, got %s", output.Type)
	}
	if output.MIMEType != "video/mp4" {
		t.Errorf("Expected MIME type video/mp4, got %s", output.MIMEType)
	}
	if output.MessageIdx != 5 {
		t.Errorf("Expected MessageIdx 5, got %d", output.MessageIdx)
	}
	if output.PartIdx != 2 {
		t.Errorf("Expected PartIdx 2, got %d", output.PartIdx)
	}
	if output.Width == nil || *output.Width != width {
		t.Errorf("Expected Width %d, got %v", width, output.Width)
	}
	if output.Height == nil || *output.Height != height {
		t.Errorf("Expected Height %d, got %v", height, output.Height)
	}
	if output.Duration == nil || *output.Duration != duration {
		t.Errorf("Expected Duration %d, got %v", duration, output.Duration)
	}
}

func TestCreateMediaOutput_WithFilePath(t *testing.T) {
	filePath := "/path/to/image.jpg"
	part := types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			MIMEType: "image/jpeg",
			FilePath: &filePath,
		},
	}

	output := createMediaOutput(part, 0, 0)

	if output.FilePath != filePath {
		t.Errorf("Expected FilePath %s, got %s", filePath, output.FilePath)
	}
}

func TestCreateMediaOutput_ImageWithThumbnail(t *testing.T) {
	// Small data that should generate thumbnail
	smallData := "smallbase64data"
	part := types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			MIMEType: "image/png",
			Data:     &smallData,
		},
	}

	output := createMediaOutput(part, 0, 0)

	if output.Type != types.ContentTypeImage {
		t.Errorf("Expected type image, got %s", output.Type)
	}
	// Small data should have thumbnail
	if output.Thumbnail == "" {
		t.Error("Expected thumbnail to be generated for small image data")
	}
	if output.Thumbnail != smallData {
		t.Error("Expected thumbnail to match original small data")
	}
}

func TestCreateMediaOutput_ImageWithoutThumbnail(t *testing.T) {
	// Large data that should not generate thumbnail
	largeData := make([]byte, 60000)
	for i := range largeData {
		largeData[i] = 'A'
	}
	largeDataStr := string(largeData)

	part := types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			MIMEType: "image/jpeg",
			Data:     &largeDataStr,
		},
	}

	output := createMediaOutput(part, 0, 0)

	// Large data should not have thumbnail
	if output.Thumbnail != "" {
		t.Error("Expected no thumbnail for large image data")
	}
}

func TestCalculateMediaSize_FromSizeKB(t *testing.T) {
	sizeKB := int64(150)
	media := &types.MediaContent{
		SizeKB: &sizeKB,
	}

	size := calculateMediaSize(media)

	expected := sizeKB * 1024
	if size != expected {
		t.Errorf("Expected size %d, got %d", expected, size)
	}
}

func TestCalculateMediaSize_FromBase64Data(t *testing.T) {
	// Base64 data of 1000 characters should be ~750 bytes decoded
	data := make([]byte, 1000)
	for i := range data {
		data[i] = 'A'
	}
	dataStr := string(data)

	media := &types.MediaContent{
		Data: &dataStr,
	}

	size := calculateMediaSize(media)

	// Should be roughly 3/4 of encoded size
	expected := int64(1000 * 3 / 4)
	if size != expected {
		t.Errorf("Expected size ~%d, got %d", expected, size)
	}
}

func TestCalculateMediaSize_NoData(t *testing.T) {
	media := &types.MediaContent{
		MIMEType: "image/jpeg",
		// No size, no data
	}

	size := calculateMediaSize(media)

	if size != 0 {
		t.Errorf("Expected size 0 for no data, got %d", size)
	}
}

func TestGenerateThumbnail_NoData(t *testing.T) {
	media := &types.MediaContent{
		MIMEType: "image/png",
		// No data
	}

	thumbnail := generateThumbnail(media)

	if thumbnail != "" {
		t.Error("Expected empty thumbnail when no data available")
	}
}

func TestGenerateThumbnail_EmptyData(t *testing.T) {
	emptyData := ""
	media := &types.MediaContent{
		MIMEType: "image/png",
		Data:     &emptyData,
	}

	thumbnail := generateThumbnail(media)

	if thumbnail != "" {
		t.Error("Expected empty thumbnail for empty data")
	}
}

func TestGenerateThumbnail_SmallData(t *testing.T) {
	// Data within threshold (~37.5KB decoded = ~50000 base64 chars)
	smallData := make([]byte, 40000)
	for i := range smallData {
		smallData[i] = 'B'
	}
	dataStr := string(smallData)

	media := &types.MediaContent{
		MIMEType: "image/jpeg",
		Data:     &dataStr,
	}

	thumbnail := generateThumbnail(media)

	if thumbnail != dataStr {
		t.Error("Expected thumbnail to be the original data for small images")
	}
}

func TestGenerateThumbnail_LargeData(t *testing.T) {
	// Data exceeding threshold
	largeData := make([]byte, 60000)
	for i := range largeData {
		largeData[i] = 'C'
	}
	dataStr := string(largeData)

	media := &types.MediaContent{
		MIMEType: "image/png",
		Data:     &dataStr,
	}

	thumbnail := generateThumbnail(media)

	if thumbnail != "" {
		t.Error("Expected no thumbnail for large images")
	}
}

func TestGetMediaOutputStatistics_Empty(t *testing.T) {
	outputs := []MediaOutput{}
	stats := GetMediaOutputStatistics(outputs)

	if stats.Total != 0 {
		t.Errorf("Expected Total 0, got %d", stats.Total)
	}
	if stats.ImageCount != 0 {
		t.Errorf("Expected ImageCount 0, got %d", stats.ImageCount)
	}
	if stats.AudioCount != 0 {
		t.Errorf("Expected AudioCount 0, got %d", stats.AudioCount)
	}
	if stats.VideoCount != 0 {
		t.Errorf("Expected VideoCount 0, got %d", stats.VideoCount)
	}
	if stats.TotalSizeBytes != 0 {
		t.Errorf("Expected TotalSizeBytes 0, got %d", stats.TotalSizeBytes)
	}
	if len(stats.ByType) != 0 {
		t.Errorf("Expected empty ByType map, got %d entries", len(stats.ByType))
	}
}

func TestGetMediaOutputStatistics_MultipleTypes(t *testing.T) {
	outputs := []MediaOutput{
		{
			Type:      types.ContentTypeImage,
			SizeBytes: 1000,
		},
		{
			Type:      types.ContentTypeImage,
			SizeBytes: 2000,
		},
		{
			Type:      types.ContentTypeAudio,
			SizeBytes: 5000,
		},
		{
			Type:      types.ContentTypeVideo,
			SizeBytes: 10000,
		},
		{
			Type:      types.ContentTypeVideo,
			SizeBytes: 15000,
		},
	}

	stats := GetMediaOutputStatistics(outputs)

	if stats.Total != 5 {
		t.Errorf("Expected Total 5, got %d", stats.Total)
	}
	if stats.ImageCount != 2 {
		t.Errorf("Expected ImageCount 2, got %d", stats.ImageCount)
	}
	if stats.AudioCount != 1 {
		t.Errorf("Expected AudioCount 1, got %d", stats.AudioCount)
	}
	if stats.VideoCount != 2 {
		t.Errorf("Expected VideoCount 2, got %d", stats.VideoCount)
	}

	expectedSize := int64(1000 + 2000 + 5000 + 10000 + 15000)
	if stats.TotalSizeBytes != expectedSize {
		t.Errorf("Expected TotalSizeBytes %d, got %d", expectedSize, stats.TotalSizeBytes)
	}

	if stats.ByType[types.ContentTypeImage] != 2 {
		t.Errorf("Expected ByType[image] 2, got %d", stats.ByType[types.ContentTypeImage])
	}
	if stats.ByType[types.ContentTypeAudio] != 1 {
		t.Errorf("Expected ByType[audio] 1, got %d", stats.ByType[types.ContentTypeAudio])
	}
	if stats.ByType[types.ContentTypeVideo] != 2 {
		t.Errorf("Expected ByType[video] 2, got %d", stats.ByType[types.ContentTypeVideo])
	}
}

func TestFormatMediaType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{types.ContentTypeImage, "Image"},
		{types.ContentTypeAudio, "Audio"},
		{types.ContentTypeVideo, "Video"},
		{"unknown", "unknown"},
		{"custom-type", "custom-type"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := FormatMediaType(tt.input)
			if result != tt.expected {
				t.Errorf("FormatMediaType(%s) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatFileSize(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1023, "1023 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1024 * 1024, "1.00 MB"},
		{1024*1024 + 512*1024, "1.50 MB"},
		{1024 * 1024 * 1024, "1.00 GB"},
		{1024*1024*1024 + 512*1024*1024, "1.50 GB"},
		{2 * 1024 * 1024 * 1024, "2.00 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := FormatFileSize(tt.bytes)
			if result != tt.expected {
				t.Errorf("FormatFileSize(%d) = %s, want %s", tt.bytes, result, tt.expected)
			}
		})
	}
}

func TestCollectMediaOutputs_WithSize(t *testing.T) {
	sizeKB := int64(250)
	data := "somedata"

	messages := []types.Message{
		{
			Role: "assistant",
			Parts: []types.ContentPart{
				{
					Type: types.ContentTypeImage,
					Media: &types.MediaContent{
						MIMEType: "image/png",
						Data:     &data,
						SizeKB:   &sizeKB,
					},
				},
			},
		},
	}

	outputs := CollectMediaOutputs(messages)

	if len(outputs) != 1 {
		t.Fatalf("Expected 1 output, got %d", len(outputs))
	}

	expectedSize := sizeKB * 1024
	if outputs[0].SizeBytes != expectedSize {
		t.Errorf("Expected SizeBytes %d, got %d", expectedSize, outputs[0].SizeBytes)
	}
}

func TestCreateMediaOutput_NonImageNoThumbnail(t *testing.T) {
	data := "audiodata"
	part := types.ContentPart{
		Type: types.ContentTypeAudio,
		Media: &types.MediaContent{
			MIMEType: "audio/mp3",
			Data:     &data,
		},
	}

	output := createMediaOutput(part, 0, 0)

	// Non-image types should never have thumbnails
	if output.Thumbnail != "" {
		t.Error("Expected no thumbnail for non-image media types")
	}
}

func TestCalculateMediaSize_EmptyData(t *testing.T) {
	emptyData := ""
	media := &types.MediaContent{
		Data: &emptyData,
	}

	size := calculateMediaSize(media)

	if size != 0 {
		t.Errorf("Expected size 0 for empty data, got %d", size)
	}
}

func TestGetMediaOutputStatistics_ZeroSize(t *testing.T) {
	outputs := []MediaOutput{
		{
			Type:      types.ContentTypeImage,
			SizeBytes: 0,
		},
		{
			Type:      types.ContentTypeAudio,
			SizeBytes: 0,
		},
	}

	stats := GetMediaOutputStatistics(outputs)

	if stats.Total != 2 {
		t.Errorf("Expected Total 2, got %d", stats.Total)
	}
	if stats.TotalSizeBytes != 0 {
		t.Errorf("Expected TotalSizeBytes 0, got %d", stats.TotalSizeBytes)
	}
	if stats.ImageCount != 1 {
		t.Errorf("Expected ImageCount 1, got %d", stats.ImageCount)
	}
	if stats.AudioCount != 1 {
		t.Errorf("Expected AudioCount 1, got %d", stats.AudioCount)
	}
}
