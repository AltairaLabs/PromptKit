package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiProvider_GetMultimodalCapabilities(t *testing.T) {
	provider := NewGeminiProvider("test", "gemini-1.5-flash", "https://generativelanguage.googleapis.com/v1beta", providers.ProviderDefaults{}, false)

	caps := provider.GetMultimodalCapabilities()

	if !caps.SupportsImages {
		t.Error("Expected Gemini to support images")
	}

	if !caps.SupportsAudio {
		t.Error("Expected Gemini to support audio")
	}

	if !caps.SupportsVideo {
		t.Error("Expected Gemini to support video")
	}

	if len(caps.ImageFormats) == 0 {
		t.Error("Expected Gemini to have supported image formats")
	}

	if len(caps.AudioFormats) == 0 {
		t.Error("Expected Gemini to have supported audio formats")
	}

	if len(caps.VideoFormats) == 0 {
		t.Error("Expected Gemini to have supported video formats")
	}
}

func TestGeminiProvider_SupportsMultimodal(t *testing.T) {
	provider := NewGeminiProvider("test", "gemini-1.5-flash", "https://generativelanguage.googleapis.com/v1beta", providers.ProviderDefaults{}, false)

	if !providers.SupportsMultimodal(provider) {
		t.Error("Expected Gemini provider to support multimodal")
	}
}

func TestGeminiProvider_ConvertLegacyMessage(t *testing.T) {
	msg := types.Message{
		Role:    "user",
		Content: "Hello, world!",
	}

	converted, err := convertMessageToGemini(msg)
	if err != nil {
		t.Fatalf("Failed to convert legacy message: %v", err)
	}

	if converted.Role != "user" {
		t.Errorf("Expected role 'user', got '%s'", converted.Role)
	}

	if len(converted.Parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(converted.Parts))
	}

	if converted.Parts[0].Text != "Hello, world!" {
		t.Errorf("Expected text 'Hello, world!', got '%s'", converted.Parts[0].Text)
	}
}

func TestGeminiProvider_ConvertAssistantRole(t *testing.T) {
	msg := types.Message{
		Role:    "assistant",
		Content: "I can help!",
	}

	converted, err := convertMessageToGemini(msg)
	if err != nil {
		t.Fatalf("Failed to convert message: %v", err)
	}

	// Gemini uses "model" instead of "assistant"
	if converted.Role != "model" {
		t.Errorf("Expected role 'model', got '%s'", converted.Role)
	}
}

func TestGeminiProvider_ConvertTextOnlyMultimodalMessage(t *testing.T) {
	text := "What's in this image?"
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{Type: "text", Text: &text},
		},
	}

	converted, err := convertMessageToGemini(msg)
	if err != nil {
		t.Fatalf("Failed to convert message: %v", err)
	}

	if converted.Role != "user" {
		t.Errorf("Expected role 'user', got '%s'", converted.Role)
	}

	if len(converted.Parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(converted.Parts))
	}

	if converted.Parts[0].Text != "What's in this image?" {
		t.Errorf("Expected text 'What's in this image?', got '%s'", converted.Parts[0].Text)
	}
}

func TestGeminiProvider_ConvertImageBase64Message(t *testing.T) {
	text := "What's in this image?"
	imageData := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{Type: "text", Text: &text},
			{
				Type: "image",
				Media: &types.MediaContent{
					Data:     &imageData,
					MIMEType: "image/png",
				},
			},
		},
	}

	converted, err := convertMessageToGemini(msg)
	if err != nil {
		t.Fatalf("Failed to convert message: %v", err)
	}

	if len(converted.Parts) != 2 {
		t.Fatalf("Expected 2 parts, got %d", len(converted.Parts))
	}

	// Check text part
	if converted.Parts[0].Text != "What's in this image?" {
		t.Errorf("Expected text part, got '%s'", converted.Parts[0].Text)
	}

	// Check image part
	if converted.Parts[1].InlineData == nil {
		t.Fatal("Expected inline data for image")
	}

	if converted.Parts[1].InlineData.MimeType != "image/png" {
		t.Errorf("Expected mime type 'image/png', got '%s'", converted.Parts[1].InlineData.MimeType)
	}

	if converted.Parts[1].InlineData.Data != imageData {
		t.Error("Image data doesn't match")
	}
}

func TestGeminiProvider_ConvertMultipleImages(t *testing.T) {
	text := "Compare these images"
	image1 := "base64data1"
	image2 := "base64data2"

	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{Type: "text", Text: &text},
			{
				Type: "image",
				Media: &types.MediaContent{
					Data:     &image1,
					MIMEType: "image/jpeg",
				},
			},
			{
				Type: "image",
				Media: &types.MediaContent{
					Data:     &image2,
					MIMEType: "image/png",
				},
			},
		},
	}

	converted, err := convertMessageToGemini(msg)
	if err != nil {
		t.Fatalf("Failed to convert message: %v", err)
	}

	if len(converted.Parts) != 3 {
		t.Fatalf("Expected 3 parts, got %d", len(converted.Parts))
	}

	// Check that we have 1 text and 2 images
	if converted.Parts[0].Text != "Compare these images" {
		t.Error("First part should be text")
	}

	if converted.Parts[1].InlineData == nil || converted.Parts[1].InlineData.MimeType != "image/jpeg" {
		t.Error("Second part should be JPEG image")
	}

	if converted.Parts[2].InlineData == nil || converted.Parts[2].InlineData.MimeType != "image/png" {
		t.Error("Third part should be PNG image")
	}
}

func TestGeminiProvider_ConvertImageURLReturnsError(t *testing.T) {
	url := "https://example.com/image.jpg"
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: "image",
				Media: &types.MediaContent{
					URL:      &url,
					MIMEType: "image/jpeg",
				},
			},
		},
	}

	_, err := convertMessageToGemini(msg)
	if err == nil {
		t.Error("Expected error for URL-based images (Gemini doesn't support URLs)")
	}
}

func TestGeminiProvider_ConvertEmptyTextPart(t *testing.T) {
	emptyText := ""
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{Type: "text", Text: &emptyText},
		},
	}

	_, err := convertMessageToGemini(msg)
	if err == nil {
		t.Error("Expected error for empty text part")
	}
}

func TestGeminiProvider_ConvertImageMissingMedia(t *testing.T) {
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{Type: "image", Media: nil},
		},
	}

	_, err := convertMessageToGemini(msg)
	if err == nil {
		t.Error("Expected error for image without media")
	}
}

func TestGeminiProvider_ConvertImageMissingDataSource(t *testing.T) {
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: "image",
				Media: &types.MediaContent{
					MIMEType: "image/jpeg",
					// No Data, URL, or FilePath
				},
			},
		},
	}

	_, err := convertMessageToGemini(msg)
	if err == nil {
		t.Error("Expected error for image without data source")
	}
}

func TestGeminiProvider_ValidateMultimodalMessage(t *testing.T) {
	provider := NewGeminiProvider("test", "gemini-1.5-flash", "https://generativelanguage.googleapis.com/v1beta", providers.ProviderDefaults{}, false)

	tests := []struct {
		name        string
		message     types.Message
		shouldError bool
	}{
		{
			name: "text only message",
			message: types.Message{
				Role:    "user",
				Content: "Hello",
			},
			shouldError: false,
		},
		{
			name: "valid image message",
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: "image",
						Media: &types.MediaContent{
							Data:     providers.StringPtr("base64data"),
							MIMEType: "image/jpeg",
						},
					},
				},
			},
			shouldError: false,
		},
		{
			name: "valid audio message",
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: "audio",
						Media: &types.MediaContent{
							Data:     providers.StringPtr("base64data"),
							MIMEType: "audio/mp3",
						},
					},
				},
			},
			shouldError: false,
		},
		{
			name: "valid video message",
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: "video",
						Media: &types.MediaContent{
							Data:     providers.StringPtr("base64data"),
							MIMEType: "video/mp4",
						},
					},
				},
			},
			shouldError: false,
		},
		{
			name: "unsupported image format",
			message: types.Message{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: "image",
						Media: &types.MediaContent{
							Data:     providers.StringPtr("base64data"),
							MIMEType: "image/bmp", // Not supported
						},
					},
				},
			},
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := providers.ValidateMultimodalMessage(provider, tt.message)
			if tt.shouldError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestGeminiProvider_ConvertMessagesToGemini(t *testing.T) {
	text1 := "First message"
	text2 := "Second message"
	imageData := "base64imagedata"

	messages := []types.Message{
		{
			Role: "user",
			Parts: []types.ContentPart{
				{Type: "text", Text: &text1},
			},
		},
		{
			Role: "assistant",
			Parts: []types.ContentPart{
				{Type: "text", Text: &text2},
			},
		},
		{
			Role: "user",
			Parts: []types.ContentPart{
				{
					Type: "image",
					Media: &types.MediaContent{
						Data:     &imageData,
						MIMEType: "image/png",
					},
				},
			},
		},
	}

	contents, sysInst, err := convertMessagesToGemini(messages, "You are helpful")
	if err != nil {
		t.Fatalf("Failed to convert messages: %v", err)
	}

	if sysInst == nil {
		t.Fatal("Expected system instruction")
	}

	if len(sysInst.Parts) != 1 || sysInst.Parts[0].Text != "You are helpful" {
		t.Error("System instruction not set correctly")
	}

	if len(contents) != 3 {
		t.Fatalf("Expected 3 contents, got %d", len(contents))
	}

	// Check first message (user)
	if contents[0].Role != "user" {
		t.Errorf("Expected first message role 'user', got '%s'", contents[0].Role)
	}

	// Check second message (model, not assistant)
	if contents[1].Role != "model" {
		t.Errorf("Expected second message role 'model', got '%s'", contents[1].Role)
	}

	// Check third message has image
	if contents[2].Parts[0].InlineData == nil {
		t.Error("Expected third message to have inline data")
	}
}

func TestGeminiProvider_AudioSupport(t *testing.T) {
	audioData := "base64audiodata"
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: "audio",
				Media: &types.MediaContent{
					Data:     &audioData,
					MIMEType: "audio/mp3",
				},
			},
		},
	}

	converted, err := convertMessageToGemini(msg)
	if err != nil {
		t.Fatalf("Failed to convert audio message: %v", err)
	}

	if len(converted.Parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(converted.Parts))
	}

	if converted.Parts[0].InlineData == nil {
		t.Fatal("Expected inline data for audio")
	}

	if converted.Parts[0].InlineData.MimeType != "audio/mp3" {
		t.Errorf("Expected mime type 'audio/mp3', got '%s'", converted.Parts[0].InlineData.MimeType)
	}
}

func TestGeminiProvider_VideoSupport(t *testing.T) {
	videoData := "base64videodata"
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: "video",
				Media: &types.MediaContent{
					Data:     &videoData,
					MIMEType: "video/mp4",
				},
			},
		},
	}

	converted, err := convertMessageToGemini(msg)
	if err != nil {
		t.Fatalf("Failed to convert video message: %v", err)
	}

	if len(converted.Parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(converted.Parts))
	}

	if converted.Parts[0].InlineData == nil {
		t.Fatal("Expected inline data for video")
	}

	if converted.Parts[0].InlineData.MimeType != "video/mp4" {
		t.Errorf("Expected mime type 'video/mp4', got '%s'", converted.Parts[0].InlineData.MimeType)
	}
}

func TestGeminiProvider_MixedMultimodal(t *testing.T) {
	text := "Analyze this audio and video"
	audioData := "base64audio"
	videoData := "base64video"

	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{Type: "text", Text: &text},
			{
				Type: "audio",
				Media: &types.MediaContent{
					Data:     &audioData,
					MIMEType: "audio/wav",
				},
			},
			{
				Type: "video",
				Media: &types.MediaContent{
					Data:     &videoData,
					MIMEType: "video/mp4",
				},
			},
		},
	}

	converted, err := convertMessageToGemini(msg)
	if err != nil {
		t.Fatalf("Failed to convert mixed multimodal message: %v", err)
	}

	if len(converted.Parts) != 3 {
		t.Fatalf("Expected 3 parts, got %d", len(converted.Parts))
	}

	// Verify each part type
	if converted.Parts[0].Text != text {
		t.Error("First part should be text")
	}

	if converted.Parts[1].InlineData == nil || converted.Parts[1].InlineData.MimeType != "audio/wav" {
		t.Error("Second part should be audio")
	}

	if converted.Parts[2].InlineData == nil || converted.Parts[2].InlineData.MimeType != "video/mp4" {
		t.Error("Third part should be video")
	}
}

// TestPredictMultimodal_Integration tests the PredictMultimodal method with HTTP mocking
func TestPredictMultimodal_Integration(t *testing.T) {
	tests := []struct {
		name           string
		messages       []types.Message
		serverResponse geminiResponse
		serverStatus   int
		wantErr        bool
		errContains    string
	}{
		{
			name: "Successful multimodal request with image",
			messages: []types.Message{
				{
					Role: "user",
					Parts: []types.ContentPart{
						types.NewTextPart("What's in this image?"),
						{
							Type: types.ContentTypeImage,
							Media: &types.MediaContent{
								MIMEType: "image/jpeg",
								Data:     providers.StringPtr(string("fake-image-data")),
							},
						},
					},
				},
			},
			serverResponse: geminiResponse{
				Candidates: []geminiCandidate{
					{
						Content: geminiContent{
							Parts: []geminiPart{{Text: "I see an interesting image."}},
							Role:  "model",
						},
						FinishReason: "STOP",
					},
				},
				UsageMetadata: &geminiUsage{
					PromptTokenCount:     150,
					CandidatesTokenCount: 50,
					TotalTokenCount:      200,
				},
			},
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name: "API error response",
			messages: []types.Message{
				{
					Role: "user",
					Parts: []types.ContentPart{
						types.NewTextPart("Test"),
					},
				},
			},
			serverResponse: geminiResponse{},
			serverStatus:   http.StatusBadRequest,
			wantErr:        true,
			errContains:    "400",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.serverStatus)
				json.NewEncoder(w).Encode(tt.serverResponse)
			}))
			defer server.Close()

			provider := NewGeminiProvider(
				"test",
				"gemini-2.0-flash",
				server.URL,
				providers.ProviderDefaults{
					Temperature: 0.7,
					TopP:        0.9,
					MaxTokens:   1000,
				},
				false,
			)

			req := providers.PredictionRequest{
				Messages:    tt.messages,
				Temperature: 0.8,
				MaxTokens:   500,
			}

			resp, err := provider.PredictMultimodal(context.Background(), req)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, resp.Content)
			assert.NotNil(t, resp.CostInfo)
			assert.Greater(t, resp.Latency, time.Duration(0))
		})
	}
}

// TestPredictMultimodalStream_Integration tests the PredictMultimodalStream method
func TestPredictMultimodalStream_Integration(t *testing.T) {
	tests := []struct {
		name         string
		messages     []types.Message
		serverChunks []geminiResponse
		wantErr      bool
		errContains  string
	}{
		{
			name: "Successful streaming with image",
			messages: []types.Message{
				{
					Role: "user",
					Parts: []types.ContentPart{
						types.NewTextPart("Describe this"),
						{
							Type: types.ContentTypeImage,
							Media: &types.MediaContent{
								MIMEType: "image/png",
								Data:     providers.StringPtr("image-data"),
							},
						},
					},
				},
			},
			serverChunks: []geminiResponse{
				{
					Candidates: []geminiCandidate{
						{
							Content: geminiContent{
								Parts: []geminiPart{{Text: "This"}},
							},
						},
					},
				},
				{
					Candidates: []geminiCandidate{
						{
							Content: geminiContent{
								Parts: []geminiPart{{Text: " is nice"}},
							},
							FinishReason: "STOP",
						},
					},
					UsageMetadata: &geminiUsage{
						PromptTokenCount:     100,
						CandidatesTokenCount: 30,
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(tt.serverChunks)
			}))
			defer server.Close()

			provider := NewGeminiProvider(
				"test",
				"gemini-2.0-flash",
				server.URL,
				providers.ProviderDefaults{Temperature: 0.7},
				false,
			)

			req := providers.PredictionRequest{
				Messages: tt.messages,
			}

			streamChan, err := provider.PredictMultimodalStream(context.Background(), req)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, streamChan)

			var chunks []providers.StreamChunk
			for chunk := range streamChan {
				chunks = append(chunks, chunk)
			}

			assert.NotEmpty(t, chunks)

			lastChunk := chunks[len(chunks)-1]
			assert.NotNil(t, lastChunk.FinishReason)
		})
	}
}
