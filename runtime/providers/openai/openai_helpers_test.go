package openai

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestPrepareOpenAIMessages(t *testing.T) {
	provider := NewOpenAIProvider("test", "gpt-4o", "https://api.openai.com/v1", providers.ProviderDefaults{}, false)

	tests := []struct {
		name           string
		req            providers.ChatRequest
		expectedCount  int
		checkFirstRole string
		checkSystem    bool
	}{
		{
			name: "Messages with system prompt",
			req: providers.ChatRequest{
				System: "You are a helpful assistant",
				Messages: []types.Message{
					{Role: "user", Content: "Hello"},
				},
			},
			expectedCount:  2,
			checkFirstRole: "system",
			checkSystem:    true,
		},
		{
			name: "Messages without system prompt",
			req: providers.ChatRequest{
				System: "",
				Messages: []types.Message{
					{Role: "user", Content: "Hello"},
					{Role: "assistant", Content: "Hi there"},
				},
			},
			expectedCount:  2,
			checkFirstRole: "user",
			checkSystem:    false,
		},
		{
			name: "Empty messages with system",
			req: providers.ChatRequest{
				System:   "Test system",
				Messages: []types.Message{},
			},
			expectedCount: 1,
			checkSystem:   true,
		},
		{
			name: "Multiple messages preserve order",
			req: providers.ChatRequest{
				Messages: []types.Message{
					{Role: "user", Content: "First"},
					{Role: "assistant", Content: "Second"},
					{Role: "user", Content: "Third"},
				},
			},
			expectedCount:  3,
			checkFirstRole: "user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages, err := provider.prepareOpenAIMessages(tt.req)
			if err != nil {
				t.Fatalf("Failed to prepare messages: %v", err)
			}

			if len(messages) != tt.expectedCount {
				t.Errorf("Expected %d messages, got %d", tt.expectedCount, len(messages))
			}

			if tt.checkFirstRole != "" && len(messages) > 0 {
				if messages[0].Role != tt.checkFirstRole {
					t.Errorf("Expected first role %q, got %q", tt.checkFirstRole, messages[0].Role)
				}
			}

			if tt.checkSystem {
				found := false
				for _, msg := range messages {
					if msg.Role == "system" {
						found = true
						if msg.Content != tt.req.System {
							t.Errorf("Expected system content %q, got %q", tt.req.System, msg.Content)
						}
						break
					}
				}
				if !found {
					t.Error("Expected to find system message but didn't")
				}
			}

			// Verify content preservation
			systemOffset := 0
			if tt.req.System != "" {
				systemOffset = 1
			}
			for i, originalMsg := range tt.req.Messages {
				msgIdx := i + systemOffset
				if msgIdx < len(messages) {
					if messages[msgIdx].Role != originalMsg.Role {
						t.Errorf("Message %d: expected role %q, got %q", i, originalMsg.Role, messages[msgIdx].Role)
					}
					if messages[msgIdx].Content != originalMsg.Content {
						t.Errorf("Message %d: expected content %q, got %q", i, originalMsg.Content, messages[msgIdx].Content)
					}
				}
			}
		})
	}
}

func TestApplyRequestDefaults(t *testing.T) {
	tests := []struct {
		name              string
		req               providers.ChatRequest
		defaults          providers.ProviderDefaults
		expectedTemp      float32
		expectedTopP      float32
		expectedMaxTokens int
	}{
		{
			name: "Uses request values when provided",
			req: providers.ChatRequest{
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
			req: providers.ChatRequest{
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
			req: providers.ChatRequest{
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
		{
			name: "All defaults when request is empty",
			req:  providers.ChatRequest{},
			defaults: providers.ProviderDefaults{
				Temperature: 0.75,
				TopP:        0.88,
				MaxTokens:   3000,
			},
			expectedTemp:      0.75,
			expectedTopP:      0.88,
			expectedMaxTokens: 3000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &OpenAIProvider{
				defaults: tt.defaults,
			}

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

func TestProcessToolCallDeltas(t *testing.T) {
	tests := []struct {
		name             string
		initialToolCalls []types.MessageToolCall
		deltas           []struct {
			Index    int    `json:"index"`
			ID       string `json:"id,omitempty"`
			Type     string `json:"type,omitempty"`
			Function struct {
				Name      string `json:"name,omitempty"`
				Arguments string `json:"arguments,omitempty"`
			} `json:"function,omitempty"`
		}
		expectedCount int
		checkID       string
		checkName     string
		checkArgs     string
	}{
		{
			name:             "Single tool call initialization",
			initialToolCalls: []types.MessageToolCall{},
			deltas: []struct {
				Index    int    `json:"index"`
				ID       string `json:"id,omitempty"`
				Type     string `json:"type,omitempty"`
				Function struct {
					Name      string `json:"name,omitempty"`
					Arguments string `json:"arguments,omitempty"`
				} `json:"function,omitempty"`
			}{
				{
					Index: 0,
					ID:    "call_123",
					Type:  "function",
					Function: struct {
						Name      string `json:"name,omitempty"`
						Arguments string `json:"arguments,omitempty"`
					}{
						Name:      "get_weather",
						Arguments: `{"location":`,
					},
				},
			},
			expectedCount: 1,
			checkID:       "call_123",
			checkName:     "get_weather",
		},
		{
			name: "Accumulate arguments across deltas",
			initialToolCalls: []types.MessageToolCall{
				{ID: "call_123", Name: "get_weather", Args: json.RawMessage(`{"location":`)},
			},
			deltas: []struct {
				Index    int    `json:"index"`
				ID       string `json:"id,omitempty"`
				Type     string `json:"type,omitempty"`
				Function struct {
					Name      string `json:"name,omitempty"`
					Arguments string `json:"arguments,omitempty"`
				} `json:"function,omitempty"`
			}{
				{
					Index: 0,
					Function: struct {
						Name      string `json:"name,omitempty"`
						Arguments string `json:"arguments,omitempty"`
					}{
						Arguments: `"SF"}`,
					},
				},
			},
			expectedCount: 1,
			checkArgs:     `{"location":"SF"}`,
		},
		{
			name:             "Multiple tool calls",
			initialToolCalls: []types.MessageToolCall{},
			deltas: []struct {
				Index    int    `json:"index"`
				ID       string `json:"id,omitempty"`
				Type     string `json:"type,omitempty"`
				Function struct {
					Name      string `json:"name,omitempty"`
					Arguments string `json:"arguments,omitempty"`
				} `json:"function,omitempty"`
			}{
				{
					Index: 0,
					ID:    "call_1",
					Function: struct {
						Name      string `json:"name,omitempty"`
						Arguments string `json:"arguments,omitempty"`
					}{
						Name: "tool1",
					},
				},
				{
					Index: 1,
					ID:    "call_2",
					Function: struct {
						Name      string `json:"name,omitempty"`
						Arguments string `json:"arguments,omitempty"`
					}{
						Name: "tool2",
					},
				},
			},
			expectedCount: 2,
		},
		{
			name:             "Expand array for higher index",
			initialToolCalls: []types.MessageToolCall{},
			deltas: []struct {
				Index    int    `json:"index"`
				ID       string `json:"id,omitempty"`
				Type     string `json:"type,omitempty"`
				Function struct {
					Name      string `json:"name,omitempty"`
					Arguments string `json:"arguments,omitempty"`
				} `json:"function,omitempty"`
			}{
				{
					Index: 3,
					ID:    "call_high",
					Function: struct {
						Name      string `json:"name,omitempty"`
						Arguments string `json:"arguments,omitempty"`
					}{
						Name: "high_index_tool",
					},
				},
			},
			expectedCount: 4,
			checkName:     "high_index_tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accumulated := tt.initialToolCalls
			processToolCallDeltas(&accumulated, tt.deltas)

			if len(accumulated) != tt.expectedCount {
				t.Errorf("Expected %d tool calls, got %d", tt.expectedCount, len(accumulated))
			}

			if tt.checkID != "" && len(accumulated) > 0 {
				found := false
				for _, tc := range accumulated {
					if tc.ID == tt.checkID {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected to find tool call with ID %q", tt.checkID)
				}
			}

			if tt.checkName != "" && len(accumulated) > 0 {
				found := false
				for _, tc := range accumulated {
					if tc.Name == tt.checkName {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected to find tool call with name %q", tt.checkName)
				}
			}

			if tt.checkArgs != "" && len(accumulated) > 0 {
				if string(accumulated[0].Args) != tt.checkArgs {
					t.Errorf("Expected args %q, got %q", tt.checkArgs, string(accumulated[0].Args))
				}
			}
		})
	}
}

func TestCreateFinalStreamChunk(t *testing.T) {
	tests := []struct {
		name              string
		accumulated       string
		toolCalls         []types.MessageToolCall
		totalTokens       int
		finishReason      *string
		usage             *openAIUsage
		expectCost        bool
		expectedTokensIn  int
		expectedTokensOut int
		expectedCached    int
	}{
		{
			name:        "Basic completion without usage",
			accumulated: "Hello, world!",
			toolCalls:   nil,
			totalTokens: 10,
			finishReason: func() *string {
				s := "stop"
				return &s
			}(),
			usage:      nil,
			expectCost: false,
		},
		{
			name:        "Completion with usage and cost",
			accumulated: "Response text",
			toolCalls:   nil,
			totalTokens: 15,
			finishReason: func() *string {
				s := "stop"
				return &s
			}(),
			usage: &openAIUsage{
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
			},
			expectCost:        true,
			expectedTokensIn:  100,
			expectedTokensOut: 50,
			expectedCached:    0,
		},
		{
			name:        "Completion with cached tokens",
			accumulated: "Cached response",
			toolCalls:   nil,
			totalTokens: 20,
			finishReason: func() *string {
				s := "stop"
				return &s
			}(),
			usage: &openAIUsage{
				PromptTokens:     200,
				CompletionTokens: 75,
				TotalTokens:      275,
				PromptTokensDetails: &openAIPromptDetails{
					CachedTokens: 50,
				},
			},
			expectCost:        true,
			expectedTokensIn:  150, // 200 - 50 cached
			expectedTokensOut: 75,
			expectedCached:    50,
		},
		{
			name:        "Completion with tool calls",
			accumulated: "",
			toolCalls: []types.MessageToolCall{
				{ID: "call_1", Name: "get_weather", Args: json.RawMessage(`{"location":"NYC"}`)},
			},
			totalTokens: 30,
			finishReason: func() *string {
				s := "tool_calls"
				return &s
			}(),
			usage: &openAIUsage{
				PromptTokens:     50,
				CompletionTokens: 25,
				TotalTokens:      75,
			},
			expectCost:        true,
			expectedTokensIn:  50,
			expectedTokensOut: 25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &OpenAIProvider{
				model: "gpt-4o",
				defaults: providers.ProviderDefaults{
					Pricing: providers.Pricing{
						InputCostPer1K:  0.0025,
						OutputCostPer1K: 0.01,
					},
				},
			}

			chunk := provider.createFinalStreamChunk(tt.accumulated, tt.toolCalls, tt.totalTokens, tt.finishReason, tt.usage)

			if chunk.Content != tt.accumulated {
				t.Errorf("Expected content %q, got %q", tt.accumulated, chunk.Content)
			}

			if len(chunk.ToolCalls) != len(tt.toolCalls) {
				t.Errorf("Expected %d tool calls, got %d", len(tt.toolCalls), len(chunk.ToolCalls))
			}

			if chunk.TokenCount != tt.totalTokens {
				t.Errorf("Expected token count %d, got %d", tt.totalTokens, chunk.TokenCount)
			}

			if tt.finishReason != nil && chunk.FinishReason != nil {
				if *chunk.FinishReason != *tt.finishReason {
					t.Errorf("Expected finish reason %q, got %q", *tt.finishReason, *chunk.FinishReason)
				}
			}

			if tt.expectCost {
				if chunk.CostInfo == nil {
					t.Error("Expected cost info but got nil")
				} else {
					if chunk.CostInfo.InputTokens != tt.expectedTokensIn {
						t.Errorf("Expected input tokens %d, got %d", tt.expectedTokensIn, chunk.CostInfo.InputTokens)
					}
					if chunk.CostInfo.OutputTokens != tt.expectedTokensOut {
						t.Errorf("Expected output tokens %d, got %d", tt.expectedTokensOut, chunk.CostInfo.OutputTokens)
					}
					if chunk.CostInfo.CachedTokens != tt.expectedCached {
						t.Errorf("Expected cached tokens %d, got %d", tt.expectedCached, chunk.CostInfo.CachedTokens)
					}
					if chunk.CostInfo.TotalCost <= 0 {
						t.Error("Expected total cost > 0")
					}
				}
			} else {
				if chunk.CostInfo != nil {
					t.Error("Expected no cost info but got one")
				}
			}
		})
	}
}

func TestOpenAIHelpers_Integration(t *testing.T) {
	t.Run("Full request preparation flow", func(t *testing.T) {
		provider := &OpenAIProvider{
			model: "gpt-4o",
			defaults: providers.ProviderDefaults{
				Temperature: 0.7,
				TopP:        0.9,
				MaxTokens:   2000,
			},
		}

		req := providers.ChatRequest{
			System: "You are a test assistant",
			Messages: []types.Message{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there"},
				{Role: "user", Content: "How are you?"},
			},
			Temperature: 0,    // Should use default
			TopP:        0.95, // Should use this
			MaxTokens:   0,    // Should use default
		}

		// Step 1: Prepare messages
		messages, err := provider.prepareOpenAIMessages(req)
		if err != nil {
			t.Fatalf("Failed to prepare messages: %v", err)
		}
		if len(messages) != 4 {
			t.Errorf("Expected 4 messages (system + 3), got %d", len(messages))
		}
		if messages[0].Role != "system" {
			t.Errorf("Expected first message to be system, got %q", messages[0].Role)
		}

		// Step 2: Apply defaults
		temp, topP, maxTokens := provider.applyRequestDefaults(req)
		if temp != 0.7 {
			t.Errorf("Expected temperature 0.7 (default), got %.2f", temp)
		}
		if topP != 0.95 {
			t.Errorf("Expected topP 0.95 (from request), got %.2f", topP)
		}
		if maxTokens != 2000 {
			t.Errorf("Expected maxTokens 2000 (default), got %d", maxTokens)
		}
	})

	t.Run("Tool call streaming flow", func(t *testing.T) {
		provider := &OpenAIProvider{
			model: "gpt-4o",
			defaults: providers.ProviderDefaults{
				Pricing: providers.Pricing{
					InputCostPer1K:  0.0025,
					OutputCostPer1K: 0.01,
				},
			},
		}

		// Simulate streaming tool call deltas
		var accumulated []types.MessageToolCall

		// First delta: ID and name
		delta1 := []struct {
			Index    int    `json:"index"`
			ID       string `json:"id,omitempty"`
			Type     string `json:"type,omitempty"`
			Function struct {
				Name      string `json:"name,omitempty"`
				Arguments string `json:"arguments,omitempty"`
			} `json:"function,omitempty"`
		}{
			{
				Index: 0,
				ID:    "call_abc",
				Function: struct {
					Name      string `json:"name,omitempty"`
					Arguments string `json:"arguments,omitempty"`
				}{
					Name:      "search",
					Arguments: `{"que`,
				},
			},
		}
		processToolCallDeltas(&accumulated, delta1)

		// Second delta: more arguments
		delta2 := []struct {
			Index    int    `json:"index"`
			ID       string `json:"id,omitempty"`
			Type     string `json:"type,omitempty"`
			Function struct {
				Name      string `json:"name,omitempty"`
				Arguments string `json:"arguments,omitempty"`
			} `json:"function,omitempty"`
		}{
			{
				Index: 0,
				Function: struct {
					Name      string `json:"name,omitempty"`
					Arguments string `json:"arguments,omitempty"`
				}{
					Arguments: `ry":"test"}`,
				},
			},
		}
		processToolCallDeltas(&accumulated, delta2)

		if len(accumulated) != 1 {
			t.Fatalf("Expected 1 accumulated tool call, got %d", len(accumulated))
		}

		tc := accumulated[0]
		if tc.ID != "call_abc" {
			t.Errorf("Expected ID 'call_abc', got %q", tc.ID)
		}
		if tc.Name != "search" {
			t.Errorf("Expected name 'search', got %q", tc.Name)
		}
		expectedArgs := `{"query":"test"}`
		if string(tc.Args) != expectedArgs {
			t.Errorf("Expected args %q, got %q", expectedArgs, string(tc.Args))
		}

		// Create final chunk
		finishReason := "tool_calls"
		usage := &openAIUsage{
			PromptTokens:     50,
			CompletionTokens: 25,
			TotalTokens:      75,
		}
		finalChunk := provider.createFinalStreamChunk("", accumulated, 75, &finishReason, usage)

		if len(finalChunk.ToolCalls) != 1 {
			t.Errorf("Expected 1 tool call in final chunk, got %d", len(finalChunk.ToolCalls))
		}
		if finalChunk.CostInfo == nil {
			t.Error("Expected cost info in final chunk")
		}
	})
}
