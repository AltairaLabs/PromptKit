package types

import (
	"encoding/json"
	"testing"
	"time"
)

// TestMessage_MarshalJSON_TextOnly tests marshaling of text-only messages
func TestMessage_MarshalJSON_TextOnly(t *testing.T) {
	msg := Message{
		Role:    "user",
		Content: "Hello, world!",
	}

	data, err := json.Marshal(&msg)
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Should have content
	if result["content"] != "Hello, world!" {
		t.Errorf("Expected content 'Hello, world!', got %v", result["content"])
	}

	// Should not have media_summary
	if _, exists := result["media_summary"]; exists {
		t.Error("Text-only message should not have media_summary")
	}
}

// TestMessage_MarshalJSON_MultimodalWithText tests multimodal message with text
func TestMessage_MarshalJSON_MultimodalWithText(t *testing.T) {
	text := "Analyze this image:"
	msg := Message{
		Role: "user",
		Parts: []ContentPart{
			{
				Type: ContentTypeText,
				Text: &text,
			},
			{
				Type: ContentTypeImage,
				Media: &MediaContent{
					FilePath: strPtr("/path/to/image.jpg"),
					MIMEType: MIMETypeImageJPEG,
				},
			},
		},
	}

	data, err := json.Marshal(&msg)
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}

	t.Logf("JSON output: %s", string(data))

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Should have content with text and media summary
	content, ok := result["content"].(string)
	if !ok {
		t.Fatalf("Content field should be a string, got type %T: %v", result["content"], result["content"])
	}

	if content != "Analyze this image: [1 image(s)]" {
		t.Errorf("Unexpected content: %s", content)
	}

	// Should have media_summary
	_, exists := result["media_summary"]
	if !exists {
		t.Error("Multimodal message should have media_summary")
	}
}

// TestMessage_MarshalJSON_MultipleImages tests message with multiple images
func TestMessage_MarshalJSON_MultipleImages(t *testing.T) {
	text := "Compare these images:"
	msg := Message{
		Role: "user",
		Parts: []ContentPart{
			{
				Type: ContentTypeText,
				Text: &text,
			},
			{
				Type: ContentTypeImage,
				Media: &MediaContent{
					FilePath: strPtr("/path/to/image1.jpg"),
					MIMEType: MIMETypeImageJPEG,
				},
			},
			{
				Type: ContentTypeImage,
				Media: &MediaContent{
					FilePath: strPtr("/path/to/image2.png"),
					MIMEType: MIMETypeImagePNG,
				},
			},
		},
	}

	data, err := json.Marshal(&msg)
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	content, _ := result["content"].(string)
	if content != "Compare these images: [2 image(s)]" {
		t.Errorf("Unexpected content: %s", content)
	}

	// Check media_summary
	summary := result["media_summary"].(map[string]interface{})
	if summary["image_parts"].(float64) != 2 {
		t.Errorf("Expected 2 image parts, got %v", summary["image_parts"])
	}
}

// TestMessage_MarshalJSON_MixedMedia tests message with multiple media types
func TestMessage_MarshalJSON_MixedMedia(t *testing.T) {
	text := "Mixed media content:"
	msg := Message{
		Role: "user",
		Parts: []ContentPart{
			{
				Type: ContentTypeText,
				Text: &text,
			},
			{
				Type: ContentTypeImage,
				Media: &MediaContent{
					FilePath: strPtr("/path/to/image.jpg"),
					MIMEType: MIMETypeImageJPEG,
				},
			},
			{
				Type: ContentTypeAudio,
				Media: &MediaContent{
					FilePath: strPtr("/path/to/audio.wav"),
					MIMEType: MIMETypeAudioWAV,
				},
			},
		},
	}

	data, err := json.Marshal(&msg)
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	content, _ := result["content"].(string)
	if content != "Mixed media content: [1 image(s), 1 audio file(s)]" {
		t.Errorf("Unexpected content: %s", content)
	}

	// Check media_summary
	summary := result["media_summary"].(map[string]interface{})
	if summary["image_parts"].(float64) != 1 {
		t.Errorf("Expected 1 image part, got %v", summary["image_parts"])
	}
	if summary["audio_parts"].(float64) != 1 {
		t.Errorf("Expected 1 audio part, got %v", summary["audio_parts"])
	}
}

// TestMessage_MarshalJSON_InlineData tests message with base64 data
func TestMessage_MarshalJSON_InlineData(t *testing.T) {
	data := "base64encodeddata=="
	msg := Message{
		Role: "user",
		Parts: []ContentPart{
			{
				Type: ContentTypeImage,
				Media: &MediaContent{
					Data:     &data,
					MIMEType: MIMETypeImageJPEG,
				},
			},
		},
	}

	jsonData, err := json.Marshal(&msg)
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(jsonData, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check media_summary
	summary := result["media_summary"].(map[string]interface{})
	mediaItems := summary["media_items"].([]interface{})
	if len(mediaItems) != 1 {
		t.Fatalf("Expected 1 media item, got %d", len(mediaItems))
	}

	item := mediaItems[0].(map[string]interface{})
	if item["source"] != "inline data" {
		t.Errorf("Expected source 'inline data', got %v", item["source"])
	}
	if item["loaded"] != true {
		t.Error("Inline data should be marked as loaded")
	}
}

// TestMessage_MarshalJSON_URLSource tests message with URL source
func TestMessage_MarshalJSON_URLSource(t *testing.T) {
	url := "https://example.com/image.jpg"
	msg := Message{
		Role: "user",
		Parts: []ContentPart{
			{
				Type: ContentTypeImage,
				Media: &MediaContent{
					URL:      &url,
					MIMEType: MIMETypeImageJPEG,
				},
			},
		},
	}

	jsonData, err := json.Marshal(&msg)
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(jsonData, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check media_summary
	summary := result["media_summary"].(map[string]interface{})
	mediaItems := summary["media_items"].([]interface{})
	if len(mediaItems) != 1 {
		t.Fatalf("Expected 1 media item, got %d", len(mediaItems))
	}

	item := mediaItems[0].(map[string]interface{})
	if item["source"] != url {
		t.Errorf("Expected source '%s', got %v", url, item["source"])
	}
}

// TestMessage_MarshalJSON_PreservesOtherFields tests that other fields are preserved
func TestMessage_MarshalJSON_PreservesOtherFields(t *testing.T) {
	now := time.Now()
	text := "Test message"
	msg := Message{
		Role:      "assistant",
		Timestamp: now,
		LatencyMs: 123,
		CostInfo: &CostInfo{
			InputTokens:  10,
			OutputTokens: 20,
			TotalCost:    0.001,
		},
		Parts: []ContentPart{
			{
				Type: ContentTypeText,
				Text: &text,
			},
		},
	}

	jsonData, err := json.Marshal(&msg)
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(jsonData, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Verify other fields are preserved
	if result["role"] != "assistant" {
		t.Errorf("Role not preserved")
	}
	if result["latency_ms"].(float64) != 123 {
		t.Errorf("LatencyMs not preserved")
	}
	if _, exists := result["cost_info"]; !exists {
		t.Error("CostInfo not preserved")
	}
	if _, exists := result["timestamp"]; !exists {
		t.Error("Timestamp not preserved")
	}
}

// TestMessage_MarshalJSON_WithDetail tests image detail level in summary
func TestMessage_MarshalJSON_WithDetail(t *testing.T) {
	detail := "high"
	msg := Message{
		Role: "user",
		Parts: []ContentPart{
			{
				Type: ContentTypeImage,
				Media: &MediaContent{
					FilePath: strPtr("/path/to/image.jpg"),
					MIMEType: MIMETypeImageJPEG,
					Detail:   &detail,
				},
			},
		},
	}

	jsonData, err := json.Marshal(&msg)
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(jsonData, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check media_summary has detail
	summary := result["media_summary"].(map[string]interface{})
	mediaItems := summary["media_items"].([]interface{})
	item := mediaItems[0].(map[string]interface{})

	if item["detail"] != "high" {
		t.Errorf("Expected detail 'high', got %v", item["detail"])
	}
}

// TestMessage_MarshalJSON_VideoContent tests video content marshaling
func TestMessage_MarshalJSON_VideoContent(t *testing.T) {
	text := "Watch this:"
	msg := Message{
		Role: "user",
		Parts: []ContentPart{
			{
				Type: ContentTypeText,
				Text: &text,
			},
			{
				Type: ContentTypeVideo,
				Media: &MediaContent{
					FilePath: strPtr("/path/to/video.mp4"),
					MIMEType: MIMETypeVideoMP4,
				},
			},
		},
	}

	jsonData, err := json.Marshal(&msg)
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(jsonData, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	content, _ := result["content"].(string)
	if content != "Watch this: [1 video(s)]" {
		t.Errorf("Unexpected content: %s", content)
	}

	// Check media_summary
	summary := result["media_summary"].(map[string]interface{})
	if summary["video_parts"].(float64) != 1 {
		t.Errorf("Expected 1 video part, got %v", summary["video_parts"])
	}
}

// TestMessage_MarshalJSON_MediaOnlyNoText tests message with only media, no text
func TestMessage_MarshalJSON_MediaOnlyNoText(t *testing.T) {
	msg := Message{
		Role: "user",
		Parts: []ContentPart{
			{
				Type: ContentTypeImage,
				Media: &MediaContent{
					FilePath: strPtr("/path/to/image.jpg"),
					MIMEType: MIMETypeImageJPEG,
				},
			},
		},
	}

	jsonData, err := json.Marshal(&msg)
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(jsonData, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	content, _ := result["content"].(string)
	if content != "[1 image(s)]" {
		t.Errorf("Unexpected content: %s", content)
	}
}

// TestMediaSummary_Counts tests that MediaSummary counts are accurate
func TestMediaSummary_Counts(t *testing.T) {
	text1 := "First text"
	text2 := "Second text"
	msg := Message{
		Role: "user",
		Parts: []ContentPart{
			{Type: ContentTypeText, Text: &text1},
			{Type: ContentTypeImage, Media: &MediaContent{FilePath: strPtr("/img1.jpg"), MIMEType: MIMETypeImageJPEG}},
			{Type: ContentTypeImage, Media: &MediaContent{FilePath: strPtr("/img2.png"), MIMEType: MIMETypeImagePNG}},
			{Type: ContentTypeText, Text: &text2},
			{Type: ContentTypeAudio, Media: &MediaContent{FilePath: strPtr("/audio.wav"), MIMEType: MIMETypeAudioWAV}},
			{Type: ContentTypeVideo, Media: &MediaContent{FilePath: strPtr("/video.mp4"), MIMEType: MIMETypeVideoMP4}},
		},
	}

	summary := msg.getMediaSummary()

	if summary.TotalParts != 6 {
		t.Errorf("Expected 6 total parts, got %d", summary.TotalParts)
	}
	if summary.TextParts != 2 {
		t.Errorf("Expected 2 text parts, got %d", summary.TextParts)
	}
	if summary.ImageParts != 2 {
		t.Errorf("Expected 2 image parts, got %d", summary.ImageParts)
	}
	if summary.AudioParts != 1 {
		t.Errorf("Expected 1 audio part, got %d", summary.AudioParts)
	}
	if summary.VideoParts != 1 {
		t.Errorf("Expected 1 video part, got %d", summary.VideoParts)
	}

	// MediaItems should not include text parts
	if len(summary.MediaItems) != 4 {
		t.Errorf("Expected 4 media items (excluding text), got %d", len(summary.MediaItems))
	}
}

// Helper function to create string pointers
func strPtr(s string) *string {
	return &s
}
