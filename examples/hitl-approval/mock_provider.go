package main

import (
	"context"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// MockProvider simulates LLM responses with predictable tool calls
type MockProvider struct {
	responses []MockResponse
	callIndex int
}

// MockResponse defines a pre-configured response
type MockResponse struct {
	Content   string
	ToolCalls []types.MessageToolCall
}

// NewMockProvider creates a mock provider with predefined responses
func NewMockProvider(responses []MockResponse) *MockProvider {
	return &MockProvider{
		responses: responses,
		callIndex: 0,
	}
}

// ID returns the provider identifier
func (p *MockProvider) ID() string {
	return "mock"
}

// Chat returns the next predefined response
func (p *MockProvider) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	if p.callIndex >= len(p.responses) {
		return providers.ChatResponse{}, fmt.Errorf("no more mock responses available (made %d calls, have %d responses)", p.callIndex, len(p.responses))
	}

	resp := p.responses[p.callIndex]
	p.callIndex++

	return providers.ChatResponse{
		Content:   resp.Content,
		ToolCalls: resp.ToolCalls,
		CostInfo: &types.CostInfo{
			InputTokens:  100,
			OutputTokens: 50,
			TotalCost:    0.001,
		},
		Latency: time.Millisecond * 100,
	}, nil
}

// ChatStream is not implemented for mock provider
func (p *MockProvider) ChatStream(ctx context.Context, req providers.ChatRequest) (<-chan providers.StreamChunk, error) {
	return nil, fmt.Errorf("streaming not supported by mock provider")
}

// SupportsStreaming returns false
func (p *MockProvider) SupportsStreaming() bool {
	return false
}

// ShouldIncludeRawOutput returns false
func (p *MockProvider) ShouldIncludeRawOutput() bool {
	return false
}

// Close is a no-op for mock provider
func (p *MockProvider) Close() error {
	return nil
}

// CalculateCost returns mock cost info
func (p *MockProvider) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
	return types.CostInfo{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalCost:    0.001,
	}
}
