package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestToolProvider_BuildRequestWithToolMessages tests that tool messages
// from conversation history are properly converted to Claude's tool_result format
func TestToolProvider_BuildRequestWithToolMessages(t *testing.T) {
	// Track what gets sent to Claude API
	var capturedRequest map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the request body
		if err := json.NewDecoder(r.Body).Decode(&capturedRequest); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		// Return a valid Claude response
		response := map[string]interface{}{
			"id":          "msg_test123",
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]interface{}{{"type": "text", "text": "Test response"}},
			"model":       "claude-3-5-haiku-20241022",
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 100, "output_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create provider
	provider := NewToolProvider(
		"test-haiku",
		"claude-3-5-haiku-20241022",
		server.URL,
		providers.ProviderDefaults{MaxTokens: 1024, Temperature: 0.7},
		false,
	)

	// Simulate a conversation with tool use and tool results:
	// 1. User message
	// 2. Assistant message with tool_use
	// 3. Tool result message
	// 4. New user message (current turn)
	messages := []types.Message{
		{Role: "user", Content: "What's the weather?"},
		{
			Role:    "assistant",
			Content: "Let me check the weather for you.",
			ToolCalls: []types.MessageToolCall{
				{ID: "toolu_123", Name: "get_weather", Args: json.RawMessage(`{"location":"SF"}`)},
			},
		},
		{
			Role:    "tool",
			Content: `{"temperature": 72, "condition": "sunny"}`,
			ToolResult: func() *types.MessageToolResult {
				r := types.NewTextToolResult("toolu_123", "get_weather", `{"temperature": 72, "condition": "sunny"}`)
				return &r
			}(),
		},
		{Role: "user", Content: "Thanks! Now check NYC."},
	}

	req := providers.PredictionRequest{
		Messages:    messages,
		MaxTokens:   1024,
		Temperature: 0.7,
	}

	// Build tool descriptors
	tools := []providers.ToolDescriptor{
		{
			Name:        "get_weather",
			Description: "Get weather for a location",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`),
		},
	}
	claudeTools, _ := provider.BuildTooling([]*providers.ToolDescriptor{&tools[0]})

	// Make the request
	ctx := context.Background()
	_, _, err := provider.PredictWithTools(ctx, req, claudeTools, "auto")
	if err != nil {
		t.Fatalf("PredictWithTools failed: %v", err)
	}

	// Verify the captured request has correct structure
	if capturedRequest == nil {
		t.Fatal("No request was captured")
	}

	// Extract messages from request
	requestMessages, ok := capturedRequest["messages"].([]interface{})
	if !ok {
		t.Fatalf("Expected messages to be array, got: %T", capturedRequest["messages"])
	}

	// Expected structure:
	// Message 0: user - "What's the weather?"
	// Message 1: assistant - text + tool_use
	// Message 2: user - tool_result + text "Thanks! Now check NYC."
	if len(requestMessages) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(requestMessages))
		for i, msg := range requestMessages {
			msgMap := msg.(map[string]interface{})
			t.Logf("Message %d: role=%s, content=%v", i, msgMap["role"], msgMap["content"])
		}
	}

	// Check message 2 (user message with tool_result)
	msg2 := requestMessages[2].(map[string]interface{})
	if msg2["role"] != "user" {
		t.Errorf("Message 2 should be 'user', got: %s", msg2["role"])
	}

	// Check content of message 2
	content2 := msg2["content"].([]interface{})
	if len(content2) != 2 {
		t.Errorf("Message 2 content should have 2 items (tool_result + text), got %d", len(content2))
		t.Logf("Content: %+v", content2)
		t.FailNow()
	}

	// First item should be tool_result
	item0 := content2[0].(map[string]interface{})
	if item0["type"] != "tool_result" {
		t.Errorf("First content item should be 'tool_result', got: %s", item0["type"])
	}
	if item0["tool_use_id"] != "toolu_123" {
		t.Errorf("Expected tool_use_id 'toolu_123', got: %s", item0["tool_use_id"])
	}

	// Second item should be text
	item1 := content2[1].(map[string]interface{})
	if item1["type"] != "text" {
		t.Errorf("Second content item should be 'text', got: %s", item1["type"])
	}
	if item1["text"] != "Thanks! Now check NYC." {
		t.Errorf("Expected text 'Thanks! Now check NYC.', got: %s", item1["text"])
	}

	t.Logf("✅ Tool results properly converted to Claude format")
	t.Logf("Request structure validated successfully")
}

// TestToolProvider_MultipleToolResultsGrouped tests that multiple consecutive
// tool messages are grouped together in a single user message
func TestToolProvider_MultipleToolResultsGrouped(t *testing.T) {
	var capturedRequest map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedRequest); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		response := map[string]interface{}{
			"id":          "msg_test123",
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]interface{}{{"type": "text", "text": "Test response"}},
			"model":       "claude-3-5-haiku-20241022",
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 100, "output_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	provider := NewToolProvider(
		"test-haiku",
		"claude-3-5-haiku-20241022",
		server.URL,
		providers.ProviderDefaults{MaxTokens: 1024, Temperature: 0.7},
		false,
	)

	// Simulate assistant making multiple tool calls, followed by multiple tool results
	messages := []types.Message{
		{Role: "user", Content: "Get weather for SF and NYC"},
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{ID: "toolu_sf", Name: "get_weather", Args: json.RawMessage(`{"location":"SF"}`)},
				{ID: "toolu_nyc", Name: "get_weather", Args: json.RawMessage(`{"location":"NYC"}`)},
			},
		},
		{
			Role:    "tool",
			Content: `{"temperature": 72, "condition": "sunny"}`,
			ToolResult: func() *types.MessageToolResult {
				r := types.NewTextToolResult("toolu_sf", "get_weather", `{"temperature": 72, "condition": "sunny"}`)
				return &r
			}(),
		},
		{
			Role:    "tool",
			Content: `{"temperature": 65, "condition": "cloudy"}`,
			ToolResult: func() *types.MessageToolResult {
				r := types.NewTextToolResult("toolu_nyc", "get_weather", `{"temperature": 65, "condition": "cloudy"}`)
				return &r
			}(),
		},
	}

	req := providers.PredictionRequest{
		Messages:    messages,
		MaxTokens:   1024,
		Temperature: 0.7,
	}

	tools := []providers.ToolDescriptor{
		{
			Name:        "get_weather",
			Description: "Get weather for a location",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`),
		},
	}
	claudeTools, _ := provider.BuildTooling([]*providers.ToolDescriptor{&tools[0]})

	ctx := context.Background()
	_, _, err := provider.PredictWithTools(ctx, req, claudeTools, "auto")
	if err != nil {
		t.Fatalf("PredictWithTools failed: %v", err)
	}

	requestMessages, ok := capturedRequest["messages"].([]interface{})
	if !ok {
		t.Fatalf("Expected messages to be array")
	}

	// Expected structure:
	// Message 0: user - "Get weather for SF and NYC"
	// Message 1: assistant - tool_use (SF) + tool_use (NYC)
	// Message 2: user - tool_result (SF) + tool_result (NYC)
	if len(requestMessages) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(requestMessages))
		for i, msg := range requestMessages {
			msgMap := msg.(map[string]interface{})
			t.Logf("Message %d: role=%s", i, msgMap["role"])
		}
		t.FailNow()
	}

	// Check message 2 has both tool results
	msg2 := requestMessages[2].(map[string]interface{})
	content2 := msg2["content"].([]interface{})

	if len(content2) != 2 {
		t.Errorf("Message 2 should have 2 tool_result items, got %d", len(content2))
		t.FailNow()
	}

	// Verify both are tool_results
	for i, item := range content2 {
		itemMap := item.(map[string]interface{})
		if itemMap["type"] != "tool_result" {
			t.Errorf("Content item %d should be 'tool_result', got: %s", i, itemMap["type"])
		}
	}

	t.Logf("✅ Multiple tool results properly grouped in single user message")
}

// TestProcessClaudeToolResult_UsesToolResultContent verifies that processClaudeToolResult
// uses ToolResult.Content (not msg.Content) for the tool result content.
// This is critical for streaming with tools to work correctly.
func TestProcessClaudeToolResult_UsesToolResultContent(t *testing.T) {
	// Create a tool result message as the SDK creates them:
	// - msg.Content is NOT set (empty)
	// - msg.ToolResult.Parts has the actual data
	toolResultMsg := types.Message{
		Role: "tool",
		// Content is intentionally empty - this is how the SDK creates tool result messages
		ToolResult: func() *types.MessageToolResult {
			r := types.NewTextToolResult("toolu_abc123", "weather", `{"temperature": 73, "conditions": "sunny"}`)
			return &r
		}(),
	}

	// Process the tool result
	result := processClaudeToolResult(toolResultMsg)

	// Verify the result uses ToolResult text content as a plain string
	contentStr, ok := result.Content.(string)
	if !ok {
		t.Fatalf("Expected string content for text-only tool result, got %T", result.Content)
	}
	if contentStr == "" {
		t.Fatal("Tool result content is empty - ToolResult.Parts should be used")
	}
	if contentStr != `{"temperature": 73, "conditions": "sunny"}` {
		t.Errorf("Expected ToolResult text content, got '%s'", contentStr)
	}
	if result.ToolUseID != "toolu_abc123" {
		t.Errorf("Expected ToolUseID 'toolu_abc123', got '%s'", result.ToolUseID)
	}
}

// TestProcessClaudeToolResult_MultimodalImageResult verifies that tool results
// containing images are serialized as Claude content block arrays.
func TestProcessClaudeToolResult_MultimodalImageResult(t *testing.T) {
	imgData := "iVBORw0KGgoAAAANSUhEUg=="
	textContent := "Chart generated successfully"
	msg := types.Message{
		Role: "tool",
		ToolResult: &types.MessageToolResult{
			ID:   "toolu_img_123",
			Name: "generate_chart",
			Parts: []types.ContentPart{
				types.NewTextPart(textContent),
				types.NewImagePartFromData(imgData, types.MIMETypeImagePNG, nil),
			},
		},
	}

	result := processClaudeToolResult(msg)

	if result.ToolUseID != "toolu_img_123" {
		t.Errorf("Expected ToolUseID 'toolu_img_123', got '%s'", result.ToolUseID)
	}

	// Content should be an array since we have media
	blocks, ok := result.Content.([]interface{})
	if !ok {
		t.Fatalf("Expected []interface{} content for multimodal tool result, got %T", result.Content)
	}
	if len(blocks) != 2 {
		t.Fatalf("Expected 2 content blocks, got %d", len(blocks))
	}

	// Verify by marshaling to JSON and inspecting
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Failed to marshal result: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	content, ok := parsed["content"].([]interface{})
	if !ok {
		t.Fatalf("Expected content to be array in JSON, got %T", parsed["content"])
	}

	// First block: text
	textBlock := content[0].(map[string]interface{})
	if textBlock["type"] != "text" {
		t.Errorf("Expected first block type 'text', got '%s'", textBlock["type"])
	}
	if textBlock["text"] != textContent {
		t.Errorf("Expected text '%s', got '%s'", textContent, textBlock["text"])
	}

	// Second block: image
	imgBlock := content[1].(map[string]interface{})
	if imgBlock["type"] != "image" {
		t.Errorf("Expected second block type 'image', got '%s'", imgBlock["type"])
	}
	source := imgBlock["source"].(map[string]interface{})
	if source["type"] != "base64" {
		t.Errorf("Expected source type 'base64', got '%s'", source["type"])
	}
	if source["media_type"] != types.MIMETypeImagePNG {
		t.Errorf("Expected media_type '%s', got '%s'", types.MIMETypeImagePNG, source["media_type"])
	}
	if source["data"] != imgData {
		t.Errorf("Expected base64 data to match")
	}
}

// TestProcessClaudeToolResult_DocumentResult verifies that tool results
// containing PDF documents serialize correctly.
func TestProcessClaudeToolResult_DocumentResult(t *testing.T) {
	pdfData := "JVBERi0xLjQK"
	msg := types.Message{
		Role: "tool",
		ToolResult: &types.MessageToolResult{
			ID:   "toolu_doc_456",
			Name: "generate_report",
			Parts: []types.ContentPart{
				types.NewTextPart("Report generated"),
				types.NewDocumentPartFromData(pdfData, types.MIMETypePDF),
			},
		},
	}

	result := processClaudeToolResult(msg)

	blocks, ok := result.Content.([]interface{})
	if !ok {
		t.Fatalf("Expected []interface{} content for document tool result, got %T", result.Content)
	}
	if len(blocks) != 2 {
		t.Fatalf("Expected 2 content blocks, got %d", len(blocks))
	}

	// Marshal and verify document block structure
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Failed to marshal result: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	content := parsed["content"].([]interface{})
	docBlock := content[1].(map[string]interface{})
	if docBlock["type"] != "document" {
		t.Errorf("Expected block type 'document', got '%s'", docBlock["type"])
	}
	source := docBlock["source"].(map[string]interface{})
	if source["type"] != "base64" {
		t.Errorf("Expected source type 'base64', got '%s'", source["type"])
	}
	if source["media_type"] != types.MIMETypePDF {
		t.Errorf("Expected media_type '%s', got '%s'", types.MIMETypePDF, source["media_type"])
	}
	if source["data"] != pdfData {
		t.Errorf("Expected base64 data to match")
	}
}

// TestProcessClaudeToolResult_TextOnlyRegression verifies that text-only tool results
// still serialize as a plain string (not wrapped in an array).
func TestProcessClaudeToolResult_TextOnlyRegression(t *testing.T) {
	msg := types.Message{
		Role: "tool",
		ToolResult: func() *types.MessageToolResult {
			r := types.NewTextToolResult("toolu_text_789", "lookup", "found result: 42")
			return &r
		}(),
	}

	result := processClaudeToolResult(msg)

	// Should be a plain string, not an array
	contentStr, ok := result.Content.(string)
	if !ok {
		t.Fatalf("Expected string content for text-only result, got %T", result.Content)
	}
	if contentStr != "found result: 42" {
		t.Errorf("Expected 'found result: 42', got '%s'", contentStr)
	}

	// Verify JSON serialization produces a string, not an array
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// content should be a string in JSON
	if _, ok := parsed["content"].(string); !ok {
		t.Errorf("Expected content to be string in JSON, got %T: %v", parsed["content"], parsed["content"])
	}
}

// TestProcessClaudeToolResult_ImageWithURL verifies image parts with URL source.
func TestProcessClaudeToolResult_ImageWithURL(t *testing.T) {
	msg := types.Message{
		Role: "tool",
		ToolResult: &types.MessageToolResult{
			ID:   "toolu_url_123",
			Name: "fetch_image",
			Parts: []types.ContentPart{
				types.NewTextPart("Image fetched"),
				types.NewImagePartFromURL("https://example.com/chart.png", nil),
			},
		},
	}

	result := processClaudeToolResult(msg)

	blocks, ok := result.Content.([]interface{})
	if !ok {
		t.Fatalf("Expected []interface{} content, got %T", result.Content)
	}
	if len(blocks) != 2 {
		t.Fatalf("Expected 2 blocks, got %d", len(blocks))
	}

	// Verify URL-based image block via JSON
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	content := parsed["content"].([]interface{})
	imgBlock := content[1].(map[string]interface{})
	source := imgBlock["source"].(map[string]interface{})
	if source["type"] != "url" {
		t.Errorf("Expected source type 'url', got '%s'", source["type"])
	}
	if source["url"] != "https://example.com/chart.png" {
		t.Errorf("Expected URL 'https://example.com/chart.png', got '%s'", source["url"])
	}
}
