package openai

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
		Pricing: providers.Pricing{
			InputCostPer1K:  0.01,
			OutputCostPer1K: 0.03,
		},
	}

	provider := NewProvider("test-openai", "gpt-4", "https://api.openai.com/v1", defaults, false)

	if provider == nil {
		t.Fatal("Expected non-nil provider")
	}

	if provider.ID() != "test-openai" {
		t.Errorf("Expected ID 'test-openai', got '%s'", provider.ID())
	}

	if provider.model != "gpt-4" {
		t.Errorf("Expected model 'gpt-4', got '%s'", provider.model)
	}

	if provider.baseURL != "https://api.openai.com/v1" {
		t.Error("BaseURL mismatch")
	}

	if provider.defaults.Temperature != 0.7 {
		t.Error("Temperature default mismatch")
	}
}

func TestNewProviderWithCredential(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Temperature: 0.7,
		TopP:        0.9,
		MaxTokens:   1000,
	}

	t.Run("with credential", func(t *testing.T) {
		cred := &mockCredential{credType: "api_key"}
		provider := NewProviderWithCredential("test-openai", "gpt-4", "https://api.openai.com/v1", defaults, false, cred, "", nil)

		if provider == nil {
			t.Fatal("Expected non-nil provider")
		}

		if provider.ID() != "test-openai" {
			t.Errorf("Expected ID 'test-openai', got '%s'", provider.ID())
		}

		if provider.model != "gpt-4" {
			t.Errorf("Expected model 'gpt-4', got '%s'", provider.model)
		}

		if provider.baseURL != "https://api.openai.com/v1" {
			t.Errorf("BaseURL mismatch: expected 'https://api.openai.com/v1', got '%s'", provider.baseURL)
		}

		if provider.credential == nil {
			t.Error("Expected credential to be set")
		}
	})

	t.Run("with nil credential", func(t *testing.T) {
		provider := NewProviderWithCredential("test-openai", "gpt-4", "https://api.openai.com/v1", defaults, false, nil, "", nil)

		if provider == nil {
			t.Fatal("Expected non-nil provider")
		}

		if provider.credential != nil {
			t.Error("Expected credential to be nil")
		}
	})
}

// mockCredential implements providers.Credential for testing
type mockCredential struct {
	credType string
}

func (m *mockCredential) Type() string { return m.credType }
func (m *mockCredential) Apply(_ context.Context, _ *http.Request) error {
	return nil
}

func TestOpenAIProvider_ID(t *testing.T) {
	ids := []string{"openai-gpt4", "openai-gpt-3.5", "custom-openai"}

	for _, id := range ids {
		provider := NewProvider(id, "model", "url", providers.ProviderDefaults{}, false)
		if provider.ID() != id {
			t.Errorf("Expected ID '%s', got '%s'", id, provider.ID())
		}
	}
}

func TestOpenAIProvider_Cost(t *testing.T) {
	pricing := providers.Pricing{
		InputCostPer1K:  0.01, // $0.01 per 1K input tokens
		OutputCostPer1K: 0.03, // $0.03 per 1K output tokens
	}

	defaults := providers.ProviderDefaults{
		Pricing: pricing,
	}

	provider := NewProvider("test", "gpt-4", "url", defaults, false)

	// Test with 1000 input and 1000 output tokens
	breakdown := provider.CalculateCost(1000, 1000, 0)
	expected := 0.01 + 0.03 // $0.04 total

	if breakdown.TotalCost != expected {
		t.Errorf("Expected cost %.4f, got %.4f", expected, breakdown.TotalCost)
	}
}

func TestOpenAIProvider_Cost_LargeTokenCounts(t *testing.T) {
	pricing := providers.Pricing{
		InputCostPer1K:  0.01,
		OutputCostPer1K: 0.03,
	}

	defaults := providers.ProviderDefaults{
		Pricing: pricing,
	}

	provider := NewProvider("test", "gpt-4", "url", defaults, false)

	// Test with 10,000 tokens
	breakdown := provider.CalculateCost(10000, 5000, 0)
	// 10,000 input = 10 * $0.01 = $0.10
	// 5,000 output = 5 * $0.03 = $0.15
	expected := 0.10 + 0.15 // $0.25

	if breakdown.TotalCost != expected {
		t.Errorf("Expected cost %.4f, got %.4f", expected, breakdown.TotalCost)
	}
}

func TestOpenAIProvider_CostBreakdown(t *testing.T) {
	pricing := providers.Pricing{
		InputCostPer1K:  0.01,
		OutputCostPer1K: 0.03,
	}

	defaults := providers.ProviderDefaults{
		Pricing: pricing,
	}

	provider := NewProvider("test", "gpt-4", "url", defaults, false)

	breakdown := provider.CalculateCost(1000, 500, 0)

	if breakdown.InputTokens != 1000 {
		t.Errorf("Expected 1000 input tokens, got %d", breakdown.InputTokens)
	}

	if breakdown.OutputTokens != 500 {
		t.Errorf("Expected 500 output tokens, got %d", breakdown.OutputTokens)
	}

	expectedInputCost := 0.01   // 1000 tokens = 1 * $0.01
	expectedOutputCost := 0.015 // 500 tokens = 0.5 * $0.03

	if breakdown.InputCostUSD != expectedInputCost {
		t.Errorf("Expected input cost %.4f, got %.4f", expectedInputCost, breakdown.InputCostUSD)
	}

	if breakdown.OutputCostUSD != expectedOutputCost {
		t.Errorf("Expected output cost %.4f, got %.4f", expectedOutputCost, breakdown.OutputCostUSD)
	}

	expectedTotal := expectedInputCost + expectedOutputCost
	if breakdown.TotalCost != expectedTotal {
		t.Errorf("Expected total cost %.4f, got %.4f", expectedTotal, breakdown.TotalCost)
	}
}

func TestOpenAIProvider_CostBreakdownWithCachedTokens(t *testing.T) {
	pricing := providers.Pricing{
		InputCostPer1K:  0.01,
		OutputCostPer1K: 0.03,
	}

	defaults := providers.ProviderDefaults{
		Pricing: pricing,
	}

	provider := NewProvider("test", "gpt-4", "url", defaults, false)

	// 1000 input (total), 500 output, 200 cached
	// Cached tokens are subtracted from input tokens: 1000 - 200 = 800 regular input
	breakdown := provider.CalculateCost(1000, 500, 200)

	// InputTokens field contains only non-cached input tokens
	if breakdown.InputTokens != 800 {
		t.Errorf("Expected 800 input tokens (1000 - 200 cached), got %d", breakdown.InputTokens)
	}

	if breakdown.OutputTokens != 500 {
		t.Error("OutputTokens mismatch")
	}

	if breakdown.CachedTokens != 200 {
		t.Error("CachedTokens mismatch")
	}

	// Cached tokens cost 50% of regular input tokens
	expectedCachedCost := 0.001 // 200 * 0.01 / 1000 * 0.5 = 0.001
	// Input cost is for 800 tokens only
	expectedInputCost := 0.008  // 800 * 0.01 / 1000 = 0.008
	expectedOutputCost := 0.015 // 500 * 0.03 / 1000 = 0.015

	if breakdown.CachedCostUSD != expectedCachedCost {
		t.Errorf("Expected cached cost %.4f, got %.4f", expectedCachedCost, breakdown.CachedCostUSD)
	}

	if breakdown.InputCostUSD != expectedInputCost {
		t.Errorf("Expected input cost %.4f, got %.4f", expectedInputCost, breakdown.InputCostUSD)
	}

	// Total should include all costs
	expectedTotal := expectedInputCost + expectedCachedCost + expectedOutputCost
	if breakdown.TotalCost != expectedTotal {
		t.Errorf("Expected total cost %.4f, got %.4f", expectedTotal, breakdown.TotalCost)
	}
}

func TestOpenAIProvider_CostBreakdown_ZeroTokens(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Pricing: providers.Pricing{
			InputCostPer1K:  0.01,
			OutputCostPer1K: 0.03,
		},
	}

	provider := NewProvider("test", "gpt-4", "url", defaults, false)

	breakdown := provider.CalculateCost(0, 0, 0)

	if breakdown.TotalCost != 0.0 {
		t.Errorf("Expected zero cost, got %.4f", breakdown.TotalCost)
	}
}

func TestProviderDefaults_Structure(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Temperature: 0.8,
		TopP:        0.95,
		MaxTokens:   2000,
		Pricing: providers.Pricing{
			InputCostPer1K:  0.005,
			OutputCostPer1K: 0.015,
		},
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

	if defaults.Pricing.InputCostPer1K != 0.005 {
		t.Error("Input pricing mismatch")
	}
}

func TestOpenAIProvider_DifferentModels(t *testing.T) {
	models := []string{"gpt-4", "gpt-4-turbo", "gpt-3.5-turbo", "gpt-4o"}

	for _, model := range models {
		provider := NewProvider("test", model, "url", providers.ProviderDefaults{}, false)
		if provider.model != model {
			t.Errorf("Model mismatch for %s", model)
		}
	}
}

func TestOpenAIProvider_DifferentBaseURLs(t *testing.T) {
	urls := []string{
		"https://api.openai.com/v1",
		"https://custom.openai.com/v1",
		"http://localhost:8080/v1",
	}

	for _, url := range urls {
		provider := NewProvider("test", "gpt-4", url, providers.ProviderDefaults{}, false)
		if provider.baseURL != url {
			t.Errorf("BaseURL mismatch for %s", url)
		}
	}
}

func TestOpenAIRequest_Structure(t *testing.T) {
	seed := 42
	req := openAIRequest{
		Model: "gpt-4",
		Messages: []openAIMessage{
			{Role: "user", Content: "Hello"},
		},
		Temperature: 0.7,
		TopP:        0.9,
		MaxTokens:   1000,
		Seed:        &seed,
	}

	if req.Model != "gpt-4" {
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
}

func TestOpenAIMessage_Structure(t *testing.T) {
	msg := openAIMessage{
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

func TestOpenAIResponse_Structure(t *testing.T) {
	resp := openAIResponse{
		ID:      "predictcmpl-123",
		Object:  "predict.completion",
		Created: 1234567890,
		Model:   "gpt-4",
		Choices: []openAIChoice{
			{
				Index: 0,
				Message: openAIMessage{
					Role:    "assistant",
					Content: "Response",
				},
				FinishReason: "stop",
			},
		},
		Usage: openAIUsage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
		},
	}

	if resp.ID != "predictcmpl-123" {
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

func TestOpenAIUsage_WithCachedTokens(t *testing.T) {
	usage := openAIUsage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		TotalTokens:      1500,
		PromptTokensDetails: &openAIPromptDetails{
			CachedTokens: 200,
		},
	}

	if usage.PromptTokensDetails == nil {
		t.Fatal("Expected PromptTokensDetails")
	}

	if usage.PromptTokensDetails.CachedTokens != 200 {
		t.Error("CachedTokens mismatch")
	}
}

func TestOpenAIError_Structure(t *testing.T) {
	err := openAIError{
		Message: "Rate limit exceeded",
		Type:    "rate_limit_error",
		Code:    "rate_limit",
	}

	if err.Message != "Rate limit exceeded" {
		t.Error("Message mismatch")
	}

	if err.Type != "rate_limit_error" {
		t.Error("Type mismatch")
	}

	if err.Code != "rate_limit" {
		t.Error("Code mismatch")
	}
}

func TestPredict_Integration(t *testing.T) {
	t.Run("Successful predict request with system message", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/chat/completions" {
				t.Errorf("Expected path '/chat/completions', got %q", r.URL.Path)
			}
			if r.Method != "POST" {
				t.Errorf("Expected method 'POST', got %q", r.Method)
			}

			// Verify headers
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("Expected Content-Type 'application/json', got %q", r.Header.Get("Content-Type"))
			}
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Errorf("Expected Authorization 'Bearer test-key', got %q", r.Header.Get("Authorization"))
			}

			// Parse request to verify system message was sent
			var req openAIRequest
			json.NewDecoder(r.Body).Decode(&req)
			if len(req.Messages) < 1 || req.Messages[0].Role != "system" {
				t.Error("Expected first message to be system message")
			}

			// Send successful response
			resp := openAIResponse{
				ID:      "predictcmpl-123",
				Object:  "predict.completion",
				Created: time.Now().Unix(),
				Model:   "gpt-4",
				Choices: []openAIChoice{
					{
						Index: 0,
						Message: openAIMessage{
							Role:    "assistant",
							Content: "Hello! How can I help you?",
						},
						FinishReason: "stop",
					},
				},
				Usage: openAIUsage{
					PromptTokens:     10,
					CompletionTokens: 8,
					TotalTokens:      18,
				},
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		provider := &Provider{
			BaseProvider: providers.NewBaseProvider("test", false, &http.Client{Timeout: 30 * time.Second}),
			model:        "gpt-4",
			baseURL:      server.URL,
			apiKey:       "test-key",
			defaults: providers.ProviderDefaults{
				Temperature: 0.7,
				TopP:        0.9,
				MaxTokens:   1000,
				Pricing: providers.Pricing{
					InputCostPer1K:  0.03,
					OutputCostPer1K: 0.06,
				},
			},
		}

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
		}

		if resp.Latency <= 0 {
			t.Error("Expected latency > 0")
		}
	})

	t.Run("API error response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"message":"Invalid request","type":"invalid_request_error"}}`))
		}))
		defer server.Close()

		provider := &Provider{
			BaseProvider: providers.NewBaseProvider("test", false, &http.Client{Timeout: 30 * time.Second}),
			model:        "gpt-4",
			baseURL:      server.URL,
			apiKey:       "test-key",
			defaults:     providers.ProviderDefaults{},
		}

		resp, err := provider.Predict(context.Background(), providers.PredictionRequest{
			Messages: []types.Message{{Role: "user", Content: "Test"}},
		})

		if err == nil {
			t.Fatal("Expected error but got nil")
		}

		if resp.Latency <= 0 {
			t.Error("Expected latency > 0 even on error")
		}
	})

	t.Run("Predict with seed parameter", func(t *testing.T) {
		var receivedSeed *int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req openAIRequest
			json.NewDecoder(r.Body).Decode(&req)
			receivedSeed = req.Seed

			resp := openAIResponse{
				Choices: []openAIChoice{{Message: openAIMessage{Content: "Test"}}},
				Usage:   openAIUsage{PromptTokens: 5, CompletionTokens: 5},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		provider := &Provider{
			BaseProvider: providers.NewBaseProvider("test", false, &http.Client{Timeout: 30 * time.Second}),
			model:        "gpt-4",
			baseURL:      server.URL,
			apiKey:       "test-key",
			defaults:     providers.ProviderDefaults{},
		}

		seed := 12345
		_, err := provider.Predict(context.Background(), providers.PredictionRequest{
			Messages: []types.Message{{Role: "user", Content: "Test"}},
			Seed:     &seed,
		})

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if receivedSeed == nil {
			t.Fatal("Expected seed to be sent but got nil")
		}
		if *receivedSeed != 12345 {
			t.Errorf("Expected seed 12345, got %d", *receivedSeed)
		}
	})

	t.Run("Uses request values over defaults", func(t *testing.T) {
		var receivedTemp float32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req openAIRequest
			json.NewDecoder(r.Body).Decode(&req)
			receivedTemp = req.Temperature

			resp := openAIResponse{
				Choices: []openAIChoice{{Message: openAIMessage{Content: "Test"}}},
				Usage:   openAIUsage{PromptTokens: 5, CompletionTokens: 5},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		provider := &Provider{
			BaseProvider: providers.NewBaseProvider("test", false, &http.Client{Timeout: 30 * time.Second}),
			model:        "gpt-4",
			baseURL:      server.URL,
			apiKey:       "test-key",
			defaults:     providers.ProviderDefaults{Temperature: 0.7},
		}

		_, err := provider.Predict(context.Background(), providers.PredictionRequest{
			Messages:    []types.Message{{Role: "user", Content: "Test"}},
			Temperature: 0.9,
		})

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if receivedTemp != 0.9 {
			t.Errorf("Expected temperature 0.9 (from request), got %.2f", receivedTemp)
		}
	})
}

func TestPredictStream_Integration(t *testing.T) {
	t.Run("Basic streaming response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Accept") != "text/event-stream" {
				t.Errorf("Expected Accept 'text/event-stream', got %q", r.Header.Get("Accept"))
			}

			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)

			// Send chunks
			chunks := []string{
				`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
				`data: {"choices":[{"delta":{"content":" world"}}]}`,
				`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
				`data: [DONE]`,
			}

			for _, chunk := range chunks {
				w.Write([]byte(chunk + "\n\n"))
				flusher.Flush()
			}
		}))
		defer server.Close()

		provider := &Provider{
			BaseProvider: providers.NewBaseProvider("test", false, &http.Client{}),
			model:        "gpt-4",
			baseURL:      server.URL,
			apiKey:       "test-key",
			defaults: providers.ProviderDefaults{
				Pricing: providers.Pricing{
					InputCostPer1K:  0.03,
					OutputCostPer1K: 0.06,
				},
			},
		}

		streamChan, err := provider.PredictStream(context.Background(), providers.PredictionRequest{
			Messages: []types.Message{{Role: "user", Content: "Test"}},
		})

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		var chunks []providers.StreamChunk
		for chunk := range streamChan {
			chunks = append(chunks, chunk)
		}

		if len(chunks) == 0 {
			t.Fatal("Expected chunks but got none")
		}

		// Check accumulated content
		lastChunk := chunks[len(chunks)-1]
		if lastChunk.Content != "Hello world" {
			t.Errorf("Expected accumulated content 'Hello world', got %q", lastChunk.Content)
		}

		// Check for cost info in final chunk
		if lastChunk.CostInfo == nil {
			t.Error("Expected cost info in final chunk")
		}
	})

	t.Run("Stream with tool calls", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)

			chunks := []string{
				`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_123","function":{"name":"get_weather","arguments":"{\"loc"}}]}}]}`,
				`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ation\":\"NYC\"}"}}]}}]}`,
				`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":20,"completion_tokens":15}}`,
				`data: [DONE]`,
			}

			for _, chunk := range chunks {
				w.Write([]byte(chunk + "\n\n"))
				flusher.Flush()
			}
		}))
		defer server.Close()

		provider := &Provider{
			BaseProvider: providers.NewBaseProvider("test", false, &http.Client{}),
			model:        "gpt-4",
			baseURL:      server.URL,
			apiKey:       "test-key",
			defaults: providers.ProviderDefaults{
				Pricing: providers.Pricing{
					InputCostPer1K:  0.03,
					OutputCostPer1K: 0.06,
				},
			},
		}

		streamChan, err := provider.PredictStream(context.Background(), providers.PredictionRequest{
			Messages: []types.Message{{Role: "user", Content: "What's the weather?"}},
		})

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		var lastChunk providers.StreamChunk
		for chunk := range streamChan {
			lastChunk = chunk
		}

		if len(lastChunk.ToolCalls) != 1 {
			t.Fatalf("Expected 1 tool call, got %d", len(lastChunk.ToolCalls))
		}

		tc := lastChunk.ToolCalls[0]
		if tc.ID != "call_123" {
			t.Errorf("Expected tool call ID 'call_123', got %q", tc.ID)
		}
		if tc.Name != "get_weather" {
			t.Errorf("Expected tool call name 'get_weather', got %q", tc.Name)
		}
		expectedArgs := `{"location":"NYC"}`
		if string(tc.Args) != expectedArgs {
			t.Errorf("Expected args %q, got %q", expectedArgs, string(tc.Args))
		}
	})

	t.Run("Context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)

			// Send one chunk
			w.Write([]byte(`data: {"choices":[{"delta":{"content":"Hello"}}]}` + "\n\n"))
			flusher.Flush()

			// Wait a bit before closing (simulates slow response)
			time.Sleep(100 * time.Millisecond)
		}))
		defer server.Close()

		provider := &Provider{
			BaseProvider: providers.NewBaseProvider("test", false, &http.Client{}),
			model:        "gpt-4",
			baseURL:      server.URL,
			apiKey:       "test-key",
			defaults:     providers.ProviderDefaults{},
		}

		ctx, cancel := context.WithCancel(context.Background())

		streamChan, err := provider.PredictStream(ctx, providers.PredictionRequest{
			Messages: []types.Message{{Role: "user", Content: "Test"}},
		})

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		// Read first chunk
		<-streamChan

		// Cancel context
		cancel()

		// Should finish (either with cancellation or normal end)
		for range streamChan {
			// Drain remaining chunks
		}
	})
}

func TestExtractContentString(t *testing.T) {
	tests := []struct {
		name     string
		content  interface{}
		expected string
	}{
		{
			name:     "Simple string content",
			content:  "Hello, world!",
			expected: "Hello, world!",
		},
		{
			name:     "Empty string",
			content:  "",
			expected: "",
		},
		{
			name: "Array with single text part",
			content: []interface{}{
				map[string]interface{}{"type": "text", "text": "Response text"},
			},
			expected: "Response text",
		},
		{
			name: "Array with multiple text parts",
			content: []interface{}{
				map[string]interface{}{"type": "text", "text": "First part. "},
				map[string]interface{}{"type": "text", "text": "Second part."},
			},
			expected: "First part. Second part.",
		},
		{
			name: "Array with mixed text and non-text parts",
			content: []interface{}{
				map[string]interface{}{"type": "text", "text": "Before image. "},
				map[string]interface{}{"type": "image_url", "image_url": map[string]string{"url": "https://example.com/img.png"}},
				map[string]interface{}{"type": "text", "text": "After image."},
			},
			expected: "Before image. After image.",
		},
		{
			name:     "Nil content",
			content:  nil,
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

func TestSupportsStreaming(t *testing.T) {
	provider := &Provider{
		BaseProvider: providers.NewBaseProvider("test", false, &http.Client{Timeout: 30 * time.Second}),
	}

	if !provider.SupportsStreaming() {
		t.Error("Expected OpenAI provider to support streaming")
	}
}

func TestOpenAIProvider_Model(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		expected string
	}{
		{"gpt-4", "gpt-4", "gpt-4"},
		{"gpt-4o", "gpt-4o", "gpt-4o"},
		{"gpt-3.5-turbo", "gpt-3.5-turbo", "gpt-3.5-turbo"},
		{"custom-model", "custom-fine-tuned-model", "custom-fine-tuned-model"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewProvider("test", tt.model, "https://api.openai.com/v1", providers.ProviderDefaults{}, false)
			if got := provider.Model(); got != tt.expected {
				t.Errorf("Model() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestPredict_ErrorPaths(t *testing.T) {
	// Test error paths in Predict/PredictStream
	tests := []struct {
		name        string
		messages    []types.Message
		expectError string
	}{
		{
			name: "unsupported audio content",
			messages: []types.Message{
				{
					Role: "user",
					Parts: []types.ContentPart{
						{Type: types.ContentTypeAudio},
					},
				},
			},
			expectError: "audio content requires audio model",
		},
		{
			name: "unsupported video content",
			messages: []types.Message{
				{
					Role: "user",
					Parts: []types.ContentPart{
						{Type: types.ContentTypeVideo},
					},
				},
			},
			expectError: "video content not supported by OpenAI API",
		},
		{
			name: "unknown content type",
			messages: []types.Message{
				{
					Role: "user",
					Parts: []types.ContentPart{
						{Type: "unknown_type"},
					},
				},
			},
			expectError: "unknown content type",
		},
		{
			name: "image without media",
			messages: []types.Message{
				{
					Role: "user",
					Parts: []types.ContentPart{
						{Type: types.ContentTypeImage, Media: nil},
					},
				},
			},
			expectError: "image part missing media content",
		},
	}

	provider := NewProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)
	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := providers.PredictionRequest{
				Messages: tt.messages,
			}

			// Test Predict
			_, err := provider.Predict(ctx, req)
			if err == nil {
				t.Error("Expected error from Predict")
			} else if !contains(err.Error(), tt.expectError) {
				t.Errorf("Predict error = %q, want to contain %q", err.Error(), tt.expectError)
			}

			// Test PredictStream
			_, err = provider.PredictStream(ctx, req)
			if err == nil {
				t.Error("Expected error from PredictStream")
			} else if !contains(err.Error(), tt.expectError) {
				t.Errorf("PredictStream error = %q, want to contain %q", err.Error(), tt.expectError)
			}
		})
	}
}

// contains checks if s contains substr
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestCalculateCost_FallbackPricing(t *testing.T) {
	// Test fallback pricing for different models (no pricing configured)
	tests := []struct {
		name          string
		model         string
		inputTokens   int
		outputTokens  int
		cachedTokens  int
		expectedTotal float64
	}{
		{
			name:          "gpt-4 fallback pricing",
			model:         "gpt-4",
			inputTokens:   1000,
			outputTokens:  1000,
			cachedTokens:  0,
			expectedTotal: 0.03 + 0.06, // $0.03 input + $0.06 output
		},
		{
			name:          "gpt-4o-mini fallback pricing",
			model:         "gpt-4o-mini",
			inputTokens:   1000,
			outputTokens:  1000,
			cachedTokens:  0,
			expectedTotal: 0.00015 + 0.0006, // very low cost model
		},
		{
			name:          "gpt-3.5-turbo fallback pricing",
			model:         "gpt-3.5-turbo",
			inputTokens:   1000,
			outputTokens:  1000,
			cachedTokens:  0,
			expectedTotal: 0.0015 + 0.002,
		},
		{
			name:          "gpt-4o fallback pricing (default)",
			model:         "gpt-4o",
			inputTokens:   1000,
			outputTokens:  1000,
			cachedTokens:  0,
			expectedTotal: 0.0025 + 0.01, // default pricing
		},
		{
			name:          "unknown model uses default pricing",
			model:         "unknown-model",
			inputTokens:   1000,
			outputTokens:  1000,
			cachedTokens:  0,
			expectedTotal: 0.0025 + 0.01, // default pricing
		},
		{
			name:          "gpt-4 with cached tokens",
			model:         "gpt-4",
			inputTokens:   1000,
			outputTokens:  500,
			cachedTokens:  200,
			expectedTotal: (800.0/1000.0)*0.03 + (200.0/1000.0)*0.015 + (500.0/1000.0)*0.06,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create provider without pricing configured to trigger fallback
			provider := NewProvider("test", tt.model, "https://api.openai.com/v1", providers.ProviderDefaults{}, false)
			cost := provider.CalculateCost(tt.inputTokens, tt.outputTokens, tt.cachedTokens)

			// Allow small floating point tolerance
			diff := cost.TotalCost - tt.expectedTotal
			if diff < -0.0001 || diff > 0.0001 {
				t.Errorf("TotalCost = %v, want %v (diff: %v)", cost.TotalCost, tt.expectedTotal, diff)
			}
		})
	}
}

func TestConvertResponseFormat(t *testing.T) {
	provider := NewProvider("test", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	tests := []struct {
		name           string
		input          *providers.ResponseFormat
		expectedNil    bool
		expectedType   string
		expectedSchema bool
		schemaName     string
		strict         bool
	}{
		{
			name:        "nil input",
			input:       nil,
			expectedNil: true,
		},
		{
			name: "text format",
			input: &providers.ResponseFormat{
				Type: providers.ResponseFormatText,
			},
			expectedType: "text",
		},
		{
			name: "json format",
			input: &providers.ResponseFormat{
				Type: providers.ResponseFormatJSON,
			},
			expectedType: "json_object",
		},
		{
			name: "json schema with custom name",
			input: &providers.ResponseFormat{
				Type:       providers.ResponseFormatJSONSchema,
				JSONSchema: []byte(`{"type": "object", "properties": {"name": {"type": "string"}}}`),
				SchemaName: "custom_schema",
				Strict:     true,
			},
			expectedType:   "json_schema",
			expectedSchema: true,
			schemaName:     "custom_schema",
			strict:         true,
		},
		{
			name: "json schema with default name",
			input: &providers.ResponseFormat{
				Type:       providers.ResponseFormatJSONSchema,
				JSONSchema: []byte(`{"type": "object"}`),
			},
			expectedType:   "json_schema",
			expectedSchema: true,
			schemaName:     "response_schema",
			strict:         false,
		},
		{
			name: "json schema with invalid JSON falls back gracefully",
			input: &providers.ResponseFormat{
				Type:       providers.ResponseFormatJSONSchema,
				JSONSchema: []byte(`{invalid json`),
				SchemaName: "fallback_test",
			},
			expectedType:   "json_schema",
			expectedSchema: true,
			schemaName:     "fallback_test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := provider.convertResponseFormat(tt.input)

			if tt.expectedNil {
				if result != nil {
					t.Error("Expected nil result")
				}
				return
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			if result.Type != tt.expectedType {
				t.Errorf("Type = %q, want %q", result.Type, tt.expectedType)
			}

			if tt.expectedSchema {
				if result.JSONSchema == nil {
					t.Fatal("Expected JSONSchema to be set")
				}
				if result.JSONSchema.Name != tt.schemaName {
					t.Errorf("SchemaName = %q, want %q", result.JSONSchema.Name, tt.schemaName)
				}
				if result.JSONSchema.Strict != tt.strict {
					t.Errorf("Strict = %v, want %v", result.JSONSchema.Strict, tt.strict)
				}
			}
		})
	}
}

func TestOpenAI_PlatformFieldsStored(t *testing.T) {
	defaults := providers.ProviderDefaults{Temperature: 0.7}
	pc := &providers.PlatformConfig{Region: "us-east-1"}
	cred := &mockCredential{credType: "api_key"}

	provider := NewProviderWithCredential("test", "gpt-4", "https://example.com", defaults, false, cred, "bedrock", pc)

	if provider.platform != "bedrock" {
		t.Errorf("Expected platform 'bedrock', got %q", provider.platform)
	}
	if provider.platformConfig == nil {
		t.Fatal("Expected platformConfig to be set")
	}
	if provider.platformConfig.Region != "us-east-1" {
		t.Errorf("Expected region 'us-east-1', got %q", provider.platformConfig.Region)
	}
}

func TestOpenAI_PlatformField(t *testing.T) {
	tests := []struct {
		platform string
		isBr     bool
	}{
		{"bedrock", true},
		{"azure", false},
		{"", false},
	}
	for _, tt := range tests {
		p := &Provider{platform: tt.platform}
		got := p.platform == "bedrock"
		if got != tt.isBr {
			t.Errorf("platform=%q == bedrock: got %v, want %v", tt.platform, got, tt.isBr)
		}
	}
}
