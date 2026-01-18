package vllm

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

	provider := NewProvider("test-vllm", "meta-llama/Llama-2-7b-chat-hf", "http://localhost:8000", defaults, false, nil)

	if provider == nil {
		t.Fatal("Expected non-nil provider")
	}

	if provider.ID() != "test-vllm" {
		t.Errorf("Expected ID 'test-vllm', got '%s'", provider.ID())
	}

	if provider.model != "meta-llama/Llama-2-7b-chat-hf" {
		t.Errorf("Expected model 'meta-llama/Llama-2-7b-chat-hf', got '%s'", provider.model)
	}

	if provider.baseURL != "http://localhost:8000" {
		t.Error("BaseURL mismatch")
	}
}

func TestNewProvider_WithAPIKey(t *testing.T) {
	additionalConfig := map[string]any{
		"api_key": "test-api-key",
	}

	provider := NewProvider("test", "model", "http://localhost:8000",
		providers.ProviderDefaults{}, false, additionalConfig)

	if provider.apiKey != "test-api-key" {
		t.Errorf("Expected apiKey 'test-api-key', got '%s'", provider.apiKey)
	}
}

func TestVLLMProvider_Cost_Free(t *testing.T) {
	provider := NewProvider("test", "model", "url", providers.ProviderDefaults{}, false, nil)

	breakdown := provider.CalculateCost(1000, 1000, 0)

	if breakdown.TotalCost != 0 {
		t.Errorf("Expected zero cost for vLLM, got %.4f", breakdown.TotalCost)
	}

	if breakdown.InputTokens != 1000 {
		t.Errorf("Expected 1000 input tokens, got %d", breakdown.InputTokens)
	}
}

func TestVLLMProvider_Cost_WithCustomPricing(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Pricing: providers.Pricing{
			InputCostPer1K:  0.5,
			OutputCostPer1K: 1.5,
		},
	}

	provider := NewProvider("test", "model", "url", defaults, false, nil)

	breakdown := provider.CalculateCost(1000, 500, 0)

	expectedInputCost := 0.5
	expectedOutputCost := 0.75
	expectedTotal := expectedInputCost + expectedOutputCost

	if breakdown.InputCostUSD != expectedInputCost {
		t.Errorf("Expected input cost %.4f, got %.4f", expectedInputCost, breakdown.InputCostUSD)
	}

	if breakdown.OutputCostUSD != expectedOutputCost {
		t.Errorf("Expected output cost %.4f, got %.4f", expectedOutputCost, breakdown.OutputCostUSD)
	}

	if breakdown.TotalCost != expectedTotal {
		t.Errorf("Expected total cost %.4f, got %.4f", expectedTotal, breakdown.TotalCost)
	}
}

func TestPredict_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}

		if r.URL.Path != vllmChatCompletionsPath {
			t.Errorf("Expected path %s, got %s", vllmChatCompletionsPath, r.URL.Path)
		}

		resp := vllmChatResponse{
			ID:      "test-id",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   "test-model",
			Choices: []vllmChatChoice{
				{
					Index: 0,
					Message: vllmMessage{
						Role:    "assistant",
						Content: "Hello! How can I help you?",
					},
					FinishReason: "stop",
				},
			},
			Usage: vllmUsage{
				PromptTokens:     10,
				CompletionTokens: 8,
				TotalTokens:      18,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewProvider("test", "model", server.URL, providers.ProviderDefaults{
		Temperature: 0.7,
		TopP:        0.9,
		MaxTokens:   100,
	}, false, nil)

	req := providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	resp, err := provider.Predict(context.Background(), req)
	if err != nil {
		t.Fatalf("Predict failed: %v", err)
	}

	if resp.Content != "Hello! How can I help you?" {
		t.Errorf("Content mismatch: got %s", resp.Content)
	}

	if resp.CostInfo == nil {
		t.Fatal("Expected cost info")
	}

	if resp.CostInfo.InputTokens != 10 {
		t.Errorf("Expected 10 input tokens, got %d", resp.CostInfo.InputTokens)
	}
}

func TestPredict_WithAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Error("Expected Authorization header with Bearer token")
		}

		resp := vllmChatResponse{
			Choices: []vllmChatChoice{
				{Message: vllmMessage{Content: "OK"}},
			},
			Usage: vllmUsage{PromptTokens: 5, CompletionTokens: 5},
		}

		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	additionalConfig := map[string]any{
		"api_key": "test-key",
	}

	provider := NewProvider("test", "model", server.URL,
		providers.ProviderDefaults{Temperature: 0.7, TopP: 0.9, MaxTokens: 100},
		false, additionalConfig)

	req := providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Test"}},
	}

	_, err := provider.Predict(context.Background(), req)
	if err != nil {
		t.Fatalf("Predict failed: %v", err)
	}
}

func TestSupportsStreaming(t *testing.T) {
	provider := NewProvider("test", "model", "url", providers.ProviderDefaults{}, false, nil)
	if !provider.SupportsStreaming() {
		t.Error("vLLM provider should support streaming")
	}
}

func TestClose(t *testing.T) {
	provider := NewProvider("test", "model", "url", providers.ProviderDefaults{}, false, nil)
	if err := provider.Close(); err != nil {
		t.Errorf("Close() failed: %v", err)
	}
}

func TestModel(t *testing.T) {
	provider := NewProvider("test", "meta-llama/Llama-2-7b", "url", providers.ProviderDefaults{}, false, nil)
	if provider.Model() != "meta-llama/Llama-2-7b" {
		t.Errorf("Expected model 'meta-llama/Llama-2-7b', got '%s'", provider.Model())
	}
}

func TestPredict_Error_NoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := vllmChatResponse{
			Choices: []vllmChatChoice{},
			Usage:   vllmUsage{PromptTokens: 5, CompletionTokens: 5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewProvider("test", "model", server.URL,
		providers.ProviderDefaults{Temperature: 0.7, TopP: 0.9, MaxTokens: 100},
		false, nil)

	req := providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Test"}},
	}

	_, err := provider.Predict(context.Background(), req)
	if err == nil {
		t.Error("Expected error for response with no choices")
	}
	if err != nil && !contains(err.Error(), "no choices") {
		t.Errorf("Expected 'no choices' error, got: %v", err)
	}
}

func TestPredict_Error_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	provider := NewProvider("test", "model", server.URL,
		providers.ProviderDefaults{Temperature: 0.7, TopP: 0.9, MaxTokens: 100},
		false, nil)

	req := providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Test"}},
	}

	_, err := provider.Predict(context.Background(), req)
	if err == nil {
		t.Error("Expected error for HTTP 500")
	}
}

func TestPredict_Error_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	provider := NewProvider("test", "model", server.URL,
		providers.ProviderDefaults{Temperature: 0.7, TopP: 0.9, MaxTokens: 100},
		false, nil)

	req := providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Test"}},
	}

	_, err := provider.Predict(context.Background(), req)
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestPredict_Error_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := vllmChatResponse{
			Error: &vllmError{
				Message: "Model not found",
				Type:    "model_error",
				Code:    "404",
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewProvider("test", "model", server.URL,
		providers.ProviderDefaults{Temperature: 0.7, TopP: 0.9, MaxTokens: 100},
		false, nil)

	req := providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Test"}},
	}

	_, err := provider.Predict(context.Background(), req)
	if err == nil {
		t.Error("Expected error for API error response")
	}
	if err != nil && !contains(err.Error(), "Model not found") {
		t.Errorf("Expected 'Model not found' in error, got: %v", err)
	}
}

func TestPredict_WithSystemMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req vllmRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Verify system message is first
		if len(req.Messages) != 2 {
			t.Errorf("Expected 2 messages, got %d", len(req.Messages))
		}
		if req.Messages[0].Role != "system" {
			t.Errorf("Expected first message to be system, got %s", req.Messages[0].Role)
		}

		resp := vllmChatResponse{
			Choices: []vllmChatChoice{
				{Message: vllmMessage{Content: "Response"}},
			},
			Usage: vllmUsage{PromptTokens: 10, CompletionTokens: 5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewProvider("test", "model", server.URL,
		providers.ProviderDefaults{Temperature: 0.7, TopP: 0.9, MaxTokens: 100},
		false, nil)

	req := providers.PredictionRequest{
		System:   "You are a helpful assistant",
		Messages: []types.Message{{Role: "user", Content: "Test"}},
	}

	_, err := provider.Predict(context.Background(), req)
	if err != nil {
		t.Errorf("Predict with system message failed: %v", err)
	}
}

func TestPredict_WithVLLMParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req vllmRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Verify vLLM-specific params
		if !req.UseBeamSearch {
			t.Error("Expected use_beam_search to be true")
		}
		if req.BestOf != 3 {
			t.Errorf("Expected best_of=3, got %d", req.BestOf)
		}
		if !req.IgnoreEOS {
			t.Error("Expected ignore_eos to be true")
		}
		if req.GuidedJSON == nil {
			t.Error("Expected guided_json to be set")
		}

		resp := vllmChatResponse{
			Choices: []vllmChatChoice{
				{Message: vllmMessage{Content: "Response"}},
			},
			Usage: vllmUsage{PromptTokens: 10, CompletionTokens: 5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	additionalConfig := map[string]any{
		"use_beam_search": true,
		"best_of":         3,
		"ignore_eos":      true,
		"guided_json":     map[string]interface{}{"type": "object"},
	}

	provider := NewProvider("test", "model", server.URL,
		providers.ProviderDefaults{Temperature: 0.7, TopP: 0.9, MaxTokens: 100},
		false, additionalConfig)

	req := providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Test"}},
	}

	_, err := provider.Predict(context.Background(), req)
	if err != nil {
		t.Errorf("Predict with vLLM params failed: %v", err)
	}
}

func TestPredict_WithSeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req vllmRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Seed == nil || *req.Seed != 42 {
			t.Error("Expected seed=42")
		}

		resp := vllmChatResponse{
			Choices: []vllmChatChoice{
				{Message: vllmMessage{Content: "Response"}},
			},
			Usage: vllmUsage{PromptTokens: 10, CompletionTokens: 5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewProvider("test", "model", server.URL,
		providers.ProviderDefaults{Temperature: 0.7, TopP: 0.9, MaxTokens: 100},
		false, nil)

	seed := 42
	req := providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Test"}},
		Seed:     &seed,
	}

	_, err := provider.Predict(context.Background(), req)
	if err != nil {
		t.Errorf("Predict with seed failed: %v", err)
	}
}

func TestPredict_IncludeRawOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := vllmChatResponse{
			Choices: []vllmChatChoice{
				{Message: vllmMessage{Content: "Response"}},
			},
			Usage: vllmUsage{PromptTokens: 10, CompletionTokens: 5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewProvider("test", "model", server.URL,
		providers.ProviderDefaults{Temperature: 0.7, TopP: 0.9, MaxTokens: 100},
		true, nil)

	req := providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Test"}},
	}

	resp, err := provider.Predict(context.Background(), req)
	if err != nil {
		t.Fatalf("Predict failed: %v", err)
	}

	if resp.RawRequest == nil {
		t.Error("Expected RawRequest to be included")
	}
	if len(resp.Raw) == 0 {
		t.Error("Expected Raw response to be included")
	}
}

func TestPredictStream_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		// Send streaming chunks
		chunks := []string{
			`data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{"content":" world"},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{"content":"!"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3}}`,
			`data: [DONE]`,
		}

		for _, chunk := range chunks {
			w.Write([]byte(chunk + "\n\n"))
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()

	provider := NewProvider("test", "model", server.URL,
		providers.ProviderDefaults{Temperature: 0.7, TopP: 0.9, MaxTokens: 100},
		false, nil)

	req := providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Test"}},
	}

	stream, err := provider.PredictStream(context.Background(), req)
	if err != nil {
		t.Fatalf("PredictStream failed: %v", err)
	}

	var lastContent string
	var gotUsage bool
	for chunk := range stream {
		if chunk.Error != nil {
			t.Errorf("Stream chunk error: %v", chunk.Error)
		}
		lastContent = chunk.Content
		if chunk.CostInfo != nil {
			gotUsage = true
		}
	}

	if lastContent != "Hello world!" {
		t.Errorf("Expected 'Hello world!', got '%s'", lastContent)
	}
	if !gotUsage {
		t.Error("Expected to receive usage info in final chunk")
	}
}

func TestPredictStream_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		for i := 0; i < 100; i++ {
			w.Write([]byte(`data: {"choices":[{"delta":{"content":"x"},"finish_reason":null}]}` + "\n\n"))
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()

	provider := NewProvider("test", "model", server.URL,
		providers.ProviderDefaults{Temperature: 0.7, TopP: 0.9, MaxTokens: 100},
		false, nil)

	req := providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "Test"}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := provider.PredictStream(ctx, req)
	if err != nil {
		t.Fatalf("PredictStream failed: %v", err)
	}

	// Read a few chunks then cancel
	count := 0
	for chunk := range stream {
		count++
		if count == 3 {
			cancel()
		}
		if chunk.Error == context.Canceled {
			return // Success - context cancellation worked
		}
	}
}

func TestPrepareMessages_EmptySystem(t *testing.T) {
	provider := NewProvider("test", "model", "url", providers.ProviderDefaults{}, false, nil)

	req := providers.PredictionRequest{
		System:   "",
		Messages: []types.Message{{Role: "user", Content: "Hello"}},
	}

	messages, err := provider.prepareMessages(&req)
	if err != nil {
		t.Fatalf("prepareMessages failed: %v", err)
	}

	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}
	if messages[0].Role != "user" {
		t.Errorf("Expected user role, got %s", messages[0].Role)
	}
}

func TestPrepareMessages_MultipleMessages(t *testing.T) {
	provider := NewProvider("test", "model", "url", providers.ProviderDefaults{}, false, nil)

	req := providers.PredictionRequest{
		System: "You are helpful",
		Messages: []types.Message{
			{Role: "user", Content: "First"},
			{Role: "assistant", Content: "Second"},
			{Role: "user", Content: "Third"},
		},
	}

	messages, err := provider.prepareMessages(&req)
	if err != nil {
		t.Fatalf("prepareMessages failed: %v", err)
	}

	if len(messages) != 4 {
		t.Errorf("Expected 4 messages, got %d", len(messages))
	}
	if messages[0].Role != "system" {
		t.Error("Expected first message to be system")
	}
}

func TestApplyRequestDefaults_AllZero(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Temperature: 0.8,
		TopP:        0.95,
		MaxTokens:   2048,
	}
	provider := NewProvider("test", "model", "url", defaults, false, nil)

	req := providers.PredictionRequest{
		Temperature: 0,
		TopP:        0,
		MaxTokens:   0,
	}

	temp, topP, maxTokens := provider.applyRequestDefaults(&req)

	if temp != 0.8 {
		t.Errorf("Expected temperature 0.8, got %f", temp)
	}
	if topP != 0.95 {
		t.Errorf("Expected topP 0.95, got %f", topP)
	}
	if maxTokens != 2048 {
		t.Errorf("Expected maxTokens 2048, got %d", maxTokens)
	}
}

func TestApplyRequestDefaults_NonZero(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Temperature: 0.8,
		TopP:        0.95,
		MaxTokens:   2048,
	}
	provider := NewProvider("test", "model", "url", defaults, false, nil)

	req := providers.PredictionRequest{
		Temperature: 0.5,
		TopP:        0.9,
		MaxTokens:   1000,
	}

	temp, topP, maxTokens := provider.applyRequestDefaults(&req)

	if temp != 0.5 {
		t.Errorf("Expected temperature 0.5, got %f", temp)
	}
	if topP != 0.9 {
		t.Errorf("Expected topP 0.9, got %f", topP)
	}
	if maxTokens != 1000 {
		t.Errorf("Expected maxTokens 1000, got %d", maxTokens)
	}
}

func TestExtractContentString(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"string content", "Hello", "Hello"},
		{"int content", 42, ""},
		{"nil content", nil, ""},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractContentString(tt.input)
			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) >= len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
