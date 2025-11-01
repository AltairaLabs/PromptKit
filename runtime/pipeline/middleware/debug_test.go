package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestDebugMiddleware(t *testing.T) {
	// Create a test pipeline with debug middleware at multiple stages
	p := pipeline.NewPipeline(
		DebugMiddleware("start"),
		&testMiddleware{name: "middleware-1"},
		DebugMiddleware("after-middleware-1"),
		&testMiddleware{name: "middleware-2"},
		DebugMiddleware("end"),
	)

	// Create test message
	userMsg := types.Message{
		Role:    "user",
		Content: "Test message",
	}

	// Execute pipeline
	result, err := p.ExecuteWithMessage(context.Background(), userMsg)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify execution completed
	if len(result.Messages) == 0 {
		t.Error("Expected messages in result")
	}
}

// testMiddleware adds a message to demonstrate state changes
type testMiddleware struct {
	name string
}

func (m *testMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	// Add a message
	execCtx.Messages = append(execCtx.Messages, types.Message{
		Role:      "assistant",
		Content:   "Response from " + m.name,
		LatencyMs: 123,
		Timestamp: time.Now(),
		CostInfo: &types.CostInfo{
			InputTokens:  100,
			OutputTokens: 50,
			TotalCost:    0.01,
		},
	})

	// Update cost tracking
	execCtx.CostInfo.InputTokens += 100
	execCtx.CostInfo.OutputTokens += 50
	execCtx.CostInfo.TotalCost += 0.01

	// Add metadata
	if execCtx.Metadata == nil {
		execCtx.Metadata = make(map[string]interface{})
	}
	execCtx.Metadata[m.name] = "executed"

	return next()
}

func (m *testMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	return nil
}

func TestDebugMiddleware_WithToolCalls(t *testing.T) {
	// Create execution context with tool calls
	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Messages: []types.Message{
			{
				Role:    "user",
				Content: "What's the weather?",
			},
			{
				Role:    "assistant",
				Content: "",
				ToolCalls: []types.MessageToolCall{
					{
						ID:   "call_123",
						Name: "get_weather",
					},
				},
				LatencyMs: 456,
				CostInfo: &types.CostInfo{
					InputTokens:  50,
					OutputTokens: 10,
					TotalCost:    0.005,
				},
			},
		},
		ToolResults: []types.MessageToolResult{
			{
				ID:        "call_123",
				Name:      "get_weather",
				Content:   "Sunny, 72°F",
				LatencyMs: 50,
			},
		},
		CostInfo: types.CostInfo{
			InputTokens:  50,
			OutputTokens: 10,
			TotalCost:    0.005,
		},
		Trace: pipeline.ExecutionTrace{
			StartedAt: time.Now().Add(-1 * time.Second),
			LLMCalls: []pipeline.LLMCall{
				{
					Sequence:  1,
					StartedAt: time.Now().Add(-1 * time.Second),
					Duration:  456 * time.Millisecond,
				},
			},
		},
	}

	// Create debug middleware
	debug := DebugMiddleware("test-stage")

	// Call logContext to verify it doesn't panic
	debugImpl := debug.(*debugMiddleware)
	debugImpl.logContext(execCtx, "test")

	// Test passes if no panic
}

func TestDebugMiddleware_EmptyContext(t *testing.T) {
	// Test with minimal context
	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
	}

	debug := DebugMiddleware("empty-test")
	debugImpl := debug.(*debugMiddleware)

	// Should not panic with empty context
	debugImpl.logContext(execCtx, "test")
}
