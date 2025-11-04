package providers

import (
	"context"
	"math"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestMockProvider_Metadata_Chat(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]interface{}
		wantLog  bool // Whether we expect debug logging about scenario context
	}{
		{
			name: "with scenario metadata",
			metadata: map[string]interface{}{
				"mock_scenario_id": "test-scenario-1",
				"mock_turn_number": 2,
			},
			wantLog: true,
		},
		{
			name: "with partial metadata",
			metadata: map[string]interface{}{
				"mock_scenario_id": "test-scenario-2",
				// No turn number
			},
			wantLog: true,
		},
		{
			name:     "without metadata",
			metadata: nil,
			wantLog:  false,
		},
		{
			name: "with non-string scenario ID",
			metadata: map[string]interface{}{
				"mock_scenario_id": 123, // Wrong type
				"mock_turn_number": 1,
			},
			wantLog: false,
		},
		{
			name: "with non-int turn number",
			metadata: map[string]interface{}{
				"mock_scenario_id": "test-scenario-3",
				"mock_turn_number": "not-a-number", // Wrong type
			},
			wantLog: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewMockProvider("test-provider", "test-model", false)

			req := ChatRequest{
				Messages: []types.Message{
					{Role: "user", Content: "Test message"},
				},
				Metadata: tt.metadata,
			}

			resp, err := provider.Chat(context.Background(), req)
			if err != nil {
				t.Errorf("Chat() error = %v, wantErr = false", err)
				return
			}

			if resp.Content == "" {
				t.Error("Chat() response content is empty")
			}

			if resp.CostInfo == nil {
				t.Error("Chat() response should include cost info")
			}
		})
	}
}

func TestMockProvider_Metadata_ChatStream(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]interface{}
	}{
		{
			name: "stream with scenario metadata",
			metadata: map[string]interface{}{
				"mock_scenario_id": "stream-scenario-1",
				"mock_turn_number": 3,
			},
		},
		{
			name:     "stream without metadata",
			metadata: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewMockProvider("test-provider", "test-model", false)

			req := ChatRequest{
				Messages: []types.Message{
					{Role: "user", Content: "Test stream message"},
				},
				Metadata: tt.metadata,
			}

			stream, err := provider.ChatStream(context.Background(), req)
			if err != nil {
				t.Errorf("ChatStream() error = %v, wantErr = false", err)
				return
			}

			// Read from stream
			chunks := make([]StreamChunk, 0)
			for chunk := range stream {
				chunks = append(chunks, chunk)
			}

			if len(chunks) == 0 {
				t.Error("ChatStream() should produce at least one chunk")
			}

			lastChunk := chunks[len(chunks)-1]
			if lastChunk.Content == "" {
				t.Error("ChatStream() last chunk content is empty")
			}

			if lastChunk.CostInfo == nil {
				t.Error("ChatStream() last chunk should include cost info")
			}
		})
	}
}

func TestMockProvider_HelperFunctions(t *testing.T) {
	provider := NewMockProvider("test-provider", "test-model", false)

	t.Run("buildMockResponseParams", func(t *testing.T) {
		req := ChatRequest{
			Messages: []types.Message{{Role: "user", Content: "test"}},
			Metadata: map[string]interface{}{
				"mock_scenario_id": "scenario-123",
				"mock_turn_number": 5,
			},
		}

		params := provider.buildMockResponseParams(req)

		if params.ProviderID != "test-provider" {
			t.Errorf("buildMockResponseParams() ProviderID = %v, want test-provider", params.ProviderID)
		}
		if params.ModelName != "test-model" {
			t.Errorf("buildMockResponseParams() ModelName = %v, want test-model", params.ModelName)
		}
		if params.ScenarioID != "scenario-123" {
			t.Errorf("buildMockResponseParams() ScenarioID = %v, want scenario-123", params.ScenarioID)
		}
		if params.TurnNumber != 5 {
			t.Errorf("buildMockResponseParams() TurnNumber = %v, want 5", params.TurnNumber)
		}
	})

	t.Run("calculateInputTokens", func(t *testing.T) {
		messages := []types.Message{
			{Content: "Short"},          // ~1 token
			{Content: "Longer message"}, // ~3 tokens
		}

		tokens := provider.calculateInputTokens(messages)
		if tokens < 1 {
			t.Error("calculateInputTokens() should return at least 1 token")
		}
	})

	t.Run("calculateOutputTokens", func(t *testing.T) {
		text := "This is a test response with multiple words"
		tokens := provider.calculateOutputTokens(text)
		if tokens < 1 {
			t.Error("calculateOutputTokens() should return at least 1 token")
		}
	})
}

func TestMockProvider_CalculateCost(t *testing.T) {
	provider := NewMockProvider("test-provider", "test-model", false)

	tests := []struct {
		name         string
		inputTokens  int
		outputTokens int
		cachedTokens int
	}{
		{"basic cost", 100, 50, 0},
		{"with cached tokens", 100, 50, 25},
		{"zero tokens", 0, 0, 0},
		{"large token counts", 10000, 5000, 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := provider.CalculateCost(tt.inputTokens, tt.outputTokens, tt.cachedTokens)

			expectedInputTokens := tt.inputTokens - tt.cachedTokens
			if cost.InputTokens != expectedInputTokens {
				t.Errorf("CalculateCost() InputTokens = %v, want %v", cost.InputTokens, expectedInputTokens)
			}
			if cost.OutputTokens != tt.outputTokens {
				t.Errorf("CalculateCost() OutputTokens = %v, want %v", cost.OutputTokens, tt.outputTokens)
			}
			if cost.CachedTokens != tt.cachedTokens {
				t.Errorf("CalculateCost() CachedTokens = %v, want %v", cost.CachedTokens, tt.cachedTokens)
			}

			// Verify total cost is the sum of input, output, and cached costs (with floating point tolerance)
			expectedTotal := cost.InputCostUSD + cost.OutputCostUSD + cost.CachedCostUSD
			const epsilon = 1e-9
			if math.Abs(cost.TotalCost-expectedTotal) > epsilon {
				t.Errorf("CalculateCost() TotalCost = %v, want %v (diff: %v)", cost.TotalCost, expectedTotal, math.Abs(cost.TotalCost-expectedTotal))
			}
		})
	}
}

func TestMockProvider_Close(t *testing.T) {
	provider := NewMockProvider("test-provider", "test-model", false)

	err := provider.Close()
	if err != nil {
		t.Errorf("Close() error = %v, wantErr = false", err)
	}
}
