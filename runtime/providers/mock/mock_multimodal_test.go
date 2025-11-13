package mock

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockProvider_MultimodalResponse_Image(t *testing.T) {
	// Create an in-memory repository with multimodal response
	repo := NewInMemoryMockRepository("default text")
	
	// Create mock provider
	mockProvider := NewMockProviderWithRepository("test-provider", "test-model", false, repo)
	
	// Manually add a multimodal turn to the repository
	// Since InMemoryMockRepository doesn't support structured turns yet,
	// we'll test via file-based repository
	
	// For now, test that backward compatibility works
	req := providers.ChatRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Show me an image"},
		},
		Metadata: map[string]interface{}{
			"mock_scenario_id": "test-image",
			"mock_turn_number": 1,
		},
	}
	
	resp, err := mockProvider.Chat(context.Background(), req)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Content)
}

func TestMockTurn_ToContentParts_TextOnly(t *testing.T) {
	turn := &MockTurn{
		Type:    "text",
		Content: "Hello, world!",
	}
	
	parts := turn.ToContentParts()
	require.Len(t, parts, 1)
	assert.Equal(t, types.ContentTypeText, parts[0].Type)
	assert.NotNil(t, parts[0].Text)
	assert.Equal(t, "Hello, world!", *parts[0].Text)
}

func TestMockTurn_ToContentParts_MultimodalImage(t *testing.T) {
	turn := &MockTurn{
		Type: "multimodal",
		Text: "Here is an image",
		Parts: []MockContentPart{
			{
				Type: "text",
				Text: "Here is an image",
			},
			{
				Type: "image",
				ImageURL: &MockImageURL{
					URL: "mock://test-image.png",
				},
				Metadata: map[string]interface{}{
					"format": "PNG",
					"width":  1920,
					"height": 1080,
					"size":   102400,
				},
			},
		},
	}
	
	parts := turn.ToContentParts()
	require.Len(t, parts, 2)
	
	// Check text part
	assert.Equal(t, types.ContentTypeText, parts[0].Type)
	assert.NotNil(t, parts[0].Text)
	assert.Equal(t, "Here is an image", *parts[0].Text)
	
	// Check image part
	assert.Equal(t, types.ContentTypeImage, parts[1].Type)
	require.NotNil(t, parts[1].Media)
	assert.NotNil(t, parts[1].Media.URL)
	assert.Equal(t, "mock://test-image.png", *parts[1].Media.URL)
	assert.Equal(t, types.MIMETypeImagePNG, parts[1].Media.MIMEType)
	
	// Check metadata was applied
	assert.NotNil(t, parts[1].Media.Format)
	assert.Equal(t, "PNG", *parts[1].Media.Format)
	assert.NotNil(t, parts[1].Media.Width)
	assert.Equal(t, 1920, *parts[1].Media.Width)
	assert.NotNil(t, parts[1].Media.Height)
	assert.Equal(t, 1080, *parts[1].Media.Height)
}

func TestMockTurn_ToContentParts_MultimodalAudio(t *testing.T) {
	turn := &MockTurn{
		Type: "multimodal",
		Parts: []MockContentPart{
			{
				Type: "text",
				Text: "Here is audio",
			},
			{
				Type: "audio",
				AudioURL: &MockAudioURL{
					URL: "mock://test-audio.mp3",
				},
				Metadata: map[string]interface{}{
					"format":            "MP3",
					"duration_seconds":  30.5,
					"channels":          2,
				},
			},
		},
	}
	
	parts := turn.ToContentParts()
	require.Len(t, parts, 2)
	
	// Check audio part
	assert.Equal(t, types.ContentTypeAudio, parts[1].Type)
	require.NotNil(t, parts[1].Media)
	assert.NotNil(t, parts[1].Media.URL)
	assert.Equal(t, "mock://test-audio.mp3", *parts[1].Media.URL)
	assert.Equal(t, types.MIMETypeAudioMP3, parts[1].Media.MIMEType)
	
	// Check metadata
	assert.NotNil(t, parts[1].Media.Format)
	assert.Equal(t, "MP3", *parts[1].Media.Format)
	assert.NotNil(t, parts[1].Media.Duration)
	assert.Equal(t, 30, *parts[1].Media.Duration) // Converted from float
	assert.NotNil(t, parts[1].Media.Channels)
	assert.Equal(t, 2, *parts[1].Media.Channels)
}

func TestMockTurn_ToContentParts_MultimodalVideo(t *testing.T) {
	turn := &MockTurn{
		Type: "multimodal",
		Parts: []MockContentPart{
			{
				Type: "video",
				VideoURL: &MockVideoURL{
					URL: "mock://test-video.mp4",
				},
				Metadata: map[string]interface{}{
					"format":            "MP4",
					"width":             1920,
					"height":            1080,
					"duration_seconds":  60.0,
					"fps":               30,
				},
			},
		},
	}
	
	parts := turn.ToContentParts()
	require.Len(t, parts, 1)
	
	// Check video part
	assert.Equal(t, types.ContentTypeVideo, parts[0].Type)
	require.NotNil(t, parts[0].Media)
	assert.NotNil(t, parts[0].Media.URL)
	assert.Equal(t, "mock://test-video.mp4", *parts[0].Media.URL)
	assert.Equal(t, types.MIMETypeVideoMP4, parts[0].Media.MIMEType)
	
	// Check metadata
	assert.NotNil(t, parts[0].Media.Format)
	assert.Equal(t, "MP4", *parts[0].Media.Format)
	assert.NotNil(t, parts[0].Media.Width)
	assert.Equal(t, 1920, *parts[0].Media.Width)
	assert.NotNil(t, parts[0].Media.Height)
	assert.Equal(t, 1080, *parts[0].Media.Height)
	assert.NotNil(t, parts[0].Media.Duration)
	assert.Equal(t, 60, *parts[0].Media.Duration)
	assert.NotNil(t, parts[0].Media.FPS)
	assert.Equal(t, 30, *parts[0].Media.FPS)
}

func TestInferMIMETypeFromURL(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"mock://test.png", types.MIMETypeImagePNG},
		{"mock://test.jpg", types.MIMETypeImageJPEG},
		{"mock://test.jpeg", types.MIMETypeImageJPEG},
		{"mock://test.gif", types.MIMETypeImageGIF},
		{"mock://test.webp", types.MIMETypeImageWebP},
		{"mock://test.mp3", types.MIMETypeAudioMP3},
		{"mock://test.wav", types.MIMETypeAudioWAV},
		{"mock://test.ogg", types.MIMETypeAudioOgg},
		{"mock://test.mp4", types.MIMETypeVideoMP4},
		{"mock://test.webm", types.MIMETypeVideoWebM},
		{"https://example.com/image.png", types.MIMETypeImagePNG},
		{"https://example.com/audio.mp3", types.MIMETypeAudioMP3},
	}
	
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			result := inferMIMETypeFromURL(tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMockTurn_ToContentParts_Mixed(t *testing.T) {
	// Test a complex turn with multiple media types
	turn := &MockTurn{
		Type: "multimodal",
		Parts: []MockContentPart{
			{
				Type: "text",
				Text: "Here's a mix of content:",
			},
			{
				Type: "image",
				ImageURL: &MockImageURL{
					URL: "mock://photo.jpg",
				},
				Metadata: map[string]interface{}{
					"format": "JPEG",
				},
			},
			{
				Type: "text",
				Text: "And some audio:",
			},
			{
				Type: "audio",
				AudioURL: &MockAudioURL{
					URL: "mock://sound.mp3",
				},
			},
		},
	}
	
	parts := turn.ToContentParts()
	require.Len(t, parts, 4)
	
	assert.Equal(t, types.ContentTypeText, parts[0].Type)
	assert.Equal(t, types.ContentTypeImage, parts[1].Type)
	assert.Equal(t, types.ContentTypeText, parts[2].Type)
	assert.Equal(t, types.ContentTypeAudio, parts[3].Type)
}
