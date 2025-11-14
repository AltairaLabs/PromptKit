package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestGeminiToolProvider_ToolLoopDetection tests that tool results are properly
// sent to Gemini to prevent infinite loops
func TestGeminiToolProvider_ToolLoopDetection(t *testing.T) {
	var requestCount int
	var capturedRequests []map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}
		capturedRequests = append(capturedRequests, req)

		// Log what we received to help debug
		contents := req["contents"].([]interface{})
		t.Logf("Request %d: %d contents", requestCount, len(contents))
		for i, content := range contents {
			contentMap := content.(map[string]interface{})
			parts := contentMap["parts"].([]interface{})
			t.Logf("  Content %d: role=%s, parts=%d", i, contentMap["role"], len(parts))
			for j, part := range parts {
				partMap := part.(map[string]interface{})
				if text, ok := partMap["text"]; ok {
					t.Logf("    Part %d: text=%s", j, text)
				}
				if fc, ok := partMap["functionCall"]; ok {
					fcMap := fc.(map[string]interface{})
					t.Logf("    Part %d: functionCall=%s", j, fcMap["name"])
				}
				if fr, ok := partMap["functionResponse"]; ok {
					frMap := fr.(map[string]interface{})
					t.Logf("    Part %d: functionResponse=%s", j, frMap["name"])
				}
			}
		}

		// First request: return a tool call
		// Second request: should include tool results, so return final text
		if requestCount == 1 {
			// Return tool call response
			response := map[string]interface{}{
				"candidates": []map[string]interface{}{
					{
						"content": map[string]interface{}{
							"role": "model",
							"parts": []map[string]interface{}{
								{
									"functionCall": map[string]interface{}{
										"name": "check_subscription_status",
										"args": map[string]string{"email": "test@example.com"},
									},
								},
							},
						},
						"finishReason": "STOP",
					},
				},
				"usageMetadata": map[string]int{
					"promptTokenCount":     100,
					"candidatesTokenCount": 20,
					"totalTokenCount":      120,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		} else {
			// Second request should have tool results - verify they're there
			hasToolResults := false
			for _, content := range contents {
				contentMap := content.(map[string]interface{})
				parts := contentMap["parts"].([]interface{})
				for _, part := range parts {
					partMap := part.(map[string]interface{})
					if _, ok := partMap["functionResponse"]; ok {
						hasToolResults = true
						break
					}
				}
			}

			if !hasToolResults {
				t.Errorf("Second request missing functionResponse! Tool loop will occur.")
			}

			// Return final text response
			response := map[string]interface{}{
				"candidates": []map[string]interface{}{
					{
						"content": map[string]interface{}{
							"role": "model",
							"parts": []map[string]interface{}{
								{"text": "Your subscription status has been checked."},
							},
						},
						"finishReason": "STOP",
					},
				},
				"usageMetadata": map[string]int{
					"promptTokenCount":     150,
					"candidatesTokenCount": 30,
					"totalTokenCount":      180,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		}
	}))
	defer server.Close()

	provider := NewGeminiToolProvider(
		"test-gemini",
		"gemini-2.0-flash-exp",
		server.URL,
		providers.ProviderDefaults{MaxTokens: 1024, Temperature: 0.7},
		false,
	)

	// Initial user message
	messages := []types.Message{
		{Role: "user", Content: "Check my subscription"},
	}

	req := providers.PredictionRequest{
		Messages:    messages,
		MaxTokens:   1024,
		Temperature: 0.7,
	}

	tools := []providers.ToolDescriptor{
		{
			Name:        "check_subscription_status",
			Description: "Check subscription status",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"email":{"type":"string"}}}`),
		},
	}
	geminiTools, _ := provider.BuildTooling([]*providers.ToolDescriptor{&tools[0]})

	ctx := context.Background()

	// First call - should return tool calls
	resp1, toolCalls1, err := provider.PredictWithTools(ctx, req, geminiTools, "auto")
	if err != nil {
		t.Fatalf("First PredictWithTools failed: %v", err)
	}

	if len(toolCalls1) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(toolCalls1))
	}

	t.Logf("First response: %d tool calls", len(toolCalls1))

	// Add assistant message with tool calls to history
	messages = append(messages, types.Message{
		Role:      "assistant",
		Content:   resp1.Content,
		ToolCalls: toolCalls1,
	})

	// Add tool result messages
	resultContent := `{"status":"active","next_billing":"2025-11-01"}`
	messages = append(messages, types.Message{
		Role:    "tool",
		Content: resultContent,
		ToolResult: &types.MessageToolResult{
			ID:      toolCalls1[0].ID,
			Name:    toolCalls1[0].Name,
			Content: resultContent,
		},
	})

	// Second call - should include tool results
	req.Messages = messages
	resp2, toolCalls2, err := provider.PredictWithTools(ctx, req, geminiTools, "auto")
	if err != nil {
		t.Fatalf("Second PredictWithTools failed: %v", err)
	}

	if len(toolCalls2) != 0 {
		t.Errorf("Expected 0 tool calls in final response, got %d", len(toolCalls2))
	}

	if resp2.Content == "" {
		t.Error("Expected text response, got empty content")
	}

	t.Logf("✅ Tool results properly included in second request, no loop detected")
}
