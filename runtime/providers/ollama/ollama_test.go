package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestNewProvider(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Temperature: 0.7,
		TopP:        0.9,
		MaxTokens:   1000,
	}

	provider := NewProvider("test-ollama", "llama2", "http://localhost:11434", defaults, false, nil)

	if provider == nil {
		t.Fatal("Expected non-nil provider")
	}

	if provider.ID() != "test-ollama" {
		t.Errorf("Expected ID 'test-ollama', got '%s'", provider.ID())
	}

	if provider.model != "llama2" {
		t.Errorf("Expected model 'llama2', got '%s'", provider.model)
	}

	if provider.baseURL != "http://localhost:11434" {
		t.Error("BaseURL mismatch")
	}

	if provider.defaults.Temperature != 0.7 {
		t.Error("Temperature default mismatch")
	}
}

func TestNewProvider_WithKeepAlive(t *testing.T) {
	additionalConfig := map[string]any{
		"keep_alive": "5m",
	}

	provider := NewProvider("test", "llama2", "http://localhost:11434",
		providers.ProviderDefaults{}, false, additionalConfig)

	if provider.keepAlive != "5m" {
		t.Errorf("Expected keepAlive '5m', got '%s'", provider.keepAlive)
	}
}

func TestOllamaProvider_ID(t *testing.T) {
	ids := []string{"ollama-llama2", "ollama-llava", "custom-ollama"}

	for _, id := range ids {
		provider := NewProvider(id, "model", "url", providers.ProviderDefaults{}, false, nil)
		if provider.ID() != id {
			t.Errorf("Expected ID '%s', got '%s'", id, provider.ID())
		}
	}
}

func TestOllamaProvider_Cost_Free(t *testing.T) {
	provider := NewProvider("test", "llama2", "url", providers.ProviderDefaults{}, false, nil)

	// Ollama is free - cost should be zero
	breakdown := provider.CalculateCost(1000, 1000, 0)

	if breakdown.TotalCost != 0 {
		t.Errorf("Expected zero cost for Ollama, got %.4f", breakdown.TotalCost)
	}

	if breakdown.InputCostUSD != 0 {
		t.Errorf("Expected zero input cost, got %.4f", breakdown.InputCostUSD)
	}

	if breakdown.OutputCostUSD != 0 {
		t.Errorf("Expected zero output cost, got %.4f", breakdown.OutputCostUSD)
	}

	if breakdown.InputTokens != 1000 {
		t.Errorf("Expected 1000 input tokens, got %d", breakdown.InputTokens)
	}

	if breakdown.OutputTokens != 1000 {
		t.Errorf("Expected 1000 output tokens, got %d", breakdown.OutputTokens)
	}
}

func TestOllamaProvider_Cost_WithCachedTokens(t *testing.T) {
	provider := NewProvider("test", "llama2", "url", providers.ProviderDefaults{}, false, nil)

	breakdown := provider.CalculateCost(1000, 500, 200)

	// Even with cached tokens, cost should be zero
	if breakdown.TotalCost != 0 {
		t.Errorf("Expected zero cost, got %.4f", breakdown.TotalCost)
	}

	// InputTokens should subtract cached tokens
	if breakdown.InputTokens != 800 {
		t.Errorf("Expected 800 input tokens (1000 - 200 cached), got %d", breakdown.InputTokens)
	}

	if breakdown.CachedTokens != 200 {
		t.Errorf("Expected 200 cached tokens, got %d", breakdown.CachedTokens)
	}
}

func TestOllamaProvider_DifferentModels(t *testing.T) {
	models := []string{"llama2", "llava:13b", "llama3.2-vision", "mistral", "codellama"}

	for _, model := range models {
		provider := NewProvider("test", model, "url", providers.ProviderDefaults{}, false, nil)
		if provider.model != model {
			t.Errorf("Model mismatch for %s", model)
		}
	}
}

func TestOllamaProvider_DifferentBaseURLs(t *testing.T) {
	urls := []string{
		"http://localhost:11434",
		"http://ollama.ollama-system:11434",
		"https://ollama.example.com",
	}

	for _, url := range urls {
		provider := NewProvider("test", "llama2", url, providers.ProviderDefaults{}, false, nil)
		if provider.baseURL != url {
			t.Errorf("BaseURL mismatch for %s", url)
		}
	}
}

func TestOllamaRequest_Structure(t *testing.T) {
	seed := 42
	req := ollamaRequest{
		Model: "llama2",
		Messages: []ollamaMessage{
			{Role: "user", Content: "Hello"},
		},
		Temperature: 0.7,
		TopP:        0.9,
		MaxTokens:   1000,
		Seed:        &seed,
		Stream:      false,
		KeepAlive:   "5m",
	}

	if req.Model != "llama2" {
		t.Error("Model mismatch")
	}

	if len(req.Messages) != 1 {
		t.Error("Messages count mismatch")
	}

	if req.Temperature != 0.7 {
		t.Error("Temperature mismatch")
	}

	if req.Seed == nil || *req.Seed != 42 {
		t.Error("Seed mismatch")
	}

	if req.KeepAlive != "5m" {
		t.Error("KeepAlive mismatch")
	}
}

func TestOllamaMessage_Structure(t *testing.T) {
	msg := ollamaMessage{
		Role:    "assistant",
		Content: "Response text",
	}

	if msg.Role != "assistant" {
		t.Error("Role mismatch")
	}

	if msg.Content != "Response text" {
		t.Error("Content mismatch")
	}
}

func TestOllamaResponse_Structure(t *testing.T) {
	resp := ollamaResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   "llama2",
		Choices: []ollamaChoice{
			{
				Index: 0,
				Message: ollamaMessage{
					Role:    "assistant",
					Content: "Response",
				},
				FinishReason: "stop",
			},
		},
		Usage: ollamaUsage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
		},
	}

	if resp.ID != "chatcmpl-123" {
		t.Error("ID mismatch")
	}

	if len(resp.Choices) != 1 {
		t.Error("Choices count mismatch")
	}

	if resp.Usage.PromptTokens != 100 {
		t.Error("PromptTokens mismatch")
	}

	if resp.Usage.CompletionTokens != 50 {
		t.Error("CompletionTokens mismatch")
	}
}

func TestOllamaError_Structure(t *testing.T) {
	err := ollamaError{
		Message: "Model not found",
		Type:    "model_not_found",
		Code:    "not_found",
	}

	if err.Message != "Model not found" {
		t.Error("Message mismatch")
	}

	if err.Type != "model_not_found" {
		t.Error("Type mismatch")
	}

	if err.Code != "not_found" {
		t.Error("Code mismatch")
	}
}

func TestPredict_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("Expected path '/v1/chat/completions', got %q", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("Expected method 'POST', got %q", r.Method)
		}

		// Verify headers - Ollama should NOT have Authorization header
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type 'application/json', got %q", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("Ollama should not send Authorization header")
		}

		// Parse request to verify system message was sent
		var req ollamaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}
		if len(req.Messages) < 1 || req.Messages[0].Role != "system" {
			t.Error("Expected first message to be system message")
		}

		// Send successful response
		resp := ollamaResponse{
			ID:      "chatcmpl-123",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   "llama2",
			Choices: []ollamaChoice{
				{
					Index: 0,
					Message: ollamaMessage{
						Role:    "assistant",
						Content: "Hello! How can I help you?",
					},
					FinishReason: "stop",
				},
			},
			Usage: ollamaUsage{
				PromptTokens:     10,
				CompletionTokens: 8,
				TotalTokens:      18,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewProvider("test", "llama2", server.URL,
		providers.ProviderDefaults{
			Temperature: 0.7,
			TopP:        0.9,
			MaxTokens:   1000,
		}, false, nil)

	resp, err := provider.Predict(context.Background(), providers.PredictionRequest{
		System: "You are a helpful assistant",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
	})

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if resp.Content != "Hello! How can I help you?" {
		t.Errorf("Expected content 'Hello! How can I help you?', got %q", resp.Content)
	}

	if resp.CostInfo == nil {
		t.Error("Expected cost info but got nil")
	} else {
		if resp.CostInfo.InputTokens != 10 {
			t.Errorf("Expected 10 input tokens, got %d", resp.CostInfo.InputTokens)
		}
		if resp.CostInfo.OutputTokens != 8 {
			t.Errorf("Expected 8 output tokens, got %d", resp.CostInfo.OutputTokens)
		}
		// Ollama should have zero cost
		if resp.CostInfo.TotalCost != 0 {
			t.Errorf("Expected zero cost, got %.4f", resp.CostInfo.TotalCost)
		}
	}

	if resp.Latency <= 0 {
		t.Error("Expected latency > 0")
	}
}

func TestPredict_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid request","type":"invalid_request_error"}}`))
	}))
	defer server.Close()

	provider := NewProvider("test", "llama2", server.URL, providers.ProviderDefaults{}, false, nil)

	resp, err := provider.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Test"}},
	})

	if err == nil {
		t.Fatal("Expected error but got nil")
	}

	if resp.Latency <= 0 {
		t.Error("Expected latency > 0 even on error")
	}
}

func TestPredict_WithSeed(t *testing.T) {
	var receivedSeed *int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}
		receivedSeed = req.Seed

		resp := ollamaResponse{
			Choices: []ollamaChoice{{Message: ollamaMessage{Content: "Test"}}},
			Usage:   ollamaUsage{PromptTokens: 5, CompletionTokens: 5},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewProvider("test", "llama2", server.URL, providers.ProviderDefaults{}, false, nil)

	seed := 12345
	_, err := provider.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Test"}},
		Seed:     &seed,
	})

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if receivedSeed == nil {
		t.Fatal("Expected seed to be sent")
	}

	if *receivedSeed != 12345 {
		t.Errorf("Expected seed 12345, got %d", *receivedSeed)
	}
}

func TestPredict_WithKeepAlive(t *testing.T) {
	var receivedKeepAlive string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}
		receivedKeepAlive = req.KeepAlive

		resp := ollamaResponse{
			Choices: []ollamaChoice{{Message: ollamaMessage{Content: "Test"}}},
			Usage:   ollamaUsage{PromptTokens: 5, CompletionTokens: 5},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewProvider("test", "llama2", server.URL, providers.ProviderDefaults{}, false,
		map[string]any{"keep_alive": "10m"})

	_, err := provider.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Test"}},
	})

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if receivedKeepAlive != "10m" {
		t.Errorf("Expected keep_alive '10m', got '%s'", receivedKeepAlive)
	}
}

func TestPredict_AppliesDefaults(t *testing.T) {
	var receivedReq ollamaRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedReq); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		resp := ollamaResponse{
			Choices: []ollamaChoice{{Message: ollamaMessage{Content: "Test"}}},
			Usage:   ollamaUsage{PromptTokens: 5, CompletionTokens: 5},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewProvider("test", "llama2", server.URL,
		providers.ProviderDefaults{
			Temperature: 0.8,
			TopP:        0.95,
			MaxTokens:   2000,
		}, false, nil)

	// Request with zero values should use defaults
	_, err := provider.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Test"}},
	})

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if receivedReq.Temperature != 0.8 {
		t.Errorf("Expected temperature 0.8, got %f", receivedReq.Temperature)
	}

	if receivedReq.TopP != 0.95 {
		t.Errorf("Expected top_p 0.95, got %f", receivedReq.TopP)
	}

	if receivedReq.MaxTokens != 2000 {
		t.Errorf("Expected max_tokens 2000, got %d", receivedReq.MaxTokens)
	}
}

func TestPredict_NoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaResponse{
			Choices: []ollamaChoice{}, // Empty choices
			Usage:   ollamaUsage{PromptTokens: 5, CompletionTokens: 0},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewProvider("test", "llama2", server.URL, providers.ProviderDefaults{}, false, nil)

	_, err := provider.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Test"}},
	})

	if err == nil {
		t.Fatal("Expected error for no choices")
	}
}

func TestPredict_OllamaAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaResponse{
			Error: &ollamaError{
				Message: "Model not found",
				Type:    "model_not_found",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewProvider("test", "nonexistent-model", server.URL,
		providers.ProviderDefaults{}, false, nil)

	_, err := provider.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Test"}},
	})

	if err == nil {
		t.Fatal("Expected error for Ollama API error")
	}
}

func TestExtractContentString(t *testing.T) {
	tests := []struct {
		name     string
		content  any
		expected string
	}{
		{
			name:     "string content",
			content:  "Hello, world!",
			expected: "Hello, world!",
		},
		{
			name: "array content with text parts",
			content: []any{
				map[string]any{"type": "text", "text": "Hello, "},
				map[string]any{"type": "text", "text": "world!"},
			},
			expected: "Hello, world!",
		},
		{
			name:     "nil content",
			content:  nil,
			expected: "",
		},
		{
			name:     "empty string",
			content:  "",
			expected: "",
		},
		{
			name:     "number content",
			content:  123,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractContentString(tt.content)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestExtractTextFromParts(t *testing.T) {
	parts := []any{
		map[string]any{"type": "text", "text": "Part 1 "},
		map[string]any{"type": "image", "url": "http://example.com/img.jpg"},
		map[string]any{"type": "text", "text": "Part 2"},
	}

	result := extractTextFromParts(parts)
	expected := "Part 1 Part 2"

	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestGetTextFromPart(t *testing.T) {
	tests := []struct {
		name     string
		part     any
		expected string
	}{
		{
			name:     "valid text part",
			part:     map[string]any{"type": "text", "text": "Hello"},
			expected: "Hello",
		},
		{
			name:     "image part",
			part:     map[string]any{"type": "image", "url": "http://example.com"},
			expected: "",
		},
		{
			name:     "missing type",
			part:     map[string]any{"text": "Hello"},
			expected: "",
		},
		{
			name:     "missing text",
			part:     map[string]any{"type": "text"},
			expected: "",
		},
		{
			name:     "not a map",
			part:     "string",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getTextFromPart(tt.part)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestSupportsStreaming(t *testing.T) {
	provider := NewProvider("test", "llama2", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	if !provider.SupportsStreaming() {
		t.Error("Expected provider to support streaming")
	}
}

func TestProviderDefaults_Structure(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Temperature: 0.8,
		TopP:        0.95,
		MaxTokens:   2000,
	}

	if defaults.Temperature != 0.8 {
		t.Error("Temperature mismatch")
	}

	if defaults.TopP != 0.95 {
		t.Error("TopP mismatch")
	}

	if defaults.MaxTokens != 2000 {
		t.Error("MaxTokens mismatch")
	}
}

func TestPredictStream_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("Expected path '/v1/chat/completions', got %q", r.URL.Path)
		}

		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("Expected ResponseWriter to support Flusher")
		}

		// Send SSE chunks
		chunks := []string{
			`{"choices":[{"delta":{"content":"Hello"}}]}`,
			`{"choices":[{"delta":{"content":" World"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
		}

		for _, chunk := range chunks {
			_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	provider := NewProvider("test", "llama2", server.URL,
		providers.ProviderDefaults{MaxTokens: 100}, false, nil)

	ch, err := provider.PredictStream(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Hi"}},
	})

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	var lastContent string
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("Unexpected error: %v", chunk.Error)
		}
		lastContent = chunk.Content
	}

	if lastContent != "Hello World" {
		t.Errorf("Expected final content 'Hello World', got '%s'", lastContent)
	}
}

func TestPredictStream_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "Internal server error"}`))
	}))
	defer server.Close()

	provider := NewProvider("test", "llama2", server.URL,
		providers.ProviderDefaults{}, false, nil)

	_, err := provider.PredictStream(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Hi"}},
	})

	if err == nil {
		t.Error("Expected error for server error")
	}
}

func TestPredictStream_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		// Send first chunk then wait
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"Hello"}}]}` + "\n\n"))
		flusher.Flush()

		// Wait for client to cancel
		<-r.Context().Done()
	}))
	defer server.Close()

	provider := NewProvider("test", "llama2", server.URL,
		providers.ProviderDefaults{MaxTokens: 100}, false, nil)

	ctx, cancel := context.WithCancel(context.Background())

	ch, err := provider.PredictStream(ctx, providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Hi"}},
	})

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Read first chunk
	<-ch

	// Cancel context
	cancel()

	// Read remaining chunks - should terminate due to cancellation or error
	// Either "canceled" or "error" finish reason is acceptable depending on timing
	var foundTermination bool
	for chunk := range ch {
		if chunk.FinishReason != nil {
			reason := *chunk.FinishReason
			if reason == "canceled" || reason == "error" {
				foundTermination = true
			}
		}
	}

	if !foundTermination {
		t.Error("Expected to find termination (canceled or error) finish reason")
	}
}

func TestStreamChunkStructures(t *testing.T) {
	// Test ollamaStreamChunk
	chunk := ollamaStreamChunk{
		Choices: []ollamaStreamChoice{
			{
				Delta: ollamaStreamDelta{
					Content: "test",
					ToolCalls: []ollamaStreamToolCall{
						{Index: 0, ID: "call_1", Function: ollamaStreamToolFunction{Name: "test"}},
					},
				},
			},
		},
	}

	if chunk.Choices[0].Delta.Content != "test" {
		t.Error("Content mismatch")
	}

	if chunk.Choices[0].Delta.ToolCalls[0].ID != "call_1" {
		t.Error("ToolCall ID mismatch")
	}
}

func TestProcessToolCallDeltas(t *testing.T) {
	var toolCalls []types.MessageToolCall

	deltas := []ollamaStreamToolCall{
		{Index: 0, ID: "call_1", Function: ollamaStreamToolFunction{Name: "test", Arguments: `{"a":`}},
		{Index: 0, Function: ollamaStreamToolFunction{Arguments: `1}`}},
	}

	processToolCallDeltas(&toolCalls, deltas[:1])
	processToolCallDeltas(&toolCalls, deltas[1:])

	if len(toolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(toolCalls))
	}

	if toolCalls[0].ID != "call_1" {
		t.Error("ID mismatch")
	}

	if toolCalls[0].Name != "test" {
		t.Error("Name mismatch")
	}

	expectedArgs := `{"a":1}`
	if string(toolCalls[0].Args) != expectedArgs {
		t.Errorf("Args mismatch: expected %s, got %s", expectedArgs, string(toolCalls[0].Args))
	}
}

func TestCreateFinalStreamChunk(t *testing.T) {
	provider := NewProvider("test", "llama2", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	finishReason := "stop"
	usage := &ollamaUsage{PromptTokens: 10, CompletionTokens: 5}
	toolCalls := []types.MessageToolCall{{ID: "call_1", Name: "test"}}

	chunk := provider.createFinalStreamChunk("content", toolCalls, 15, &finishReason, usage)

	if chunk.Content != "content" {
		t.Error("Content mismatch")
	}

	if *chunk.FinishReason != "stop" {
		t.Error("FinishReason mismatch")
	}

	if chunk.CostInfo == nil {
		t.Error("Expected cost info")
	} else if chunk.CostInfo.TotalCost != 0 {
		t.Error("Expected zero cost")
	}

	if len(chunk.ToolCalls) != 1 {
		t.Error("ToolCalls mismatch")
	}
}

func TestParseStreamChunk(t *testing.T) {
	provider := NewProvider("test", "llama2", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	t.Run("valid chunk", func(t *testing.T) {
		data := `{"choices":[{"delta":{"content":"test"}}]}`
		chunk, ok := provider.parseStreamChunk(data)

		if !ok {
			t.Error("Expected parse to succeed")
		}

		if len(chunk.Choices) != 1 {
			t.Error("Expected 1 choice")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		_, ok := provider.parseStreamChunk("invalid")

		if ok {
			t.Error("Expected parse to fail")
		}
	})
}

func TestHandleContextCancellation(t *testing.T) {
	provider := NewProvider("test", "llama2", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	t.Run("not canceled", func(t *testing.T) {
		ctx := context.Background()
		outChan := make(chan providers.StreamChunk, 1)

		result := provider.handleContextCancellation(ctx, "content", nil, outChan)

		if result {
			t.Error("Expected false for non-canceled context")
		}
	})

	t.Run("canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		outChan := make(chan providers.StreamChunk, 1)

		result := provider.handleContextCancellation(ctx, "content", nil, outChan)

		if !result {
			t.Error("Expected true for canceled context")
		}

		chunk := <-outChan
		if chunk.FinishReason == nil || *chunk.FinishReason != "canceled" {
			t.Error("Expected canceled finish reason")
		}
	})
}

func TestSendDoneChunk(t *testing.T) {
	provider := NewProvider("test", "llama2", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	outChan := make(chan providers.StreamChunk, 1)
	toolCalls := []types.MessageToolCall{{ID: "call_1"}}

	provider.sendDoneChunk("content", toolCalls, 10, outChan)

	chunk := <-outChan

	if chunk.Content != "content" {
		t.Error("Content mismatch")
	}

	if chunk.TokenCount != 10 {
		t.Error("TokenCount mismatch")
	}

	if chunk.FinishReason == nil || *chunk.FinishReason != "stop" {
		t.Error("Expected stop finish reason")
	}
}

func TestProcessStreamChoice(t *testing.T) {
	provider := NewProvider("test", "llama2", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	outChan := make(chan providers.StreamChunk, 10)
	var toolCalls []types.MessageToolCall

	choice := ollamaStreamChoice{
		Delta: ollamaStreamDelta{
			Content: "Hello",
		},
	}

	newAccum, newTokens := provider.processStreamChoice(
		choice, "", 0, &toolCalls, nil, outChan,
	)

	if newAccum != "Hello" {
		t.Errorf("Expected accumulated 'Hello', got '%s'", newAccum)
	}

	if newTokens != 1 {
		t.Errorf("Expected 1 token, got %d", newTokens)
	}

	// Check chunk was sent
	select {
	case chunk := <-outChan:
		if chunk.Delta != "Hello" {
			t.Error("Delta mismatch")
		}
	default:
		t.Error("Expected chunk to be sent")
	}
}
