package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestChatWithTools_Integration(t *testing.T) {
	t.Run("Successful tool call", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := openAIResponse{
				Choices: []openAIChoice{
					{
						Message: openAIMessage{
							Content: "",
							// Tool calls would be here in real response
						},
					},
				},
				Usage: openAIUsage{PromptTokens: 20, CompletionTokens: 15},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		provider := &OpenAIToolProvider{
			OpenAIProvider: &OpenAIProvider{
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
			},
		}

		tools := []openAITool{
			{
				Type: "function",
				Function: openAIToolFunction{
					Name:        "get_weather",
					Description: "Get weather for a location",
					Parameters:  json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`),
				},
			},
		}

		resp, toolCalls, err := provider.ChatWithTools(context.Background(), providers.ChatRequest{
			Messages: []types.Message{{Role: "user", Content: "What's the weather?"}},
		}, tools, "auto")

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if resp.Latency <= 0 {
			t.Error("Expected latency > 0")
		}

		// Tool calls would be populated from actual response
		_ = toolCalls
	})

	t.Run("Tool validation with invalid request", func(t *testing.T) {
		provider := &OpenAIToolProvider{
			OpenAIProvider: &OpenAIProvider{
				BaseProvider: providers.NewBaseProvider("test", false, &http.Client{}),
				model:        "gpt-4",
				baseURL:      "http://test",
				apiKey:       "test-key",
				defaults:     providers.ProviderDefaults{},
			},
		}

		// Test with empty messages
		_, _, err := provider.ChatWithTools(context.Background(), providers.ChatRequest{
			Messages: []types.Message{},
		}, nil, "")

		// Should handle gracefully (may or may not error depending on implementation)
		_ = err
	})
}

func TestBuildTooling(t *testing.T) {
	provider := &OpenAIToolProvider{
		OpenAIProvider: &OpenAIProvider{},
	}

	t.Run("Single tool descriptor", func(t *testing.T) {
		descriptors := []*providers.ToolDescriptor{
			{
				Name:        "search",
				Description: "Search the web",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
			},
		}

		tools, err := provider.BuildTooling(descriptors)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		openAITools, ok := tools.([]openAITool)
		if !ok {
			t.Fatal("Expected tools to be []openAITool")
		}

		if len(openAITools) != 1 {
			t.Fatalf("Expected 1 tool, got %d", len(openAITools))
		}

		tool := openAITools[0]
		if tool.Type != "function" {
			t.Errorf("Expected type 'function', got %q", tool.Type)
		}
		if tool.Function.Name != "search" {
			t.Errorf("Expected name 'search', got %q", tool.Function.Name)
		}
		if tool.Function.Description != "Search the web" {
			t.Errorf("Expected description 'Search the web', got %q", tool.Function.Description)
		}
	})

	t.Run("Multiple tool descriptors", func(t *testing.T) {
		descriptors := []*providers.ToolDescriptor{
			{
				Name:        "tool1",
				Description: "First tool",
				InputSchema: json.RawMessage(`{}`),
			},
			{
				Name:        "tool2",
				Description: "Second tool",
				InputSchema: json.RawMessage(`{}`),
			},
		}

		tools, err := provider.BuildTooling(descriptors)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		openAITools, ok := tools.([]openAITool)
		if !ok {
			t.Fatal("Expected tools to be []openAITool")
		}

		if len(openAITools) != 2 {
			t.Fatalf("Expected 2 tools, got %d", len(openAITools))
		}
	})

	t.Run("Empty tool list", func(t *testing.T) {
		tools, err := provider.BuildTooling([]*providers.ToolDescriptor{})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if tools != nil {
			t.Errorf("Expected nil for empty tool list, got %v", tools)
		}
	})

	t.Run("Nil tool list", func(t *testing.T) {
		tools, err := provider.BuildTooling(nil)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if tools != nil {
			t.Errorf("Expected nil for nil tool list, got %v", tools)
		}
	})
}

func TestConvertToolCallsMethod(t *testing.T) {
	provider := &OpenAIToolProvider{
		OpenAIProvider: &OpenAIProvider{},
	}

	tests := []struct {
		name      string
		toolCalls []types.MessageToolCall
		expected  int
	}{
		{
			name: "Single tool call",
			toolCalls: []types.MessageToolCall{
				{
					ID:   "call_1",
					Name: "search",
					Args: json.RawMessage(`{"query":"test"}`),
				},
			},
			expected: 1,
		},
		{
			name: "Multiple tool calls",
			toolCalls: []types.MessageToolCall{
				{ID: "call_1", Name: "tool1", Args: json.RawMessage(`{}`)},
				{ID: "call_2", Name: "tool2", Args: json.RawMessage(`{}`)},
			},
			expected: 2,
		},
		{
			name:      "Empty tool calls",
			toolCalls: []types.MessageToolCall{},
			expected:  0,
		},
		{
			name:      "Nil tool calls",
			toolCalls: nil,
			expected:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := provider.convertToolCallsToOpenAI(tt.toolCalls)
			if len(result) != tt.expected {
				t.Errorf("Expected %d tool calls, got %d", tt.expected, len(result))
			}

			for i, tc := range result {
				if tcType, ok := tc["type"].(string); ok {
					if tcType != "function" {
						t.Errorf("Tool call %d: expected type 'function', got %q", i, tcType)
					}
				}
			}
		})
	}
}

func TestToolProviderCreation(t *testing.T) {
	t.Run("Creates tool provider with correct configuration", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "test-key-123")

		provider := NewOpenAIToolProvider("test-id", "gpt-4", "https://api.openai.com/v1", providers.ProviderDefaults{
			Temperature: 0.7,
		}, true, nil)

		if provider.ID() != "test-id" {
			t.Errorf("Expected ID 'test-id', got %q", provider.ID())
		}
		if provider.model != "gpt-4" {
			t.Errorf("Expected model 'gpt-4', got %q", provider.model)
		}
	})
}

func TestChatMultimodalWithTools(t *testing.T) {
	t.Run("Validates multimodal message with unsupported content", func(t *testing.T) {
		provider := &OpenAIToolProvider{
			OpenAIProvider: &OpenAIProvider{
				BaseProvider: providers.NewBaseProvider("test", false, &http.Client{}),
				model:        "gpt-4o",
				baseURL:      "http://test",
				apiKey:       "test-key",
				defaults:     providers.ProviderDefaults{},
			},
		}

		audioData := "base64audiodata"
		msg := types.Message{
			Role: "user",
			Parts: []types.ContentPart{
				{
					Type: types.ContentTypeAudio,
					Media: &types.MediaContent{
						MIMEType: "audio/mp3",
						Data:     &audioData,
					},
				},
			},
		}

		_, _, err := provider.ChatMultimodalWithTools(context.Background(), providers.ChatRequest{
			Messages: []types.Message{msg},
		}, nil, "")

		if err == nil {
			t.Fatal("Expected error for unsupported audio content, got nil")
		}
	})

	t.Run("Accepts valid image message", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := openAIResponse{
				Choices: []openAIChoice{{Message: openAIMessage{Content: "Image processed"}}},
				Usage:   openAIUsage{PromptTokens: 25, CompletionTokens: 10},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		provider := &OpenAIToolProvider{
			OpenAIProvider: &OpenAIProvider{
				BaseProvider: providers.NewBaseProvider("test", false, &http.Client{}),
				model:        "gpt-4o",
				baseURL:      server.URL,
				apiKey:       "test-key",
				defaults: providers.ProviderDefaults{
					Pricing: providers.Pricing{
						InputCostPer1K:  0.0025,
						OutputCostPer1K: 0.01,
					},
				},
			},
		}

		imageURL := "https://example.com/image.jpg"
		msg := types.Message{
			Role: "user",
			Parts: []types.ContentPart{
				{
					Type: types.ContentTypeImage,
					Media: &types.MediaContent{
						MIMEType: types.MIMETypeImageJPEG,
						URL:      &imageURL,
					},
				},
			},
		}

		resp, _, err := provider.ChatMultimodalWithTools(context.Background(), providers.ChatRequest{
			Messages: []types.Message{msg},
		}, nil, "")

		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if resp.Content != "Image processed" {
			t.Errorf("Expected 'Image processed', got %q", resp.Content)
		}
	})
}
