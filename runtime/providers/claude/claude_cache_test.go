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

// TestClaudeProvider_CacheControlNotSentForHaiku tests that cache_control is not included
// for models that don't support it (like Claude 3.5 Haiku)
func TestClaudeProvider_CacheControlNotSentForHaiku(t *testing.T) {
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
			"model":   "claude-3-5-haiku-20241022",
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

	// Create provider with Haiku model
	provider := NewClaudeProvider(
		"test-haiku",
		"claude-3-5-haiku-20241022",
		server.URL,
		providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   100,
			TopP:        1.0,
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

	// THE BUG: cache_control should NOT be present for Haiku
	if cacheControl, exists := firstContent["cache_control"]; exists {
		t.Errorf("CRITICAL BUG: cache_control should not be sent for Claude 3.5 Haiku, but found: %v", cacheControl)
		t.Errorf("This causes a 400 error: 'messages.0.cache_control: Extra inputs are not permitted'")
	}
}

// TestClaudeToolProvider_CacheControlNotSentForHaiku tests the same for tool provider
func TestClaudeToolProvider_CacheControlNotSentForHaiku(t *testing.T) {
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
			"model":   "claude-3-5-haiku-20241022",
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

	// Create tool provider with Haiku model
	provider := NewClaudeToolProvider(
		"test-haiku",
		"claude-3-5-haiku-20241022",
		server.URL,
		providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   100,
			TopP:        1.0,
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

	// CRITICAL TEST: Verify that cache_control was NOT sent in system blocks
	system, ok := capturedRequest["system"].([]interface{})
	if ok && len(system) > 0 {
		systemBlock, ok := system[0].(map[string]interface{})
		if ok {
			if cacheControl, exists := systemBlock["cache_control"]; exists {
				t.Errorf("CRITICAL BUG: cache_control should not be sent for Claude 3.5 Haiku system prompt, but found: %v", cacheControl)
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
						t.Errorf("CRITICAL BUG: cache_control should not be sent for Claude 3.5 Haiku message, but found: %v", cacheControl)
					}
				}
			}
		}
	}
}
