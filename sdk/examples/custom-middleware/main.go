package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// MetricsMiddleware tracks execution metrics
type MetricsMiddleware struct {
	serviceName string
}

func (m *MetricsMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	start := time.Now()

	fmt.Printf("[%s] Starting execution...\n", m.serviceName)

	// Execute pipeline
	err := next()

	duration := time.Since(start)

	// Record metrics
	totalTokens := execCtx.CostInfo.InputTokens + execCtx.CostInfo.OutputTokens
	fmt.Printf("\n[%s] Execution completed:\n", m.serviceName)
	fmt.Printf("  Duration: %v\n", duration)
	fmt.Printf("  Tokens: %d (input: %d, output: %d)\n",
		totalTokens,
		execCtx.CostInfo.InputTokens,
		execCtx.CostInfo.OutputTokens,
	)
	fmt.Printf("  Cost: $%.4f\n", execCtx.CostInfo.TotalCost)
	fmt.Printf("  LLM Calls: %d\n", len(execCtx.Trace.LLMCalls))

	return err
}

func (m *MetricsMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	return nil
}

// LoggingMiddleware logs execution details
type LoggingMiddleware struct{}

func (m *LoggingMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	fmt.Println("\n[Logging] Input Messages:")
	for i, msg := range execCtx.Messages {
		fmt.Printf("  %d. [%s] %s\n", i+1, msg.Role, truncate(msg.Content, 50))
	}

	err := next()

	if execCtx.Response != nil {
		fmt.Printf("\n[Logging] Response: %s\n", truncate(execCtx.Response.Content, 100))
	}

	return err
}

func (m *LoggingMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	return nil
}

// CustomContextMiddleware adds custom context to the system prompt
type CustomContextMiddleware struct {
	context string
}

func (m *CustomContextMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	// Add custom context to system prompt
	if m.context != "" {
		if execCtx.SystemPrompt == "" {
			execCtx.SystemPrompt = m.context
		} else {
			execCtx.SystemPrompt = execCtx.SystemPrompt + "\n\n" + m.context
		}
		fmt.Printf("[Context] Added custom context (%d chars)\n", len(m.context))
	}

	return next()
}

func (m *CustomContextMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	return nil
}

func main() {
	fmt.Println("=== Custom Middleware Example ===")

	// Create mock provider (use real provider by setting OPENAI_API_KEY) // NOSONAR: Example comment
	provider := providers.NewMockProvider("mock", "mock-model", false)

	// Build pipeline with custom middleware
	pipe := sdk.NewPipelineBuilder().
		// Add custom context first
		WithMiddleware(&CustomContextMiddleware{
			context: "Context: This is a demo conversation showing custom middleware.",
		}).
		// Add logging
		WithMiddleware(&LoggingMiddleware{}).
		// Add metrics tracking
		WithMiddleware(&MetricsMiddleware{
			serviceName: "chat-api",
		}).
		// Add provider using convenience method (simple provider without tools)
		// For tool support, use: WithProvider(provider, registry, policy)
		// For template substitution, use: WithTemplate()
		WithSimpleProvider(provider).
		Build()

	// Execute pipeline
	ctx := context.Background()
	result, err := pipe.Execute(ctx, "user", "What is the capital of France?")
	if err != nil {
		log.Fatalf("Execution failed: %v", err)
	}

	// Show final result
	fmt.Println("\n=== Final Result ===")
	fmt.Printf("Response: %s\n", result.Response.Content)
	fmt.Printf("Total Messages: %d\n", len(result.Messages))
	fmt.Printf("LLM Calls: %d\n", len(result.Trace.LLMCalls))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
