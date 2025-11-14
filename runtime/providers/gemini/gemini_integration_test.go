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

// TestPredict_Integration tests the Predict method with HTTP mocking
func TestPredict_Integration(t *testing.T) {
	tests := []struct {
		name           string
		messages       []types.Message
		system         string
		serverResponse geminiResponse
		serverStatus   int
		wantErr        bool
		errContains    string
	}{
		{
			name: "Successful chat request",
			messages: []types.Message{
				{Role: "user", Content: "Hello"},
			},
			system: "You are helpful",
			serverResponse: geminiResponse{
				Candidates: []geminiCandidate{
					{
						Content: geminiContent{
							Parts: []geminiPart{{Text: "Hi there!"}},
							Role:  "model",
						},
						FinishReason: "STOP",
					},
				},
				UsageMetadata: &geminiUsage{
					PromptTokenCount:     10,
					CandidatesTokenCount: 5,
					TotalTokenCount:      15,
				},
			},
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name: "API error response",
			messages: []types.Message{
				{Role: "user", Content: "Test"},
			},
			serverResponse: geminiResponse{},
			serverStatus:   http.StatusBadRequest,
			wantErr:        true,
			errContains:    "400",
		},
		{
			name: "Empty candidates",
			messages: []types.Message{
				{Role: "user", Content: "Test"},
			},
			serverResponse: geminiResponse{
				Candidates: []geminiCandidate{},
			},
			serverStatus: http.StatusOK,
			wantErr:      true,
			errContains:  "no candidates",
		},
		{
			name: "Safety blocked response",
			messages: []types.Message{
				{Role: "user", Content: "Test"},
			},
			serverResponse: geminiResponse{
				Candidates: []geminiCandidate{
					{
						Content:      geminiContent{Parts: []geminiPart{{Text: ""}}},
						FinishReason: "SAFETY",
					},
				},
			},
			serverStatus: http.StatusOK,
			wantErr:      true,
			errContains:  "safety filters",
		},
		{
			name: "Prompt blocked with feedback",
			messages: []types.Message{
				{Role: "user", Content: "Bad content"},
			},
			serverResponse: geminiResponse{
				Candidates: []geminiCandidate{},
				PromptFeedback: &geminiPromptFeedback{
					BlockReason: "SAFETY",
					SafetyRatings: []geminiSafetyRating{
						{Category: "HARM_CATEGORY_HARASSMENT", Probability: "HIGH"},
					},
				},
			},
			serverStatus: http.StatusOK,
			wantErr:      true,
			errContains:  "prompt blocked",
		},
		{
			name: "MAX_TOKENS finish reason",
			messages: []types.Message{
				{Role: "user", Content: "Test"},
			},
			serverResponse: geminiResponse{
				Candidates: []geminiCandidate{
					{
						Content:      geminiContent{Parts: []geminiPart{}},
						FinishReason: "MAX_TOKENS",
					},
				},
			},
			serverStatus: http.StatusOK,
			wantErr:      true,
			errContains:  "MAX_TOKENS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.serverStatus)
				json.NewEncoder(w).Encode(tt.serverResponse)
			}))
			defer server.Close()

			// Create provider
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

			// Create request
			req := providers.PredictionRequest{
				Messages:    tt.messages,
				System:      tt.system,
				Temperature: 0.8,
			}

			// Execute
			resp, err := provider.Predict(context.Background(), req)

			// Validate
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

// TestPredictStream_Integration tests the PredictStream method
func TestPredictStream_Integration(t *testing.T) {
	tests := []struct {
		name         string
		messages     []types.Message
		serverChunks []geminiResponse
		wantErr      bool
		errContains  string
	}{
		{
			name: "Successful streaming",
			messages: []types.Message{
				{Role: "user", Content: "Hello"},
			},
			serverChunks: []geminiResponse{
				{
					Candidates: []geminiCandidate{
						{
							Content: geminiContent{
								Parts: []geminiPart{{Text: "Hello"}},
							},
						},
					},
				},
				{
					Candidates: []geminiCandidate{
						{
							Content: geminiContent{
								Parts: []geminiPart{{Text: " there"}},
							},
						},
					},
				},
				{
					Candidates: []geminiCandidate{
						{
							Content: geminiContent{
								Parts: []geminiPart{{Text: "!"}},
							},
							FinishReason: "STOP",
						},
					},
					UsageMetadata: &geminiUsage{
						PromptTokenCount:     10,
						CandidatesTokenCount: 5,
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(tt.serverChunks)
			}))
			defer server.Close()

			// Create provider
			provider := NewGeminiProvider(
				"test",
				"gemini-2.0-flash",
				server.URL,
				providers.ProviderDefaults{Temperature: 0.7},
				false,
			)

			// Create request
			req := providers.PredictionRequest{
				Messages: tt.messages,
			}

			// Execute
			streamChan, err := provider.PredictStream(context.Background(), req)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, streamChan)

			// Collect chunks
			var chunks []providers.StreamChunk
			for chunk := range streamChan {
				chunks = append(chunks, chunk)
			}

			// Should have received multiple chunks
			assert.NotEmpty(t, chunks)

			// Last chunk should have finish reason
			lastChunk := chunks[len(chunks)-1]
			assert.NotNil(t, lastChunk.FinishReason)
			assert.Equal(t, "STOP", *lastChunk.FinishReason)
			assert.NotNil(t, lastChunk.CostInfo)
		})
	}
}

// TestHelperFunctions tests the extracted helper functions
func TestMakeGeminiHTTPRequest(t *testing.T) {
	tests := []struct {
		name         string
		serverStatus int
		serverBody   interface{}
		wantErr      bool
		errContains  string
	}{
		{
			name:         "Successful request",
			serverStatus: http.StatusOK,
			serverBody:   geminiResponse{Candidates: []geminiCandidate{{Content: geminiContent{Parts: []geminiPart{{Text: "ok"}}}}}},
			wantErr:      false,
		},
		{
			name:         "HTTP error",
			serverStatus: http.StatusInternalServerError,
			serverBody:   map[string]string{"error": "Internal error"},
			wantErr:      true,
			errContains:  "500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.serverStatus)
				json.NewEncoder(w).Encode(tt.serverBody)
			}))
			defer server.Close()

			provider := NewGeminiProvider("test", "gemini-2.0-flash", server.URL, providers.ProviderDefaults{}, false)

			geminiReq := geminiRequest{
				Contents: []geminiContent{{Parts: []geminiPart{{Text: "test"}}}},
				GenerationConfig: geminiGenConfig{
					Temperature:     0.7,
					TopP:            0.9,
					MaxOutputTokens: 1000,
				},
			}

			chatResp := providers.PredictionResponse{}
			start := time.Now()

			respBody, chatResp, err := provider.makeGeminiHTTPRequest(context.Background(), geminiReq, chatResp, start)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, respBody)
		})
	}
}

// TestParseAndValidateGeminiResponse tests response parsing
func TestParseAndValidateGeminiResponse(t *testing.T) {
	tests := []struct {
		name        string
		respBody    string
		wantErr     bool
		errContains string
	}{
		{
			name:     "Valid response",
			respBody: `{"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"},"finishReason":"STOP"}]}`,
			wantErr:  false,
		},
		{
			name:        "Invalid JSON",
			respBody:    `{invalid json`,
			wantErr:     true,
			errContains: "unmarshal",
		},
		{
			name:        "Empty candidates",
			respBody:    `{"candidates":[]}`,
			wantErr:     true,
			errContains: "no candidates",
		},
		{
			name:        "Safety blocked",
			respBody:    `{"candidates":[{"content":{"parts":[{"text":""}]},"finishReason":"SAFETY"}]}`,
			wantErr:     true,
			errContains: "safety filters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewGeminiProvider("test", "gemini-2.0-flash", "https://api.test", providers.ProviderDefaults{}, false)

			chatResp := providers.PredictionResponse{}
			start := time.Now()

			_, candidate, _, err := provider.parseAndValidateGeminiResponse([]byte(tt.respBody), chatResp, start)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, candidate.Content.Parts)
		})
	}
}

// TestHandleNoCandidatesError tests error handling for blocked prompts
func TestHandleNoCandidatesError(t *testing.T) {
	provider := NewGeminiProvider("test", "gemini-2.0-flash", "https://api.test", providers.ProviderDefaults{}, false)

	tests := []struct {
		name        string
		response    geminiResponse
		errContains string
	}{
		{
			name: "Prompt blocked with safety ratings",
			response: geminiResponse{
				PromptFeedback: &geminiPromptFeedback{
					BlockReason: "SAFETY",
					SafetyRatings: []geminiSafetyRating{
						{Category: "HARM_CATEGORY_HARASSMENT", Probability: "HIGH"},
					},
				},
			},
			errContains: "prompt blocked: SAFETY",
		},
		{
			name: "No explicit block reason",
			response: geminiResponse{
				UsageMetadata: &geminiUsage{
					PromptTokenCount: 100,
				},
			},
			errContains: "used 100 prompt tokens",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chatResp := providers.PredictionResponse{}
			start := time.Now()

			_, err := provider.handleNoCandidatesError(tt.response, chatResp, []byte("{}"), start)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

// TestHandleGeminiFinishReason tests finish reason error handling
func TestHandleGeminiFinishReason(t *testing.T) {
	provider := NewGeminiProvider("test", "gemini-2.0-flash", "https://api.test", providers.ProviderDefaults{}, false)

	tests := []struct {
		name         string
		finishReason string
		errContains  string
	}{
		{
			name:         "MAX_TOKENS",
			finishReason: "MAX_TOKENS",
			errContains:  "MAX_TOKENS",
		},
		{
			name:         "SAFETY",
			finishReason: "SAFETY",
			errContains:  "safety filters",
		},
		{
			name:         "RECITATION",
			finishReason: "RECITATION",
			errContains:  "recitation",
		},
		{
			name:         "Unknown reason",
			finishReason: "UNKNOWN",
			errContains:  "UNKNOWN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chatResp := providers.PredictionResponse{}
			start := time.Now()

			_, err := provider.handleGeminiFinishReason(tt.finishReason, chatResp, []byte("{}"), start)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

// TestProcessGeminiStreamChunk tests stream chunk processing
func TestProcessGeminiStreamChunk(t *testing.T) {
	provider := NewGeminiProvider("test", "gemini-2.0-flash", "https://api.test", providers.ProviderDefaults{}, false)

	tests := []struct {
		name         string
		chunk        geminiResponse
		accumulated  string
		totalTokens  int
		wantFinished bool
		wantContent  string
		wantTokens   int
	}{
		{
			name: "Regular chunk with text",
			chunk: geminiResponse{
				Candidates: []geminiCandidate{
					{
						Content: geminiContent{
							Parts: []geminiPart{{Text: "Hello"}},
						},
					},
				},
			},
			accumulated:  "Previous ",
			totalTokens:  1,
			wantFinished: false,
			wantContent:  "Previous Hello",
			wantTokens:   2,
		},
		{
			name: "Finish chunk with usage",
			chunk: geminiResponse{
				Candidates: []geminiCandidate{
					{
						Content: geminiContent{
							Parts: []geminiPart{{Text: "!"}},
						},
						FinishReason: "STOP",
					},
				},
				UsageMetadata: &geminiUsage{
					PromptTokenCount:     10,
					CandidatesTokenCount: 5,
				},
			},
			accumulated:  "Hello",
			totalTokens:  5,
			wantFinished: true,
			wantContent:  "Hello!",
			wantTokens:   6,
		},
		{
			name: "Empty chunk",
			chunk: geminiResponse{
				Candidates: []geminiCandidate{},
			},
			accumulated:  "Hello",
			totalTokens:  5,
			wantFinished: false,
			wantContent:  "Hello",
			wantTokens:   5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outChan := make(chan providers.StreamChunk, 10)
			defer close(outChan)

			content, tokens, finished := provider.processGeminiStreamChunk(tt.chunk, tt.accumulated, tt.totalTokens, outChan)

			assert.Equal(t, tt.wantContent, content)
			assert.Equal(t, tt.wantTokens, tokens)
			assert.Equal(t, tt.wantFinished, finished)
		})
	}
}
