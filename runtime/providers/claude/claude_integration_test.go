package claude

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

// TestPredict_Integration tests the Predict method with real HTTP mocking
func TestPredict_Integration(t *testing.T) {
	tests := []struct {
		name           string
		request        providers.PredictionRequest
		mockResponse   claudeResponse
		mockStatusCode int
		expectError    bool
		errorContains  string
	}{
		{
			name: "Successful request",
			request: providers.PredictionRequest{
				Messages: []types.Message{
					{Role: "user", Content: "Hello"},
				},
				Temperature: 0.7,
				TopP:        0.9,
				MaxTokens:   100,
			},
			mockResponse: claudeResponse{
				ID:    "msg_123",
				Type:  "message",
				Role:  "assistant",
				Model: "claude-3-opus",
				Content: []claudeContent{
					{Type: "text", Text: "Hello! How can I help you?"},
				},
				Usage: claudeUsage{
					InputTokens:  10,
					OutputTokens: 20,
				},
			},
			mockStatusCode: http.StatusOK,
			expectError:    false,
		},
		{
			name: "API error response",
			request: providers.PredictionRequest{
				Messages: []types.Message{
					{Role: "user", Content: "Hello"},
				},
			},
			mockResponse: claudeResponse{
				Error: &claudeError{
					Type:    "invalid_request_error",
					Message: "Invalid API key",
				},
			},
			mockStatusCode: http.StatusOK,
			expectError:    true,
			errorContains:  "Invalid API key",
		},
		{
			name: "HTTP error",
			request: providers.PredictionRequest{
				Messages: []types.Message{
					{Role: "user", Content: "Hello"},
				},
			},
			mockStatusCode: http.StatusUnauthorized,
			expectError:    true,
			errorContains:  "401",
		},
		{
			name: "Empty content in response",
			request: providers.PredictionRequest{
				Messages: []types.Message{
					{Role: "user", Content: "Hello"},
				},
			},
			mockResponse: claudeResponse{
				ID:      "msg_123",
				Content: []claudeContent{},
			},
			mockStatusCode: http.StatusOK,
			expectError:    true,
			errorContains:  "no content in response",
		},
		{
			name: "No text content found",
			request: providers.PredictionRequest{
				Messages: []types.Message{
					{Role: "user", Content: "Hello"},
				},
			},
			mockResponse: claudeResponse{
				ID: "msg_123",
				Content: []claudeContent{
					{Type: "image", Text: ""},
				},
			},
			mockStatusCode: http.StatusOK,
			expectError:    true,
			errorContains:  "no text content found",
		},
		{
			name: "With cache read tokens",
			request: providers.PredictionRequest{
				Messages: []types.Message{
					{Role: "user", Content: "Hello"},
				},
			},
			mockResponse: claudeResponse{
				ID:    "msg_123",
				Type:  "message",
				Role:  "assistant",
				Model: "claude-3-5-sonnet-20241022",
				Content: []claudeContent{
					{Type: "text", Text: "Cached response"},
				},
				Usage: claudeUsage{
					InputTokens:              10,
					OutputTokens:             20,
					CacheReadInputTokens:     5,
					CacheCreationInputTokens: 100,
				},
			},
			mockStatusCode: http.StatusOK,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "POST", r.Method)
				assert.Equal(t, "/messages", r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.NotEmpty(t, r.Header.Get("X-API-Key"))

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
			resp, err := provider.Predict(context.Background(), tt.request)

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
				assert.Greater(t, resp.Latency, time.Duration(0))
			}
		})
	}
}

// TestPredictStream_Integration tests the PredictStream method with real HTTP mocking
func TestPredictStream_Integration(t *testing.T) {
	t.Run("Successful streaming", func(t *testing.T) {
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
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

`,
				`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" there"}}

`,
				`event: content_block_stop
data: {"type":"content_block_stop","index":0}

`,
				`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}

`,
				`event: message_stop
data: {"type":"message_stop","message":{"usage":{"input_tokens":10,"output_tokens":5}}}

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
		streamChan, err := provider.PredictStream(context.Background(), providers.PredictionRequest{
			Messages: []types.Message{
				{Role: "user", Content: "Hello"},
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

// TestConvertMessagesToClaudeFormat tests message conversion with cache control
func TestConvertMessagesToClaudeFormat(t *testing.T) {
	tests := []struct {
		name               string
		supportsCaching    bool
		messages           []types.Message
		expectCacheControl bool
	}{
		{
			name:            "No caching support",
			supportsCaching: false,
			messages: []types.Message{
				{Role: "user", Content: "Hello"},
			},
			expectCacheControl: false,
		},
		{
			name:            "Caching enabled but message too short",
			supportsCaching: true,
			messages: []types.Message{
				{Role: "user", Content: "Hi"},
			},
			expectCacheControl: false,
		},
		{
			name:            "Caching enabled with long message",
			supportsCaching: true,
			messages: []types.Message{
				{Role: "user", Content: string(make([]byte, 10000))}, // Long message
			},
			expectCacheControl: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &ClaudeProvider{
				model: "claude-3-5-sonnet-20241022", // Supports caching
			}

			if !tt.supportsCaching {
				provider.model = "claude-2" // Doesn't support caching
			}

			result := provider.convertMessagesToClaudeFormat(tt.messages)
			require.NotEmpty(t, result)

			lastMsg := result[len(result)-1]
			if tt.expectCacheControl {
				assert.NotNil(t, lastMsg.Content[0].CacheControl)
			} else {
				assert.Nil(t, lastMsg.Content[0].CacheControl)
			}
		})
	}
}

// TestCreateSystemBlocks tests system block creation with cache control
func TestCreateSystemBlocks(t *testing.T) {
	tests := []struct {
		name               string
		systemPrompt       string
		modelSupportsCache bool
		expectCacheControl bool
	}{
		{
			name:               "Empty system prompt",
			systemPrompt:       "",
			modelSupportsCache: true,
			expectCacheControl: false,
		},
		{
			name:               "Short system prompt",
			systemPrompt:       "You are helpful",
			modelSupportsCache: true,
			expectCacheControl: false,
		},
		{
			name:               "Long system prompt with caching",
			systemPrompt:       string(make([]byte, 5000)),
			modelSupportsCache: true,
			expectCacheControl: true,
		},
		{
			name:               "Long system prompt without cache support",
			systemPrompt:       string(make([]byte, 5000)),
			modelSupportsCache: false,
			expectCacheControl: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &ClaudeProvider{
				model: "claude-3-5-sonnet-20241022",
			}

			if !tt.modelSupportsCache {
				provider.model = "claude-2"
			}

			result := provider.createSystemBlocks(tt.systemPrompt)

			if tt.systemPrompt == "" {
				assert.Nil(t, result)
			} else {
				require.NotEmpty(t, result)
				if tt.expectCacheControl {
					assert.NotNil(t, result[0].CacheControl)
				} else {
					assert.Nil(t, result[0].CacheControl)
				}
			}
		})
	}
}

// TestApplyDefaults tests default value application
func TestApplyDefaults(t *testing.T) {
	provider := &ClaudeProvider{
		defaults: providers.ProviderDefaults{
			Temperature: 0.7,
			TopP:        0.9,
			MaxTokens:   1000,
		},
	}

	tests := []struct {
		name        string
		temperature float32
		topP        float32
		maxTokens   int
		expectTemp  float32
		expectTopP  float32
		expectMax   int
	}{
		{
			name:        "All zero values use defaults",
			temperature: 0,
			topP:        0,
			maxTokens:   0,
			expectTemp:  0.7,
			expectTopP:  0.9,
			expectMax:   1000,
		},
		{
			name:        "Non-zero values preserved",
			temperature: 0.5,
			topP:        0.8,
			maxTokens:   500,
			expectTemp:  0.5,
			expectTopP:  0.8,
			expectMax:   500,
		},
		{
			name:        "Mixed zero and non-zero",
			temperature: 0,
			topP:        0.8,
			maxTokens:   500,
			expectTemp:  0.7,
			expectTopP:  0.8,
			expectMax:   500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			temp, topP, maxTokens := provider.applyDefaults(tt.temperature, tt.topP, tt.maxTokens)
			assert.Equal(t, tt.expectTemp, temp)
			assert.Equal(t, tt.expectTopP, topP)
			assert.Equal(t, tt.expectMax, maxTokens)
		})
	}
}

// TestCalculateCost tests cost calculation for different models
func TestCalculateCost(t *testing.T) {
	tests := []struct {
		name         string
		model        string
		tokensIn     int
		tokensOut    int
		cachedTokens int
		expectCost   bool // Just verify cost is calculated
	}{
		{
			name:         "Claude 3.5 Sonnet",
			model:        "claude-3-5-sonnet-20241022",
			tokensIn:     1000,
			tokensOut:    500,
			cachedTokens: 0,
			expectCost:   true,
		},
		{
			name:         "Claude 3.5 Haiku",
			model:        "claude-3-5-haiku-20241022",
			tokensIn:     1000,
			tokensOut:    500,
			cachedTokens: 0,
			expectCost:   true,
		},
		{
			name:         "Claude 3 Opus",
			model:        "claude-3-opus-20240229",
			tokensIn:     1000,
			tokensOut:    500,
			cachedTokens: 0,
			expectCost:   true,
		},
		{
			name:         "With cached tokens",
			model:        "claude-3-5-sonnet-20241022",
			tokensIn:     1000,
			tokensOut:    500,
			cachedTokens: 200,
			expectCost:   true,
		},
		{
			name:         "Unknown model falls back to Sonnet pricing",
			model:        "claude-unknown",
			tokensIn:     1000,
			tokensOut:    500,
			cachedTokens: 0,
			expectCost:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := providers.NewBaseProvider("test", false, &http.Client{})
			provider := &ClaudeProvider{
				BaseProvider: base,
				model:        tt.model,
			}

			costInfo := provider.CalculateCost(tt.tokensIn, tt.tokensOut, tt.cachedTokens)

			assert.Equal(t, tt.tokensIn-tt.cachedTokens, costInfo.InputTokens)
			assert.Equal(t, tt.tokensOut, costInfo.OutputTokens)
			assert.Equal(t, tt.cachedTokens, costInfo.CachedTokens)
			assert.Greater(t, costInfo.TotalCost, 0.0)
			assert.Greater(t, costInfo.InputCostUSD, 0.0)
			assert.Greater(t, costInfo.OutputCostUSD, 0.0)

			if tt.cachedTokens > 0 {
				assert.Greater(t, costInfo.CachedCostUSD, 0.0)
			}
		})
	}
}

// TestMakeClaudeHTTPRequest tests HTTP request handling
func TestMakeClaudeHTTPRequest(t *testing.T) {
	tests := []struct {
		name           string
		mockStatusCode int
		expectError    bool
	}{
		{
			name:           "Successful request",
			mockStatusCode: http.StatusOK,
			expectError:    false,
		},
		{
			name:           "HTTP error",
			mockStatusCode: http.StatusInternalServerError,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.mockStatusCode)
				if tt.mockStatusCode == http.StatusOK {
					w.Write([]byte(`{"content":[{"type":"text","text":"response"}]}`))
				}
			}))
			defer server.Close()

			base := providers.NewBaseProvider("test", false, &http.Client{})
			provider := &ClaudeProvider{
				BaseProvider: base,
				baseURL:      server.URL,
				apiKey:       "test-key",
			}

			claudeReq := claudeRequest{
				Model:     "claude-3-opus",
				MaxTokens: 100,
				Messages:  []claudeMessage{},
			}

			_, _, err := provider.makeClaudeHTTPRequest(context.Background(), claudeReq, providers.PredictionResponse{}, time.Now())

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestParseAndValidateClaudeResponse tests response parsing
func TestParseAndValidateClaudeResponse(t *testing.T) {
	tests := []struct {
		name        string
		response    claudeResponse
		expectError bool
		errorMsg    string
	}{
		{
			name: "Valid response",
			response: claudeResponse{
				Content: []claudeContent{
					{Type: "text", Text: "Hello"},
				},
			},
			expectError: false,
		},
		{
			name: "API error",
			response: claudeResponse{
				Error: &claudeError{
					Message: "API error",
				},
			},
			expectError: true,
			errorMsg:    "API error",
		},
		{
			name:        "Empty content",
			response:    claudeResponse{Content: []claudeContent{}},
			expectError: true,
			errorMsg:    "no content",
		},
		{
			name: "No text content",
			response: claudeResponse{
				Content: []claudeContent{
					{Type: "image", Text: ""},
				},
			},
			expectError: true,
			errorMsg:    "no text content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &ClaudeProvider{}

			respBody, _ := json.Marshal(tt.response)
			_, _, _, err := provider.parseAndValidateClaudeResponse(respBody, providers.PredictionResponse{}, time.Now())

			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestProcessClaudeContentDelta tests stream content delta processing
func TestProcessClaudeContentDelta(t *testing.T) {
	provider := &ClaudeProvider{}
	outChan := make(chan providers.StreamChunk, 10)

	event := struct {
		Type  string `json:"type"`
		Delta *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta,omitempty"`
		Message *struct {
			StopReason string       `json:"stop_reason"`
			Usage      *claudeUsage `json:"usage,omitempty"`
		} `json:"message,omitempty"`
	}{
		Type: "content_block_delta",
		Delta: &struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{
			Type: "text_delta",
			Text: "Hello",
		},
	}

	accumulated, tokens := provider.processClaudeContentDelta(event, "", 0, outChan)

	assert.Equal(t, "Hello", accumulated)
	assert.Equal(t, 1, tokens)
	assert.Len(t, outChan, 1)

	chunk := <-outChan
	assert.Equal(t, "Hello", chunk.Content)
	assert.Equal(t, "Hello", chunk.Delta)
}

// TestProcessClaudeMessageStop tests stream message stop processing
func TestProcessClaudeMessageStop(t *testing.T) {
	provider := &ClaudeProvider{
		defaults: providers.ProviderDefaults{
			Pricing: providers.Pricing{
				InputCostPer1K:  0.003,
				OutputCostPer1K: 0.015,
			},
		},
	}
	outChan := make(chan providers.StreamChunk, 10)

	event := struct {
		Type  string `json:"type"`
		Delta *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta,omitempty"`
		Message *struct {
			StopReason string       `json:"stop_reason"`
			Usage      *claudeUsage `json:"usage,omitempty"`
		} `json:"message,omitempty"`
	}{
		Type: "message_stop",
		Message: &struct {
			StopReason string       `json:"stop_reason"`
			Usage      *claudeUsage `json:"usage,omitempty"`
		}{
			StopReason: "end_turn",
			Usage: &claudeUsage{
				InputTokens:  100,
				OutputTokens: 50,
			},
		},
	}

	provider.processClaudeMessageStop(event, "accumulated text", 10, outChan)

	assert.Len(t, outChan, 1)
	chunk := <-outChan
	assert.Equal(t, "accumulated text", chunk.Content)
	assert.NotNil(t, chunk.FinishReason)
	assert.Equal(t, "end_turn", *chunk.FinishReason)
	assert.NotNil(t, chunk.CostInfo)
}
