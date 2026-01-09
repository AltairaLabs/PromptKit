package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestNewToolProvider(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Temperature: 0.7,
		TopP:        0.9,
		MaxTokens:   1000,
	}

	provider := NewToolProvider("test-ollama", "llama2", "http://localhost:11434",
		defaults, false, nil)

	if provider == nil {
		t.Fatal("Expected non-nil provider")
	}

	if provider.ID() != "test-ollama" {
		t.Errorf("Expected ID 'test-ollama', got '%s'", provider.ID())
	}

	if provider.model != "llama2" {
		t.Errorf("Expected model 'llama2', got '%s'", provider.model)
	}
}

func TestToolProvider_BuildTooling(t *testing.T) {
	provider := NewToolProvider("test", "llama2", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	descriptors := []*providers.ToolDescriptor{
		{
			Name:        "get_weather",
			Description: "Get the current weather in a location",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {"location": {"type": "string"}}}`),
		},
		{
			Name:        "search",
			Description: "Search for information",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {"query": {"type": "string"}}}`),
		},
	}

	tools, err := provider.BuildTooling(descriptors)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	toolsSlice, ok := tools.([]ollamaTool)
	if !ok {
		t.Fatal("Expected tools to be []ollamaTool")
	}

	if len(toolsSlice) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(toolsSlice))
	}

	if toolsSlice[0].Type != "function" {
		t.Errorf("Expected type 'function', got '%s'", toolsSlice[0].Type)
	}

	if toolsSlice[0].Function.Name != "get_weather" {
		t.Errorf("Expected name 'get_weather', got '%s'", toolsSlice[0].Function.Name)
	}
}

func TestToolProvider_BuildTooling_Empty(t *testing.T) {
	provider := NewToolProvider("test", "llama2", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	tools, err := provider.BuildTooling(nil)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if tools != nil {
		t.Error("Expected nil tools for empty descriptors")
	}
}

func TestToolProvider_PredictWithTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("Expected path '/v1/chat/completions', got %q", r.URL.Path)
		}

		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		// Verify tools were sent
		if _, ok := req["tools"]; !ok {
			t.Error("Expected tools in request")
		}

		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": "",
						"tool_calls": []map[string]any{
							{
								"id":   "call_123",
								"type": "function",
								"function": map[string]any{
									"name":      "get_weather",
									"arguments": `{"location": "San Francisco"}`,
								},
							},
						},
					},
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     20,
				"completion_tokens": 15,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewToolProvider("test", "llama2", server.URL,
		providers.ProviderDefaults{MaxTokens: 1000}, false, nil)

	tools, _ := provider.BuildTooling([]*providers.ToolDescriptor{
		{
			Name:        "get_weather",
			Description: "Get the current weather",
			InputSchema: json.RawMessage(`{"type": "object"}`),
		},
	})

	resp, toolCalls, err := provider.PredictWithTools(
		context.Background(),
		providers.PredictionRequest{
			Messages: []types.Message{{Role: "user", Content: "What's the weather?"}},
		},
		tools,
		"auto",
	)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(toolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(toolCalls))
	}

	if toolCalls[0].Name != "get_weather" {
		t.Errorf("Expected tool name 'get_weather', got '%s'", toolCalls[0].Name)
	}

	if toolCalls[0].ID != "call_123" {
		t.Errorf("Expected tool ID 'call_123', got '%s'", toolCalls[0].ID)
	}

	if resp.CostInfo == nil {
		t.Error("Expected cost info")
	} else if resp.CostInfo.TotalCost != 0 {
		t.Error("Expected zero cost for Ollama")
	}
}

func TestToolProvider_PredictWithTools_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "Internal server error"}`))
	}))
	defer server.Close()

	provider := NewToolProvider("test", "llama2", server.URL,
		providers.ProviderDefaults{}, false, nil)

	_, _, err := provider.PredictWithTools(
		context.Background(),
		providers.PredictionRequest{
			Messages: []types.Message{{Role: "user", Content: "Test"}},
		},
		nil,
		"auto",
	)

	if err == nil {
		t.Error("Expected error for server error")
	}
}

func TestToolProvider_ToolChoice(t *testing.T) {
	tests := []struct {
		name           string
		toolChoice     string
		expectedChoice any
	}{
		{"auto", "auto", nil},
		{"required", "required", "required"},
		{"none", "none", "none"},
		{"specific function", "get_weather", map[string]any{
			"type":     "function",
			"function": map[string]string{"name": "get_weather"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedChoice any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req map[string]any
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Fatalf("Failed to decode request: %v", err)
				}
				receivedChoice = req["tool_choice"]

				resp := map[string]any{
					"choices": []map[string]any{
						{"message": map[string]any{"content": "Test"}},
					},
					"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 5},
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(resp); err != nil {
					t.Fatalf("Failed to encode response: %v", err)
				}
			}))
			defer server.Close()

			provider := NewToolProvider("test", "llama2", server.URL,
				providers.ProviderDefaults{MaxTokens: 100}, false, nil)

			tools := []ollamaTool{{Type: "function", Function: ollamaToolFunction{Name: "test"}}}

			_, _, _ = provider.PredictWithTools(
				context.Background(),
				providers.PredictionRequest{
					Messages: []types.Message{{Role: "user", Content: "Test"}},
				},
				tools,
				tt.toolChoice,
			)

			if tt.expectedChoice == nil && receivedChoice != nil {
				t.Errorf("Expected no tool_choice, got %v", receivedChoice)
			}
		})
	}
}

func TestToolProvider_ConvertToolCallsToOllama(t *testing.T) {
	provider := NewToolProvider("test", "llama2", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	toolCalls := []types.MessageToolCall{
		{
			ID:   "call_1",
			Name: "get_weather",
			Args: json.RawMessage(`{"location": "NYC"}`),
		},
		{
			ID:   "call_2",
			Name: "search",
			Args: json.RawMessage(`{"query": "news"}`),
		},
	}

	result := provider.convertToolCallsToOllama(toolCalls)

	if len(result) != 2 {
		t.Fatalf("Expected 2 tool calls, got %d", len(result))
	}

	if result[0]["id"] != "call_1" {
		t.Error("ID mismatch")
	}

	if result[0]["type"] != "function" {
		t.Error("Type mismatch")
	}

	fn, ok := result[0]["function"].(map[string]any)
	if !ok {
		t.Fatal("Expected function map")
	}

	if fn["name"] != "get_weather" {
		t.Error("Function name mismatch")
	}
}

func TestToolProvider_ConvertSingleMessageForTools(t *testing.T) {
	provider := NewToolProvider("test", "llama2", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	t.Run("simple message", func(t *testing.T) {
		msg := &types.Message{
			Role:    "user",
			Content: "Hello",
		}

		result := provider.convertSingleMessageForTools(msg)

		if result["role"] != "user" {
			t.Error("Role mismatch")
		}
		if result["content"] != "Hello" {
			t.Error("Content mismatch")
		}
	})

	t.Run("message with tool calls", func(t *testing.T) {
		msg := &types.Message{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{ID: "call_1", Name: "test", Args: json.RawMessage(`{}`)},
			},
		}

		result := provider.convertSingleMessageForTools(msg)

		if result["tool_calls"] == nil {
			t.Error("Expected tool_calls in result")
		}
	})

	t.Run("tool result message", func(t *testing.T) {
		msg := &types.Message{
			Role: "tool",
			ToolResult: &types.MessageToolResult{
				ID:      "call_1",
				Name:    "test",
				Content: "result",
			},
			Content: "result",
		}

		result := provider.convertSingleMessageForTools(msg)

		if result["tool_call_id"] != "call_1" {
			t.Error("Expected tool_call_id")
		}
		if result["name"] != "test" {
			t.Error("Expected name")
		}
	})
}

func TestToolProvider_ParseToolResponse(t *testing.T) {
	provider := NewToolProvider("test", "llama2", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	t.Run("response with tool calls", func(t *testing.T) {
		respBytes := []byte(`{
			"choices": [{
				"message": {
					"content": "",
					"tool_calls": [
						{"id": "call_1", "type": "function", "function": {"name": "test", "arguments": "{}"}}
					]
				}
			}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5}
		}`)

		resp, toolCalls, err := provider.parseToolResponse(respBytes)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if len(toolCalls) != 1 {
			t.Errorf("Expected 1 tool call, got %d", len(toolCalls))
		}

		if resp.CostInfo.TotalCost != 0 {
			t.Error("Expected zero cost")
		}
	})

	t.Run("response without tool calls", func(t *testing.T) {
		respBytes := []byte(`{
			"choices": [{"message": {"content": "Hello"}}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 5}
		}`)

		resp, toolCalls, err := provider.parseToolResponse(respBytes)

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if len(toolCalls) != 0 {
			t.Errorf("Expected 0 tool calls, got %d", len(toolCalls))
		}

		if resp.Content != "Hello" {
			t.Errorf("Expected content 'Hello', got '%s'", resp.Content)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		_, _, err := provider.parseToolResponse([]byte(`invalid`))

		if err == nil {
			t.Error("Expected error for invalid JSON")
		}
	})

	t.Run("no choices", func(t *testing.T) {
		respBytes := []byte(`{"choices": [], "usage": {}}`)

		_, _, err := provider.parseToolResponse(respBytes)

		if err == nil {
			t.Error("Expected error for no choices")
		}
	})
}

func TestToolProvider_AddToolChoiceToRequest(t *testing.T) {
	provider := NewToolProvider("test", "llama2", "http://localhost:11434",
		providers.ProviderDefaults{}, false, nil)

	tests := []struct {
		name       string
		toolChoice string
		checkFunc  func(req map[string]any) bool
	}{
		{
			name:       "empty choice",
			toolChoice: "",
			checkFunc: func(req map[string]any) bool {
				_, exists := req["tool_choice"]
				return !exists
			},
		},
		{
			name:       "auto choice",
			toolChoice: "auto",
			checkFunc: func(req map[string]any) bool {
				_, exists := req["tool_choice"]
				return !exists
			},
		},
		{
			name:       "required choice",
			toolChoice: "required",
			checkFunc: func(req map[string]any) bool {
				return req["tool_choice"] == "required"
			},
		},
		{
			name:       "none choice",
			toolChoice: "none",
			checkFunc: func(req map[string]any) bool {
				return req["tool_choice"] == "none"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := make(map[string]any)
			provider.addToolChoiceToRequest(req, tt.toolChoice)

			if !tt.checkFunc(req) {
				t.Errorf("Check failed for tool choice '%s'", tt.toolChoice)
			}
		})
	}
}

func TestFactoryRegistration(t *testing.T) {
	spec := providers.ProviderSpec{
		ID:      "test-ollama",
		Type:    "ollama",
		Model:   "llama2",
		BaseURL: "http://localhost:11434",
		Defaults: providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   1000,
		},
	}

	provider, err := providers.CreateProviderFromSpec(spec)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if provider == nil {
		t.Fatal("Expected non-nil provider")
	}

	if provider.ID() != "test-ollama" {
		t.Errorf("Expected ID 'test-ollama', got '%s'", provider.ID())
	}
}

func TestToolProvider_PredictStreamWithTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("Expected path '/v1/chat/completions', got %q", r.URL.Path)
		}

		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		// Verify tools were sent
		if _, ok := req["tools"]; !ok {
			t.Error("Expected tools in request")
		}

		// Verify streaming is enabled
		if req["stream"] != true {
			t.Error("Expected stream to be true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("Streaming not supported")
		}

		// Send tool call via streaming
		chunks := []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_123","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"location\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"NYC\"}"}}]}}]}`,
			`{"choices":[{"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":20,"completion_tokens":15}}`,
		}

		for _, chunk := range chunks {
			_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	provider := NewToolProvider("test", "llama2", server.URL,
		providers.ProviderDefaults{MaxTokens: 1000}, false, nil)

	tools, _ := provider.BuildTooling([]*providers.ToolDescriptor{
		{
			Name:        "get_weather",
			Description: "Get the current weather",
			InputSchema: json.RawMessage(`{"type": "object"}`),
		},
	})

	ch, err := provider.PredictStreamWithTools(
		context.Background(),
		providers.PredictionRequest{
			Messages: []types.Message{{Role: "user", Content: "What's the weather in NYC?"}},
		},
		tools,
		"auto",
	)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	var lastChunk providers.StreamChunk
	for chunk := range ch {
		lastChunk = chunk
	}

	if len(lastChunk.ToolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(lastChunk.ToolCalls))
	}

	if lastChunk.ToolCalls[0].Name != "get_weather" {
		t.Errorf("Expected tool name 'get_weather', got '%s'", lastChunk.ToolCalls[0].Name)
	}

	if lastChunk.ToolCalls[0].ID != "call_123" {
		t.Errorf("Expected tool ID 'call_123', got '%s'", lastChunk.ToolCalls[0].ID)
	}
}

func TestToolProvider_PredictStreamWithTools_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "Internal server error"}`))
	}))
	defer server.Close()

	provider := NewToolProvider("test", "llama2", server.URL,
		providers.ProviderDefaults{}, false, nil)

	tools, _ := provider.BuildTooling([]*providers.ToolDescriptor{
		{Name: "test", Description: "Test", InputSchema: json.RawMessage(`{}`)},
	})

	_, err := provider.PredictStreamWithTools(
		context.Background(),
		providers.PredictionRequest{
			Messages: []types.Message{{Role: "user", Content: "Test"}},
		},
		tools,
		"auto",
	)

	if err == nil {
		t.Error("Expected error for server error")
	}
}
