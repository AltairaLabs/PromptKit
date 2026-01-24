package gemini

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ============================================================================
// NewToolProviderWithCredential Tests
// ============================================================================

func TestNewToolProviderWithCredential(t *testing.T) {
	defaults := providers.ProviderDefaults{
		Temperature: 0.7,
		MaxTokens:   1000,
	}

	t.Run("with credential", func(t *testing.T) {
		cred := &mockAPIKeyCredential{apiKey: "test-key"}
		provider := NewToolProviderWithCredential("test-gemini", "gemini-pro", "https://api.example.com", defaults, false, cred)

		if provider == nil {
			t.Fatal("Expected non-nil provider")
		}

		if provider.Provider == nil {
			t.Fatal("Expected non-nil GeminiProvider")
		}

		if provider.ID() != "test-gemini" {
			t.Errorf("Expected id 'test-gemini', got '%s'", provider.ID())
		}
	})

	t.Run("with nil credential", func(t *testing.T) {
		provider := NewToolProviderWithCredential("test-gemini", "gemini-pro", "https://api.example.com", defaults, false, nil)

		if provider == nil {
			t.Fatal("Expected non-nil provider")
		}
	})
}

func TestGeminiToolResponseParsing(t *testing.T) {
	// This is the actual response from Gemini that contains a function call
	geminiResponseJSON := `{
  "candidates": [
    {
      "content": {
        "parts": [
          {
            "functionCall": {
              "name": "getTodayStep",
              "args": {
                "project_id": "finish first draft"
              }
            },
            "thoughtSignature": "CpQCAdHtim//eFEosgdaIKSUFynLIA1Y3O+5yKnnzRmeUlMvFlCAB7lBGHVf8/7rO4/emJfKNevf7K6cRaeWu6Aa10jLOs7gNe7gWp/MgBQ586iJwBUduWQAst4er9SweS128cwOzJ2Z/CtlMuCJBvGFtVuVM1ZRsEyeCV87+HzlJAIFDl2P+XcztKMpgkhQ4OR6/eDt/h3nCqUfCclkztpy3MufXNPCFrNHpexPRKi4MskJDtg+XtjToKYBkicDu+3aeAQ/VP3t2IbK+Y9o+L/k9w16kIcP1xrAqJAqC38Gc+xR/qDSE5Qpg8BP3CEdKgeN9fgjh86mf0p2AWD2XId8CNbFlwytpEVxIMtmESjGCuZxieNy"
          }
        ],
        "role": "model"
      },
      "finishReason": "STOP",
      "index": 0,
      "finishMessage": "Model generated function call(s)."
    }
  ],
  "usageMetadata": {
    "promptTokenCount": 181,
    "candidatesTokenCount": 19,
    "totalTokenCount": 259,
    "promptTokensDetails": [
      {
        "modality": "TEXT",
        "tokenCount": 181
      }
    ],
    "thoughtsTokenCount": 59
  },
  "modelVersion": "gemini-2.5-flash",
  "responseId": "5frnaPn5DeqFkdUPiOqu6AQ"
}`

	// Create a Gemini tool provider
	geminiProvider := NewProvider(
		"test-gemini",
		"gemini-2.5-flash",
		"",
		providers.ProviderDefaults{},
		false,
	)
	provider := &ToolProvider{
		Provider: geminiProvider,
	}

	// Parse the response
	predictResp, toolCalls, err := provider.parseToolResponse([]byte(geminiResponseJSON), providers.PredictionResponse{})
	if err != nil {
		t.Fatalf("Failed to parse Gemini response: %v", err)
	}

	// Verify that tool calls were extracted
	if len(toolCalls) == 0 {
		t.Errorf("Expected tool calls to be extracted, got 0")

		// Debug: Print what we got
		t.Logf("Predict response: %+v", predictResp)
		t.Logf("Tool calls: %+v", toolCalls)

		// Let's manually debug the parsing
		var resp geminiResponse
		if err := json.Unmarshal([]byte(geminiResponseJSON), &resp); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		t.Logf("Parsed response: %+v", resp)
		t.Logf("Number of candidates: %d", len(resp.Candidates))

		if len(resp.Candidates) > 0 {
			t.Logf("First candidate: %+v", resp.Candidates[0])
			t.Logf("Content parts: %+v", resp.Candidates[0].Content.Parts)

			for i, part := range resp.Candidates[0].Content.Parts {
				t.Logf("Part %d: %+v", i, part)

				// Try to manually extract function call
				partBytes, _ := json.Marshal(part)
				var rawPart map[string]interface{}
				if json.Unmarshal(partBytes, &rawPart) == nil {
					t.Logf("Raw part %d: %+v", i, rawPart)

					if funcCall, ok := rawPart["functionCall"].(map[string]interface{}); ok {
						t.Logf("Found functionCall: %+v", funcCall)
					} else {
						t.Logf("No functionCall found in raw part")
					}
				}
			}
		}

		return
	}

	// Verify the tool call details
	if len(toolCalls) != 1 {
		t.Errorf("Expected 1 tool call, got %d", len(toolCalls))
	}

	toolCall := toolCalls[0]
	if toolCall.Name != "getTodayStep" {
		t.Errorf("Expected tool name 'getTodayStep', got '%s'", toolCall.Name)
	}

	// Verify the arguments
	var args map[string]interface{}
	if err := json.Unmarshal(toolCall.Args, &args); err != nil {
		t.Fatalf("Failed to unmarshal tool args: %v", err)
	}

	if projectID, ok := args["project_id"].(string); !ok || projectID != "finish first draft" {
		t.Errorf("Expected project_id 'finish first draft', got %v", args["project_id"])
	}

	// Verify token counts
	if predictResp.CostInfo.InputTokens != 181 {
		t.Errorf("Expected 181 input tokens, got %d", predictResp.CostInfo.InputTokens)
	}

	if predictResp.CostInfo.OutputTokens != 19 {
		t.Errorf("Expected 19 output tokens, got %d", predictResp.CostInfo.OutputTokens)
	}

	t.Logf("SUCCESS: Tool call extracted correctly: %+v", toolCall)
}

// ============================================================================
// BuildToolRequest Defaults Tests
// ============================================================================

func TestGeminiToolProvider_BuildToolRequest_AppliesDefaults(t *testing.T) {
	tests := []struct {
		name              string
		reqTemp           float32
		reqTopP           float32
		reqMaxTokens      int
		defaultTemp       float32
		defaultTopP       float32
		defaultMaxTokens  int
		expectedTemp      float32
		expectedTopP      float32
		expectedMaxTokens int
	}{
		{
			name:              "Uses request values when provided",
			reqTemp:           0.8,
			reqTopP:           0.95,
			reqMaxTokens:      500,
			defaultTemp:       0.7,
			defaultTopP:       0.9,
			defaultMaxTokens:  1000,
			expectedTemp:      0.8,
			expectedTopP:      0.95,
			expectedMaxTokens: 500,
		},
		{
			name:              "Falls back to defaults for zero values",
			reqTemp:           0,
			reqTopP:           0,
			reqMaxTokens:      0,
			defaultTemp:       0.7,
			defaultTopP:       0.9,
			defaultMaxTokens:  2000,
			expectedTemp:      0.7,
			expectedTopP:      0.9,
			expectedMaxTokens: 2000,
		},
		{
			name:              "Mixed values - some request, some defaults",
			reqTemp:           0.6,
			reqTopP:           0,
			reqMaxTokens:      1500,
			defaultTemp:       0.5,
			defaultTopP:       0.92,
			defaultMaxTokens:  1000,
			expectedTemp:      0.6,
			expectedTopP:      0.92,
			expectedMaxTokens: 1500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewToolProvider(
				"test",
				"gemini-2.0-flash",
				"",
				providers.ProviderDefaults{
					Temperature: tt.defaultTemp,
					TopP:        tt.defaultTopP,
					MaxTokens:   tt.defaultMaxTokens,
				},
				false,
			)

			req := providers.PredictionRequest{
				Temperature: tt.reqTemp,
				TopP:        tt.reqTopP,
				MaxTokens:   tt.reqMaxTokens,
				Messages:    []types.Message{{Role: "user", Content: "Hello"}},
			}

			request := provider.buildToolRequest(req, nil, "")

			genConfig, ok := request["generationConfig"].(map[string]interface{})
			if !ok {
				t.Fatal("Expected generationConfig in request")
			}

			if temp, ok := genConfig["temperature"].(float32); !ok || temp != tt.expectedTemp {
				t.Errorf("Expected temperature %.2f, got %v", tt.expectedTemp, genConfig["temperature"])
			}

			if topP, ok := genConfig["topP"].(float32); !ok || topP != tt.expectedTopP {
				t.Errorf("Expected topP %.2f, got %v", tt.expectedTopP, genConfig["topP"])
			}

			if maxTokens, ok := genConfig["maxOutputTokens"].(int); !ok || maxTokens != tt.expectedMaxTokens {
				t.Errorf("Expected maxOutputTokens %d, got %v", tt.expectedMaxTokens, genConfig["maxOutputTokens"])
			}
		})
	}
}

func TestGeminiProvider_ApplyRequestDefaults(t *testing.T) {
	tests := []struct {
		name              string
		req               providers.PredictionRequest
		defaults          providers.ProviderDefaults
		expectedTemp      float32
		expectedTopP      float32
		expectedMaxTokens int
	}{
		{
			name: "Uses request values when provided",
			req: providers.PredictionRequest{
				Temperature: 0.8,
				TopP:        0.95,
				MaxTokens:   500,
			},
			defaults: providers.ProviderDefaults{
				Temperature: 0.7,
				TopP:        0.9,
				MaxTokens:   1000,
			},
			expectedTemp:      0.8,
			expectedTopP:      0.95,
			expectedMaxTokens: 500,
		},
		{
			name: "Falls back to defaults for zero values",
			req: providers.PredictionRequest{
				Temperature: 0,
				TopP:        0,
				MaxTokens:   0,
			},
			defaults: providers.ProviderDefaults{
				Temperature: 0.7,
				TopP:        0.9,
				MaxTokens:   2000,
			},
			expectedTemp:      0.7,
			expectedTopP:      0.9,
			expectedMaxTokens: 2000,
		},
		{
			name: "Mixed values - some request, some defaults",
			req: providers.PredictionRequest{
				Temperature: 0.6,
				TopP:        0,
				MaxTokens:   1500,
			},
			defaults: providers.ProviderDefaults{
				Temperature: 0.5,
				TopP:        0.92,
				MaxTokens:   1000,
			},
			expectedTemp:      0.6,
			expectedTopP:      0.92,
			expectedMaxTokens: 1500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewProvider("test", "gemini-2.0-flash", "", tt.defaults, false)

			temp, topP, maxTokens := provider.applyRequestDefaults(tt.req)

			if temp != tt.expectedTemp {
				t.Errorf("Expected temperature %.2f, got %.2f", tt.expectedTemp, temp)
			}
			if topP != tt.expectedTopP {
				t.Errorf("Expected topP %.2f, got %.2f", tt.expectedTopP, topP)
			}
			if maxTokens != tt.expectedMaxTokens {
				t.Errorf("Expected maxTokens %d, got %d", tt.expectedMaxTokens, maxTokens)
			}
		})
	}
}

// ============================================================================
// PredictStreamWithTools Tests
// ============================================================================

func TestGeminiToolProvider_PredictStreamWithTools_ImplementsToolSupport(t *testing.T) {
	geminiProvider := NewProvider("test", "gemini-2.0-flash", "", providers.ProviderDefaults{}, false)
	provider := &ToolProvider{Provider: geminiProvider}

	// Verify it implements ToolSupport interface which includes PredictStreamWithTools
	var toolSupport providers.ToolSupport = provider

	// If this compiles, the interface is implemented correctly
	_ = toolSupport.BuildTooling
	_ = toolSupport.PredictWithTools
}

func TestGeminiToolProvider_PredictStreamWithTools_BuildsRequestWithTools(t *testing.T) {
	geminiProvider := NewProvider("test", "gemini-2.0-flash", "", providers.ProviderDefaults{}, false)
	provider := &ToolProvider{Provider: geminiProvider}

	// Verify the provider implements the interface with PredictStreamWithTools
	var _ providers.ToolSupport = provider

	// Build tools
	schema := json.RawMessage(`{"type": "object", "properties": {"query": {"type": "string"}}}`)
	descriptors := []*providers.ToolDescriptor{
		{
			Name:        "search",
			Description: "Search for information",
			InputSchema: schema,
		},
	}

	tools, err := provider.BuildTooling(descriptors)
	if err != nil {
		t.Fatalf("Failed to build tooling: %v", err)
	}

	if tools == nil {
		t.Fatal("Expected non-nil tools")
	}

	// Verify tools are properly formatted for Gemini
	geminiToolDecl, ok := tools.(geminiToolDeclaration)
	if !ok {
		t.Fatalf("Expected geminiToolDeclaration, got %T", tools)
	}

	if len(geminiToolDecl.FunctionDeclarations) != 1 {
		t.Fatalf("Expected 1 function declaration, got %d", len(geminiToolDecl.FunctionDeclarations))
	}

	if geminiToolDecl.FunctionDeclarations[0].Name != "search" {
		t.Errorf("Expected function name 'search', got '%s'", geminiToolDecl.FunctionDeclarations[0].Name)
	}
}

func TestGeminiToolProvider_PredictStreamWithTools_TextResponse(t *testing.T) {
	// Gemini returns a JSON array (not SSE format)
	jsonResponse := `[{"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"}}]},{"candidates":[{"content":{"parts":[{"text":" Gemini"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5}}]`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request contains tools
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		_ = json.Unmarshal(body, &req)

		if _, hasTools := req["tools"]; !hasTools {
			t.Error("Expected tools in request")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(jsonResponse))
	}))
	defer server.Close()

	geminiProvider := NewProvider("test", "gemini-2.0-flash", server.URL, providers.ProviderDefaults{}, false)
	provider := &ToolProvider{Provider: geminiProvider}

	schema := json.RawMessage(`{"type": "object", "properties": {"q": {"type": "string"}}}`)
	tools, _ := provider.BuildTooling([]*providers.ToolDescriptor{
		{Name: "search", Description: "Search", InputSchema: schema},
	})

	ctx := context.Background()
	stream, err := provider.PredictStreamWithTools(ctx, providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "test"}},
	}, tools, "auto")

	if err != nil {
		t.Fatalf("PredictStreamWithTools failed: %v", err)
	}

	var chunks []providers.StreamChunk
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("Stream error: %v", chunk.Error)
		}
		chunks = append(chunks, chunk)
	}

	if len(chunks) == 0 {
		t.Fatal("Expected chunks")
	}

	final := chunks[len(chunks)-1]
	if final.Content != "Hello Gemini" {
		t.Errorf("Final content: got %q, want %q", final.Content, "Hello Gemini")
	}
}

func TestGeminiToolProvider_PredictStreamWithTools_ToolCallResponse(t *testing.T) {
	// Gemini returns a JSON array with function call - test that the request is made correctly
	// The streaming parser may not extract tool calls; we're testing the HTTP flow here
	jsonResponse := `[{"candidates":[{"content":{"parts":[{"functionCall":{"name":"search","args":{"q":"test"}}}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":15,"candidatesTokenCount":10}}]`

	requestReceived := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived = true
		// Verify request contains tools
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		_ = json.Unmarshal(body, &req)

		if _, hasTools := req["tools"]; !hasTools {
			t.Error("Expected tools in request")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(jsonResponse))
	}))
	defer server.Close()

	geminiProvider := NewProvider("test", "gemini-2.0-flash", server.URL, providers.ProviderDefaults{}, false)
	provider := &ToolProvider{Provider: geminiProvider}

	schema := json.RawMessage(`{"type": "object", "properties": {"q": {"type": "string"}}}`)
	tools, _ := provider.BuildTooling([]*providers.ToolDescriptor{
		{Name: "search", Description: "Search", InputSchema: schema},
	})

	ctx := context.Background()
	stream, err := provider.PredictStreamWithTools(ctx, providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "search for test"}},
	}, tools, "auto")

	if err != nil {
		t.Fatalf("PredictStreamWithTools failed: %v", err)
	}

	// Drain the stream
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("Stream error: %v", chunk.Error)
		}
	}

	if !requestReceived {
		t.Fatal("Expected request to be made to server")
	}
}

func TestGeminiToolProvider_PredictStreamWithTools_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal error"}`))
	}))
	defer server.Close()

	geminiProvider := NewProvider("test", "gemini-2.0-flash", server.URL, providers.ProviderDefaults{}, false)
	provider := &ToolProvider{Provider: geminiProvider}

	ctx := context.Background()
	_, err := provider.PredictStreamWithTools(ctx, providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "test"}},
	}, nil, "auto")

	if err == nil {
		t.Fatal("Expected error for HTTP 500")
	}
}

// TestGeminiToolProvider_PredictStreamWithTools_URLFormat verifies the streaming URL format.
// IMPORTANT: The URL must NOT contain "alt=sse" because:
// - With alt=sse: Google returns SSE format (text lines prefixed with "data:")
// - Without alt=sse: Google returns JSON array format
// The streamResponse function parses as JSON array, so alt=sse breaks parsing.
// Duplex streaming uses WebSockets (not HTTP), so alt=sse is irrelevant there.
func TestGeminiToolProvider_PredictStreamWithTools_URLFormat(t *testing.T) {
	var capturedURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		// Return valid JSON array response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"candidates":[{"content":{"parts":[{"text":"test"}],"role":"model"},"finishReason":"STOP"}]}]`))
	}))
	defer server.Close()

	geminiProvider := NewProvider("test", "gemini-2.0-flash", server.URL, providers.ProviderDefaults{}, false)
	provider := &ToolProvider{Provider: geminiProvider}

	ctx := context.Background()
	stream, err := provider.PredictStreamWithTools(ctx, providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "test"}},
	}, nil, "auto")
	if err != nil {
		t.Fatalf("PredictStreamWithTools failed: %v", err)
	}

	// Drain the stream
	for range stream {
	}

	// Verify URL does NOT contain alt=sse
	if strings.Contains(capturedURL, "alt=sse") {
		t.Errorf("Streaming URL should NOT contain 'alt=sse' (causes SSE format instead of JSON array).\n"+
			"Got URL: %s", capturedURL)
	}

	// Verify it's using streamGenerateContent endpoint
	if !strings.Contains(capturedURL, "streamGenerateContent") {
		t.Errorf("Expected streamGenerateContent in URL, got: %s", capturedURL)
	}
}
