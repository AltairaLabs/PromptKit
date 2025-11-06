package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChatMultimodal_Integration tests multimodal chat with HTTP mocking
func TestChatMultimodal_Integration(t *testing.T) {
	tests := []struct {
		name           string
		request        providers.ChatRequest
		mockResponse   claudeResponse
		mockStatusCode int
		expectError    bool
		errorContains  string
	}{
		{
			name: "Successful multimodal request with image",
			request: providers.ChatRequest{
				Messages: []types.Message{
					{
						Role: "user",
						Parts: []types.ContentPart{
							types.NewTextPart("What's in this image?"),
							types.NewImagePartFromData("base64encodedimagedata", types.MIMETypeImageJPEG, nil),
						},
					},
				},
			},
			mockResponse: claudeResponse{
				ID:    "msg_123",
				Type:  "message",
				Role:  "assistant",
				Model: "claude-3-opus",
				Content: []claudeContent{
					{Type: "text", Text: "I can see a cat in the image."},
				},
				Usage: claudeUsage{
					InputTokens:  50,
					OutputTokens: 20,
				},
			},
			mockStatusCode: http.StatusOK,
			expectError:    false,
		},
		{
			name: "API error response",
			request: providers.ChatRequest{
				Messages: []types.Message{
					{
						Role: "user",
						Parts: []types.ContentPart{
							types.NewTextPart("Hello"),
						},
					},
				},
			},
			mockResponse: claudeResponse{
				Error: &claudeError{
					Type:    "invalid_request_error",
					Message: "Invalid multimodal content",
				},
			},
			mockStatusCode: http.StatusOK,
			expectError:    true,
			errorContains:  "Invalid multimodal content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "POST", r.Method)
				assert.Equal(t, "/messages", r.URL.Path)

				w.WriteHeader(tt.mockStatusCode)
				if tt.mockStatusCode == http.StatusOK {
					respBody, _ := json.Marshal(tt.mockResponse)
					w.Write(respBody)
				}
			}))
			defer server.Close()

			// Create provider with mock server URL
			base := providers.NewBaseProvider("test", false, &http.Client{})
			provider := &ClaudeProvider{
				BaseProvider: base,
				model:        "claude-3-opus",
				baseURL:      server.URL,
				apiKey:       "test-key",
				defaults: providers.ProviderDefaults{
					Temperature: 0.5,
					TopP:        0.9,
					MaxTokens:   200,
				},
			}

			// Execute
			resp, err := provider.ChatMultimodal(context.Background(), tt.request)

			// Assert
			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, resp.Content)
				assert.NotNil(t, resp.CostInfo)
			}
		})
	}
}

// TestChatMultimodalStream_Integration tests streaming multimodal chat
func TestChatMultimodalStream_Integration(t *testing.T) {
	t.Run("Successful streaming with image", func(t *testing.T) {
		// Create mock server that returns SSE stream
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "POST", r.Method)
			assert.Equal(t, "/messages", r.URL.Path)

			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)

			// Simulate SSE stream
			events := []string{
				`event: message_start
data: {"type":"message_start","message":{"id":"msg_123","type":"message","role":"assistant"}}

`,
				`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

`,
				`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"I see"}}

`,
				`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" a cat"}}

`,
				`event: content_block_stop
data: {"type":"content_block_stop","index":0}

`,
				`event: message_stop
data: {"type":"message_stop","message":{"usage":{"input_tokens":50,"output_tokens":10}}}

`,
			}

			flusher, _ := w.(http.Flusher)
			for _, event := range events {
				w.Write([]byte(event))
				flusher.Flush()
			}
		}))
		defer server.Close()

		// Create provider with mock server URL
		base := providers.NewBaseProvider("test", false, &http.Client{})
		provider := &ClaudeProvider{
			BaseProvider: base,
			model:        "claude-3-opus",
			baseURL:      server.URL,
			apiKey:       "test-key",
			defaults: providers.ProviderDefaults{
				Temperature: 0.5,
				TopP:        0.9,
				MaxTokens:   200,
			},
		}

		// Execute
		streamChan, err := provider.ChatMultimodalStream(context.Background(), providers.ChatRequest{
			Messages: []types.Message{
				{
					Role: "user",
					Parts: []types.ContentPart{
						types.NewTextPart("What's in this image?"),
						types.NewImagePartFromData("base64data", types.MIMETypeImagePNG, nil),
					},
				},
			},
		})
		require.NoError(t, err)

		// Collect chunks
		var chunks []providers.StreamChunk
		for chunk := range streamChan {
			chunks = append(chunks, chunk)
		}

		// Assert
		assert.NotEmpty(t, chunks)
		lastChunk := chunks[len(chunks)-1]
		assert.NotNil(t, lastChunk.FinishReason)
	})
}
