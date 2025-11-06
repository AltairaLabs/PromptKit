package mock

import (
	"context"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"testing"
)

func TestCreateProviderFromSpec_Mock(t *testing.T) {
	spec := providers.ProviderSpec{
		ID:    "test-mock",
		Type:  "mock",
		Model: "test-model",
	}

	provider, err := providers.CreateProviderFromSpec(spec)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if provider == nil {
		t.Fatal("Expected provider to be created, got nil")
	}

	if provider.ID() != "test-mock" {
		t.Errorf("Expected ID 'test-mock', got '%s'", provider.ID())
	}

	// Test that it can handle a chat request
	ctx := context.Background()
	req := providers.ChatRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	resp, err := provider.Chat(ctx, req)
	if err != nil {
		t.Fatalf("Expected no error from Chat, got: %v", err)
	}

	if resp.Content == "" {
		t.Error("Expected non-empty response content")
	}

	if resp.CostInfo == nil {
		t.Error("Expected cost info to be set")
	}

	if resp.CostInfo.TotalCost == 0 {
		t.Error("Expected non-zero total cost")
	}
}

func TestMockProvider_Chat(t *testing.T) {
	provider := NewMockProvider("test-id", "test-model", false)

	ctx := context.Background()
	req := providers.ChatRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Test message"},
		},
	}

	resp, err := provider.Chat(ctx, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	expectedContent := "Mock response from test-id model test-model"
	if resp.Content != expectedContent {
		t.Errorf("Expected content '%s', got '%s'", expectedContent, resp.Content)
	}

	if resp.CostInfo == nil {
		t.Fatal("Expected cost info to be set")
	}

	if resp.CostInfo.InputTokens == 0 {
		t.Error("Expected non-zero input tokens")
	}

	if resp.CostInfo.OutputTokens == 0 {
		t.Error("Expected non-zero output tokens")
	}
}

func TestMockProvider_ChatStream(t *testing.T) {
	provider := NewMockProvider("test-id", "test-model", false)

	ctx := context.Background()
	req := providers.ChatRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Test message"},
		},
	}

	stream, err := provider.ChatStream(ctx, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	chunks := 0
	for chunk := range stream {
		chunks++
		if chunk.Content == "" {
			t.Error("Expected non-empty content in chunk")
		}
		if chunk.FinalResult == nil {
			t.Error("Expected final result in chunk")
		}
		// FinalResult is interface{}, so we need to type assert
		if finalResult, ok := chunk.FinalResult.(*providers.ChatResponse); ok {
			if finalResult.CostInfo == nil {
				t.Error("Expected cost info in final result")
			}
		}
	}

	if chunks == 0 {
		t.Error("Expected at least one chunk")
	}
}

func TestMockProvider_SupportsStreaming(t *testing.T) {
	provider := NewMockProvider("test-id", "test-model", false)

	if !provider.SupportsStreaming() {
		t.Error("Expected mock provider to support streaming")
	}
}

func TestMockProvider_ShouldIncludeRawOutput(t *testing.T) {
	tests := []struct {
		name             string
		includeRawOutput bool
		expectedInclude  bool
	}{
		{
			name:             "Include raw output true",
			includeRawOutput: true,
			expectedInclude:  true,
		},
		{
			name:             "Include raw output false",
			includeRawOutput: false,
			expectedInclude:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewMockProvider("test-id", "test-model", tt.includeRawOutput)
			if provider.ShouldIncludeRawOutput() != tt.expectedInclude {
				t.Errorf("Expected ShouldIncludeRawOutput to be %v, got %v",
					tt.expectedInclude, provider.ShouldIncludeRawOutput())
			}
		})
	}
}
