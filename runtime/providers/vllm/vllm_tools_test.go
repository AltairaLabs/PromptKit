package vllm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestBuildTooling(t *testing.T) {
	p := NewProvider("vllm-test", "test-model", "http://localhost:8000", providers.ProviderDefaults{}, false, nil)

	tests := []struct {
		name     string
		tools    []*providers.ToolDescriptor
		wantLen  int
		wantName string
	}{
		{
			name: "single tool",
			tools: []*providers.ToolDescriptor{
				{
					Name:        "get_weather",
					Description: "Get the weather forecast",
					InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"location": {
								"type": "string",
								"description": "City name"
							}
						},
						"required": ["location"]
					}`),
				},
			},
			wantLen:  1,
			wantName: "get_weather",
		},
		{
			name: "multiple tools",
			tools: []*providers.ToolDescriptor{
				{
					Name:        "tool1",
					Description: "First tool",
					InputSchema: json.RawMessage(`{"type": "object"}`),
				},
				{
					Name:        "tool2",
					Description: "Second tool",
					InputSchema: json.RawMessage(`{"type": "object"}`),
				},
			},
			wantLen: 2,
		},
		{
			name:    "empty tools",
			tools:   []*providers.ToolDescriptor{},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.BuildTooling(tt.tools)
			if err != nil {
				t.Fatalf("BuildTooling failed: %v", err)
			}

			// Empty tools returns nil
			if len(tt.tools) == 0 {
				if result != nil {
					t.Errorf("Expected nil for empty tools, got %v", result)
				}
				return
			}

			tools, ok := result.([]vllmTool)
			if !ok {
				t.Fatalf("Expected []vllmTool, got %T", result)
			}

			if len(tools) != tt.wantLen {
				t.Errorf("Expected %d tools, got %d", tt.wantLen, len(tools))
			}

			if tt.wantName != "" && len(tools) > 0 {
				if tools[0].Function.Name != tt.wantName {
					t.Errorf("Expected tool name %s, got %s", tt.wantName, tools[0].Function.Name)
				}
			}
		})
	}
}

func TestPredictWithTools_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("Expected /v1/chat/completions, got %s", r.URL.Path)
		}

		// Parse request to verify tools are included
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		// Verify tools field exists
		if _, ok := reqBody["tools"]; !ok {
			t.Error("Request missing tools field")
		}

		// Return mock response with tool call
		resp := vllmChatResponse{
			ID:      "chatcmpl-123",
			Model:   "vllm-test",
			Created: 1234567890,
			Choices: []vllmChatChoice{
				{
					Index: 0,
					Message: vllmMessage{
						Role:    "assistant",
						Content: "",
						ToolCalls: []vllmToolCall{
							{
								ID:   "call_abc123",
								Type: "function",
								Function: vllmFunctionCall{
									Name:      "get_weather",
									Arguments: `{"location":"San Francisco"}`,
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
			Usage: vllmUsage{
				PromptTokens:     10,
				CompletionTokens: 20,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create provider
	p := NewProvider("vllm-test", "test-model", server.URL, providers.ProviderDefaults{}, false, nil)

	// Build tools
	tools, err := p.BuildTooling([]*providers.ToolDescriptor{
		{
			Name:        "get_weather",
			Description: "Get weather",
			InputSchema: json.RawMessage(`{"type": "object"}`),
		},
	})
	if err != nil {
		t.Fatalf("BuildTooling failed: %v", err)
	}

	// Make prediction with tools
	req := providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "What's the weather in SF?"},
		},
		MaxTokens: 100,
	}

	resp, toolCalls, err := p.PredictWithTools(context.Background(), req, tools, "auto")
	if err != nil {
		t.Fatalf("PredictWithTools failed: %v", err)
	}

	// Verify response
	if len(toolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(toolCalls))
	}

	tc := toolCalls[0]
	if tc.ID != "call_abc123" {
		t.Errorf("Expected tool call ID 'call_abc123', got %s", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("Expected tool name 'get_weather', got %s", tc.Name)
	}

	var args map[string]interface{}
	if err := json.Unmarshal(tc.Args, &args); err != nil {
		t.Fatalf("Failed to unmarshal args: %v", err)
	}
	if args["location"] != "San Francisco" {
		t.Errorf("Expected location 'San Francisco', got %v", args["location"])
	}

	// Verify cost calculation
	if resp.CostInfo.InputTokens != 10 {
		t.Errorf("Expected 10 input tokens, got %d", resp.CostInfo.InputTokens)
	}
	if resp.CostInfo.OutputTokens != 20 {
		t.Errorf("Expected 20 output tokens, got %d", resp.CostInfo.OutputTokens)
	}
}

func TestPredictWithTools_NoTools(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := vllmChatResponse{
			ID:      "chatcmpl-123",
			Model:   "vllm-test",
			Created: 1234567890,
			Choices: []vllmChatChoice{
				{
					Index: 0,
					Message: vllmMessage{
						Role:    "assistant",
						Content: "I cannot check the weather without tools.",
					},
					FinishReason: "stop",
				},
			},
			Usage: vllmUsage{
				PromptTokens:     10,
				CompletionTokens: 15,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create provider
	p := NewProvider("vllm-test", "test-model", server.URL, providers.ProviderDefaults{}, false, nil)

	// Make prediction without tools
	req := providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "What's the weather?"},
		},
		MaxTokens: 100,
	}

	resp, toolCalls, err := p.PredictWithTools(context.Background(), req, nil, "")
	if err != nil {
		t.Fatalf("PredictWithTools failed: %v", err)
	}

	// Verify no tool calls
	if len(toolCalls) != 0 {
		t.Errorf("Expected 0 tool calls, got %d", len(toolCalls))
	}

	// Verify regular response
	if resp.Content == "" {
		t.Error("Expected content in response")
	}
}

func TestPredictStreamWithTools_Success(t *testing.T) {
	// Create mock SSE stream
	sseData := `data: {"id":"chatcmpl-123","model":"vllm-test","created":1234567890,"choices":[{"index":0,"delta":{"role":"assistant","content":"","tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-123","model":"vllm-test","created":1234567890,"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"location\":\""}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-123","model":"vllm-test","created":1234567890,"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"SF\"}"}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-123","model":"vllm-test","created":1234567890,"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseData))
	}))
	defer server.Close()

	// Create provider
	p := NewProvider("vllm-test", "test-model", server.URL, providers.ProviderDefaults{}, false, nil)

	// Build tools
	tools, err := p.BuildTooling([]*providers.ToolDescriptor{
		{
			Name:        "get_weather",
			Description: "test",
			InputSchema: json.RawMessage(`{"type": "object"}`),
		},
	})
	if err != nil {
		t.Fatalf("BuildTooling failed: %v", err)
	}

	// Make streaming prediction
	req := providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "What's the weather in SF?"},
		},
		MaxTokens: 100,
	}

	chunks, err := p.PredictStreamWithTools(context.Background(), req, tools, "auto")
	if err != nil {
		t.Fatalf("PredictStreamWithTools failed: %v", err)
	}

	// Collect chunks
	var finalChunk *providers.StreamChunk
	chunkCount := 0
	for chunk := range chunks {
		chunkCount++
		if chunk.Error != nil {
			t.Fatalf("Stream error: %v", chunk.Error)
		}
		if chunk.FinishReason != nil {
			finalChunk = &chunk
		}
	}

	// Verify we got chunks
	if chunkCount == 0 {
		t.Error("Expected stream chunks, got none")
	}

	// Verify final chunk with tool calls
	if finalChunk == nil {
		t.Fatal("Expected final chunk with finish reason")
	}

	if *finalChunk.FinishReason != "tool_calls" {
		t.Errorf("Expected finish reason 'tool_calls', got %s", *finalChunk.FinishReason)
	}

	if len(finalChunk.ToolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(finalChunk.ToolCalls))
	}

	tc := finalChunk.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("Expected tool call ID 'call_abc', got %s", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("Expected tool name 'get_weather', got %s", tc.Name)
	}

	// Verify args were accumulated
	var args map[string]interface{}
	if err := json.Unmarshal(tc.Args, &args); err != nil {
		t.Fatalf("Failed to unmarshal args: %v", err)
	}
	if args["location"] != "SF" {
		t.Errorf("Expected location 'SF', got %v", args["location"])
	}
}

func TestPredictWithTools_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"Internal server error"}}`))
	}))
	defer server.Close()

	p := NewProvider("vllm-test", "test-model", server.URL, providers.ProviderDefaults{}, false, nil)

	tools, _ := p.BuildTooling([]*providers.ToolDescriptor{
		{
			Name:        "test_tool",
			Description: "test",
			InputSchema: json.RawMessage(`{"type": "object"}`),
		},
	})

	req := providers.PredictionRequest{
		Messages:  []types.Message{{Role: "user", Content: "test"}},
		MaxTokens: 100,
	}

	_, _, err := p.PredictWithTools(context.Background(), req, tools, "auto")
	if err == nil {
		t.Error("Expected error for HTTP error response")
	}
}

func TestPredictStreamWithTools_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"Bad request"}}`))
	}))
	defer server.Close()

	p := NewProvider("vllm-test", "test-model", server.URL, providers.ProviderDefaults{}, false, nil)

	tools, _ := p.BuildTooling([]*providers.ToolDescriptor{
		{
			Name:        "test_tool",
			Description: "test",
			InputSchema: json.RawMessage(`{"type": "object"}`),
		},
	})

	req := providers.PredictionRequest{
		Messages:  []types.Message{{Role: "user", Content: "test"}},
		MaxTokens: 100,
	}

	_, err := p.PredictStreamWithTools(context.Background(), req, tools, "auto")
	if err == nil {
		t.Error("Expected error for HTTP error response")
	}
}

func TestPredictWithTools_VLLMParams(t *testing.T) {
	// Create mock server that captures the request
	var receivedReq map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)

		resp := vllmChatResponse{
			ID:      "chatcmpl-123",
			Model:   "vllm-test",
			Created: 1234567890,
			Choices: []vllmChatChoice{
				{
					Index:        0,
					Message:      vllmMessage{Role: "assistant", Content: "test"},
					FinishReason: "stop",
				},
			},
			Usage: vllmUsage{PromptTokens: 10, CompletionTokens: 5},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create provider with vLLM-specific config
	additionalConfig := map[string]any{
		"use_beam_search":     true,
		"best_of":             3,
		"ignore_eos":          true,
		"skip_special_tokens": true,
		"guided_json":         map[string]interface{}{"type": "object"},
		"guided_regex":        "test.*",
		"guided_choice":       []string{"a", "b", "c"},
	}
	p := NewProvider("vllm-test", "test-model", server.URL, providers.ProviderDefaults{}, false, additionalConfig)

	// Build tools
	tools, _ := p.BuildTooling([]*providers.ToolDescriptor{
		{
			Name:        "get_weather",
			Description: "test",
			InputSchema: json.RawMessage(`{"type": "object"}`),
		},
	})

	// Make prediction with seed
	seed := 42
	req := providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: "test"},
		},
		MaxTokens: 100,
		Seed:      &seed,
	}

	_, _, err := p.PredictWithTools(context.Background(), req, tools, "auto")
	if err != nil {
		t.Fatalf("PredictWithTools failed: %v", err)
	}

	// Verify vLLM-specific params in request
	if receivedReq["use_beam_search"] != true {
		t.Error("Expected use_beam_search to be true")
	}
	if receivedReq["best_of"] != float64(3) {
		t.Error("Expected best_of to be 3")
	}
	if receivedReq["ignore_eos"] != true {
		t.Error("Expected ignore_eos to be true")
	}
	if receivedReq["skip_special_tokens"] != true {
		t.Error("Expected skip_special_tokens to be true")
	}
	if receivedReq["guided_json"] == nil {
		t.Error("Expected guided_json to be set")
	}
	if receivedReq["guided_regex"] != "test.*" {
		t.Error("Expected guided_regex to be set")
	}
	if receivedReq["guided_choice"] == nil {
		t.Error("Expected guided_choice to be set")
	}
	if receivedReq["seed"] != float64(42) {
		t.Error("Expected seed to be 42")
	}
}

func TestPredictWithTools_ParseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`invalid json`))
	}))
	defer server.Close()

	p := NewProvider("vllm-test", "test-model", server.URL, providers.ProviderDefaults{}, false, nil)

	tools, _ := p.BuildTooling([]*providers.ToolDescriptor{
		{
			Name:        "test_tool",
			Description: "test",
			InputSchema: json.RawMessage(`{"type": "object"}`),
		},
	})

	req := providers.PredictionRequest{
		Messages:  []types.Message{{Role: "user", Content: "test"}},
		MaxTokens: 100,
	}

	_, _, err := p.PredictWithTools(context.Background(), req, tools, "auto")
	if err == nil {
		t.Error("Expected error for invalid JSON response")
	}
}

func TestPredictWithTools_EmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := vllmChatResponse{
			ID:      "chatcmpl-123",
			Model:   "vllm-test",
			Created: 1234567890,
			Choices: []vllmChatChoice{},
			Usage:   vllmUsage{PromptTokens: 10, CompletionTokens: 5},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := NewProvider("vllm-test", "test-model", server.URL, providers.ProviderDefaults{}, false, nil)

	tools, _ := p.BuildTooling([]*providers.ToolDescriptor{
		{
			Name:        "test_tool",
			Description: "test",
			InputSchema: json.RawMessage(`{"type": "object"}`),
		},
	})

	req := providers.PredictionRequest{
		Messages:  []types.Message{{Role: "user", Content: "test"}},
		MaxTokens: 100,
	}

	_, _, err := p.PredictWithTools(context.Background(), req, tools, "auto")
	if err == nil {
		t.Error("Expected error for empty choices")
	}
}
