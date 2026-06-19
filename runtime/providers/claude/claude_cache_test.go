package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestClaudeProvider_CacheControlNotSentWhenDisabled tests that cache_control is not included
// when DisablePromptCaching is set to true in ProviderDefaults.
func TestClaudeProvider_CacheControlNotSentWhenDisabled(t *testing.T) {
	// Create a test server that captures the request
	var capturedRequest map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the request body
		if err := json.NewDecoder(r.Body).Decode(&capturedRequest); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		// Return a valid response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "msg_test",
			"type":    "message",
			"role":    "assistant",
			"content": []map[string]string{{"type": "text", "text": "Hello"}},
			"model":   "claude-haiku-4-5",
			"usage": map[string]int{
				"input_tokens":  10,
				"output_tokens": 5,
			},
		})
	}))
	defer server.Close()

	// Set API key for test
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	// Create provider with caching explicitly disabled via DisablePromptCaching
	provider := NewProvider(
		"test-haiku",
		"claude-haiku-4-5",
		server.URL,
		providers.ProviderDefaults{
			Temperature:          0.7,
			MaxTokens:            100,
			TopP:                 1.0,
			DisablePromptCaching: true,
		},
		false,
	)

	// Create a predict request with a long message that would trigger cache_control
	longContent := ""
	for i := 0; i < 10000; i++ {
		longContent += "This is a long message to test caching. "
	}

	req := providers.PredictionRequest{
		Messages: []types.Message{
			{Role: "user", Content: longContent},
		},
		Temperature: 0.7,
		MaxTokens:   100,
	}

	// Execute the predict
	_, err := provider.Predict(context.Background(), req)
	if err != nil {
		t.Fatalf("Predict failed: %v", err)
	}

	// CRITICAL TEST: Verify that cache_control was NOT sent
	messages, ok := capturedRequest["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		t.Fatal("No messages in captured request")
	}

	firstMessage, ok := messages[0].(map[string]interface{})
	if !ok {
		t.Fatal("First message is not a map")
	}

	content, ok := firstMessage["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatal("No content in first message")
	}

	firstContent, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatal("First content is not a map")
	}

	// cache_control should NOT be present when DisablePromptCaching is true
	if cacheControl, exists := firstContent["cache_control"]; exists {
		t.Errorf("cache_control should not be sent when DisablePromptCaching=true, but found: %v", cacheControl)
	}
}

// TestClaudeProvider_CacheControlSentForCurrentModels tests that cache_control IS included
// for current models (haiku-4-5, sonnet-4-6, opus-4-8) when caching is enabled (default).
func TestClaudeProvider_CacheControlSentForCurrentModels(t *testing.T) {
	currentModels := []string{
		"claude-haiku-4-5",
		"claude-sonnet-4-6",
		"claude-opus-4-8",
	}

	for _, model := range currentModels {
		t.Run(model, func(t *testing.T) {
			var capturedRequest map[string]interface{}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&capturedRequest); err != nil {
					t.Fatalf("Failed to decode request: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"id":      "msg_test",
					"type":    "message",
					"role":    "assistant",
					"content": []map[string]string{{"type": "text", "text": "Hello"}},
					"model":   model,
					"usage": map[string]int{
						"input_tokens":  10,
						"output_tokens": 5,
					},
				})
			}))
			defer server.Close()

			os.Setenv("ANTHROPIC_API_KEY", "test-key")
			defer os.Unsetenv("ANTHROPIC_API_KEY")

			provider := NewProvider(
				"test-provider",
				model,
				server.URL,
				providers.ProviderDefaults{
					Temperature: 0.7,
					MaxTokens:   100,
					TopP:        1.0,
					// DisablePromptCaching defaults to false → caching enabled
				},
				false,
			)

			longContent := ""
			for i := 0; i < 10000; i++ {
				longContent += "This is a long message to test caching. "
			}

			req := providers.PredictionRequest{
				Messages: []types.Message{
					{Role: "user", Content: longContent},
				},
				Temperature: 0.7,
				MaxTokens:   100,
			}

			_, err := provider.Predict(context.Background(), req)
			if err != nil {
				t.Fatalf("Predict failed: %v", err)
			}

			messages, ok := capturedRequest["messages"].([]interface{})
			if !ok || len(messages) == 0 {
				t.Fatal("No messages in captured request")
			}

			firstMessage, ok := messages[0].(map[string]interface{})
			if !ok {
				t.Fatal("First message is not a map")
			}

			content, ok := firstMessage["content"].([]interface{})
			if !ok || len(content) == 0 {
				t.Fatal("No content in first message")
			}

			firstContent, ok := content[0].(map[string]interface{})
			if !ok {
				t.Fatal("First content is not a map")
			}

			// cache_control SHOULD be present for current models with caching enabled
			if _, exists := firstContent["cache_control"]; !exists {
				t.Errorf("cache_control should be sent for %q with default caching settings, but was absent", model)
			}
		})
	}
}

// TestToolProvider_CacheControlNotSentWhenDisabled tests that the tool provider does not
// send cache_control when DisablePromptCaching is set to true in ProviderDefaults.
func TestToolProvider_CacheControlNotSentWhenDisabled(t *testing.T) {
	// Create a test server that captures the request
	var capturedRequest map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the request body
		if err := json.NewDecoder(r.Body).Decode(&capturedRequest); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		// Return a valid response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "msg_test",
			"type":    "message",
			"role":    "assistant",
			"content": []map[string]string{{"type": "text", "text": "Hello"}},
			"model":   "claude-haiku-4-5",
			"usage": map[string]int{
				"input_tokens":  10,
				"output_tokens": 5,
			},
		})
	}))
	defer server.Close()

	// Set API key for test
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	// Create tool provider with caching explicitly disabled
	provider := NewToolProvider(
		"test-provider",
		"claude-haiku-4-5",
		server.URL,
		providers.ProviderDefaults{
			Temperature:          0.7,
			MaxTokens:            100,
			TopP:                 1.0,
			DisablePromptCaching: true,
		},
		false,
	)

	// Create a predict request with a long system prompt
	longSystem := ""
	for i := 0; i < 10000; i++ {
		longSystem += "This is a long system prompt. "
	}

	req := providers.PredictionRequest{
		System: longSystem,
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
		Temperature: 0.7,
		MaxTokens:   100,
	}

	// Execute the predict with tools
	tools := []*providers.ToolDescriptor{
		{
			Name:        "test_tool",
			Description: "A test tool",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		},
	}

	_, _, err := provider.PredictWithTools(context.Background(), req, tools, "auto")
	if err != nil {
		t.Fatalf("PredictWithTools failed: %v", err)
	}

	// Verify that cache_control was NOT sent in system blocks when disabled
	system, ok := capturedRequest["system"].([]interface{})
	if ok && len(system) > 0 {
		systemBlock, ok := system[0].(map[string]interface{})
		if ok {
			if cacheControl, exists := systemBlock["cache_control"]; exists {
				t.Errorf("cache_control should not be sent when DisablePromptCaching=true, but found: %v", cacheControl)
			}
		}
	}

	// Also check messages
	messages, ok := capturedRequest["messages"].([]interface{})
	if ok && len(messages) > 0 {
		lastMessage, ok := messages[len(messages)-1].(map[string]interface{})
		if ok {
			content, ok := lastMessage["content"].([]interface{})
			if ok && len(content) > 0 {
				lastContent, ok := content[0].(map[string]interface{})
				if ok {
					if cacheControl, exists := lastContent["cache_control"]; exists {
						t.Errorf("cache_control should not be sent when DisablePromptCaching=true, but found: %v", cacheControl)
					}
				}
			}
		}
	}
}
