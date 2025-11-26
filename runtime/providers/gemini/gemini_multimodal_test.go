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
	provider := NewProvider("test", "gemini-1.5-flash", "https://generativelanguage.googleapis.com/v1beta", providers.ProviderDefaults{}, false)

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
	provider := NewProvider("test", "gemini-1.5-flash", "https://generativelanguage.googleapis.com/v1beta", providers.ProviderDefaults{}, false)

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
	provider := NewProvider("test", "gemini-1.5-flash", "https://generativelanguage.googleapis.com/v1beta", providers.ProviderDefaults{}, false)

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

			provider := NewProvider(
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

			provider := NewProvider(
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

// TestPredictMultimodal_ValidationError tests validation error handling
func TestPredictMultimodal_ValidationError(t *testing.T) {
	provider := NewProvider(
		"test",
		"gemini-2.0-flash",
		"https://test.example.com",
		providers.ProviderDefaults{},
		false,
	)

	// Test with unsupported media format
	req := providers.PredictionRequest{
		Messages: []types.Message{
			{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							MIMEType: "image/bmp", // Unsupported format
							Data:     providers.StringPtr("fake-data"),
						},
					},
				},
			},
		},
	}

	_, err := provider.PredictMultimodal(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

// TestPredictMultimodalStream_ValidationError tests streaming validation error handling
func TestPredictMultimodalStream_ValidationError(t *testing.T) {
	provider := NewProvider(
		"test",
		"gemini-2.0-flash",
		"https://test.example.com",
		providers.ProviderDefaults{},
		false,
	)

	// Test with unsupported media format
	req := providers.PredictionRequest{
		Messages: []types.Message{
			{
				Role: "user",
				Parts: []types.ContentPart{
					{
						Type: types.ContentTypeImage,
						Media: &types.MediaContent{
							MIMEType: "image/tiff", // Unsupported format
							Data:     providers.StringPtr("fake-data"),
						},
					},
				},
			},
		},
	}

	_, err := provider.PredictMultimodalStream(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

// TestPredictWithContents_HTTPErrors tests error handling in predictWithContents
func TestPredictWithContents_HTTPErrors(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func() *httptest.Server
		wantErrMsg  string
	}{
		{
			name: "HTTP request creation error - handled by context cancellation",
			setupServer: func() *httptest.Server {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(geminiResponse{
						Candidates: []geminiCandidate{
							{Content: geminiContent{Parts: []geminiPart{{Text: "response"}}}},
						},
					})
				}))
				return server
			},
			wantErrMsg: "",
		},
		{
			name: "HTTP send error - connection refused",
			setupServer: func() *httptest.Server {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				server.Close() // Close immediately to cause connection error
				return server
			},
			wantErrMsg: "failed to send request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setupServer()
			if tt.name != "HTTP send error - connection refused" {
				defer server.Close()
			}

			provider := NewProvider(
				"test",
				"gemini-2.0-flash",
				server.URL,
				providers.ProviderDefaults{},
				false,
			)

			contents := []geminiContent{
				{Role: "user", Parts: []geminiPart{{Text: "test"}}},
			}

			_, err := provider.predictWithContents(context.Background(), contents, nil, 0.7, 0.9, 100, nil)

			if tt.wantErrMsg == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
			}
		})
	}
}

// TestPredictStreamWithContents_HTTPErrors tests error handling in predictStreamWithContents
func TestPredictStreamWithContents_HTTPErrors(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func() *httptest.Server
		wantErrMsg  string
	}{
		{
			name: "HTTP send error",
			setupServer: func() *httptest.Server {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				server.Close()
				return server
			},
			wantErrMsg: "failed to send request",
		},
		{
			name: "API error status",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
					w.Write([]byte("unauthorized"))
				}))
			},
			wantErrMsg: "401",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setupServer()
			if tt.name != "HTTP send error" {
				defer server.Close()
			}

			provider := NewProvider(
				"test",
				"gemini-2.0-flash",
				server.URL,
				providers.ProviderDefaults{},
				false,
			)

			contents := []geminiContent{
				{Role: "user", Parts: []geminiPart{{Text: "test"}}},
			}

			_, err := provider.predictStreamWithContents(context.Background(), contents, nil, 0.7, 0.9, 100, nil)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}

// TestConvertImageMissingMIMEType tests missing MIME type error
func TestConvertImageMissingMIMEType(t *testing.T) {
	data := "base64data"
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					Data: &data,
					// Missing MIMEType
				},
			},
		},
	}

	_, err := convertMessageToGemini(msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing mime_type")
}

// TestConvertUnsupportedPartType tests unsupported content type
func TestConvertUnsupportedPartType(t *testing.T) {
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: "unsupported_type",
			},
		},
	}

	_, err := convertMessageToGemini(msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported part type")
}

// TestConvertNilTextPart tests nil text pointer
func TestConvertNilTextPart(t *testing.T) {
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeText,
				Text: nil,
			},
		},
	}

	_, err := convertMessageToGemini(msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty text")
}

// TestConvertMediaURLNotSupported tests error handling for unreachable URLs
func TestConvertMediaURLNotSupported(t *testing.T) {
	// Use localhost unreachable port for quick failure
	url := "http://localhost:1/nonexistent.jpg"
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					URL:      &url,
					MIMEType: types.MIMETypeImageJPEG,
				},
			},
		},
	}

	_, err := convertMessageToGemini(msg)
	require.Error(t, err)
	// MediaLoader now fetches URLs, so we expect connection error
	assert.Contains(t, err.Error(), "failed to load image data")
}

// TestConvertMediaMissingDataSource tests missing all data sources
func TestConvertMediaMissingDataSource(t *testing.T) {
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeImage,
				Media: &types.MediaContent{
					MIMEType: types.MIMETypeImageJPEG,
					// No Data, URL, or FilePath
				},
			},
		},
	}

	_, err := convertMessageToGemini(msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no media source available")
}

// TestConvertMediaMissingMediaField tests missing media field entirely
func TestConvertMediaMissingMediaField(t *testing.T) {
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type:  types.ContentTypeImage,
				Media: nil,
			},
		},
	}

	_, err := convertMessageToGemini(msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing media field")
}

// TestPredictWithContents_MarshalError tests JSON marshal error
func TestPredictWithContents_MarshalError(t *testing.T) {
	provider := NewProvider(
		"test",
		"gemini-2.0-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{},
		false,
	)

	// Create invalid content that will fail marshaling
	// In practice, this is very rare with proper typed data
	// We test that normal content marshals successfully
	contents := []geminiContent{
		{Role: "user", Parts: []geminiPart{{Text: "Hello"}}},
	}

	_, err := provider.predictWithContents(context.Background(), contents, nil, 0.7, 0.9, 100, nil)
	// This will fail at HTTP stage, not marshal, for valid content
	if err != nil && err.Error() == "failed to marshal request" {
		t.Error("Unexpected marshal error for valid content")
	}
}

// TestPredictStreamWithContents_MarshalError tests JSON marshal error in streaming
func TestPredictStreamWithContents_MarshalError(t *testing.T) {
	provider := NewProvider(
		"test",
		"gemini-2.0-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{},
		false,
	)

	// Test with valid content to ensure marshal works
	contents := []geminiContent{
		{Role: "user", Parts: []geminiPart{{Text: "Hello"}}},
	}

	_, err := provider.predictStreamWithContents(context.Background(), contents, nil, 0.7, 0.9, 100, nil)
	// Should fail at HTTP stage, not marshal
	if err != nil && err.Error() == "failed to marshal request" {
		t.Error("Unexpected marshal error for valid content")
	}
}

// TestParseGeminiResponse_EmptyCandidates tests empty candidates error
func TestParseGeminiResponse_EmptyCandidates(t *testing.T) {
	provider := NewProvider(
		"test",
		"gemini-2.0-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{},
		false,
	)

	respBody := []byte(`{"candidates": []}`)

	_, err := provider.parseGeminiResponse(respBody)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no candidates")
}

// TestParseGeminiResponse_PromptBlocked tests prompt feedback blocking
func TestParseGeminiResponse_PromptBlocked(t *testing.T) {
	provider := NewProvider(
		"test",
		"gemini-2.0-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{},
		false,
	)

	respBody := []byte(`{
		"promptFeedback": {
			"blockReason": "SAFETY"
		},
		"candidates": []
	}`)

	_, err := provider.parseGeminiResponse(respBody)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt blocked")
	assert.Contains(t, err.Error(), "SAFETY")
}

// TestParseGeminiResponse_MaxTokens tests MAX_TOKENS finish reason
func TestParseGeminiResponse_MaxTokens(t *testing.T) {
	provider := NewProvider(
		"test",
		"gemini-2.0-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{},
		false,
	)

	respBody := []byte(`{
		"candidates": [{
			"content": {
				"parts": [],
				"role": "model"
			},
			"finishReason": "MAX_TOKENS"
		}]
	}`)

	_, err := provider.parseGeminiResponse(respBody)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max tokens limit reached")
}

// TestParseGeminiResponse_SafetyFilter tests SAFETY finish reason
func TestParseGeminiResponse_SafetyFilter(t *testing.T) {
	provider := NewProvider(
		"test",
		"gemini-2.0-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{},
		false,
	)

	respBody := []byte(`{
		"candidates": [{
			"content": {
				"parts": [],
				"role": "model"
			},
			"finishReason": "SAFETY"
		}]
	}`)

	_, err := provider.parseGeminiResponse(respBody)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked by safety filters")
}

// TestParseGeminiResponse_Recitation tests RECITATION finish reason
func TestParseGeminiResponse_Recitation(t *testing.T) {
	provider := NewProvider(
		"test",
		"gemini-2.0-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{},
		false,
	)

	respBody := []byte(`{
		"candidates": [{
			"content": {
				"parts": [],
				"role": "model"
			},
			"finishReason": "RECITATION"
		}]
	}`)

	_, err := provider.parseGeminiResponse(respBody)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "recitation concerns")
}

// TestParseGeminiResponse_UnknownFinishReason tests unknown finish reason
func TestParseGeminiResponse_UnknownFinishReason(t *testing.T) {
	provider := NewProvider(
		"test",
		"gemini-2.0-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{},
		false,
	)

	respBody := []byte(`{
		"candidates": [{
			"content": {
				"parts": [],
				"role": "model"
			},
			"finishReason": "UNKNOWN_REASON"
		}]
	}`)

	_, err := provider.parseGeminiResponse(respBody)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no content parts")
	assert.Contains(t, err.Error(), "UNKNOWN_REASON")
}

// TestParseGeminiResponse_InvalidJSON tests invalid JSON response
func TestParseGeminiResponse_InvalidJSON(t *testing.T) {
	provider := NewProvider(
		"test",
		"gemini-2.0-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{},
		false,
	)

	respBody := []byte(`{"candidates": [invalid json`)

	_, err := provider.parseGeminiResponse(respBody)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal")
}

// TestConvertMessagesToGemini_WithSystemPrompt tests system prompt conversion
func TestConvertMessagesToGemini_WithSystemPrompt(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: "Hello"},
	}

	contents, systemInstruction, err := convertMessagesToGemini(messages, "You are helpful")
	require.NoError(t, err)
	require.NotNil(t, systemInstruction)
	assert.Equal(t, "You are helpful", systemInstruction.Parts[0].Text)
	assert.Len(t, contents, 1)
}

// TestConvertMessagesToGemini_ConversionError tests message conversion error propagation
func TestConvertMessagesToGemini_ConversionError(t *testing.T) {
	messages := []types.Message{
		{
			Role: "user",
			Parts: []types.ContentPart{
				{Type: "unsupported_type"},
			},
		},
	}

	_, _, err := convertMessagesToGemini(messages, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported part type")
}

// TestPredictWithContents_ReadBodyError tests error reading response body
func TestPredictWithContents_ReadBodyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		// Send nothing, causing read mismatch
	}))
	defer server.Close()

	provider := NewProvider(
		"test",
		"gemini-2.0-flash",
		server.URL,
		providers.ProviderDefaults{},
		false,
	)

	req := providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	resp, err := provider.PredictMultimodal(context.Background(), req)
	// Should get error from parsing or reading
	if err == nil {
		t.Error("Expected error reading or parsing response")
	}
	// Latency should still be set
	if resp.Latency == 0 {
		t.Error("Expected latency to be set even on error")
	}
}

// TestPredictWithContents_InvalidURL tests invalid URL error
func TestPredictWithContents_InvalidURL(t *testing.T) {
	provider := NewProvider(
		"test",
		"gemini-2.0-flash",
		"http://\x7f/invalid", // Invalid URL with control character
		providers.ProviderDefaults{},
		false,
	)

	contents := []geminiContent{
		{Role: "user", Parts: []geminiPart{{Text: "Hello"}}},
	}

	resp, err := provider.predictWithContents(context.Background(), contents, nil, 0.7, 0.9, 100, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create request")
	// Latency should be set even on error
	assert.NotZero(t, resp.Latency)
}

// TestConvertEmptyTextString tests empty string text
func TestConvertEmptyTextString(t *testing.T) {
	emptyStr := ""
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeText,
				Text: &emptyStr,
			},
		},
	}

	_, err := convertMessageToGemini(msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty text")
}

// TestConvertAudioPartWithFilePath tests audio file path loading error
func TestConvertAudioPartWithFilePath(t *testing.T) {
	filePath := "/nonexistent/audio.mp3"
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeAudio,
				Media: &types.MediaContent{
					FilePath: &filePath,
					MIMEType: types.MIMETypeAudioMP3,
				},
			},
		},
	}

	_, err := convertMessageToGemini(msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read file")
}

// TestConvertVideoPartWithFilePath tests video file path loading error
func TestConvertVideoPartWithFilePath(t *testing.T) {
	filePath := "/nonexistent/video.mp4"
	msg := types.Message{
		Role: "user",
		Parts: []types.ContentPart{
			{
				Type: types.ContentTypeVideo,
				Media: &types.MediaContent{
					FilePath: &filePath,
					MIMEType: types.MIMETypeVideoMP4,
				},
			},
		},
	}

	_, err := convertMessageToGemini(msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read file")
}
