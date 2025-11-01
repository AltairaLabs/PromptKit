package sdk

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

func TestPipelineBuilder_Basic(t *testing.T) {
	// Create mock provider
	mockProvider := providers.NewMockProvider("test", "test-model", false)

	// Build pipeline with simple provider middleware (no tools)
	pipe := NewPipelineBuilder().
		WithSimpleProvider(mockProvider).
		Build()

	if pipe == nil {
		t.Fatal("expected non-nil pipeline")
	}

	// Execute pipeline
	ctx := context.Background()
	result, err := pipe.Execute(ctx, "user", "Hello!")
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	if result.Response == nil {
		t.Error("expected response")
	}

	if len(result.Messages) == 0 {
		t.Error("expected messages in result")
	}

	if len(result.Trace.LLMCalls) == 0 {
		t.Error("expected LLM calls in trace")
	}
}

func TestPipelineBuilder_WithCustomMiddleware(t *testing.T) {
	mockProvider := providers.NewMockProvider("test", "test-model", false)

	// Create custom middleware that modifies system prompt
	customMiddleware := &testMiddleware{
		systemPrompt: "Custom system prompt",
	}

	// Build pipeline with custom middleware before provider
	pipe := NewPipelineBuilder().
		WithMiddleware(customMiddleware).
		WithSimpleProvider(mockProvider).
		Build()

	ctx := context.Background()
	result, err := pipe.Execute(ctx, "user", "Hello!")
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	// Verify custom middleware ran
	if !customMiddleware.executed {
		t.Error("expected custom middleware to execute")
	}

	if result.Response == nil {
		t.Error("expected response")
	}
}

func TestPipelineBuilder_WithConfig(t *testing.T) {
	mockProvider := providers.NewMockProvider("test", "test-model", false)

	config := &pipeline.PipelineRuntimeConfig{
		MaxConcurrentExecutions: 50,
		ExecutionTimeout:        10 * time.Second,
	}

	pipe := NewPipelineBuilder().
		WithConfig(config).
		WithSimpleProvider(mockProvider).
		Build()

	if pipe == nil {
		t.Fatal("expected non-nil pipeline")
	}

	ctx := context.Background()
	_, err := pipe.Execute(ctx, "user", "Test message")
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
}

func TestPipelineBuilder_MultipleMiddleware(t *testing.T) {
	mockProvider := providers.NewMockProvider("test", "test-model", false)

	middleware1 := &testMiddleware{name: "first"}
	middleware2 := &testMiddleware{name: "second"}
	middleware3 := &testMiddleware{name: "third"}

	pipe := NewPipelineBuilder().
		WithMiddleware(middleware1).
		WithMiddleware(middleware2).
		WithSimpleProvider(mockProvider).
		WithMiddleware(middleware3).
		Build()

	ctx := context.Background()
	_, err := pipe.Execute(ctx, "user", "Test")
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	// Verify all middleware executed
	if !middleware1.executed {
		t.Error("expected middleware1 to execute")
	}
	if !middleware2.executed {
		t.Error("expected middleware2 to execute")
	}
	if !middleware3.executed {
		t.Error("expected middleware3 to execute")
	}
}

// testMiddleware is a simple test middleware
type testMiddleware struct {
	name         string
	systemPrompt string
	executed     bool
}

func (m *testMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	m.executed = true

	if m.systemPrompt != "" {
		execCtx.SystemPrompt = m.systemPrompt
	}

	// Store execution order in metadata
	order, _ := execCtx.Metadata["execution_order"].([]string)
	order = append(order, m.name)
	execCtx.Metadata["execution_order"] = order

	return next()
}

func (m *testMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	return nil
}

func TestPipelineBuilder_WithProvider(t *testing.T) {
	mockProvider := providers.NewMockProvider("test", "test-model", false)

	// Build pipeline with WithProvider (includes tool support)
	pipe := NewPipelineBuilder().
		WithProvider(mockProvider, nil, nil).
		Build()

	if pipe == nil {
		t.Fatal("expected non-nil pipeline")
	}

	ctx := context.Background()
	result, err := pipe.Execute(ctx, "user", "Hello!")
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	if result.Response == nil {
		t.Error("expected response")
	}
}

func TestPipelineBuilder_WithTemplate(t *testing.T) {
	mockProvider := providers.NewMockProvider("test", "test-model", false)

	// Build pipeline with template middleware
	pipe := NewPipelineBuilder().
		WithTemplate().
		WithSimpleProvider(mockProvider).
		Build()

	if pipe == nil {
		t.Fatal("expected non-nil pipeline")
	}

	ctx := context.Background()
	result, err := pipe.Execute(ctx, "system", "Hello {{name}}!")
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	if result.Response == nil {
		t.Error("expected response")
	}
}
